package session_test

import (
	"slices"
	"testing"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
	"github.com/mvgv/dndboard/server/srd"
)

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

func start(t *testing.T, d dice.Roller, cs ...session.CreatureSpec) *session.Session {
	t.Helper()
	s, err := session.New(session.Config{Board: mustBoard(t, hall), Creatures: cs, Dice: d})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func endTurn(t *testing.T, s *session.Session) {
	t.Helper()
	submit(t, s, session.EndTurn{Actor: s.State().Active})
}

func pos(x, y int) board.Pos { return board.Pos{X: x, Y: y} }

// Initiative (SRD "Playing the Game", Initiative): everyone makes a Dexterity
// check, ranked highest to lowest; the order stays the same every round.
func TestInitiativeIsRankedHighToLowAndRepeatsEachRound(t *testing.T) {
	// fighter +1, goblins +2. Rolls: fighter 10 (11), g1 5 (7), g2 18 (20).
	s := start(t, dice.Fixed(10, 5, 18),
		fighter("fighter", 1, 1), goblin("g1", 8, 1), goblin("g2", 9, 3))
	want := []session.ID{"g2", "fighter", "g1"}
	if got := s.State().Order; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	var seen []session.ID
	for range 6 {
		seen = append(seen, s.State().Active)
		endTurn(t, s)
	}
	if !slices.Equal(seen, slices.Concat(want, want)) {
		t.Fatalf("turns = %v, want the same order twice", seen)
	}
	if got := s.State().Round; got != 3 {
		t.Fatalf("round = %d, want 3", got)
	}
}

// Ties: the GM decides (monsters among themselves, GM against characters) and
// the players decide among characters. The session leaves the choice to the
// caller, who lists creatures in the order ties should resolve.
func TestInitiativeTiesKeepTheOrderTheCallerListed(t *testing.T) {
	// Every total is 12: fighter 11+1, goblins 10+2.
	for _, tc := range []struct {
		specs []session.CreatureSpec
		want  []session.ID
	}{
		{[]session.CreatureSpec{goblin("g1", 8, 1), fighter("fighter", 1, 1), goblin("g2", 9, 3)}, []session.ID{"g1", "fighter", "g2"}},
		{[]session.CreatureSpec{fighter("fighter", 1, 1), goblin("g1", 8, 1), goblin("g2", 9, 3)}, []session.ID{"fighter", "g1", "g2"}},
	} {
		d20s := make([]int, len(tc.specs))
		for i, sp := range tc.specs {
			d20s[i] = 12 - sp.InitiativeMod
		}
		s := start(t, dice.Fixed(d20s...), tc.specs...)
		if got := s.State().Order; !slices.Equal(got, tc.want) {
			t.Errorf("tied order = %v, want %v", got, tc.want)
		}
	}
}

// "For a group of identical creatures, the GM makes a single roll."
func TestGroupOfIdenticalCreaturesSharesOneRoll(t *testing.T) {
	g1, g2 := goblin("g1", 8, 1), goblin("g2", 9, 3)
	g1.Group, g2.Group = "goblins", "goblins"
	// Two d20s only: the fighter, then one for the whole group (6+2 = 8).
	s := start(t, dice.Fixed(10, 6), fighter("fighter", 1, 1), g1, g2)
	var totals []int
	for _, e := range s.Log() {
		if r, ok := e.(session.InitiativeRolled); ok {
			totals = append(totals, r.Total)
		}
	}
	if !slices.Equal(totals, []int{11, 8, 8}) {
		t.Fatalf("initiative totals = %v, want [11 8 8]", totals)
	}
}

// "If a combatant is surprised ... Disadvantage on their Initiative roll."
func TestSurprisedCombatantRollsInitiativeWithDisadvantage(t *testing.T) {
	g := goblin("g1", 8, 1)
	g.Surprised = true
	s := start(t, dice.Fixed(10, 19, 4), fighter("fighter", 1, 1), g) // goblin keeps the 4
	for _, e := range s.Log() {
		if r, ok := e.(session.InitiativeRolled); ok && r.Creature == "g1" && (r.D20 != 4 || r.Total != 6) {
			t.Fatalf("goblin initiative = %+v, want d20 4, total 6", r)
		}
	}
	if got := s.State().Active; got != "fighter" {
		t.Fatalf("active = %v, want fighter", got)
	}
}

// Breaking Up Your Move: "go 10 feet, take an action, and then go 20 feet" at
// Speed 30 — movement before and after the action draws on one pool.
func TestMovementCanBeSplitAroundTheAction(t *testing.T) {
	// fighter 15 first, goblin 7. Attack: d20 12 (+5 = 17 vs AC 15 hits), d8 4.
	s := start(t, dice.Fixed(14, 5, 12, 4), fighter("fighter", 1, 1), goblin("g1", 4, 1))
	submit(t, s, session.Move{Actor: "fighter", To: pos(3, 1)}) // 10 ft
	submit(t, s, session.Attack{Actor: "fighter", Target: "g1"})
	la := s.LegalActions()
	if la.MovementLeftFt != 20 || la.Action || len(la.Attacks) != 0 || len(la.Moves) == 0 {
		t.Fatalf("after move and attack: %+v", la)
	}
	submit(t, s, session.Move{Actor: "fighter", To: pos(3, 2)}) // 5 ft, still in reach: no Opportunity Attack
	if got := s.LegalActions().MovementLeftFt; got != 15 {
		t.Fatalf("movement left = %d, want 15", got)
	}
	if r := rejection(t, s, session.Move{Actor: "fighter", To: pos(7, 2)}); r.Code != session.Unreachable {
		t.Fatalf("20 ft with 15 ft left: code = %v", r.Code)
	}
}

// Dash: "extra movement equal to your Speed"; it uses the action. The turn's
// movement and action reset on the next turn.
func TestDashDoublesMovementAndSpendsTheAction(t *testing.T) {
	s := start(t, dice.Fixed(14, 5), fighter("fighter", 1, 1), goblin("g1", 11, 3))
	ev := submit(t, s, session.Dash{Actor: "fighter"})
	if d, _ := find[session.Dashed](ev); d.ExtraFt != 30 {
		t.Fatalf("dashed = %+v", d)
	}
	la := s.LegalActions()
	if la.MovementLeftFt != 60 || la.Action || la.Dash || la.Disengage || len(la.Attacks) != 0 {
		t.Fatalf("after Dash: %+v", la)
	}
	submit(t, s, session.Move{Actor: "fighter", To: pos(10, 1)}) // 9 squares = 45 ft
	if r := rejection(t, s, session.Dash{Actor: "fighter"}); r.Code != session.ActionUsed {
		t.Fatalf("second action: code = %v", r.Code)
	}
	endTurn(t, s)
	endTurn(t, s)
	if la := s.LegalActions(); la.MovementLeftFt != 30 || !la.Action || !la.Dash {
		t.Fatalf("next turn: %+v", la)
	}
}

// The Bonus Action is separate from the action, and only creatures with a Bonus
// Action feature (SRD "Monsters", Goblin Warrior: Nimble Escape) have one.
func TestBonusActionIsSeparateFromTheAction(t *testing.T) {
	s := start(t, dice.Fixed(2, 18), fighter("fighter", 1, 1), goblin("g1", 2, 1)) // goblin first
	if la := s.LegalActions(); !la.NimbleEscape || !la.BonusAction || !la.Action {
		t.Fatalf("goblin options: %+v", la)
	}
	submit(t, s, session.NimbleEscape{Actor: "g1"})
	la := s.LegalActions()
	if la.NimbleEscape || la.BonusAction || !la.Action || len(la.Attacks) != 1 {
		t.Fatalf("after Nimble Escape: %+v", la)
	}
	if r := rejection(t, s, session.NimbleEscape{Actor: "g1"}); r.Code != session.BonusActionUsed {
		t.Fatalf("code = %v", r.Code)
	}
	endTurn(t, s)
	if la := s.LegalActions(); la.NimbleEscape || la.BonusAction {
		t.Fatalf("the Fighter has no Bonus Action: %+v", la)
	}
	if r := rejection(t, s, session.NimbleEscape{Actor: "fighter"}); r.Code != session.NoSuchOption {
		t.Fatalf("code = %v", r.Code)
	}
}

// Opportunity Attacks (SRD "Playing the Game"): a Reaction when a creature
// leaves reach; each creature has one Reaction per round, regained on its turn.
func TestOpportunityAttackSpendsTheOneReactionPerRound(t *testing.T) {
	// Initiative: fighter 21, g1 7, g2 6. Then g1 and g2 each attack and miss.
	s := start(t, dice.Fixed(20, 5, 4, 3, 2),
		fighter("fighter", 5, 2), goblin("g1", 4, 2), goblin("g2", 6, 2))

	ev := submit(t, s, session.Move{Actor: "fighter", To: pos(5, 3)}) // still adjacent to both
	if _, ok := find[session.OpportunityAttackMade](ev); ok {
		t.Fatal("staying in reach must not provoke")
	}
	ev = submit(t, s, session.Move{Actor: "fighter", To: pos(8, 3)}) // leaves both reaches
	var made []session.ID
	for _, e := range ev {
		if oa, ok := e.(session.OpportunityAttackMade); ok {
			made = append(made, oa.Attacker)
		}
	}
	if !slices.Equal(made, []session.ID{"g1", "g2"}) {
		t.Fatalf("opportunity attackers = %v, want g1 and g2; events %+v", made, ev)
	}
	if st := s.State(); st.Creatures["g1"].ReactionAvailable || st.Creatures["g2"].ReactionAvailable {
		t.Fatal("Reactions must be spent")
	}
	// Back into g2's reach and out again: its Reaction is spent, so no dice are
	// needed (an extra roll would panic the scripted dice).
	submit(t, s, session.Move{Actor: "fighter", To: pos(7, 3)})
	ev = submit(t, s, session.Move{Actor: "fighter", To: pos(8, 3)})
	if _, ok := find[session.OpportunityAttackMade](ev); ok {
		t.Fatal("a second Opportunity Attack in the same round")
	}
	endTurn(t, s) // g1's turn: its Reaction is regained
	if !s.State().Creatures["g1"].ReactionAvailable {
		t.Fatal("Reaction should be regained at the start of the creature's turn")
	}
}

// Disengage: "your movement doesn't provoke Opportunity Attacks for the rest of
// the current turn." Nimble Escape is the same, as a Bonus Action.
func TestDisengageAndNimbleEscapeAvoidOpportunityAttacks(t *testing.T) {
	// Initiative: fighter 21, goblin 7. No other dice: an attack would panic.
	s := start(t, dice.Fixed(20, 5), fighter("fighter", 5, 2), goblin("g1", 4, 2))
	submit(t, s, session.Disengage{Actor: "fighter"})
	ev := submit(t, s, session.Move{Actor: "fighter", To: pos(9, 2)})
	if _, ok := find[session.OpportunityAttackMade](ev); ok {
		t.Fatal("Disengage must prevent the Opportunity Attack")
	}
	endTurn(t, s)
	submit(t, s, session.NimbleEscape{Actor: "g1"})
	submit(t, s, session.Move{Actor: "g1", To: pos(3, 2)}) // fighter is far: nothing to provoke
}

func TestOpportunityAttackThatDownsTheMoverStopsTheMove(t *testing.T) {
	f := fighter("fighter", 5, 2)
	f.MaxHP = 1
	// Initiative fighter 21, g1 7. Reaction attack: 15+4 = 19 vs AC 16 hits, 3+2 = 5.
	s := start(t, dice.Fixed(20, 5, 15, 3), f, goblin("g1", 4, 2))
	ev := submit(t, s, session.Move{Actor: "fighter", To: pos(8, 2)})
	if _, ok := find[session.Moved](ev); ok {
		t.Fatal("a downed creature must not complete its move")
	}
	if got := s.State().Creatures["fighter"].Pos; got != pos(5, 2) {
		t.Fatalf("fighter at %v, want 5,2", got)
	}
	if end, ok := find[session.CombatEnded](ev); !ok || end.Winner != "monsters" {
		t.Fatalf("want CombatEnded for monsters, events %+v", ev)
	}
}

// Ending Combat: "when one side or the other is defeated ... killed or knocked
// out or have surrendered or fled."
func TestCombatEndsWhenTheLastOpponentIsDowned(t *testing.T) {
	g1, g2 := goblin("g1", 5, 2), goblin("g2", 3, 2)
	g1.MaxHP, g2.MaxHP = 1, 1
	// Initiative: fighter 21, g1 7, g2 6. Fighter hits g1, later g2: d20 15, d8 1 (1+3 = 4).
	s := start(t, dice.Fixed(20, 5, 4, 15, 1, 15, 1), fighter("fighter", 4, 2), g1, g2)

	ev := submit(t, s, session.Attack{Actor: "fighter", Target: "g1"})
	if _, ok := find[session.CombatEnded](ev); ok {
		t.Fatal("one goblin is still standing")
	}
	// g1 is skipped; g2 takes no action; the round turns over.
	endTurn(t, s)
	if got := s.State().Active; got != "g2" {
		t.Fatalf("active = %v, want g2 (downed g1 is skipped)", got)
	}
	endTurn(t, s)
	ev = submit(t, s, session.Attack{Actor: "fighter", Target: "g2"})
	end, ok := find[session.CombatEnded](ev)
	if !ok || end.Winner != "party" {
		t.Fatalf("want CombatEnded for party, events %+v", ev)
	}
	st := s.State()
	if !st.Over || st.Winner != "party" {
		t.Fatalf("state = %+v", st)
	}
	if r := rejection(t, s, session.EndTurn{Actor: "fighter"}); r.Code != session.CombatOver {
		t.Fatalf("code = %v", r.Code)
	}
	if la := s.LegalActions(); la.EndTurn || la.Action || la.Leave || len(la.Moves) != 0 {
		t.Fatalf("no options after combat: %+v", la)
	}
}

func TestCombatEndsWhenTheLastOpponentFlees(t *testing.T) {
	g1, g2 := goblin("g1", 5, 2), goblin("g2", 9, 2)
	g1.MaxHP = 1
	// Initiative: fighter 21, g1 7, g2 6. Fighter downs g1 (d20 15, d8 1).
	s := start(t, dice.Fixed(20, 5, 4, 15, 1), fighter("fighter", 4, 2), g1, g2)
	submit(t, s, session.Attack{Actor: "fighter", Target: "g1"})
	endTurn(t, s) // g2's turn
	ev := submit(t, s, session.Leave{Actor: "g2"})
	if left, ok := find[session.CreatureLeft](ev); !ok || left.Creature != "g2" {
		t.Fatalf("want CreatureLeft g2, events %+v", ev)
	}
	if end, ok := find[session.CombatEnded](ev); !ok || end.Winner != "party" {
		t.Fatalf("want CombatEnded for party, events %+v", ev)
	}
	if st := s.State(); !st.Over || !st.Creatures["g2"].Gone {
		t.Fatalf("state = %+v", st)
	}
}

func TestFleeingCreatureIsSkippedAndCombatContinues(t *testing.T) {
	// Two goblins; g1 leaves, g2 stays: combat goes on and g1 is never active again.
	s := start(t, dice.Fixed(20, 15, 4), fighter("fighter", 1, 1), goblin("g1", 8, 1), goblin("g2", 9, 3))
	endTurn(t, s)
	ev := submit(t, s, session.Leave{Actor: "g1"})
	if _, ok := find[session.CombatEnded](ev); ok {
		t.Fatal("g2 is still in the fight")
	}
	if got := s.State().Active; got != "g2" {
		t.Fatalf("active = %v, want g2", got)
	}
	endTurn(t, s)
	endTurn(t, s)
	if got := s.State().Active; got != "g2" {
		t.Fatalf("active = %v, want g2 (g1 gone)", got)
	}
	if r := rejection(t, s, session.Attack{Actor: "g2", Target: "g1"}); r.Code != session.InvalidTarget {
		t.Fatalf("attacking a creature that left: code = %v, want InvalidTarget", r.Code)
	}
}

// The mover downed by an Opportunity Attack loses the rest of its turn when the
// combat goes on: the turn passes and the downed creature can no longer act.
func TestMoverDownedByOpportunityAttackPassesTheTurnWhenCombatContinues(t *testing.T) {
	f1 := fighter("f1", 5, 2)
	f1.MaxHP = 1
	// Initiative: f1 21, f2 11, g1 7. Reaction attack: 15+4 = 19 vs AC 16 hits, 3+2 = 5.
	s := start(t, dice.Fixed(20, 10, 5, 15, 3), f1, fighter("f2", 1, 1), goblin("g1", 4, 2))
	ev := submit(t, s, session.Move{Actor: "f1", To: pos(8, 2)})
	if _, ok := find[session.CombatEnded](ev); ok {
		t.Fatal("f2 is still standing")
	}
	if _, ok := find[session.Moved](ev); ok {
		t.Fatal("a downed creature must not complete its move")
	}
	if _, ok := find[session.TurnEnded](ev); !ok {
		t.Fatalf("the downed mover's turn must end, events %+v", ev)
	}
	if got := s.State().Active; got != "f2" {
		t.Fatalf("active = %v, want f2", got)
	}
	for _, in := range []session.Intent{
		session.Attack{Actor: "f1", Target: "g1"},
		session.Move{Actor: "f1", To: pos(6, 2)},
		session.Dash{Actor: "f1"},
	} {
		if r := rejection(t, s, in); r.Code != session.NotYourTurn {
			t.Errorf("%T from the downed creature: code = %v, want NotYourTurn", in, r.Code)
		}
	}
}
