package table_test

import (
	"errors"
	"testing"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
	"github.com/mvgv/dndboard/server/srd"
	"github.com/mvgv/dndboard/server/table"
)

// Hall: 11x3 floor inside walls.
var hall = []string{
	"#############",
	"#...........#",
	"#...........#",
	"#...........#",
	"#############",
}

func fighter(id string, x, y int) session.CreatureSpec {
	f := srd.Fighter()
	f.ID, f.Pos = session.ID(id), board.Pos{X: x, Y: y}
	return f
}

func goblin(id string, x, y int) session.CreatureSpec {
	g := srd.GoblinWarrior()
	g.ID, g.Pos = session.ID(id), board.Pos{X: x, Y: y}
	return g
}

// fakeBrain plays the monsters from a script of intents, then ends their turns.
type fakeBrain struct {
	script []session.Intent
	calls  int
}

func (b *fakeBrain) Choose(st session.State, _ session.LegalActions) session.Intent {
	b.calls++
	if len(b.script) == 0 {
		return session.EndTurn{Actor: st.Active}
	}
	in := b.script[0]
	b.script = b.script[1:]
	return in
}

// duel: the Fighter (a Player Character) at 1,2 against a Goblin Warrior at
// 5,2. Initiative: fighter 20, goblin 1, so the Fighter goes first.
func duel(t *testing.T, brain table.Brain, rolls ...int) *table.Table {
	t.Helper()
	tb, err := table.New(table.Config{
		Rows:             hall,
		Creatures:        []session.CreatureSpec{fighter("fighter", 1, 2), goblin("goblin", 5, 2)},
		PlayerCharacters: []session.ID{"fighter"},
		Dice:             dice.Fixed(append([]int{20, 1}, rolls...)...),
		Brain:            brain,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

func next(t *testing.T, ch <-chan table.Message) table.Message {
	t.Helper()
	select {
	case m, ok := <-ch:
		if !ok {
			t.Fatal("update channel closed")
		}
		return m
	default:
		t.Fatal("no update broadcast")
	}
	return table.Message{}
}

func none(t *testing.T, ch <-chan table.Message) {
	t.Helper()
	select {
	case m := <-ch:
		t.Fatalf("unexpected broadcast %+v", m)
	default:
	}
}

func submit(t *testing.T, tb *table.Table, in table.Intent) {
	t.Helper()
	if err := tb.Submit(in); err != nil {
		t.Fatalf("%+v rejected: %v", in, err)
	}
}

func creature(t *testing.T, st *table.State, id string) table.Creature {
	t.Helper()
	for _, c := range st.Creatures {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no creature %q in state", id)
	return table.Creature{}
}

func events(m table.Message, typ string) []table.Event {
	var out []table.Event
	for _, e := range m.Events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestWelcomeCarriesBoardStateAndLegalActions(t *testing.T) {
	tb := duel(t, &fakeBrain{})
	w, _, leave := tb.Join()
	defer leave()

	if w.Type != "welcome" || w.Board == nil || w.State == nil {
		t.Fatalf("welcome = %+v", w)
	}
	if w.Board.Width != 13 || w.Board.Height != 5 || len(w.Board.Rows) != 5 || w.Board.Rows[2] != hall[2] {
		t.Fatalf("board = %+v", w.Board)
	}
	f := creature(t, w.State, "fighter")
	if !f.PlayerCharacter || f.CreatureType != "Humanoid" || f.X != 1 || f.Y != 2 || f.HP != 12 {
		t.Fatalf("fighter = %+v", f)
	}
	g := creature(t, w.State, "goblin")
	if g.PlayerCharacter || g.CreatureType != "Fey" {
		t.Fatalf("goblin = %+v", g)
	}
	if w.State.Active != "fighter" || w.Legal == nil || w.Legal.Creature != "fighter" {
		t.Fatalf("active %q, legal %+v", w.State.Active, w.Legal)
	}
}

// AC: reachable squares and valid targets come from the server's legal actions.
func TestLegalActionsListReachableSquaresAndTargets(t *testing.T) {
	tb := duel(t, &fakeBrain{})
	w, ch, leave := tb.Join()
	defer leave()

	// 30 ft of Speed in an open hall: every floor square within 6 squares,
	// but not the goblin's square, and no target in reach yet.
	la := w.Legal
	if la.MovementLeftFt != 30 || len(la.Attacks) != 0 || !la.EndTurn || !la.Dash {
		t.Fatalf("legal = %+v", la)
	}
	costs := map[[2]int]int{}
	for _, m := range la.Moves {
		costs[[2]int{m.X, m.Y}] = m.CostFt
	}
	if costs[[2]int{4, 2}] != 15 || costs[[2]int{7, 2}] != 30 {
		t.Fatalf("move costs = %v", costs)
	}
	if _, ok := costs[[2]int{5, 2}]; ok {
		t.Fatal("the goblin's square is offered as a move")
	}
	if _, ok := costs[[2]int{8, 2}]; ok {
		t.Fatal("a square beyond Speed is offered as a move")
	}

	submit(t, tb, table.Intent{Kind: "move", Actor: "fighter", To: &table.Square{X: 4, Y: 2}})
	u := next(t, ch)
	if u.Legal == nil || len(u.Legal.Attacks) != 1 || u.Legal.Attacks[0].Target != "goblin" || u.Legal.MovementLeftFt != 15 {
		t.Fatalf("legal after move = %+v", u.Legal)
	}
}

// AC: an intent resolves on the server and updates every connected client.
func TestIntentUpdatesEveryConnectedClient(t *testing.T) {
	tb := duel(t, &fakeBrain{})
	_, a, leaveA := tb.Join()
	defer leaveA()
	_, b, leaveB := tb.Join()
	defer leaveB()

	submit(t, tb, table.Intent{Kind: "move", Actor: "fighter", To: &table.Square{X: 4, Y: 2}})
	for _, ch := range []<-chan table.Message{a, b} {
		u := next(t, ch)
		mv := events(u, "moved")
		if u.Type != "update" || len(mv) != 1 || mv[0].To.X != 4 || mv[0].CostFt != 15 {
			t.Fatalf("update = %+v", u)
		}
		if f := creature(t, u.State, "fighter"); f.X != 4 || f.Y != 2 {
			t.Fatalf("fighter after move = %+v", f)
		}
	}
}

func TestRejectedIntentChangesNothingAndBroadcastsNothing(t *testing.T) {
	tb := duel(t, &fakeBrain{})
	_, ch, leave := tb.Join()
	defer leave()

	for _, in := range []table.Intent{
		{Kind: "move", Actor: "fighter", To: &table.Square{X: 9, Y: 2}}, // beyond Speed
		{Kind: "attack", Actor: "fighter", Target: "goblin"},            // out of reach
		{Kind: "end_turn", Actor: "goblin"},                             // a monster, not a Player Character
		{Kind: "fly", Actor: "fighter"},                                 // unknown intent
		{Kind: "move", Actor: "fighter"},                                // no square
	} {
		var rej *session.Rejection
		if err := tb.Submit(in); !errors.As(err, &rej) || rej.Reason == "" {
			t.Fatalf("%+v: want a Rejection with a reason, got %v", in, err)
		}
	}
	none(t, ch)
}

// The Player Character's turn ends, the monsters' brain plays the goblin, and
// the next update carries both turns and hands legal actions back.
func TestMonsterTurnsRunOnTheServer(t *testing.T) {
	brain := &fakeBrain{script: []session.Intent{
		session.Move{Actor: "goblin", To: board.Pos{X: 2, Y: 2}},
		session.Attack{Actor: "goblin", Target: "fighter"},
	}}
	// Goblin attack d20 15 (19 vs AC 16) hits; scimitar d6 3 + 2 = 5.
	tb := duel(t, brain, 15, 3)
	_, ch, leave := tb.Join()
	defer leave()

	submit(t, tb, table.Intent{Kind: "end_turn", Actor: "fighter"})
	u := next(t, ch)
	if len(events(u, "moved")) != 1 || len(events(u, "attack_rolled")) != 1 {
		t.Fatalf("events = %+v", u.Events)
	}
	if f := creature(t, u.State, "fighter"); f.HP != 7 {
		t.Fatalf("fighter HP = %d, want 7", f.HP)
	}
	if u.State.Active != "fighter" || u.State.Round != 2 || u.Legal == nil || u.Legal.Creature != "fighter" {
		t.Fatalf("after the goblin's turn: active %q round %d legal %+v", u.State.Active, u.State.Round, u.Legal)
	}
}

// AC: Player Character rolls are visible to all; monster rolls show only the result.
func TestPlayerCharacterRollsAreShownAndMonsterRollsOnlyTheirResult(t *testing.T) {
	brain := &fakeBrain{script: []session.Intent{session.Attack{Actor: "goblin", Target: "fighter"}}}
	// Fighter: d20 15 (20 vs AC 15) hits, longsword d8 5 + 3 = 8.
	// Goblin: d20 15 (19 vs AC 16) hits, scimitar d6 3 + 2 = 5.
	tb := duel(t, brain, 15, 5, 15, 3)
	_, ch, leave := tb.Join()
	defer leave()

	submit(t, tb, table.Intent{Kind: "move", Actor: "fighter", To: &table.Square{X: 4, Y: 2}})
	next(t, ch)
	submit(t, tb, table.Intent{Kind: "attack", Actor: "fighter", Target: "goblin"})
	u := next(t, ch)
	atk, dmg := events(u, "attack_rolled"), events(u, "damage_dealt")
	if len(atk) != 1 || len(dmg) != 1 {
		t.Fatalf("events = %+v", u.Events)
	}
	if a := atk[0]; a.Hidden || a.D20 == nil || *a.D20 != 15 || *a.Modifier != 5 || *a.Total != 20 || !*a.Hit {
		t.Fatalf("fighter's attack roll = %+v", a)
	}
	if d := dmg[0]; d.Hidden || len(d.Rolls) != 1 || d.Rolls[0] != 5 || *d.Sides != 8 || *d.Bonus != 3 || *d.Amount != 8 || *d.HPAfter != 2 {
		t.Fatalf("fighter's damage = %+v", d)
	}

	submit(t, tb, table.Intent{Kind: "end_turn", Actor: "fighter"})
	u = next(t, ch)
	atk, dmg = events(u, "attack_rolled"), events(u, "damage_dealt")
	if len(atk) != 1 || len(dmg) != 1 {
		t.Fatalf("events = %+v", u.Events)
	}
	if a := atk[0]; !a.Hidden || a.D20 != nil || a.Modifier != nil || a.Total != nil || a.Hit == nil || !*a.Hit {
		t.Fatalf("goblin's attack roll = %+v", a)
	}
	if d := dmg[0]; !d.Hidden || d.Rolls != nil || d.Sides != nil || d.Bonus != nil || *d.Amount != 5 || *d.HPAfter != 7 {
		t.Fatalf("goblin's damage = %+v", d)
	}
}

func TestMonsterInitiativeRollIsHidden(t *testing.T) {
	tb := duel(t, &fakeBrain{})
	w, _, leave := tb.Join()
	defer leave()
	for _, e := range events(w, "initiative_rolled") {
		switch e.Creature {
		case "fighter":
			if e.Hidden || e.D20 == nil || *e.D20 != 20 || *e.Total != 21 {
				t.Fatalf("fighter initiative = %+v", e)
			}
		case "goblin":
			if !e.Hidden || e.D20 != nil || e.Total != nil {
				t.Fatalf("goblin initiative = %+v", e)
			}
		}
	}
	if len(events(w, "initiative_rolled")) != 2 {
		t.Fatalf("welcome events = %+v", w.Events)
	}
}

func TestNoLegalActionsOnceCombatIsOver(t *testing.T) {
	// Fighter: d20 20 is a Critical Hit, 2d8 8+8 + 3 = 19 downs the goblin.
	tb := duel(t, &fakeBrain{}, 20, 8, 8)
	_, ch, leave := tb.Join()
	defer leave()

	submit(t, tb, table.Intent{Kind: "move", Actor: "fighter", To: &table.Square{X: 4, Y: 2}})
	next(t, ch)
	submit(t, tb, table.Intent{Kind: "attack", Actor: "fighter", Target: "goblin"})
	u := next(t, ch)
	if !u.State.Over || u.State.Winner != "party" || u.Legal != nil || len(events(u, "combat_ended")) != 1 {
		t.Fatalf("after the last goblin falls: state %+v legal %+v", u.State, u.Legal)
	}
}

func TestApproachBrainClosesInAndAttacks(t *testing.T) {
	// Goblin: moves next to the fighter, then attacks: d20 15 (19 vs AC 16)
	// hits, scimitar d6 3 + 2 = 5.
	tb := duel(t, table.Approach{}, 15, 3)
	_, ch, leave := tb.Join()
	defer leave()

	submit(t, tb, table.Intent{Kind: "end_turn", Actor: "fighter"})
	u := next(t, ch)
	mv := events(u, "moved")
	if len(mv) != 1 || mv[0].Creature != "goblin" || mv[0].To.X != 2 {
		t.Fatalf("goblin moves = %+v", mv)
	}
	if f := creature(t, u.State, "fighter"); f.HP != 7 || u.State.Active != "fighter" {
		t.Fatalf("after the goblin's turn: %+v, active %q", f, u.State.Active)
	}
}

// A Reaction Prompt pauses the game: the table shows it and waits instead of
// letting the Brain retry. Answering it from the client is #16.
func TestReactionPromptPausesTheTable(t *testing.T) {
	f := fighter("fighter", 5, 2)
	f.Reactions = session.ReactAsk
	brain := &fakeBrain{script: []session.Intent{session.Move{Actor: "goblin", To: board.Pos{X: 1, Y: 2}}}}
	tb, err := table.New(table.Config{
		Rows:             hall,
		Creatures:        []session.CreatureSpec{f, goblin("goblin", 4, 2)},
		PlayerCharacters: []session.ID{"fighter"},
		Dice:             dice.Fixed(5, 20), // the goblin goes first
		Brain:            brain,
	})
	if err != nil {
		t.Fatal(err)
	}
	w, _, leave := tb.Join()
	defer leave()

	p := events(w, "reaction_prompted")
	if len(p) != 1 || p[0].Creature != "fighter" || p[0].Target != "goblin" || p[0].TimeoutS != 30 {
		t.Fatalf("prompt events = %+v", p)
	}
	if pr := w.State.Pending; pr == nil || pr.Creature != "fighter" || pr.Target != "goblin" || pr.RemainingS != 30 {
		t.Fatalf("pending = %+v", w.State.Pending)
	}
	if w.Legal != nil {
		t.Fatalf("legal actions while paused: %+v", w.Legal)
	}
	if brain.calls != 1 {
		t.Fatalf("brain asked %d times while paused, want 1", brain.calls)
	}
}

func TestLeftClientsGetNoUpdates(t *testing.T) {
	tb := duel(t, &fakeBrain{})
	_, ch, leave := tb.Join()
	leave()
	submit(t, tb, table.Intent{Kind: "end_turn", Actor: "fighter"})
	if _, ok := <-ch; ok {
		t.Fatal("a client that left still receives updates")
	}
}
