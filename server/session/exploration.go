package session

import (
	"container/heap"
	"fmt"
	"slices"
	"time"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
)

// DefaultPerceptionRangeFt applies when ExplorationConfig.PerceptionRangeFt is
// zero: how far apart opposing creatures with a clear line between them come
// into each other's perception.
const DefaultPerceptionRangeFt = 60

// ExplorationConfig starts the exploration of a Board.
type ExplorationConfig struct {
	Board     *board.Board
	Creatures []CreatureSpec
	// Seed seeds the dice when Dice is nil.
	Seed uint64
	// Dice overrides the seeded roller (scenario tests).
	Dice dice.Roller
	// PerceptionRangeFt is the distance at which opposing creatures notice
	// each other; zero means DefaultPerceptionRangeFt.
	PerceptionRangeFt int
	// ReactionTimeout is handed to the combat that exploration turns into.
	ReactionTimeout time.Duration
}

// Exploration is the play outside combat. There are no turns: every creature
// moves whenever its owner asks, so Player Characters move simultaneously.
// When opposing creatures come into each other's perception, Stealth is
// checked for both sides and the exploration turns into a combat Session,
// with the creatures that failed to notice an enemy surprised.
//
// Approximations: perception range is one distance for everyone, sight is
// blocked only by walls (light, Darkvision and doors are not applied here),
// and a creature has no Speed limit outside combat. It is not safe for
// concurrent use.
type Exploration struct {
	board   *board.Board
	dice    dice.Roller
	specs   []CreatureSpec // in listing order; Pos is the current square
	rangeFt int
	combat  *Session
	log     []Event
	timeout time.Duration
}

// NewExploration places the creatures. It needs at least two teams, as the
// combat it ends in does.
func NewExploration(cfg ExplorationConfig) (*Exploration, error) {
	if cfg.Board == nil {
		return nil, fmt.Errorf("session: no board")
	}
	e := &Exploration{board: cfg.Board, dice: cfg.Dice, rangeFt: cfg.PerceptionRangeFt, timeout: cfg.ReactionTimeout}
	if e.dice == nil {
		e.dice = dice.Seeded(cfg.Seed)
	}
	if e.rangeFt <= 0 {
		e.rangeFt = DefaultPerceptionRangeFt
	}
	teams := map[Team]bool{}
	for _, spec := range cfg.Creatures {
		switch {
		case spec.ID == "":
			return nil, fmt.Errorf("session: creature without ID")
		case e.index(spec.ID) >= 0:
			return nil, fmt.Errorf("session: duplicate creature %q", spec.ID)
		}
		if cost, ok := cfg.Board.EntryCost(spec.Pos); !ok || cost == 0 {
			return nil, fmt.Errorf("session: %q placed on a wall at %v", spec.ID, spec.Pos)
		}
		if o := e.at(spec.Pos); o >= 0 {
			return nil, fmt.Errorf("session: %q and %q share %v", spec.ID, e.specs[o].ID, spec.Pos)
		}
		e.specs = append(e.specs, spec)
		teams[spec.Team] = true
	}
	if len(teams) < 2 {
		return nil, fmt.Errorf("session: exploration needs at least two teams")
	}
	return e, nil
}

func (e *Exploration) index(id ID) int {
	return slices.IndexFunc(e.specs, func(c CreatureSpec) bool { return c.ID == id })
}

func (e *Exploration) at(p board.Pos) int {
	return slices.IndexFunc(e.specs, func(c CreatureSpec) bool { return c.Pos == p })
}

// Position is where a creature stands.
func (e *Exploration) Position(id ID) (board.Pos, bool) {
	i := e.index(id)
	if i < 0 {
		return board.Pos{}, false
	}
	return e.specs[i].Pos, true
}

// Combat is the combat Session once exploration has ended, nil before.
func (e *Exploration) Combat() *Session { return e.combat }

// Log returns every event since the exploration started.
func (e *Exploration) Log() []Event { return slices.Clone(e.log) }

// Submit resolves an exploration intent; only Move is understood. An illegal
// intent returns a *Rejection and changes nothing. Once combat has begun every
// intent is rejected with NotExploring: submit to Combat() instead.
func (e *Exploration) Submit(in Intent) ([]Event, error) {
	if e.combat != nil {
		return nil, reject(NotExploring, "combat has begun")
	}
	mv, ok := in.(Move)
	if !ok {
		return nil, reject(NoSuchOption, "%T is not an exploration intent", in)
	}
	i := e.index(mv.Actor)
	if i < 0 {
		return nil, fmt.Errorf("session: unknown creature %q", mv.Actor)
	}
	path, ok := e.path(i, mv.To)
	if !ok {
		return nil, reject(Unreachable, "%v is not reachable", mv.To)
	}
	start := e.specs[i].Pos
	costFt := 0
	var out []Event
	for _, p := range path[1:] {
		cost, _ := e.board.EntryCost(p)
		costFt += cost * board.SquareFeet
		e.specs[i].Pos = p
		if pairs := e.contacts(); len(pairs) > 0 {
			out = append(out, Moved{Creature: mv.Actor, From: start, To: p, CostFt: costFt})
			out = append(out, e.startCombat(pairs)...)
			e.log = append(e.log, out...)
			return out, nil
		}
	}
	out = append(out, Moved{Creature: mv.Actor, From: start, To: e.specs[i].Pos, CostFt: costFt})
	e.log = append(e.log, out...)
	return out, nil
}

// contact is an opposing pair that perceives each other, listed by index.
type contact struct{ a, b int }

// contacts lists the opposing pairs within perception range with a clear line
// between them.
func (e *Exploration) contacts() []contact {
	var out []contact
	for a := range e.specs {
		for b := a + 1; b < len(e.specs); b++ {
			x, y := e.specs[a], e.specs[b]
			if x.Team != y.Team && board.Distance(x.Pos, y.Pos)*board.SquareFeet <= e.rangeFt && e.clear(x.Pos, y.Pos) {
				out = append(out, contact{a, b})
			}
		}
	}
	return out
}

// clear reports whether no wall lies between two squares.
func (e *Exploration) clear(a, b board.Pos) bool {
	for _, p := range board.Line(a, b) {
		if e.board.At(p) == board.Wall {
			return false
		}
	}
	return true
}

// startCombat checks Stealth for every creature in contact (SRD "Playing the
// Game", Surprise: a creature that fails to notice a threat is surprised),
// then starts the combat. A creature notices an opponent whose Stealth total is
// below its passive Perception (10 + Perception modifier); it is surprised when
// it notices none of the opponents in contact with it.
func (e *Exploration) startCombat(pairs []contact) []Event {
	var out []Event
	rolled := map[int]int{} // creature index -> Stealth total
	for i := range e.specs {
		if !slices.ContainsFunc(pairs, func(p contact) bool { return p.a == i || p.b == i }) {
			continue
		}
		c := e.specs[i]
		d20 := e.dice.Roll(20)
		total := d20 + c.StealthMod
		rolled[i] = total
		out = append(out, StealthChecked{Creature: c.ID, D20: d20, Modifier: c.StealthMod, Total: total})
	}
	noticed := map[int]bool{}
	for _, p := range pairs {
		if rolled[p.b] < 10+e.specs[p.a].PerceptionMod {
			noticed[p.a] = true
		}
		if rolled[p.a] < 10+e.specs[p.b].PerceptionMod {
			noticed[p.b] = true
		}
	}
	specs := slices.Clone(e.specs)
	for i := range specs {
		if _, inContact := rolled[i]; inContact && !noticed[i] {
			specs[i].Surprised = true
			out = append(out, Surprised{Creature: specs[i].ID})
		}
	}
	ids := make([]ID, len(specs))
	for i, c := range specs {
		ids[i] = c.ID
	}
	out = append(out, CombatStarted{Creatures: ids})
	s, err := New(Config{Board: e.board, Creatures: specs, Dice: e.dice, ReactionTimeout: e.timeout})
	if err != nil {
		// Unreachable: the creatures were validated by NewExploration.
		panic(err)
	}
	e.combat = s
	return append(out, s.Log()...)
}

// path finds the cheapest path for creature i to an empty square. Allies can be
// walked through, opponents and walls cannot.
func (e *Exploration) path(i int, to board.Pos) ([]board.Pos, bool) {
	from := e.specs[i].Pos
	if from == to || !e.board.In(to) || e.at(to) >= 0 {
		return nil, false
	}
	if _, ok := e.board.EntryCost(to); !ok {
		return nil, false
	}
	dist := map[board.Pos]int{from: 0}
	prev := map[board.Pos]board.Pos{}
	q := &stepQueue{{from, 0}}
	for q.Len() > 0 {
		cur := heap.Pop(q).(stepCost)
		if cur.cost > dist[cur.pos] {
			continue
		}
		if cur.pos == to {
			break
		}
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				n := board.Pos{X: cur.pos.X + dx, Y: cur.pos.Y + dy}
				if (dx == 0 && dy == 0) || !e.board.In(n) || !e.board.CanStep(cur.pos, n) {
					continue
				}
				if o := e.at(n); o >= 0 && e.specs[o].Team != e.specs[i].Team {
					continue
				}
				cost, _ := e.board.EntryCost(n)
				if d, seen := dist[n]; !seen || cur.cost+cost < d {
					dist[n] = cur.cost + cost
					prev[n] = cur.pos
					heap.Push(q, stepCost{n, cur.cost + cost})
				}
			}
		}
	}
	if _, ok := dist[to]; !ok {
		return nil, false
	}
	path := []board.Pos{to}
	for p := to; p != from; {
		p = prev[p]
		path = append(path, p)
	}
	slices.Reverse(path)
	return path, true
}

type stepCost struct {
	pos  board.Pos
	cost int
}

type stepQueue []stepCost

func (q stepQueue) Len() int           { return len(q) }
func (q stepQueue) Less(i, j int) bool { return q[i].cost < q[j].cost }
func (q stepQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *stepQueue) Push(x any)        { *q = append(*q, x.(stepCost)) }
func (q *stepQueue) Pop() any {
	old := *q
	x := old[len(old)-1]
	*q = old[:len(old)-1]
	return x
}

// StealthChecked is a Dexterity (Stealth) check made as opposing creatures
// come into perception range. A creature notices it when the total is below
// its own passive Perception.
type StealthChecked struct {
	Creature ID
	D20      int
	Modifier int
	Total    int
}

// Surprised marks a creature that noticed none of the opponents it came into
// contact with: it rolls Initiative with Disadvantage.
type Surprised struct{ Creature ID }

// CombatStarted ends exploration: combat begins with these creatures, and the
// Initiative rolls follow.
type CombatStarted struct{ Creatures []ID }

func (StealthChecked) event() {}
func (Surprised) event()      {}
func (CombatStarted) event()  {}
