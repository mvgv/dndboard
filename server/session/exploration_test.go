package session_test

import (
	"errors"
	"testing"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
	"github.com/mvgv/dndboard/server/srd"
)

// explore puts two party members at 1,1 and 1,3 and a goblin at 9,2 in the
// hall. Perception range is 30 ft (6 squares) and every Stealth and Perception
// modifier is +0, so passive Perception is 10.
func explore(t *testing.T, rows []string, d dice.Roller) *session.Exploration {
	t.Helper()
	f := srd.Fighter()
	f.ID, f.Pos, f.StealthMod, f.PerceptionMod = "fighter", board.Pos{X: 1, Y: 1}, 0, 0
	c := srd.Fighter()
	c.ID, c.Pos, c.StealthMod, c.PerceptionMod = "cleric", board.Pos{X: 1, Y: 3}, 0, 0
	g := srd.GoblinWarrior()
	g.ID, g.Pos, g.StealthMod, g.PerceptionMod = "goblin", board.Pos{X: 9, Y: 2}, 0, 0
	e, err := session.NewExploration(session.ExplorationConfig{
		Board:             mustBoard(t, rows),
		Creatures:         []session.CreatureSpec{f, c, g},
		Dice:              d,
		PerceptionRangeFt: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func exploreSubmit(t *testing.T, e *session.Exploration, in session.Intent) []session.Event {
	t.Helper()
	ev, err := e.Submit(in)
	if err != nil {
		t.Fatalf("%T rejected: %v", in, err)
	}
	return ev
}

func stealthOf(evs []session.Event, id session.ID) (session.StealthChecked, bool) {
	for _, e := range evs {
		if s, ok := e.(session.StealthChecked); ok && s.Creature == id {
			return s, true
		}
	}
	return session.StealthChecked{}, false
}

func surprised(evs []session.Event, id session.ID) bool {
	for _, e := range evs {
		if s, ok := e.(session.Surprised); ok && s.Creature == id {
			return true
		}
	}
	return false
}

func initiativeOf(evs []session.Event, id session.ID) (session.InitiativeRolled, bool) {
	for _, e := range evs {
		if r, ok := e.(session.InitiativeRolled); ok && r.Creature == id {
			return r, true
		}
	}
	return session.InitiativeRolled{}, false
}

// Several Player Characters can move at the same time outside combat.
func TestExplorationPartyMovesSimultaneously(t *testing.T) {
	e := explore(t, hall, dice.Fixed(1)) // no dice should be needed
	// Both submit with no turn order, and each can move more than its Speed.
	exploreSubmit(t, e, session.Move{Actor: "fighter", To: board.Pos{X: 2, Y: 1}})
	exploreSubmit(t, e, session.Move{Actor: "cleric", To: board.Pos{X: 2, Y: 3}})
	exploreSubmit(t, e, session.Move{Actor: "fighter", To: board.Pos{X: 2, Y: 2}})
	if p, _ := e.Position("fighter"); p != (board.Pos{X: 2, Y: 2}) {
		t.Fatalf("fighter at %v", p)
	}
	if p, _ := e.Position("cleric"); p != (board.Pos{X: 2, Y: 3}) {
		t.Fatalf("cleric at %v", p)
	}
	if e.Combat() != nil {
		t.Fatal("combat started with the goblin out of range")
	}
}

func TestExplorationRejectsWallsAndOccupiedSquares(t *testing.T) {
	e := explore(t, hall, dice.Fixed(1))
	for _, to := range []board.Pos{{X: 0, Y: 1}, {X: 1, Y: 3}} { // wall, ally's square
		_, err := e.Submit(session.Move{Actor: "fighter", To: to})
		var rej *session.Rejection
		if !errors.As(err, &rej) || rej.Code != session.Unreachable {
			t.Fatalf("move to %v: want Unreachable, got %v", to, err)
		}
	}
	if _, err := e.Submit(session.Move{Actor: "nobody", To: board.Pos{X: 2, Y: 1}}); err == nil {
		t.Fatal("unknown actor accepted")
	}
}

// Stealth is checked for both sides when they come into perception range.
func TestExplorationChecksStealthOfBothSides(t *testing.T) {
	// Stealth: fighter 8, goblin 15; then initiative (the surprised fighter
	// rolls twice).
	e := explore(t, hall, dice.Fixed(8, 15, 10, 10, 10, 10))
	evs := exploreSubmit(t, e, session.Move{Actor: "fighter", To: board.Pos{X: 3, Y: 1}})
	for id, want := range map[session.ID]int{"fighter": 8, "goblin": 15} {
		s, ok := stealthOf(evs, id)
		if !ok {
			t.Fatalf("no Stealth check for %s", id)
		}
		if s.D20 != want || s.Total != want {
			t.Fatalf("%s Stealth = %+v, want %d", id, s, want)
		}
	}
}

func TestExplorationFarCreaturesDoNotRoll(t *testing.T) {
	e := explore(t, hall, dice.Fixed(1))
	evs := exploreSubmit(t, e, session.Move{Actor: "fighter", To: board.Pos{X: 2, Y: 1}})
	if _, ok := stealthOf(evs, "goblin"); ok {
		t.Fatal("stealth rolled out of perception range")
	}
}

func TestExplorationNoContactThroughWalls(t *testing.T) {
	rows := []string{
		"#############",
		"#....#......#",
		"#....#......#",
		"#....#......#",
		"#############",
	}
	e := explore(t, rows, dice.Fixed(1))
	// Fighter 1,1 -> 4,2 is 5 squares from the goblin at 9,2, behind a wall.
	exploreSubmit(t, e, session.Move{Actor: "fighter", To: board.Pos{X: 4, Y: 2}})
	if e.Combat() != nil {
		t.Fatal("combat started through a wall")
	}
}

// Surprised creatures roll initiative with disadvantage.
func TestExplorationSurprisedRollInitiativeWithDisadvantage(t *testing.T) {
	// Only the fighter is in range of the goblin at 3,1 (the cleric at 1,3 is 8
	// away). Stealth, in listing order: fighter 8 (<10, noticed), goblin 15
	// (>=10, unnoticed). Initiative: fighter 17 then 4 (disadvantage), cleric 6
	// (not in contact), goblin 9.
	e := explore(t, hall, dice.Fixed(8, 15, 17, 4, 6, 9))
	evs := exploreSubmit(t, e, session.Move{Actor: "fighter", To: board.Pos{X: 3, Y: 1}})
	if !surprised(evs, "fighter") {
		t.Fatal("the fighter did not notice the goblin and should be surprised")
	}
	if r, _ := initiativeOf(evs, "fighter"); r.D20 != 4 {
		t.Fatalf("fighter initiative %+v: want the lower of 17 and 4", r)
	}
	if surprised(evs, "goblin") || surprised(evs, "cleric") {
		t.Fatal("only the fighter is surprised")
	}
	if r, _ := initiativeOf(evs, "goblin"); r.D20 != 9 {
		t.Fatalf("goblin initiative %+v: want a single roll of 9", r)
	}
}

// Nobody is surprised when both sides notice each other; when neither does,
// both are (each failed to notice a threat).
func TestExplorationSurpriseWhenBothNoticeOrNeitherDoes(t *testing.T) {
	cases := map[string]struct {
		rolls []int
		want  bool
	}{
		"both noticed": {[]int{2, 2, 10, 10, 10}, false},
		"neither":      {[]int{20, 20, 10, 10, 10, 10, 10}, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := explore(t, hall, dice.Fixed(c.rolls...))
			evs := exploreSubmit(t, e, session.Move{Actor: "fighter", To: board.Pos{X: 3, Y: 1}})
			for _, id := range []session.ID{"fighter", "goblin"} {
				if surprised(evs, id) != c.want {
					t.Fatalf("%s surprised = %v, want %v", id, !c.want, c.want)
				}
			}
			if surprised(evs, "cleric") {
				t.Fatal("the cleric was out of contact")
			}
		})
	}
}

// Exploration ends and combat begins with an event.
func TestExplorationEndsWithCombatStartedEvent(t *testing.T) {
	e := explore(t, hall, dice.Fixed(2, 2, 10, 10, 10))
	evs := exploreSubmit(t, e, session.Move{Actor: "fighter", To: board.Pos{X: 5, Y: 1}})
	started, at := session.CombatStarted{}, -1
	for i, ev := range evs {
		if c, ok := ev.(session.CombatStarted); ok {
			started, at = c, i
		}
	}
	if at < 0 {
		t.Fatalf("no CombatStarted in %v", evs)
	}
	if len(started.Creatures) != 3 {
		t.Fatalf("combat creatures = %v", started.Creatures)
	}
	if _, ok := initiativeOf(evs[:at], "goblin"); ok {
		t.Fatal("initiative rolled before CombatStarted")
	}
	if _, ok := initiativeOf(evs[at:], "goblin"); !ok {
		t.Fatal("initiative not rolled after CombatStarted")
	}
	// The move stops where contact happened: perception range is 6 squares, so
	// the fighter halts at the first square 6 away from the goblin instead of
	// reaching 5,1 (which route it takes there is not specified).
	if p, _ := e.Position("fighter"); board.Distance(p, board.Pos{X: 9, Y: 2}) != 6 {
		t.Fatalf("fighter at %v, want a square 6 from the goblin", p)
	}
	if e.Combat() == nil {
		t.Fatal("no combat session")
	}
	_, err := e.Submit(session.Move{Actor: "cleric", To: board.Pos{X: 2, Y: 3}})
	var rej *session.Rejection
	if !errors.As(err, &rej) || rej.Code != session.NotExploring {
		t.Fatalf("exploration intent during combat: %v", err)
	}
}

func TestExplorationNeedsTwoTeams(t *testing.T) {
	f := srd.Fighter()
	f.ID = "fighter"
	if _, err := session.NewExploration(session.ExplorationConfig{Board: mustBoard(t, hall), Creatures: []session.CreatureSpec{f}}); err == nil {
		t.Fatal("exploration with one team accepted")
	}
}
