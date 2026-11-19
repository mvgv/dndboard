// Package session is the game-session seam: submit an intent, receive events,
// visible state and legal actions. The rules engine behind it is the sole
// mechanical authority (ADR 0001).
package session

import (
	"cmp"
	"container/heap"
	"fmt"
	"slices"
	"time"

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
	// ReactionTimeout is how long a prompted owner has to answer before the
	// creature does not react; configured per Campaign. Zero means
	// DefaultReactionTimeout.
	ReactionTimeout time.Duration
}

// DefaultReactionTimeout applies when Config.ReactionTimeout is zero.
const DefaultReactionTimeout = 30 * time.Second

type creature struct {
	CreatureSpec
	hp           int
	gone         bool // fled or surrendered: no longer in the combat
	reactionUsed bool // regained at the start of the creature's own turn
}

func (c *creature) down() bool { return c.hp <= 0 }

// out reports whether the creature no longer takes part in the combat.
func (c *creature) out() bool { return c.down() || c.gone }

// Session is one combat. It is not safe for concurrent use.
type Session struct {
	board     *board.Board
	dice      dice.Roller
	creatures map[ID]*creature
	order     []ID
	turn      int // index into order
	round     int
	movedFt   int
	dashedFt  int  // extra movement from the Dash action this turn
	acted     bool // the action is spent
	bonused   bool // the Bonus Action is spent
	disengage bool // movement provokes no Opportunity Attacks this turn
	over      bool
	winner    Team
	log       []Event
	timeout   time.Duration
	pending   *pendingMove // a move paused on a reaction prompt
}

// pendingMove is a move in progress along its path. It pauses when a Player
// Character's owner is prompted and resumes with Answer or Advance.
type pendingMove struct {
	path     []board.Pos // path[0] is the start square
	i        int         // the step path[i] -> path[i+1] is being processed
	checked  bool        // cand holds the reactors for the current step
	cand     []ID        // creatures still to decide on the current step
	startPos board.Pos
	costFt   int
	asked    ID            // the creature being prompted
	waited   time.Duration // time spent on the current prompt
}

// New places the creatures, rolls Initiative and starts the first turn.
func New(cfg Config) (*Session, error) {
	if cfg.Board == nil {
		return nil, fmt.Errorf("session: no board")
	}
	s := &Session{board: cfg.Board, dice: cfg.Dice, creatures: map[ID]*creature{}, timeout: cfg.ReactionTimeout}
	if s.timeout <= 0 {
		s.timeout = DefaultReactionTimeout
	}
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
// lowest (SRD "Playing the Game", Initiative). A group of identical creatures
// makes a single roll; a surprised combatant has Disadvantage. The order then
// stays the same every round. Ties are decided by the GM (and by the players
// among characters), so the session leaves that choice to the caller: ties keep
// the order in which creatures are listed in Config.
func (s *Session) rollInitiative() {
	type result struct{ d20, total int }
	groups := map[string]result{}
	total := map[ID]int{}
	for _, id := range s.order {
		c := s.creatures[id]
		r, shared := groups[c.Group]
		if c.Group == "" || !shared {
			d := s.dice.Roll(20)
			if c.Surprised {
				d = min(d, s.dice.Roll(20))
			}
			r = result{d, d + c.InitiativeMod}
			if c.Group != "" {
				groups[c.Group] = r
			}
		}
		total[id] = r.total
		s.log = append(s.log, InitiativeRolled{Creature: id, D20: r.d20, Total: r.total})
	}
	slices.SortStableFunc(s.order, func(a, b ID) int { return cmp.Compare(total[b], total[a]) })
}

func (s *Session) active() *creature { return s.creatures[s.order[s.turn]] }

func (s *Session) startTurn(out *[]Event) {
	c := s.active()
	s.movedFt, s.dashedFt = 0, 0
	s.acted, s.bonused, s.disengage = false, false, false
	c.reactionUsed = false
	*out = append(*out, TurnStarted{Creature: c.ID, Round: s.round})
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
	if s.pending != nil {
		return nil, reject(AwaitingReaction, "waiting for %s to answer a reaction prompt", s.pending.asked)
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
	case Dash:
		ev, err := s.takeAction(func(c *creature) []Event {
			s.dashedFt += c.SpeedFt
			return []Event{Dashed{Creature: c.ID, ExtraFt: c.SpeedFt}}
		})
		if err != nil {
			return nil, err
		}
		out = ev
	case Disengage:
		ev, err := s.takeAction(func(c *creature) []Event {
			s.disengage = true
			return []Event{Disengaged{Creature: c.ID}}
		})
		if err != nil {
			return nil, err
		}
		out = ev
	case NimbleEscape:
		c := s.active()
		switch {
		case !c.NimbleEscape:
			return nil, reject(NoSuchOption, "%s has no Nimble Escape", c.ID)
		case s.bonused:
			return nil, reject(BonusActionUsed, "%s already took a Bonus Action this turn", c.ID)
		}
		s.bonused, s.disengage = true, true
		out = []Event{Disengaged{Creature: c.ID, Bonus: true}}
	case Leave:
		out = s.leave()
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
	if _, ok := s.reachable(c)[in.To]; !ok {
		return nil, reject(Unreachable, "%v is not reachable with %d ft of movement left", in.To, c.SpeedFt+s.dashedFt-s.movedFt)
	}
	m := &pendingMove{path: s.pathTo(c, in.To), startPos: c.Pos}
	return s.runMove(m), nil
}

// reactors lists the hostile creatures that may make an Opportunity Attack as
// the mover steps from a to b: still holding their Reaction, with a in reach
// and b out of it.
func (s *Session) reactors(c *creature, a, b board.Pos) []ID {
	var ids []ID
	for _, id := range s.order {
		o := s.creatures[id]
		if o.Team == c.Team || o.out() || o.reactionUsed || !inReach(o, a) || inReach(o, b) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// runMove walks the mover along its path one square at a time. Opportunity
// Attacks (SRD "Playing the Game", Opportunity Attacks): a hostile creature
// that still has its Reaction may attack right before the mover leaves its
// reach, unless the mover Disengaged; leaving and re-entering reach along the
// path provokes again. A Player Character's owner may be prompted, which pauses
// the move (s.pending) until Answer or Advance resumes it. Submit logs the
// events it returns; Answer and Advance log their own.
func (s *Session) runMove(m *pendingMove) []Event {
	c := s.active()
	var out []Event
	for m.i < len(m.path)-1 {
		a, b := m.path[m.i], m.path[m.i+1]
		if !m.checked {
			m.checked = true
			m.cand = nil
			if !s.disengage {
				m.cand = s.reactors(c, a, b)
			}
		}
		for len(m.cand) > 0 {
			o := s.creatures[m.cand[0]]
			if o.out() || o.reactionUsed {
				m.cand = m.cand[1:]
				continue
			}
			switch o.Reactions {
			case ReactNever:
				m.cand = m.cand[1:]
			case ReactAsk:
				m.asked, m.waited = o.ID, 0
				s.pending = m
				return append(out, ReactionPrompted{Creature: o.ID, Target: c.ID, Timeout: s.timeout})
			default:
				m.cand = m.cand[1:]
				out = append(out, s.opportunityAttack(o, c)...)
				if c.down() {
					return append(out, s.moverDowned(m)...)
				}
			}
		}
		cost, _ := s.board.EntryCost(b)
		c.Pos = b
		m.costFt += cost * board.SquareFeet
		s.movedFt += cost * board.SquareFeet
		m.i++
		m.checked = false
	}
	return append(out, Moved{Creature: c.ID, From: m.startPos, To: c.Pos, CostFt: m.costFt})
}

func (s *Session) opportunityAttack(o, mover *creature) []Event {
	o.reactionUsed = true
	return append([]Event{OpportunityAttackMade{Attacker: o.ID, Target: mover.ID}}, s.resolveAttack(o, mover)...)
}

// moverDowned: the mover stops where it stands, and a downed creature's turn
// is over unless the combat ended with it.
func (s *Session) moverDowned(m *pendingMove) []Event {
	var out []Event
	if c := s.active(); c.Pos != m.startPos {
		out = append(out, Moved{Creature: c.ID, From: m.startPos, To: c.Pos, CostFt: m.costFt})
	}
	if !s.over {
		out = append(out, s.endTurn()...)
	}
	return out
}

// Answer is the owner's reply to the pending reaction prompt.
func (s *Session) Answer(who ID, react bool) ([]Event, error) {
	m := s.pending
	if m == nil {
		return nil, reject(NoPendingReaction, "no reaction prompt is pending")
	}
	if who != m.asked {
		return nil, reject(NoPendingReaction, "the prompt is for %s, not %s", m.asked, who)
	}
	return s.resolvePrompt(m, react, false), nil
}

// Advance tells the session that d of waiting time has passed. When a prompt
// has been pending for the whole timeout, the creature does not react and the
// move resumes. Without a pending prompt it does nothing.
func (s *Session) Advance(d time.Duration) []Event {
	m := s.pending
	if m == nil {
		return nil
	}
	m.waited += d
	if m.waited < s.timeout {
		return nil
	}
	return s.resolvePrompt(m, false, true)
}

func (s *Session) resolvePrompt(m *pendingMove, react, timedOut bool) []Event {
	c, o := s.active(), s.creatures[m.asked]
	s.pending = nil
	m.cand = m.cand[1:]
	var out []Event
	switch {
	case react:
		out = s.opportunityAttack(o, c)
		if c.down() {
			out = append(out, s.moverDowned(m)...)
			s.log = append(s.log, out...)
			return out
		}
	case timedOut:
		out = []Event{ReactionTimedOut{Creature: o.ID}}
	default:
		out = []Event{ReactionDeclined{Creature: o.ID}}
	}
	out = append(out, s.runMove(m)...)
	s.log = append(s.log, out...)
	return out
}

// takeAction spends the action on one of the standard actions.
func (s *Session) takeAction(do func(*creature) []Event) ([]Event, error) {
	c := s.active()
	if s.acted {
		return nil, reject(ActionUsed, "%s already took an action this turn", c.ID)
	}
	s.acted = true
	return do(c), nil
}

// finishIfOver ends the combat when one side is defeated or gone.
func (s *Session) finishIfOver(out []Event) []Event {
	if w, done := s.victor(); done {
		s.over, s.winner = true, w
		out = append(out, CombatEnded{Winner: w})
	}
	return out
}

// leave removes the active creature from the combat: it fled or surrendered
// (SRD "Playing the Game", Ending Combat).
func (s *Session) leave() []Event {
	c := s.active()
	c.gone = true
	out := s.finishIfOver([]Event{CreatureLeft{Creature: c.ID}})
	if s.over {
		return out
	}
	return append(out, s.endTurn()...)
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
	case t.out():
		return nil, reject(InvalidTarget, "%s is down or gone", t.ID)
	case !inReach(c, t.Pos):
		return nil, reject(OutOfReach, "%s is %d ft away, beyond %s's %d ft reach",
			t.ID, board.Distance(c.Pos, t.Pos)*board.SquareFeet, c.Attack.Name, c.Attack.ReachFt)
	}
	s.acted = true
	return s.resolveAttack(c, t), nil
}

// resolveAttack makes one attack of c against t and applies the result.
func (s *Session) resolveAttack(c, t *creature) []Event {
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
		return out
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
		out = s.finishIfOver(out)
	}
	return out
}

func (s *Session) victor() (Team, bool) {
	var standing Team
	for _, c := range s.creatures {
		if c.out() {
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
		if !s.active().out() {
			break
		}
	}
	s.startTurn(&out)
	return out
}

// inReach reports whether a creature standing at p is within c's melee reach.
func inReach(c *creature, p board.Pos) bool {
	return board.Distance(c.Pos, p)*board.SquareFeet <= c.Attack.ReachFt
}

// reachable maps each square the creature can end its move in to its cost in
// feet, given movement already spent this turn. Each step to an adjacent square
// (orthogonal or diagonal) costs 1 square, 2 for Difficult Terrain; diagonals
// can't cross a wall corner (SRD "Playing the Game", Grid). An ally's space can
// be passed through at normal cost but not ended in; a hostile creature's space
// can't be entered ("Playing the Game", Moving around Other Creatures). Passing
// hostile creatures two sizes apart or Incapacitated is not modeled yet.
func (s *Session) reachable(c *creature) map[board.Pos]int {
	cost, _ := s.search(c)
	return cost
}

// pathTo returns the cheapest path from the creature's square to a reachable
// square, both included. Equally cheap paths are not distinguished: the first
// one found is used.
func (s *Session) pathTo(c *creature, to board.Pos) []board.Pos {
	_, prev := s.search(c)
	path := []board.Pos{to}
	for p := to; p != c.Pos; {
		p = prev[p]
		path = append(path, p)
	}
	slices.Reverse(path)
	return path
}

func (s *Session) search(c *creature) (map[board.Pos]int, map[board.Pos]board.Pos) {
	budget := (c.SpeedFt + s.dashedFt - s.movedFt) / board.SquareFeet
	occupant := map[board.Pos]*creature{}
	for _, o := range s.creatures {
		if o != c && !o.out() {
			occupant[o.Pos] = o
		}
	}
	best := map[board.Pos]int{c.Pos: 0}
	prev := map[board.Pos]board.Pos{}
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
				prev[next] = cur.pos
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
	return out, prev
}

// State returns the visible state.
func (s *Session) State() State {
	st := State{Round: s.round, Active: s.active().ID, Order: slices.Clone(s.order),
		Creatures: map[ID]CreatureState{}, Over: s.over, Winner: s.winner}
	for id, c := range s.creatures {
		st.Creatures[id] = CreatureState{Name: c.Name, Team: c.Team, Pos: c.Pos,
			HP: c.hp, MaxHP: c.MaxHP, AC: c.AC, Down: c.down(),
			Gone: c.gone, ReactionAvailable: !c.out() && !c.reactionUsed}
	}
	if m := s.pending; m != nil {
		st.Pending = &PendingReaction{Creature: m.asked, Target: s.active().ID, Remaining: s.timeout - m.waited}
	}
	return st
}

// LegalActions returns the active creature's legal choices, sorted for stable output.
func (s *Session) LegalActions() LegalActions {
	c := s.active()
	la := LegalActions{Creature: c.ID}
	if s.over || s.pending != nil {
		return la
	}
	la.EndTurn, la.Leave = true, true
	la.MovementLeftFt = c.SpeedFt + s.dashedFt - s.movedFt
	la.Action, la.Reaction = !s.acted, !c.reactionUsed
	la.Dash, la.Disengage = !s.acted, !s.acted
	la.NimbleEscape = c.NimbleEscape && !s.bonused
	la.BonusAction = la.NimbleEscape // the only Bonus Action modeled so far
	for p, cost := range s.reachable(c) {
		la.Moves = append(la.Moves, MoveOption{To: p, CostFt: cost})
	}
	slices.SortFunc(la.Moves, func(a, b MoveOption) int {
		return cmp.Or(cmp.Compare(a.CostFt, b.CostFt), cmp.Compare(a.To.Y, b.To.Y), cmp.Compare(a.To.X, b.To.X))
	})
	if !s.acted {
		for _, id := range s.order {
			t := s.creatures[id]
			if t.Team != c.Team && !t.out() && inReach(c, t.Pos) {
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
