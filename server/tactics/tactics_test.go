package tactics_test

import (
	"reflect"
	"testing"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
	"github.com/mvgv/dndboard/server/srd"
	"github.com/mvgv/dndboard/server/tactics"
)

// Hall: 11x3 floor inside walls.
var hall = []string{
	"#############",
	"#...........#",
	"#...........#",
	"#...........#",
	"#############",
}

// Initiative bonuses that make the monsters act first, in the order listed
// (the Initiative check is d20 + modifier, so these dominate the roll).
const (
	first  = 100
	second = 50
	late   = -50
	later  = -60
)

func fighter(id string, x, y int) session.CreatureSpec {
	f := srd.Fighter()
	f.ID, f.Pos = session.ID(id), board.Pos{X: x, Y: y}
	f.InitiativeMod = late
	return f
}

func goblin(id string, x, y int) session.CreatureSpec {
	g := srd.GoblinWarrior()
	g.ID, g.Pos = session.ID(id), board.Pos{X: x, Y: y}
	g.InitiativeMod = first
	return g
}

func start(t *testing.T, seed uint64, specs ...session.CreatureSpec) *session.Session {
	t.Helper()
	b, err := board.Parse(hall)
	if err != nil {
		t.Fatal(err)
	}
	s, err := session.New(session.Config{Board: b, Creatures: specs, Dice: dice.Seeded(seed)})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func hurt(t *testing.T, s *session.Session, id string, amount int) {
	t.Helper()
	if _, err := s.Apply(session.InflictDamage{Target: session.ID(id), Amount: amount, Type: "Slashing"}); err != nil {
		t.Fatal(err)
	}
}

func choose(t *testing.T, b tactics.Brain, s *session.Session) session.Intent {
	t.Helper()
	in, ok := b.Choose(s.State(), s.LegalActions())
	if !ok {
		t.Fatal("brain made no choice")
	}
	return in
}

func submit(t *testing.T, s *session.Session, in session.Intent) {
	t.Helper()
	if _, err := s.Submit(in); err != nil {
		t.Fatalf("%T rejected: %v", in, err)
	}
}

func dist(a, b board.Pos) int { return board.Distance(a, b) }

func posOf(s *session.Session, id string) board.Pos { return s.State().Creatures[session.ID(id)].Pos }

// AC: Acceptance "Monster turns resolve without input, choosing only from legal actions".
func TestMonsterTurnResolvesWithoutInput(t *testing.T) {
	s := start(t, 1, goblin("g", 5, 2), fighter("f", 1, 2))
	brain := tactics.NewUtility([]session.CreatureSpec{goblin("g", 5, 2), fighter("f", 1, 2)}, nil)
	ev, err := tactics.PlayTurn(s, brain)
	if err != nil {
		t.Fatal(err)
	}
	var moved, attacked, ended bool
	for _, e := range ev {
		switch e.(type) {
		case session.Moved:
			moved = true
		case session.AttackRolled:
			attacked = true
		case session.TurnEnded:
			ended = true
		}
	}
	if !moved || !attacked || !ended {
		t.Fatalf("want move, attack and end of turn, got %v", ev)
	}
	if got := s.State().Active; got != "f" {
		t.Fatalf("active = %s, want the turn passed to f", got)
	}
}

// recorder wraps a brain and checks every intent it returns is legal.
type recorder struct {
	tactics.Brain
	t     *testing.T
	count int
}

func (r *recorder) Choose(st session.State, la session.LegalActions) (session.Intent, bool) {
	in, ok := r.Brain.Choose(st, la)
	if ok {
		r.count++
		if !tactics.Legal(la, in) {
			r.t.Fatalf("illegal intent %#v for %+v", in, la)
		}
	}
	return in, ok
}

// AC: choosing only from legal actions, over a whole fight (Submit would reject otherwise).
func TestWholeFightOnlyLegalIntents(t *testing.T) {
	specs := func() []session.CreatureSpec {
		return []session.CreatureSpec{
			goblin("g1", 9, 1), goblin("g2", 9, 3), fighter("f1", 1, 1), fighter("f2", 1, 3),
		}
	}
	for seed := uint64(1); seed <= 20; seed++ {
		s := start(t, seed, specs()...)
		rec := &recorder{Brain: tactics.NewUtility(specs(), nil), t: t}
		for round := 0; round < 60 && !s.State().Over; round++ {
			// Both sides are driven by the brain: the intents must all be legal.
			if _, err := tactics.PlayTurn(s, rec); err != nil {
				t.Fatalf("seed %d: %v", seed, err)
			}
		}
		if rec.count == 0 {
			t.Fatal("brain never chose")
		}
	}
}

// AC: scoring accounts for hit chance × expected damage.
func TestPrefersTheTargetItHitsMoreOften(t *testing.T) {
	hard, easy := fighter("hard", 4, 2), fighter("easy", 6, 2)
	hard.AC, easy.AC = 20, 10
	hard.InitiativeMod, easy.InitiativeMod = late, later
	g := goblin("g", 5, 2)
	s := start(t, 1, g, hard, easy)
	b := tactics.NewUtility([]session.CreatureSpec{g, hard, easy}, nil)
	if got := choose(t, b, s); got != (session.Attack{Actor: "g", Target: "easy"}) {
		t.Fatalf("got %#v, want an attack on the low-AC target", got)
	}
}

// AC: scoring accounts for the chance to drop a character. The goblin's scimitar
// does 3..8; the target at 5 Hit Points falls on a 5+ (4 in 6 of its hits).
func TestPrefersTheTargetItMayDrop(t *testing.T) {
	healthy, weak := fighter("healthy", 4, 2), fighter("weak", 6, 2)
	healthy.InitiativeMod, weak.InitiativeMod = late, later
	g := goblin("g", 5, 2)
	s := start(t, 1, g, healthy, weak)
	hurt(t, s, "weak", 7)
	b := tactics.NewUtility([]session.CreatureSpec{g, healthy, weak}, nil)
	if got := choose(t, b, s); got != (session.Attack{Actor: "g", Target: "weak"}) {
		t.Fatalf("got %#v, want an attack on the target it can drop", got)
	}
}

// AC: scoring accounts for opportunity-attack risk. A hurt cowardly goblin
// without Nimble Escape disengages before backing off instead of walking away
// from the Fighter's Reaction.
func TestRetreatDisengagesBeforeMovingAway(t *testing.T) {
	g := goblin("g", 5, 2)
	g.NimbleEscape = false
	f := fighter("f", 4, 2)
	s := start(t, 1, g, f)
	hurt(t, s, "g", 7)
	b := tactics.NewUtility([]session.CreatureSpec{g, f}, map[session.ID]tactics.Personality{"g": tactics.Cowardly})

	if got := choose(t, b, s); got != (session.Disengage{Actor: "g"}) {
		t.Fatalf("got %#v, want Disengage first", got)
	}
	submit(t, s, session.Disengage{Actor: "g"})
	mv, ok := choose(t, b, s).(session.Move)
	if !ok || dist(mv.To, posOf(s, "f")) < 4 {
		t.Fatalf("got %#v, want a move well away from the Fighter", mv)
	}
}

// AC: personality weights change choices. Same state, different personalities.
func TestCowardlyMonsterRetreatsWhenHurtButFightsWhenHealthy(t *testing.T) {
	g := goblin("g", 5, 2)
	f := fighter("f", 4, 2)
	specs := []session.CreatureSpec{g, f}
	cowardly := map[session.ID]tactics.Personality{"g": tactics.Cowardly}

	healthy := start(t, 1, g, f)
	if got := choose(t, tactics.NewUtility(specs, cowardly), healthy); got != (session.Attack{Actor: "g", Target: "f"}) {
		t.Fatalf("healthy cowardly goblin: got %#v, want an attack", got)
	}

	wounded := start(t, 1, g, f)
	hurt(t, wounded, "g", 7)
	b := tactics.NewUtility(specs, cowardly)
	// Nimble Escape is a free Disengage, so the goblin takes it, then runs.
	if got := choose(t, b, wounded); got != (session.NimbleEscape{Actor: "g"}) {
		t.Fatalf("hurt cowardly goblin: got %#v, want Nimble Escape", got)
	}
	submit(t, wounded, session.NimbleEscape{Actor: "g"})
	mv, ok := choose(t, b, wounded).(session.Move)
	if !ok || dist(mv.To, posOf(wounded, "f")) < 4 {
		t.Fatalf("got %#v, want a retreat", mv)
	}
}

func TestFerociousMonsterKeepsFightingWhenHurt(t *testing.T) {
	g := goblin("g", 5, 2)
	f := fighter("f", 4, 2)
	s := start(t, 1, g, f)
	hurt(t, s, "g", 7)
	b := tactics.NewUtility([]session.CreatureSpec{g, f}, map[session.ID]tactics.Personality{"g": tactics.Ferocious})
	if got := choose(t, b, s); got != (session.Attack{Actor: "g", Target: "f"}) {
		t.Fatalf("got %#v, want an attack", got)
	}
}

func TestProtectiveMonsterAttacksWhoThreatensTheLeader(t *testing.T) {
	guard, boss := goblin("guard", 5, 2), goblin("boss", 6, 1)
	boss.InitiativeMod = second
	far, near := fighter("far", 4, 2), fighter("near", 6, 2)
	far.InitiativeMod, near.InitiativeMod = late, later
	// "far" stands first in the turn order and is 2 squares from the boss; "near" is adjacent to it.
	specs := []session.CreatureSpec{guard, boss, far, near}

	s := start(t, 1, specs...)
	plain := tactics.NewUtility(specs, nil)
	if got := choose(t, plain, s); got != (session.Attack{Actor: "guard", Target: "far"}) {
		t.Fatalf("balanced: got %#v, want the first of two equal targets", got)
	}
	protective := tactics.NewUtility(specs, map[session.ID]tactics.Personality{"guard": tactics.Protective("boss")})
	if got := choose(t, protective, s); got != (session.Attack{Actor: "guard", Target: "near"}) {
		t.Fatalf("protective: got %#v, want the enemy near the leader", got)
	}
}

// AC: same state and seed always produce the same choice.
func TestSameStateAndSeedGiveTheSameChoices(t *testing.T) {
	run := func() []session.Event {
		specs := []session.CreatureSpec{goblin("g1", 9, 1), goblin("g2", 9, 3), fighter("f1", 1, 1), fighter("f2", 1, 3)}
		s := start(t, 7, specs...)
		b := tactics.NewUtility(specs, map[session.ID]tactics.Personality{"g1": tactics.Cowardly})
		for i := 0; i < 40 && !s.State().Over; i++ {
			if _, err := tactics.PlayTurn(s, b); err != nil {
				t.Fatal(err)
			}
		}
		return s.Log()
	}
	a, b := run(), run()
	if len(a) == 0 || !reflect.DeepEqual(a, b) {
		t.Fatal("two runs from the same seed diverged")
	}

	g, f := goblin("g", 5, 2), fighter("f", 1, 2)
	s := start(t, 3, g, f)
	br := tactics.NewUtility([]session.CreatureSpec{g, f}, nil)
	if choose(t, br, s) != choose(t, br, s) {
		t.Fatal("same state gave different choices")
	}
}

type fixedBrain struct {
	in session.Intent
	ok bool
}

func (f fixedBrain) Choose(session.State, session.LegalActions) (session.Intent, bool) {
	return f.in, f.ok
}

// AC: the interface allows plugging another brain later with fallback to this one.
func TestAnotherBrainPlugsInWithFallback(t *testing.T) {
	g, f := goblin("g", 4, 2), fighter("f", 3, 2)
	s := start(t, 1, g, f)
	util := tactics.NewUtility([]session.CreatureSpec{g, f}, nil)
	want := choose(t, util, s)

	cases := []struct {
		name string
		top  tactics.Brain
		want session.Intent
	}{
		{"declines", fixedBrain{}, want},
		{"illegal intent", fixedBrain{session.Attack{Actor: "g", Target: "nobody"}, true}, want},
		{"intent for another creature", fixedBrain{session.EndTurn{Actor: "f"}, true}, want},
		{"legal intent wins", fixedBrain{session.Dodge{Actor: "g"}, true}, session.Dodge{Actor: "g"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tactics.Chain(tc.top, util).Choose(s.State(), s.LegalActions())
			if !ok || got != tc.want {
				t.Fatalf("got %#v ok=%v, want %#v", got, ok, tc.want)
			}
		})
	}

	// With no brain able to choose, the turn still ends.
	ev, err := tactics.PlayTurn(s, fixedBrain{})
	if err != nil || s.State().Active != "f" {
		t.Fatalf("turn did not end: %v %v", ev, err)
	}
}
