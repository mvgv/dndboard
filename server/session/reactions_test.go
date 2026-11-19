package session_test

import (
	"testing"
	"time"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
)

// goblinLeads starts a fight where the goblin acts first (initiative: goblin
// 20+2, fighter 5+1) next to a fighter with the given reaction rule. The
// goblin walks away, which gives the fighter an Opportunity Attack opportunity.
func goblinLeads(t *testing.T, rule session.ReactionRule, timeout time.Duration, more ...int) *session.Session {
	t.Helper()
	f := fighter("fighter", 5, 2)
	f.Reactions = rule
	rolls := append([]int{5, 20}, more...)
	s, err := session.New(session.Config{
		Board: mustBoard(t, hall), Creatures: []session.CreatureSpec{f, goblin("g1", 4, 2)},
		Dice: dice.Fixed(rolls...), ReactionTimeout: timeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.State().Active; got != "g1" {
		t.Fatalf("active = %s, want g1", got)
	}
	return s
}

func TestPromptPausesTheTurnUntilTheOwnerAnswers(t *testing.T) {
	// Fighter's Opportunity Attack: 15+5 = 20 vs AC 15 hits, d8 4+3 = 7.
	s := goblinLeads(t, session.ReactAsk, time.Minute, 15, 4)
	ev := submit(t, s, session.Move{Actor: "g1", To: pos(1, 2)})
	p, ok := find[session.ReactionPrompted](ev)
	if !ok || p.Creature != "fighter" || p.Target != "g1" || p.Timeout != time.Minute {
		t.Fatalf("want a prompt for the fighter with the campaign timeout, events %+v", ev)
	}
	if _, ok := find[session.Moved](ev); ok {
		t.Fatal("the move must wait for the answer")
	}
	if st := s.State(); st.Pending == nil || st.Pending.Creature != "fighter" || st.Creatures["g1"].Pos != pos(4, 2) {
		t.Fatalf("want a pending prompt and an unmoved goblin, state %+v", st)
	}
	if rej := rejection(t, s, session.EndTurn{Actor: "g1"}); rej.Code != session.AwaitingReaction {
		t.Fatalf("code = %v, want AwaitingReaction", rej.Code)
	}
	if la := s.LegalActions(); la.EndTurn || len(la.Moves) > 0 {
		t.Fatal("no legal actions while waiting for the answer")
	}
	if _, err := s.Answer("g1", true); err == nil {
		t.Fatal("only the prompted creature's owner may answer")
	}

	ev, err := s.Answer("fighter", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := find[session.OpportunityAttackMade](ev); !ok {
		t.Fatalf("want an Opportunity Attack, events %+v", ev)
	}
	if mv, ok := find[session.Moved](ev); !ok || mv.To != pos(1, 2) {
		t.Fatalf("the move should resume after the reaction, events %+v", ev)
	}
	if s.State().Pending != nil {
		t.Fatal("prompt should be cleared")
	}
	if s.State().Creatures["fighter"].ReactionAvailable {
		t.Fatal("Reaction must be spent")
	}
}

func TestDecliningTheReactionLetsTheMoveProceed(t *testing.T) {
	s := goblinLeads(t, session.ReactAsk, time.Minute) // no dice: an attack would panic
	submit(t, s, session.Move{Actor: "g1", To: pos(1, 2)})
	ev, err := s.Answer("fighter", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := find[session.ReactionDeclined](ev); !ok {
		t.Fatalf("want ReactionDeclined, events %+v", ev)
	}
	if _, ok := find[session.Moved](ev); !ok {
		t.Fatal("move should complete")
	}
	if !s.State().Creatures["fighter"].ReactionAvailable {
		t.Fatal("declining keeps the Reaction")
	}
}

func TestTimeoutMeansNoReaction(t *testing.T) {
	s := goblinLeads(t, session.ReactAsk, 30*time.Second) // no dice: an attack would panic
	submit(t, s, session.Move{Actor: "g1", To: pos(1, 2)})
	if ev := s.Advance(29 * time.Second); len(ev) != 0 {
		t.Fatalf("still waiting, got %+v", ev)
	}
	if s.State().Pending == nil {
		t.Fatal("prompt should still be pending")
	}
	ev := s.Advance(time.Second)
	if _, ok := find[session.ReactionTimedOut](ev); !ok {
		t.Fatalf("want ReactionTimedOut, events %+v", ev)
	}
	if _, ok := find[session.OpportunityAttackMade](ev); ok {
		t.Fatal("a timeout must not react")
	}
	if _, ok := find[session.Moved](ev); !ok {
		t.Fatal("the turn should continue after the timeout")
	}
	if s.State().Pending != nil || len(s.Advance(time.Hour)) != 0 {
		t.Fatal("nothing pending after the timeout")
	}
}

func TestDefaultTimeoutApplies(t *testing.T) {
	s := goblinLeads(t, session.ReactAsk, 0)
	ev := submit(t, s, session.Move{Actor: "g1", To: pos(1, 2)})
	if p, _ := find[session.ReactionPrompted](ev); p.Timeout != session.DefaultReactionTimeout {
		t.Fatalf("timeout = %v, want default", p.Timeout)
	}
}

func TestAutomaticRulesResolveWithoutPrompting(t *testing.T) {
	// always: attacks at once (15+5 = 20 hits, d8 4).
	s := goblinLeads(t, session.ReactAlways, time.Minute, 15, 4)
	ev := submit(t, s, session.Move{Actor: "g1", To: pos(1, 2)})
	if _, ok := find[session.ReactionPrompted](ev); ok {
		t.Fatal("always must not prompt")
	}
	if _, ok := find[session.OpportunityAttackMade](ev); !ok {
		t.Fatal("always must attack")
	}
	// never: no attack and no prompt (no dice scripted).
	s = goblinLeads(t, session.ReactNever, time.Minute)
	ev = submit(t, s, session.Move{Actor: "g1", To: pos(1, 2)})
	if _, ok := find[session.ReactionPrompted](ev); ok {
		t.Fatal("never must not prompt")
	}
	if _, ok := find[session.OpportunityAttackMade](ev); ok {
		t.Fatal("never must not attack")
	}
	if _, ok := find[session.Moved](ev); !ok {
		t.Fatal("move should complete")
	}
}

func TestMonsterReactionsAreDecidedWithoutPrompting(t *testing.T) {
	// Fighter moves first (21 vs 7) away from a goblin with the default rule.
	s := start(t, dice.Fixed(20, 5, 5, 3), fighter("fighter", 5, 2), goblin("g1", 4, 2))
	ev := submit(t, s, session.Move{Actor: "fighter", To: pos(8, 2)})
	if _, ok := find[session.ReactionPrompted](ev); ok {
		t.Fatal("monsters never prompt")
	}
	if _, ok := find[session.OpportunityAttackMade](ev); !ok {
		t.Fatal("the monster should react")
	}
}

func TestDisengageSkipsThePrompt(t *testing.T) {
	s := goblinLeads(t, session.ReactAsk, time.Minute)
	submit(t, s, session.Disengage{Actor: "g1"})
	ev := submit(t, s, session.Move{Actor: "g1", To: pos(1, 2)})
	if _, ok := find[session.ReactionPrompted](ev); ok {
		t.Fatal("Disengage must not prompt")
	}
}

// Leaving reach provokes even when the move ends in reach again: the path goes
// around a wall, so the first step leaves the goblin's reach.
func TestLeavingAndReEnteringReachAlongThePathProvokes(t *testing.T) {
	rows := []string{
		"#######",
		"#.....#",
		"#..#..#",
		"#.....#",
		"#######",
	}
	// Initiative fighter 21, goblin 7; Opportunity Attack misses (d20 2).
	s, err := session.New(session.Config{
		Board:     mustBoard(t, rows),
		Creatures: []session.CreatureSpec{fighter("fighter", 2, 2), goblin("g1", 3, 3)},
		Dice:      dice.Fixed(20, 5, 2),
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := submit(t, s, session.Move{Actor: "fighter", To: board.Pos{X: 4, Y: 2}})
	oa, ok := find[session.OpportunityAttackMade](ev)
	if !ok || oa.Attacker != "g1" {
		t.Fatalf("want an Opportunity Attack from g1, events %+v", ev)
	}
	if mv, ok := find[session.Moved](ev); !ok || mv.CostFt != 20 {
		t.Fatalf("want a 20 ft move, events %+v", ev)
	}
}

func TestAnsweredAttackThatDownsTheMoverEndsItsTurn(t *testing.T) {
	// Goblin (1 HP) is hit by the fighter's reaction: 15+5 vs AC 15, d8 4+3.
	f := fighter("fighter", 5, 2)
	f.Reactions = session.ReactAsk
	g := goblin("g1", 4, 2)
	g.MaxHP = 1
	s := start(t, dice.Fixed(5, 20, 15, 4), f, g)
	submit(t, s, session.Move{Actor: "g1", To: pos(1, 2)})
	ev, err := s.Answer("fighter", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := find[session.Moved](ev); ok {
		t.Fatal("a downed creature must not complete its move")
	}
	if _, ok := find[session.CombatEnded](ev); !ok {
		t.Fatalf("want CombatEnded, events %+v", ev)
	}
	if got := s.State().Creatures["g1"].Pos; got != pos(4, 2) {
		t.Fatalf("goblin at %v, want 4,2", got)
	}
}

func TestTwoPromptsInSequenceOnTheSameStep(t *testing.T) {
	f1, f2 := fighter("f1", 5, 2), fighter("f2", 5, 3)
	f1.Reactions, f2.Reactions = session.ReactAsk, session.ReactAsk
	// Initiative: f1 5+1, f2 4+1, goblin 20+2. f2's attack misses (d20 2).
	s := start(t, dice.Fixed(5, 4, 20, 2), f1, f2, goblin("g1", 4, 2))
	if s.State().Active != "g1" {
		t.Fatalf("active = %s", s.State().Active)
	}
	ev := submit(t, s, session.Move{Actor: "g1", To: pos(1, 1)})
	if p, _ := find[session.ReactionPrompted](ev); p.Creature != "f1" {
		t.Fatalf("first prompt = %+v, want f1", p)
	}
	ev, err := s.Answer("f1", false)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := find[session.ReactionPrompted](ev); !ok || p.Creature != "f2" {
		t.Fatalf("second prompt = %+v, want f2", ev)
	}
	if _, ok := find[session.Moved](ev); ok {
		t.Fatal("still waiting on f2")
	}
	ev, err = s.Answer("f2", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := find[session.Moved](ev); !ok {
		t.Fatalf("move should complete, events %+v", ev)
	}
}

func TestPendingRemainingCountsDown(t *testing.T) {
	s := goblinLeads(t, session.ReactAsk, time.Minute)
	submit(t, s, session.Move{Actor: "g1", To: pos(1, 2)})
	s.Advance(20 * time.Second)
	if p := s.State().Pending; p == nil || p.Remaining != 40*time.Second {
		t.Fatalf("pending = %+v, want 40s remaining", p)
	}
}
