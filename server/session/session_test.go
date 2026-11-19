package session_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
	"github.com/mvgv/dndboard/server/srd"
)

// Hall: 11x3 floor inside walls.
var hall = []string{
	"#############",
	"#...........#",
	"#...........#",
	"#...........#",
	"#############",
}

func mustBoard(t *testing.T, rows []string) *board.Board {
	t.Helper()
	b, err := board.Parse(rows)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// newFight puts the Fighter at 1,2 and the Goblin Warrior at 5,2 (4 squares apart).
func newFight(t *testing.T, rows []string, d dice.Roller) *session.Session {
	t.Helper()
	f := srd.Fighter()
	f.ID, f.Pos = "fighter", board.Pos{X: 1, Y: 2}
	g := srd.GoblinWarrior()
	g.ID, g.Pos = "goblin", board.Pos{X: 5, Y: 2}
	s, err := session.New(session.Config{
		Board:     mustBoard(t, rows),
		Creatures: []session.CreatureSpec{f, g},
		Dice:      d,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func submit(t *testing.T, s *session.Session, in session.Intent) []session.Event {
	t.Helper()
	ev, err := s.Submit(in)
	if err != nil {
		t.Fatalf("%T rejected: %v", in, err)
	}
	return ev
}

func rejection(t *testing.T, s *session.Session, in session.Intent) *session.Rejection {
	t.Helper()
	ev, err := s.Submit(in)
	var rej *session.Rejection
	if !errors.As(err, &rej) {
		t.Fatalf("%T: want Rejection, got err=%v", in, err)
	}
	if len(ev) != 0 || rej.Reason == "" {
		t.Fatalf("rejection must carry no events and a reason: %v %+v", ev, rej)
	}
	return rej
}

func find[E session.Event](evs []session.Event) (E, bool) {
	for _, e := range evs {
		if v, ok := e.(E); ok {
			return v, true
		}
	}
	var zero E
	return zero, false
}

// Initiative: Dexterity check, d20 + modifier (SRD "Playing the Game", Initiative).
// Fighter Dex 12 (+1), Goblin Warrior Dex 15 (+2).
// Script: fighter d20=15 (16), goblin d20=10 (12) -> fighter first.

func TestFighterVsGoblinExchange(t *testing.T) {
	s := newFight(t, hall, dice.Fixed(
		15, 10, // initiative: fighter, goblin
		14, 6, // fighter attack: d20, d8 -> 14+5=19 vs AC 15 hits, 6+3=9
		10,       // goblin attack: d20 -> 10+4=14 vs AC 16 misses
		20, 4, 5, // fighter attack: natural 20, 2d8 -> 4+5+3=12
	))

	if got := s.State().Active; got != "fighter" {
		t.Fatalf("active = %q, want fighter", got)
	}

	submit(t, s, session.Move{Actor: "fighter", To: board.Pos{X: 4, Y: 2}})
	ev := submit(t, s, session.Attack{Actor: "fighter", Target: "goblin"})

	roll, _ := find[session.AttackRolled](ev)
	if roll.D20 != 14 || roll.Total != 19 || roll.AC != 15 || !roll.Hit || roll.Critical {
		t.Fatalf("attack roll = %+v", roll)
	}
	dmg, _ := find[session.DamageDealt](ev)
	if dmg.Amount != 9 || dmg.HPAfter != 1 || dmg.Type != "Slashing" {
		t.Fatalf("damage = %+v", dmg)
	}
	submit(t, s, session.EndTurn{Actor: "fighter"})

	if got := s.State().Active; got != "goblin" {
		t.Fatalf("active = %q, want goblin", got)
	}
	ev = submit(t, s, session.Attack{Actor: "goblin", Target: "fighter"})
	roll, _ = find[session.AttackRolled](ev)
	if roll.Total != 14 || roll.AC != 16 || roll.Hit {
		t.Fatalf("goblin roll = %+v", roll)
	}
	if _, dealt := find[session.DamageDealt](ev); dealt {
		t.Fatal("a miss must not deal damage")
	}
	submit(t, s, session.EndTurn{Actor: "goblin"})

	ev = submit(t, s, session.Attack{Actor: "fighter", Target: "goblin"})
	roll, _ = find[session.AttackRolled](ev)
	if !roll.Critical || !roll.Hit {
		t.Fatalf("natural 20 must hit as a Critical Hit: %+v", roll)
	}
	dmg, _ = find[session.DamageDealt](ev)
	if !reflect.DeepEqual(dmg.Rolls, []int{4, 5}) || dmg.Amount != 12 || dmg.HPAfter != 0 {
		t.Fatalf("critical damage = %+v", dmg)
	}
	if _, ok := find[session.CreatureDowned](ev); !ok {
		t.Fatal("goblin at 0 HP must go down")
	}
	end, ok := find[session.CombatEnded](ev)
	if !ok || end.Winner != "party" {
		t.Fatalf("combat end = %+v ok=%v", end, ok)
	}
	rejection(t, s, session.EndTurn{Actor: "fighter"})
}

func TestAttackHitsWhenTotalEqualsAC(t *testing.T) {
	// Goblin Warrior AC 15; fighter modifier +5: d20 10 -> 15 hits, 9 -> 14 misses.
	for _, tc := range []struct {
		d20 int
		hit bool
	}{{10, true}, {9, false}} {
		s := newFight(t, hall, dice.Fixed(15, 10, tc.d20, 1))
		submit(t, s, session.Move{Actor: "fighter", To: board.Pos{X: 4, Y: 2}})
		ev := submit(t, s, session.Attack{Actor: "fighter", Target: "goblin"})
		roll, _ := find[session.AttackRolled](ev)
		if roll.Hit != tc.hit {
			t.Errorf("d20 %d: hit = %v, want %v", tc.d20, roll.Hit, tc.hit)
		}
	}
}

func TestLegalActionsListReachableSquaresAndTargets(t *testing.T) {
	s := newFight(t, hall, dice.Fixed(15, 10))
	la := s.LegalActions()
	if la.Creature != "fighter" {
		t.Fatalf("legal actions for %q", la.Creature)
	}
	cost := map[board.Pos]int{}
	for _, m := range la.Moves {
		cost[m.To] = m.CostFt
	}
	want := map[board.Pos]int{
		{X: 2, Y: 2}: 5,  // adjacent
		{X: 2, Y: 1}: 5,  // diagonal costs 1 square
		{X: 3, Y: 3}: 10, // two diagonal-or-straight steps
		{X: 4, Y: 2}: 15,
		{X: 7, Y: 2}: 30, // Speed 30 ft = 6 squares
	}
	for p, c := range want {
		if got, ok := cost[p]; !ok || got != c {
			t.Errorf("square %v cost = %d (listed %v), want %d", p, got, ok, c)
		}
	}
	for _, p := range []board.Pos{{X: 0, Y: 2}, {X: 1, Y: 2}, {X: 5, Y: 2}} {
		if _, ok := cost[p]; ok {
			t.Errorf("square %v (wall, own square, occupied) must not be reachable", p)
		}
	}
	if len(la.Attacks) != 0 {
		t.Fatalf("goblin is 20 ft away, no melee targets: %+v", la.Attacks)
	}

	submit(t, s, session.Move{Actor: "fighter", To: board.Pos{X: 4, Y: 2}})
	la = s.LegalActions()
	if len(la.Attacks) != 1 || la.Attacks[0].Target != "goblin" || la.Attacks[0].Weapon != "Longsword" {
		t.Fatalf("attacks after closing in: %+v", la.Attacks)
	}
	for _, m := range la.Moves {
		if m.CostFt > 15 {
			t.Errorf("15 ft of 30 ft spent; %v costs %d", m.To, m.CostFt)
		}
	}
}

func TestDifficultTerrainCostsDoubleAndWallCornersBlockDiagonals(t *testing.T) {
	// SRD "Playing the Game", Grid: Difficult Terrain costs 2 squares to enter;
	// diagonal movement can't cross the corner of a wall.
	rows := []string{
		"#########",
		"#~......#",
		"#.#.....#",
		"#.......#",
		"#########",
	}
	s := newFight(t, rows, dice.Fixed(15, 10))
	cost := map[board.Pos]int{}
	for _, m := range s.LegalActions().Moves {
		cost[m.To] = m.CostFt
	}
	want := map[board.Pos]int{
		{X: 1, Y: 1}: 10, // Difficult Terrain
		{X: 1, Y: 3}: 5,
		{X: 2, Y: 3}: 10, // diagonal from 1,2 would cross the wall corner at 2,2
		{X: 2, Y: 1}: 15, // only via the Difficult Terrain square 1,1
	}
	for p, c := range want {
		if got := cost[p]; got != c {
			t.Errorf("square %v cost = %d ft, want %d", p, got, c)
		}
	}
	if _, ok := cost[board.Pos{X: 2, Y: 2}]; ok {
		t.Error("wall square must not be reachable")
	}
}

func TestRejectsIllegalIntentsWithReason(t *testing.T) {
	s := newFight(t, hall, dice.Fixed(15, 10, 14, 6))

	if r := rejection(t, s, session.Move{Actor: "goblin", To: board.Pos{X: 4, Y: 2}}); r.Code != session.NotYourTurn {
		t.Errorf("code = %v, want NotYourTurn", r.Code)
	}
	if r := rejection(t, s, session.Attack{Actor: "fighter", Target: "goblin"}); r.Code != session.OutOfReach {
		t.Errorf("out-of-range attack code = %v, want OutOfReach", r.Code)
	}
	if r := rejection(t, s, session.Move{Actor: "fighter", To: board.Pos{X: 1, Y: 0}}); r.Code != session.Unreachable {
		t.Errorf("wall square code = %v, want Unreachable", r.Code)
	}
	if r := rejection(t, s, session.Move{Actor: "fighter", To: board.Pos{X: 8, Y: 2}}); r.Code != session.Unreachable {
		t.Errorf("7 squares away with Speed 30 ft: code = %v, want Unreachable", r.Code)
	}
	if r := rejection(t, s, session.Move{Actor: "fighter", To: board.Pos{X: 5, Y: 2}}); r.Code != session.Unreachable {
		t.Errorf("occupied square code = %v, want Unreachable", r.Code)
	}
	if r := rejection(t, s, session.Attack{Actor: "fighter", Target: "nobody"}); r.Code != session.InvalidTarget {
		t.Errorf("unknown target code = %v, want InvalidTarget", r.Code)
	}

	// 15 ft spent, 15 ft (3 squares) left; 8,2 is 4 squares away.
	submit(t, s, session.Move{Actor: "fighter", To: board.Pos{X: 4, Y: 2}})
	if r := rejection(t, s, session.Move{Actor: "fighter", To: board.Pos{X: 8, Y: 2}}); r.Code != session.Unreachable {
		t.Errorf("beyond remaining movement code = %v, want Unreachable", r.Code)
	}

	submit(t, s, session.Attack{Actor: "fighter", Target: "goblin"})
	if r := rejection(t, s, session.Attack{Actor: "fighter", Target: "goblin"}); r.Code != session.ActionUsed {
		t.Errorf("second attack code = %v, want ActionUsed", r.Code)
	}
	if got := s.State().Creatures["goblin"].HP; got != 1 {
		t.Errorf("rejected intents must not change state: goblin HP = %d", got)
	}
}

func TestSameSeedReproducesTheSameEvents(t *testing.T) {
	run := func(seed uint64) []session.Event {
		f := srd.Fighter()
		f.ID, f.Pos = "fighter", board.Pos{X: 1, Y: 2}
		g := srd.GoblinWarrior()
		g.ID, g.Pos = "goblin", board.Pos{X: 2, Y: 2}
		s, err := session.New(session.Config{
			Board: mustBoard(t, hall), Creatures: []session.CreatureSpec{f, g}, Seed: seed,
		})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 20 && !s.State().Over; i++ {
			actor := s.State().Active
			other := session.ID("goblin")
			if actor == "goblin" {
				other = "fighter"
			}
			submit(t, s, session.Attack{Actor: actor, Target: other})
			if !s.State().Over {
				submit(t, s, session.EndTurn{Actor: actor})
			}
		}
		return s.Log()
	}
	a, b := run(42), run(42)
	if len(a) == 0 || !reflect.DeepEqual(a, b) {
		t.Fatal("same seed must reproduce the same event log")
	}
	if reflect.DeepEqual(a, run(43)) {
		t.Fatal("different seeds should differ")
	}
}

func TestAlliesCanBeCrossedButNotEndedIn(t *testing.T) {
	// SRD "Playing the Game", Moving around Other Creatures: you can pass
	// through an ally's space (not Difficult Terrain for you) but can't end
	// a move in another creature's space; a hostile space blocks.
	rows := []string{
		"#######",
		"#.....#",
		"#######",
	}
	f := srd.Fighter()
	f.ID, f.Pos = "fighter", board.Pos{X: 1, Y: 1}
	ally := srd.Fighter()
	ally.ID, ally.Pos = "ally", board.Pos{X: 2, Y: 1}
	g := srd.GoblinWarrior()
	g.ID, g.Pos = "goblin", board.Pos{X: 4, Y: 1}
	s, err := session.New(session.Config{
		Board: mustBoard(t, rows), Creatures: []session.CreatureSpec{f, ally, g},
		Dice: dice.Fixed(19, 2, 2),
	})
	if err != nil {
		t.Fatal(err)
	}
	cost := map[board.Pos]int{}
	for _, m := range s.LegalActions().Moves {
		cost[m.To] = m.CostFt
	}
	if !reflect.DeepEqual(cost, map[board.Pos]int{{X: 3, Y: 1}: 10}) {
		t.Fatalf("moves = %v, want only 3,1 at 10 ft through the ally", cost)
	}
}
