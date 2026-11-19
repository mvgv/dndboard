package session_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
)

// Scenario tests for visibility and hidden information. Sources are SRD 5.2
// "Rules Glossary": Bright Light, Dim Light, Darkness, Darkvision, Lightly
// Obscured, Heavily Obscured, Blinded, Passive Perception, Search; and
// "Playing the Game" (Exploration: vision and light). Every scenario starts with
// initiative dice 15, 10, 5 (so creatures act in the order they are listed).

func pc(id string, x, y int) session.CreatureSpec {
	f := fighter(id, x, y)
	f.PlayerCharacter = true
	return f
}

func startView(t *testing.T, cfg session.Config) *session.Session {
	t.Helper()
	if cfg.Dice == nil {
		cfg.Dice = dice.Fixed(15, 10, 5, 4, 3, 2)
	}
	cfg.Board = mustBoard(t, hallRows)
	s, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var hallRows = []string{
	"#############",
	"#...........#",
	"#...........#",
	"#...........#",
	"#############",
}

func sees(s *session.Session, viewer, other session.ID) bool {
	_, ok := s.ViewFor(viewer).Creatures[other]
	return ok
}

// "Bright Light is normal illumination": in a lit hall everyone sees everyone.
func TestBrightLightEveryoneSeesInLineOfSight(t *testing.T) {
	s := startView(t, session.Config{Creatures: []session.CreatureSpec{pc("a", 1, 2), goblin("g", 9, 2)}})
	if !sees(s, "a", "g") || !sees(s, "g", "a") {
		t.Fatal("creatures in a lit hall with clear line of sight must see each other")
	}
}

// "Darkness is Heavily Obscured ... you have the Blinded condition while trying
// to see something in a Heavily Obscured space": without light or Darkvision a
// creature in Darkness is unseen; a torch lights it.
func TestDarkCreatureIsUnseenWithoutLightOrDarkvision(t *testing.T) {
	s := startView(t, session.Config{
		Ambient:   session.Dark,
		Creatures: []session.CreatureSpec{pc("a", 1, 2), goblin("g", 9, 2)},
	})
	if sees(s, "a", "g") {
		t.Fatal("a creature in Darkness must be unseen without light or Darkvision")
	}
	if !sees(s, "a", "a") {
		t.Fatal("a viewer always sees itself")
	}
}

func TestLightSourceIlluminatesWithinItsBrightAndDimRadius(t *testing.T) {
	// Torch at 8,2: bright 20 ft (4 squares), dim 10 ft more.
	torch := []session.LightSource{{Pos: board.Pos{X: 8, Y: 2}, BrightFt: 20, DimFt: 10}}
	s := startView(t, session.Config{
		Ambient: session.Dark, Lights: torch,
		Creatures: []session.CreatureSpec{pc("a", 1, 2), goblin("near", 9, 2), goblin("far", 1, 1)},
	})
	if !sees(s, "a", "near") {
		t.Fatal("a creature 1 square from a torch must be seen")
	}
	if sees(s, "a", "far") {
		t.Fatal("a creature 7 squares from the torch is in Darkness")
	}
}

// "Darkvision: you can see in Dim Light within a specified range as if it were
// Bright Light and in Darkness within that range as if it were Dim Light."
func TestDarkvisionSeesInDarknessWithinRangeOnly(t *testing.T) {
	dv := pc("dv", 1, 2)
	dv.DarkvisionFt = 60 // 12 squares
	short := pc("short", 1, 3)
	short.DarkvisionFt = 20 // 4 squares
	s := startView(t, session.Config{
		Ambient:   session.Dark,
		Creatures: []session.CreatureSpec{dv, short, goblin("g", 9, 2)},
	})
	if !sees(s, "dv", "g") {
		t.Fatal("Darkvision 60 ft must see a creature 8 squares away in Darkness")
	}
	if sees(s, "short", "g") {
		t.Fatal("Darkvision 20 ft must not see a creature 8 squares away")
	}
}

// Visible state differs per Player Character.
func TestVisibleStateDiffersPerPlayerCharacter(t *testing.T) {
	dv := pc("dv", 2, 2)
	dv.DarkvisionFt = 60
	s := startView(t, session.Config{
		Ambient:   session.Dark,
		Creatures: []session.CreatureSpec{dv, pc("human", 1, 1), goblin("g", 9, 2)},
	})
	a, b := s.ViewFor("dv"), s.ViewFor("human")
	if len(a.Creatures) == len(b.Creatures) {
		t.Fatalf("views must differ: dv sees %d, human sees %d", len(a.Creatures), len(b.Creatures))
	}
	if len(a.Visible) <= len(b.Visible) {
		t.Fatalf("Darkvision must see more squares: %d vs %d", len(a.Visible), len(b.Visible))
	}
}

// Line of sight: a closed Door blocks it, an open one does not.
func TestClosedDoorBlocksLineOfSight(t *testing.T) {
	rows := []string{
		"#########",
		"#...#...#",
		"#.......#",
		"#...#...#",
		"#########",
	}
	// Wall segments at x=4, y=1 and y=3; the door fills the gap at 4,2.
	cfg := session.Config{
		Dice:      dice.Fixed(15, 10),
		Board:     mustBoard(t, rows),
		Creatures: []session.CreatureSpec{pc("a", 3, 2), goblin("g", 5, 2)},
		Objects:   []session.ObjectSpec{{ID: "door", Kind: session.Door, Pos: board.Pos{X: 4, Y: 2}}},
	}
	s, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if sees(s, "a", "g") {
		t.Fatal("a closed door must block line of sight")
	}
}

// "Blinded": Blinded creatures see nothing but themselves.
func TestBlindedCreatureSeesNothing(t *testing.T) {
	blind := pc("a", 1, 2)
	blind.Conditions = []session.Condition{session.CondBlinded}
	s := startView(t, session.Config{Creatures: []session.CreatureSpec{blind, goblin("g", 3, 2)}})
	if sees(s, "a", "g") {
		t.Fatal("a Blinded creature can't see other creatures")
	}
}

// Hidden information: secret doors, traps and hidden monsters never reach a
// client-bound view or event stream until discovered.
func TestUndiscoveredSecretsNeverAppearInViewsOrEvents(t *testing.T) {
	ambusher := goblin("ambusher", 5, 2)
	ambusher.HiddenDC = 18
	s := startView(t, session.Config{
		Dice:      dice.Fixed(15, 10, 5), // the hidden ambusher rolls initiative with Advantage
		Creatures: []session.CreatureSpec{pc("a", 1, 2), ambusher},
		Objects:   []session.ObjectSpec{{ID: "secret", Kind: session.Door, Pos: board.Pos{X: 6, Y: 0}, Secret: true, DC: 25}},
		Traps:     []session.TrapSpec{{ID: "pit", Pos: board.Pos{X: 3, Y: 2}, DC: 25}},
	})
	v := s.ViewFor("a")
	if _, ok := v.Creatures["ambusher"]; ok {
		t.Fatal("hidden monster leaked into the view")
	}
	if _, ok := v.Objects["secret"]; ok {
		t.Fatal("secret door leaked into the view")
	}
	if _, ok := v.Traps["pit"]; ok {
		t.Fatal("trap leaked into the view")
	}
	// Events: a hidden monster's actions must not be sent to the Player Character.
	evs := []session.Event{
		session.Moved{Creature: "ambusher", To: board.Pos{X: 4, Y: 2}},
		session.Moved{Creature: "a", To: board.Pos{X: 2, Y: 2}},
	}
	got := s.EventsFor("a", evs)
	if len(got) != 1 {
		t.Fatalf("events for a = %v, want only a's own move", got)
	}
	// Utilizing an undiscovered secret door is rejected as if it did not exist.
	if rej := rejection(t, s, session.Utilize{Actor: "a", Object: "secret"}); rej.Code != session.InvalidTarget {
		t.Fatalf("code = %v, want InvalidTarget", rej.Code)
	}
}

// Passive Perception is 10 + the check bonus: it finds a trap and a secret door
// whose DC is at or below it, in sight, and nothing above.
func TestPassivePerceptionRevealsAtOrBelowItsValue(t *testing.T) {
	watcher := pc("a", 1, 2)
	watcher.PerceptionMod = 2 // passive 12
	s := startView(t, session.Config{
		Dice:      dice.Fixed(15, 10),
		Creatures: []session.CreatureSpec{watcher, goblin("g", 11, 3)},
		Traps: []session.TrapSpec{
			{ID: "trap-easy", Pos: board.Pos{X: 3, Y: 2}, DC: 12},
			{ID: "trap-hard", Pos: board.Pos{X: 4, Y: 2}, DC: 13},
		},
	})
	v := s.ViewFor("a")
	if _, ok := v.Traps["trap-easy"]; !ok {
		t.Fatal("trap at DC 12 must be found by passive Perception 12")
	}
	if _, ok := v.Traps["trap-hard"]; ok {
		t.Fatal("trap at DC 13 must stay hidden from passive Perception 12")
	}
}

func TestPassivePerceptionFindsSecretDoorsAtOrBelowItsValue(t *testing.T) {
	watcher := pc("a", 1, 2)
	watcher.PerceptionMod = 2
	s := startView(t, session.Config{
		Dice:      dice.Fixed(15, 10),
		Creatures: []session.CreatureSpec{watcher, goblin("g", 11, 3)},
		Objects: []session.ObjectSpec{
			{ID: "sd-easy", Kind: session.Door, Pos: board.Pos{X: 1, Y: 0}, Secret: true, DC: 12},
			{ID: "sd-hard", Kind: session.Door, Pos: board.Pos{X: 3, Y: 0}, Secret: true, DC: 13},
		},
	})
	v := s.ViewFor("a")
	if _, ok := v.Objects["sd-easy"]; !ok {
		t.Fatal("secret door at DC 12 must be found by passive Perception 12")
	}
	if _, ok := v.Objects["sd-hard"]; ok {
		t.Fatal("secret door at DC 13 must stay hidden")
	}
}

// Passive Perception drops by 5 with Disadvantage: a trap in Dim Light (Lightly
// Obscured) needs the lower score.
func TestPassivePerceptionHasDisadvantageInDimLight(t *testing.T) {
	watcher := pc("a", 1, 2)
	watcher.PerceptionMod = 2 // 12, or 7 with Disadvantage
	s := startView(t, session.Config{
		Ambient:   session.Dim,
		Dice:      dice.Fixed(15, 10),
		Creatures: []session.CreatureSpec{watcher, goblin("g", 11, 3)},
		Traps: []session.TrapSpec{
			{ID: "t7", Pos: board.Pos{X: 3, Y: 2}, DC: 7},
			{ID: "t8", Pos: board.Pos{X: 4, Y: 2}, DC: 8},
		},
	})
	v := s.ViewFor("a")
	if _, ok := v.Traps["t7"]; !ok {
		t.Fatal("DC 7 trap must be found in Dim Light (passive 7)")
	}
	if _, ok := v.Traps["t8"]; ok {
		t.Fatal("DC 8 trap must stay hidden in Dim Light (passive 7)")
	}
}

// Passive Perception is noticed as the Player Character moves into sight.
func TestPassivePerceptionRunsAsThePlayerCharacterMoves(t *testing.T) {
	watcher := pc("a", 1, 2)
	watcher.PerceptionMod = 5 // passive 15, or 10 in Dim Light
	watcher.DarkvisionFt = 20 // sees Darkness as Dim within 4 squares
	s := startView(t, session.Config{
		Ambient:   session.Dark,
		Dice:      dice.Fixed(15, 10),
		Creatures: []session.CreatureSpec{watcher, goblin("g", 11, 3)},
		Traps:     []session.TrapSpec{{ID: "pit", Pos: board.Pos{X: 7, Y: 2}, DC: 10}},
	})
	if _, ok := s.ViewFor("a").Traps["pit"]; ok {
		t.Fatal("trap beyond Darkvision must not be found yet")
	}
	evs := submit(t, s, session.Move{Actor: "a", To: board.Pos{X: 5, Y: 2}})
	d := all[session.Discovered](evs)
	if len(d) != 1 || d[0].ID != "pit" || !d[0].Passive || d[0].Finder != "a" {
		t.Fatalf("discoveries = %+v, want passive find of pit by a", d)
	}
	if _, ok := s.ViewFor("a").Traps["pit"]; !ok {
		t.Fatal("discovered trap must appear in the view")
	}
}

// "Search: a Wisdom (Perception) check": the engine rolls and reveals what the
// total meets, in sight.
func TestSearchRevealsTrapsAndSecretDoorsTheRollMeets(t *testing.T) {
	a := pc("a", 1, 2)
	a.PerceptionMod = 1
	s := startView(t, session.Config{
		Dice:      dice.Fixed(15, 10, 14), // initiative, then the Search d20: 14+1 = 15
		Creatures: []session.CreatureSpec{a, goblin("g", 11, 3)},
		Objects: []session.ObjectSpec{
			{ID: "sd-15", Kind: session.Door, Pos: board.Pos{X: 2, Y: 0}, Secret: true, DC: 15},
			{ID: "sd-16", Kind: session.Door, Pos: board.Pos{X: 4, Y: 0}, Secret: true, DC: 16},
		},
		Traps: []session.TrapSpec{
			{ID: "trap-15", Pos: board.Pos{X: 3, Y: 2}, DC: 15},
			{ID: "trap-16", Pos: board.Pos{X: 4, Y: 2}, DC: 16},
		},
	})
	evs := submit(t, s, session.Search{Actor: "a"})
	got, _ := find[session.Searched](evs)
	if got.D20 != 14 || got.Total != 15 {
		t.Fatalf("Searched = %+v, want d20 14 total 15", got)
	}
	var ids []session.ID
	for _, d := range all[session.Discovered](evs) {
		if d.Passive || d.Finder != "a" {
			t.Fatalf("search discovery %+v must be active by a", d)
		}
		ids = append(ids, d.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []session.ID{"sd-15", "trap-15"}) {
		t.Fatalf("discovered %v, want sd-15 and trap-15", ids)
	}
	v := s.ViewFor("a")
	if _, ok := v.Objects["sd-15"]; !ok {
		t.Fatal("found secret door must be in the view")
	}
	if _, ok := v.Traps["trap-16"]; ok {
		t.Fatal("trap above the roll must stay hidden")
	}
}

// Once found, a secret door is a normal Door that Utilize can open.
func TestDiscoveredSecretDoorCanBeUtilized(t *testing.T) {
	a := pc("a", 1, 1)
	a.PerceptionMod = 5
	s := startView(t, session.Config{
		Dice:      dice.Fixed(15, 10),
		Creatures: []session.CreatureSpec{a, goblin("g", 11, 3)},
		Objects:   []session.ObjectSpec{{ID: "sd", Kind: session.Door, Pos: board.Pos{X: 1, Y: 0}, Secret: true, DC: 10}},
	})
	_ = submit(t, s, session.Utilize{Actor: "a", Object: "sd"})
	if !s.ViewFor("a").Objects["sd"].Open {
		t.Fatal("the discovered door must open")
	}
}

// Hidden information leaks through any event that names it, not just Moved:
// conditions, healing and a lever that works an undiscovered secret door must
// stay out of the client-bound events.
func TestEventsNeverNameHiddenCreaturesOrSecretDoors(t *testing.T) {
	ambusher := goblin("ambusher", 5, 2)
	ambusher.HiddenDC = 18
	s := startView(t, session.Config{
		Dice:      dice.Fixed(15, 10, 5), // the hidden ambusher rolls initiative with Advantage
		Creatures: []session.CreatureSpec{pc("a", 1, 1), ambusher},
		Objects: []session.ObjectSpec{
			{ID: "secret", Kind: session.Door, Pos: board.Pos{X: 6, Y: 0}, Secret: true, DC: 25},
			{ID: "lever", Kind: session.Lever, Pos: board.Pos{X: 1, Y: 0}, Controls: "secret"},
		},
	})
	var evs []session.Event
	apply := func(e session.Effect) {
		t.Helper()
		got, err := s.Apply(e)
		if err != nil {
			t.Fatalf("%T rejected: %v", e, err)
		}
		evs = append(evs, got...)
	}
	apply(session.InflictCondition{Target: "ambusher", Condition: session.CondPoisoned})
	apply(session.InflictDamage{Target: "ambusher", Amount: 3, Type: "fire"})
	apply(session.Heal{Target: "ambusher", Amount: 2})
	apply(session.InflictCondition{Target: "a", Condition: session.CondFrightened, Source: "ambusher"})
	evs = append(evs, submit(t, s, session.Utilize{Actor: "a", Object: "lever"})...)

	if _, ok := find[session.HealingReceived](evs); !ok {
		t.Fatal("scenario must have produced a HealingReceived to filter")
	}
	if _, ok := find[session.ObjectChanged](evs); !ok {
		t.Fatal("scenario must have produced an ObjectChanged to filter")
	}
	got := s.EventsFor("a", evs)
	for _, e := range got {
		if text := fmt.Sprintf("%+v", e); strings.Contains(text, "ambusher") || strings.Contains(text, "secret") {
			t.Fatalf("client-bound event leaks hidden information: %T %s", e, text)
		}
	}
	// What the viewer may know still arrives: its own condition (source
	// cleared) and the lever it pulled.
	if c, ok := find[session.ConditionApplied](got); !ok || c.Creature != "a" || c.Source != "" {
		t.Fatalf("own condition missing or leaking its source: %+v", c)
	}
	if _, ok := find[session.ObjectUsed](got); !ok {
		t.Fatal("the lever the viewer pulled must be reported")
	}
}

// Whose turn it is names a creature: View.Active must not name a hidden one.
func TestViewActiveDoesNotNameAHiddenCreature(t *testing.T) {
	ambusher := goblin("ambusher", 5, 2)
	ambusher.HiddenDC = 18
	s := startView(t, session.Config{
		Dice:      dice.Fixed(15, 10, 5), // a goes first, the hidden ambusher second
		Creatures: []session.CreatureSpec{pc("a", 1, 2), ambusher},
	})
	if got := s.ViewFor("a").Active; got != "a" {
		t.Fatalf("Active = %q, want a on its own turn", got)
	}
	submit(t, s, session.EndTurn{Actor: "a"})
	if got := s.State().Active; got != "ambusher" {
		t.Fatalf("scenario: server Active = %q, want the ambusher's turn", got)
	}
	if got := s.ViewFor("a").Active; got != "" {
		t.Fatalf("View.Active = %q leaks the hidden creature's turn", got)
	}
}

// "You have Disadvantage on Wisdom (Perception) checks to see something in a
// Lightly Obscured space": Search rolls with Disadvantage against a trap in Dim
// Light, where it would otherwise find the trap with the first die.
func TestSearchHasDisadvantageAgainstTrapsInDimLight(t *testing.T) {
	a := pc("a", 1, 2)
	a.PerceptionMod = 1
	s := startView(t, session.Config{
		Ambient: session.Dim,
		// Initiative 15 and 10, then the Search's two dice: Disadvantage keeps the 4.
		Dice:      dice.Fixed(15, 10, 14, 4),
		Creatures: []session.CreatureSpec{a, goblin("g", 11, 3)},
		Traps:     []session.TrapSpec{{ID: "pit", Pos: board.Pos{X: 3, Y: 2}, DC: 10}},
	})
	// Passive Perception in Dim Light is 11 - 5 = 6, so the pit is still hidden.
	evs := submit(t, s, session.Search{Actor: "a"})
	got := all[session.Searched](evs)
	if len(got) != 1 || got[0].Mode != session.Disadvantage || got[0].D20 != 4 {
		t.Fatalf("Searched = %+v, want one roll with Disadvantage keeping the 4", got)
	}
	if len(all[session.Discovered](evs)) != 0 {
		t.Fatal("total 5 must not find a DC 10 trap")
	}
}
