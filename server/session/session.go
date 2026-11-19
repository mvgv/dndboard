// Package session is the game-session seam: submit an intent, receive events,
// visible state and legal actions. The rules engine behind it is the sole
// mechanical authority (ADR 0001).
package session

import (
	"cmp"
	"container/heap"
	"fmt"
	"slices"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
)

// Config starts a combat on a hand-built Board.
type Config struct {
	Board     *board.Board
	Creatures []CreatureSpec
	// Seed seeds the dice when Dice is nil.
	Seed uint64
	// Dice overrides the seeded roller (scenario tests).
	Dice dice.Roller
}

type creature struct {
	CreatureSpec
	hp int
}

func (c *creature) down() bool { return c.hp <= 0 }

// Session is one combat. It is not safe for concurrent use.
type Session struct {
	board     *board.Board
	dice      dice.Roller
	creatures map[ID]*creature
	order     []ID
	turn      int // index into order
	round     int
	movedFt   int
	acted     bool
	over      bool
	winner    Team
	log       []Event
}

// New places the creatures, rolls Initiative and starts the first turn.
func New(cfg Config) (*Session, error) {
	if cfg.Board == nil {
		return nil, fmt.Errorf("session: no board")
	}
	s := &Session{board: cfg.Board, dice: cfg.Dice, creatures: map[ID]*creature{}}
	if s.dice == nil {
		s.dice = dice.Seeded(cfg.Seed)
	}
	teams := map[Team]bool{}
	for _, spec := range cfg.Creatures {
		if spec.ID == "" {
			return nil, fmt.Errorf("session: creature without ID")
		}
		if _, dup := s.creatures[spec.ID]; dup {
			return nil, fmt.Errorf("session: duplicate creature %q", spec.ID)
		}
		if cost, ok := cfg.Board.EntryCost(spec.Pos); !ok || cost == 0 {
			return nil, fmt.Errorf("session: %q placed on a wall at %v", spec.ID, spec.Pos)
		}
		for _, o := range s.creatures {
			if o.Pos == spec.Pos {
				return nil, fmt.Errorf("session: %q and %q share %v", spec.ID, o.ID, spec.Pos)
			}
		}
		s.creatures[spec.ID] = &creature{CreatureSpec: spec, hp: spec.MaxHP}
		s.order = append(s.order, spec.ID)
		teams[spec.Team] = true
	}
	if len(teams) < 2 {
		return nil, fmt.Errorf("session: combat needs at least two teams")
	}
	s.rollInitiative()
	s.round = 1
	s.startTurn(&s.log)
	return s, nil
}

// rollInitiative: every participant makes a Dexterity check, ranked highest to
// lowest (SRD "Playing the Game", Initiative). Ties are decided by the GM; we
// use the higher modifier, then the order creatures were placed.
func (s *Session) rollInitiative() {
	total := map[ID]int{}
	for _, id := range s.order {
		c := s.creatures[id]
		d := s.dice.Roll(20)
		total[id] = d + c.InitiativeMod
		s.log = append(s.log, InitiativeRolled{Creature: id, D20: d, Total: total[id]})
	}
	slices.SortStableFunc(s.order, func(a, b ID) int {
		if c := cmp.Compare(total[b], total[a]); c != 0 {
			return c
		}
		return cmp.Compare(s.creatures[b].InitiativeMod, s.creatures[a].InitiativeMod)
	})
}

func (s *Session) active() *creature { return s.creatures[s.order[s.turn]] }

func (s *Session) startTurn(out *[]Event) {
	s.movedFt, s.acted = 0, false
	*out = append(*out, TurnStarted{Creature: s.active().ID, Round: s.round})
}

// Log returns every event since the session started.
func (s *Session) Log() []Event { return slices.Clone(s.log) }

// Submit validates and resolves an intent. An illegal intent returns a
// *Rejection and changes nothing.
func (s *Session) Submit(in Intent) ([]Event, error) {
	if s.over {
		return nil, reject(CombatOver, "combat is over")
	}
	if a := in.actor(); a != s.active().ID {
		return nil, reject(NotYourTurn, "it is %s's turn, not %s's", s.active().ID, a)
	}
	var out []Event
	switch in := in.(type) {
	case Move:
		ev, err := s.move(in)
		if err != nil {
			return nil, err
		}
		out = ev
	case Attack:
		ev, err := s.attack(in)
		if err != nil {
			return nil, err
		}
		out = ev
	case EndTurn:
		out = s.endTurn()
	default:
		return nil, fmt.Errorf("session: unknown intent %T", in)
	}
	s.log = append(s.log, out...)
	return out, nil
}

func (s *Session) move(in Move) ([]Event, error) {
	c := s.active()
	cost, ok := s.reachable(c)[in.To]
	if !ok {
		return nil, reject(Unreachable, "%v is not reachable with %d ft of movement left", in.To, c.SpeedFt-s.movedFt)
	}
	from := c.Pos
	c.Pos = in.To
	s.movedFt += cost
	return []Event{Moved{Creature: c.ID, From: from, To: in.To, CostFt: cost}}, nil
}

func (s *Session) attack(in Attack) ([]Event, error) {
	c := s.active()
	if s.acted {
		return nil, reject(ActionUsed, "%s already took an action this turn", c.ID)
	}
	t, ok := s.creatures[in.Target]
	switch {
	case !ok:
		return nil, reject(InvalidTarget, "no creature %q", in.Target)
	case t.Team == c.Team:
		return nil, reject(InvalidTarget, "%s is not hostile", t.ID)
	case t.down():
		return nil, reject(InvalidTarget, "%s is down", t.ID)
	case !inReach(c, t):
		return nil, reject(OutOfReach, "%s is %d ft away, beyond %s's %d ft reach",
			t.ID, board.Distance(c.Pos, t.Pos)*board.SquareFeet, c.Attack.Name, c.Attack.ReachFt)
	}
	s.acted = true

	// Attack roll: hits if the total equals or exceeds AC; a natural 20 hits
	// as a Critical Hit, a natural 1 misses (SRD "Playing the Game", Attack Rolls).
	a := c.Attack
	d20 := s.dice.Roll(20)
	total := d20 + a.ToHit
	crit := d20 == 20
	hit := crit || (d20 != 1 && total >= t.AC)
	out := []Event{AttackRolled{
		Attacker: c.ID, Target: t.ID, Weapon: a.Name, D20: d20, Modifier: a.ToHit,
		Total: total, AC: t.AC, Hit: hit, Critical: crit,
	}}
	if !hit {
		return out, nil
	}

	// Damage: roll the damage dice (twice as many on a Critical Hit) and add
	// the modifier; never below 0 (SRD "Playing the Game", Damage Rolls, Critical Hits).
	n := a.DiceCount
	if crit {
		n *= 2
	}
	rolls := make([]int, n)
	sum := a.DamageBonus
	for i := range rolls {
		rolls[i] = s.dice.Roll(a.DiceSides)
		sum += rolls[i]
	}
	amount := max(sum, 0)
	t.hp = max(t.hp-amount, 0)
	out = append(out, DamageDealt{Target: t.ID, Rolls: rolls, Bonus: a.DamageBonus, Amount: amount, Type: a.DamageType, HPAfter: t.hp})
	if t.down() {
		out = append(out, CreatureDowned{Creature: t.ID})
		if w, done := s.victor(); done {
			s.over, s.winner = true, w
			out = append(out, CombatEnded{Winner: w})
		}
	}
	return out, nil
}

func (s *Session) victor() (Team, bool) {
	var standing Team
	for _, c := range s.creatures {
		if c.down() {
			continue
		}
		if standing != "" && standing != c.Team {
			return "", false
		}
		standing = c.Team
	}
	return standing, true
}

func (s *Session) endTurn() []Event {
	out := []Event{TurnEnded{Creature: s.active().ID}}
	for {
		s.turn++
		if s.turn == len(s.order) {
			s.turn = 0
			s.round++
		}
		if !s.active().down() {
			break
		}
	}
	s.startTurn(&out)
	return out
}

func inReach(c, t *creature) bool {
	return board.Distance(c.Pos, t.Pos)*board.SquareFeet <= c.Attack.ReachFt
}

// reachable maps each square the creature can end its move in to its cost in
// feet, given movement already spent this turn. Each step to an adjacent square
// (orthogonal or diagonal) costs 1 square, 2 for Difficult Terrain; diagonals
// can't cross a wall corner (SRD "Playing the Game", Grid). An ally's space can
// be passed through at normal cost but not ended in; a hostile creature's space
// can't be entered ("Playing the Game", Moving around Other Creatures). Passing
// hostile creatures two sizes apart or Incapacitated is not modeled yet.
func (s *Session) reachable(c *creature) map[board.Pos]int {
	budget := (c.SpeedFt - s.movedFt) / board.SquareFeet
	occupant := map[board.Pos]*creature{}
	for _, o := range s.creatures {
		if o != c && !o.down() {
			occupant[o.Pos] = o
		}
	}
	best := map[board.Pos]int{c.Pos: 0}
	q := &posQueue{{c.Pos, 0}}
	for q.Len() > 0 {
		cur := heap.Pop(q).(node)
		if cur.cost > best[cur.pos] {
			continue
		}
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				next := board.Pos{X: cur.pos.X + dx, Y: cur.pos.Y + dy}
				if next == cur.pos || !s.board.CanStep(cur.pos, next) {
					continue
				}
				step, _ := s.board.EntryCost(next)
				if o := occupant[next]; o != nil && o.Team != c.Team {
					continue
				}
				nc := cur.cost + step
				if nc > budget {
					continue
				}
				if old, seen := best[next]; seen && old <= nc {
					continue
				}
				best[next] = nc
				heap.Push(q, node{next, nc})
			}
		}
	}
	out := map[board.Pos]int{}
	for p, sq := range best {
		if p != c.Pos && occupant[p] == nil {
			out[p] = sq * board.SquareFeet
		}
	}
	return out
}

// State returns the visible state.
func (s *Session) State() State {
	st := State{Round: s.round, Active: s.active().ID, Order: slices.Clone(s.order),
		Creatures: map[ID]CreatureState{}, Over: s.over, Winner: s.winner}
	for id, c := range s.creatures {
		st.Creatures[id] = CreatureState{Name: c.Name, Team: c.Team, Pos: c.Pos,
			HP: c.hp, MaxHP: c.MaxHP, AC: c.AC, Down: c.down()}
	}
	return st
}

// LegalActions returns the active creature's legal choices, sorted for stable output.
func (s *Session) LegalActions() LegalActions {
	c := s.active()
	la := LegalActions{Creature: c.ID}
	if s.over {
		return la
	}
	la.EndTurn = true
	for p, cost := range s.reachable(c) {
		la.Moves = append(la.Moves, MoveOption{To: p, CostFt: cost})
	}
	slices.SortFunc(la.Moves, func(a, b MoveOption) int {
		return cmp.Or(cmp.Compare(a.CostFt, b.CostFt), cmp.Compare(a.To.Y, b.To.Y), cmp.Compare(a.To.X, b.To.X))
	})
	if !s.acted {
		for _, id := range s.order {
			t := s.creatures[id]
			if t.Team != c.Team && !t.down() && inReach(c, t) {
				la.Attacks = append(la.Attacks, AttackOption{Target: id, Weapon: c.Attack.Name})
			}
		}
	}
	return la
}

type node struct {
	pos  board.Pos
	cost int
}

type posQueue []node

func (q posQueue) Len() int           { return len(q) }
func (q posQueue) Less(i, j int) bool { return q[i].cost < q[j].cost }
func (q posQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *posQueue) Push(x any)        { *q = append(*q, x.(node)) }
func (q *posQueue) Pop() any {
	old := *q
	n := old[len(old)-1]
	*q = old[:len(old)-1]
	return n
}
