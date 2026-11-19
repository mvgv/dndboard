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
	hp           int
	gone         bool // fled or surrendered: no longer in the combat
	reactionUsed bool // regained at the start of the creature's own turn

	dead                 bool
	stable               bool // at 0 Hit Points, no Death Saving Throws
	deathSucc, deathFail int
	exhaustion           int
	conds                map[Condition]ID // conditions the creature itself carries, with their source
}

func (c *creature) down() bool { return c.hp <= 0 }

// out reports whether the creature no longer takes part in the combat: dead or
// gone. A character at 0 Hit Points is still in it, making Death Saving Throws.
func (c *creature) out() bool { return c.dead || c.gone }

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
		c := &creature{CreatureSpec: spec, hp: spec.MaxHP, conds: map[Condition]ID{}}
		for _, k := range spec.Conditions {
			if k == CondExhaustion {
				c.exhaustion++
			} else {
				c.conds[k] = ""
			}
		}
		s.creatures[spec.ID] = c
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
			// Initiative is a Dexterity check: Disadvantage if surprised or
			// Incapacitated (SRD "Rules Glossary", Incapacitated) or Poisoned or
			// Frightened; Advantage if Invisible; Exhaustion reduces the roll.
			d, _ := s.rollD20(c.has(CondInvisible), c.Surprised || c.incapacitated() || c.checkDisadvantage())
			r = result{d, d + c.InitiativeMod - c.d20Penalty()}
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
	if c.dying() {
		*out = append(*out, s.deathSave(c)...)
		*out = s.settle(*out)
	}
	// A creature at 0 Hit Points can do nothing on its turn: pass it unless it
	// just regained Hit Points or the combat is over.
	if c.down() && !s.over {
		*out = append(*out, s.endTurn()...)
	}
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
	case Dash:
		ev, err := s.takeAction(func(c *creature) []Event {
			s.dashedFt += c.speed()
			return []Event{Dashed{Creature: c.ID, ExtraFt: c.speed()}}
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
	case Stand:
		ev, err := s.stand(s.active())
		if err != nil {
			return nil, err
		}
		out = ev
	case Stabilize:
		ev, err := s.stabilizeAttempt(in)
		if err != nil {
			return nil, err
		}
		out = ev
	case NimbleEscape:
		c := s.active()
		switch {
		case c.incapacitated():
			return nil, reject(Incapacitated, "%s is Incapacitated", c.ID)
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
	cost, ok := s.reachable(c)[in.To]
	if !ok {
		return nil, reject(Unreachable, "%v is not reachable with %d ft of movement left", in.To, s.movementLeft(c))
	}
	from := c.Pos
	var out []Event
	// Opportunity Attacks (SRD "Playing the Game", Opportunity Attacks): a
	// hostile creature that still has its Reaction attacks right before the mover
	// leaves its reach, unless the mover Disengaged. Only the start and end
	// squares are compared, so passing through a reach is not modeled.
	if !s.disengage {
		for _, id := range s.order {
			o := s.creatures[id]
			if o.Team == c.Team || o.out() || o.reactionUsed || o.incapacitated() || !inReach(o, from) || inReach(o, in.To) {
				continue
			}
			o.reactionUsed = true
			out = append(out, OpportunityAttackMade{Attacker: o.ID, Target: c.ID})
			out = append(out, s.resolveAttack(o, c)...)
			if c.down() {
				// The mover never leaves its square, and a downed creature's
				// turn is over unless the combat ended with it.
				if !s.over {
					out = append(out, s.endTurn()...)
				}
				return out, nil
			}
		}
	}
	c.Pos = in.To
	s.movedFt += cost
	return append(out, Moved{Creature: c.ID, From: from, To: in.To, CostFt: cost}), nil
}

// takeAction spends the action on one of the standard actions.
func (s *Session) takeAction(do func(*creature) []Event) ([]Event, error) {
	c := s.active()
	if c.incapacitated() {
		return nil, reject(Incapacitated, "%s is Incapacitated and can't take actions", c.ID)
	}
	if s.acted {
		return nil, reject(ActionUsed, "%s already took an action this turn", c.ID)
	}
	s.acted = true
	return do(c), nil
}

// finishIfOver ends the combat when one side is defeated or gone.
func (s *Session) finishIfOver(out []Event) []Event {
	if s.over {
		return out
	}
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
	if c.incapacitated() {
		return nil, reject(Incapacitated, "%s is Incapacitated and can't take actions", c.ID)
	}
	if s.acted {
		return nil, reject(ActionUsed, "%s already took an action this turn", c.ID)
	}
	t, ok := s.creatures[in.Target]
	switch {
	case ok && c.has(CondCharmed) && c.conds[CondCharmed] == t.ID:
		// SRD "Rules Glossary", Charmed: can't attack the charmer.
		return nil, reject(InvalidTarget, "%s is charmed by %s and can't attack them", c.ID, t.ID)
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
	adv, dis := attackMode(c, t)
	d20, mode := s.rollD20(adv, dis)
	mod := a.ToHit - c.d20Penalty()
	total := d20 + mod
	hit := d20 == 20 || (d20 != 1 && total >= t.AC)
	// A Critical Hit is a natural 20, or any hit on a Paralyzed or Unconscious
	// target within 5 feet.
	crit := hit && (d20 == 20 || autoCrit(c, t))
	out := []Event{AttackRolled{
		Attacker: c.ID, Target: t.ID, Weapon: a.Name, D20: d20, Modifier: mod,
		Total: total, AC: t.AC, Hit: hit, Critical: crit, Mode: mode,
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
	return append(out, s.applyDamage(t, DamageDealt{
		Target: t.ID, Rolls: rolls, Bonus: a.DamageBonus, Raw: max(sum, 0), Type: a.DamageType,
	}, crit)...)
}

// movementLeft is the movement the creature has left this turn.
func (s *Session) movementLeft(c *creature) int {
	if c.speed() == 0 {
		return 0
	}
	return max(c.speed()+s.dashedFt-s.movedFt, 0)
}

func (s *Session) victor() (Team, bool) {
	var standing Team
	for _, c := range s.creatures {
		if c.defeated() {
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
	budget := s.movementLeft(c) / board.SquareFeet
	// Prone creatures crawl: each foot costs 1 extra foot (SRD "Rules Glossary",
	// Crawling). A Frightened creature can't willingly move closer to the source
	// of its fear.
	crawl := 1
	if c.has(CondProne) {
		crawl = 2
	}
	var fear *creature
	if src := s.creatures[c.conds[CondFrightened]]; c.has(CondFrightened) && src != nil && !src.out() {
		fear = src
	}
	occupant := map[board.Pos]*creature{}
	for _, o := range s.creatures {
		if o != c && !o.out() {
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
				if fear != nil && board.Distance(next, fear.Pos) < board.Distance(cur.pos, fear.Pos) {
					continue
				}
				nc := cur.cost + step*crawl
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
			HP: c.hp, MaxHP: c.MaxHP, AC: c.AC, Down: c.down(),
			Gone: c.gone, ReactionAvailable: !c.out() && !c.reactionUsed && !c.incapacitated(),
			Dead: c.dead, Stable: c.stable, DeathSaveSuccesses: c.deathSucc, DeathSaveFailures: c.deathFail,
			Conditions: c.conditions(), ExhaustionLevel: c.exhaustion}
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
	la.EndTurn, la.Leave = true, true
	la.MovementLeftFt = s.movementLeft(c)
	// Incapacitated: no action, Bonus Action or Reaction (SRD "Rules Glossary").
	can := !c.incapacitated()
	la.Action, la.Reaction = can && !s.acted, can && !c.reactionUsed
	la.Dash, la.Disengage = la.Action, la.Action
	la.NimbleEscape = can && c.NimbleEscape && !s.bonused
	la.BonusAction = la.NimbleEscape // the only Bonus Action modeled so far
	if c.own(CondProne) && c.speed() > 0 && c.speed()/2 <= la.MovementLeftFt {
		la.Stand = true
	}
	if la.Action {
		for _, id := range s.order {
			if t := s.creatures[id]; t != c && t.dying() && board.Distance(c.Pos, t.Pos)*board.SquareFeet <= 5 {
				la.Stabilize = append(la.Stabilize, id)
			}
		}
	}
	for p, cost := range s.reachable(c) {
		la.Moves = append(la.Moves, MoveOption{To: p, CostFt: cost})
	}
	slices.SortFunc(la.Moves, func(a, b MoveOption) int {
		return cmp.Or(cmp.Compare(a.CostFt, b.CostFt), cmp.Compare(a.To.Y, b.To.Y), cmp.Compare(a.To.X, b.To.X))
	})
	if la.Action {
		for _, id := range s.order {
			t := s.creatures[id]
			if t.Team != c.Team && !t.out() && inReach(c, t.Pos) && !(c.has(CondCharmed) && c.conds[CondCharmed] == id) {
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
