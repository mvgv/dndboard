// Package session is the game-session seam: submit an intent, receive events,
// visible state and legal actions. The rules engine behind it is the sole
// mechanical authority (ADR 0001).
package session

import (
	"cmp"
	"container/heap"
	"fmt"
	"slices"

	"github.com/mvgv/dndboard/content"
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
	dodging      bool // until the start of its own turn
	features     []*featureState
}

func (c *creature) down() bool { return c.hp <= 0 }

// out reports whether the creature no longer takes part in the combat.
func (c *creature) out() bool { return c.down() || c.gone }

func (c *creature) feature(id string) *featureState {
	for _, f := range c.features {
		if f.ID == id {
			return f
		}
	}
	return nil
}

// help is a pending Help action: the next attack roll by an ally of helper
// against target has Advantage.
type help struct{ helper, target *creature }

// Session is one combat. It is not safe for concurrent use.
type Session struct {
	board       *board.Board
	dice        dice.Roller
	creatures   map[ID]*creature
	order       []ID
	turn        int // index into order
	turnSeq     int // counts every turn started, for once-per-turn limits
	round       int
	movedFt     int
	dashedFt    int  // extra movement from the Dash action this turn
	acted       bool // the action is spent
	bonused     bool // the Bonus Action is spent
	disengage   bool // movement provokes no Opportunity Attacks this turn
	lightAttack bool // took the Attack action with a Light weapon this turn
	helps       []help
	over        bool
	winner      Team
	log         []Event
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
		c := &creature{CreatureSpec: spec, hp: spec.MaxHP}
		for _, f := range spec.Features {
			fs, err := newFeature(f)
			if err != nil {
				return nil, fmt.Errorf("session: %q: %w", spec.ID, err)
			}
			c.features = append(c.features, fs)
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
	s.turnSeq++
	s.movedFt, s.dashedFt = 0, 0
	s.acted, s.bonused, s.disengage, s.lightAttack = false, false, false, false
	c.reactionUsed, c.dodging = false, false
	// Help's benefit "expires at the start of your next turn".
	s.helps = slices.DeleteFunc(s.helps, func(h help) bool { return h.helper == c })
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
	var (
		out []Event
		err error
	)
	switch in := in.(type) {
	case Move:
		out, err = s.move(in)
	case Attack:
		out, err = s.attack(in)
	case OffHandAttack:
		out, err = s.offHandAttack(in)
	case Dash:
		out, err = s.takeAction(func(c *creature) []Event {
			s.dashedFt += c.SpeedFt
			return []Event{Dashed{Creature: c.ID, ExtraFt: c.SpeedFt}}
		})
	case Disengage:
		out, err = s.takeAction(func(c *creature) []Event {
			s.disengage = true
			return []Event{Disengaged{Creature: c.ID}}
		})
	case Dodge:
		out, err = s.takeAction(func(c *creature) []Event {
			c.dodging = true
			return []Event{Dodged{Creature: c.ID}}
		})
	case Help:
		out, err = s.help(in)
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
	case UseFeature:
		out, err = s.useFeature(in)
	case Leave:
		out = s.leave()
	case EndTurn:
		out = s.endTurn()
	default:
		return nil, fmt.Errorf("session: unknown intent %T", in)
	}
	if err != nil {
		return nil, err
	}
	s.log = append(s.log, out...)
	return out, nil
}

func (s *Session) move(in Move) ([]Event, error) {
	c := s.active()
	cost, ok := s.reachable(c)[in.To]
	if !ok {
		return nil, reject(Unreachable, "%v is not reachable with %d ft of movement left", in.To, c.SpeedFt+s.dashedFt-s.movedFt)
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
			if o.Team == c.Team || o.out() || o.reactionUsed || !inReach(o.Attack, o.Pos, from) || inReach(o.Attack, o.Pos, in.To) {
				continue
			}
			o.reactionUsed = true
			out = append(out, OpportunityAttackMade{Attacker: o.ID, Target: c.ID})
			out = append(out, s.resolveAttack(o, c, o.Attack)...)
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

// hostileTarget checks that id names a hostile creature still in the combat
// within reachFt of c.
func (s *Session) hostileTarget(c *creature, id ID, reachFt int, what string) (*creature, error) {
	t, ok := s.creatures[id]
	switch {
	case !ok:
		return nil, reject(InvalidTarget, "no creature %q", id)
	case t.Team == c.Team:
		return nil, reject(InvalidTarget, "%s is not hostile", t.ID)
	case t.out():
		return nil, reject(InvalidTarget, "%s is down or gone", t.ID)
	case board.Distance(c.Pos, t.Pos)*board.SquareFeet > reachFt:
		return nil, reject(OutOfReach, "%s is %d ft away, beyond %s's %d ft reach",
			t.ID, board.Distance(c.Pos, t.Pos)*board.SquareFeet, what, reachFt)
	}
	return t, nil
}

func (s *Session) attack(in Attack) ([]Event, error) {
	c := s.active()
	if s.acted {
		return nil, reject(ActionUsed, "%s already took an action this turn", c.ID)
	}
	t, err := s.hostileTarget(c, in.Target, c.Attack.ReachFt, c.Attack.Name)
	if err != nil {
		return nil, err
	}
	s.acted = true
	s.lightAttack = slices.Contains(c.Attack.Properties, "light")
	return s.resolveAttack(c, t, c.Attack), nil
}

// offHandAttack: after the Attack action with a Light weapon, one extra attack
// with a different Light weapon as a Bonus Action (SRD "Equipment", Light).
func (s *Session) offHandAttack(in OffHandAttack) ([]Event, error) {
	c := s.active()
	switch {
	case c.OffHand == nil:
		return nil, reject(NoSuchOption, "%s has no off-hand weapon", c.ID)
	case !s.lightAttack:
		return nil, reject(NoSuchOption, "%s hasn't taken the Attack action with a Light weapon this turn", c.ID)
	case s.bonused:
		return nil, reject(BonusActionUsed, "%s already took a Bonus Action this turn", c.ID)
	}
	t, err := s.hostileTarget(c, in.Target, c.OffHand.ReachFt, c.OffHand.Name)
	if err != nil {
		return nil, err
	}
	s.bonused = true
	return s.resolveAttack(c, t, *c.OffHand), nil
}

// help: "Assist an Attack Roll. You momentarily distract an enemy within 5
// feet of you, giving Advantage to the next attack roll by one of your allies
// against that enemy." (SRD "Rules Glossary", Help).
func (s *Session) help(in Help) ([]Event, error) {
	c := s.active()
	if s.acted {
		return nil, reject(ActionUsed, "%s already took an action this turn", c.ID)
	}
	t, err := s.hostileTarget(c, in.Target, board.SquareFeet, "Help")
	if err != nil {
		return nil, err
	}
	s.acted = true
	s.helps = append(s.helps, help{helper: c, target: t})
	return []Event{Helped{Creature: c.ID, Target: t.ID}}, nil
}

// useFeature activates a feature, or extends an active one with the Bonus
// Action when its activation allows it.
func (s *Session) useFeature(in UseFeature) ([]Event, error) {
	c := s.active()
	f := c.feature(in.Feature)
	switch {
	case f == nil || f.Activation == nil:
		return nil, reject(NoSuchOption, "%s has no feature %q to use", c.ID, in.Feature)
	case f.active && !f.extendableBy(content.BonusActionExtension):
		return nil, reject(NoSuchOption, "%s's %s is already active", c.ID, f.Name)
	case !f.active && f.usesLeft == 0:
		return nil, reject(NoUsesLeft, "%s has no uses of %s left", c.ID, f.Name)
	case s.bonused: // content.BonusAction is the only activation cost
		return nil, reject(BonusActionUsed, "%s already took a Bonus Action this turn", c.ID)
	}
	s.bonused = true
	if f.active {
		f.extended = true
		return []Event{FeatureExtended{Creature: c.ID, Feature: f.ID}}, nil
	}
	f.usesLeft--
	f.active, f.extended, f.since = true, true, s.round
	return []Event{FeatureActivated{Creature: c.ID, Feature: f.ID, UsesLeft: f.usesLeft}}, nil
}

// expireFeatures ends c's active features whose duration runs out at the end
// of its turn. An activation lasts until the end of the holder's next turn
// unless extended during it, and never beyond MaxRounds: entered in round r,
// it can last through the holder's turn in round r+MaxRounds-1.
func (s *Session) expireFeatures(c *creature) []Event {
	var out []Event
	for _, f := range c.features {
		if !f.active {
			continue
		}
		switch a := f.Activation; {
		case a.MaxRounds > 0 && s.round >= f.since+a.MaxRounds-1:
			out = append(out, s.endFeature(c, f, EndedMaxDuration))
		case !f.extended:
			out = append(out, s.endFeature(c, f, EndedExpired))
		}
		f.extended = false
	}
	return out
}

func (s *Session) endFeature(c *creature, f *featureState, reason string) Event {
	f.active, f.extended = false, false
	return FeatureEnded{Creature: c.ID, Feature: f.ID, Reason: reason}
}

// rollMode: Advantage from a pending Help by one of c's allies against t
// (used up by this roll), Disadvantage when t is Dodging; the two cancel
// (SRD "Rules Glossary", Advantage).
func (s *Session) rollMode(c, t *creature) (adv, dis bool) {
	if i := slices.IndexFunc(s.helps, func(h help) bool {
		return h.target == t && h.helper != c && h.helper.Team == c.Team
	}); i >= 0 {
		adv = true
		s.helps = slices.Delete(s.helps, i, i+1)
	}
	// A dodger loses the benefit when Incapacitated; Speed 0 and seeing the
	// attacker are not modeled yet.
	dis = t.dodging && !t.out()
	return adv && !dis, dis && !adv
}

// resolveAttack makes one attack of c with a against t and applies the result.
func (s *Session) resolveAttack(c, t *creature, a MeleeAttack) []Event {
	// Attack roll: hits if the total equals or exceeds AC; a natural 20 hits
	// as a Critical Hit, a natural 1 misses (SRD "Playing the Game", Attack Rolls).
	adv, dis := s.rollMode(c, t)
	d20s := []int{s.dice.Roll(20)}
	d20 := d20s[0]
	if adv || dis {
		second := s.dice.Roll(20)
		d20s = append(d20s, second)
		if adv {
			d20 = max(d20, second)
		} else {
			d20 = min(d20, second)
		}
	}
	total := d20 + a.ToHit
	crit := d20 == 20
	hits := crit || (d20 != 1 && total >= t.AC)
	out := []Event{AttackRolled{
		Attacker: c.ID, Target: t.ID, Weapon: a.Name, D20: d20, D20s: d20s, Modifier: a.ToHit,
		Total: total, AC: t.AC, Hit: hits, Critical: crit, Advantage: adv, Disadvantage: dis,
	}}
	// An attack roll against an enemy on the holder's turn extends features
	// such as Rage.
	if c == s.active() {
		for _, f := range c.features {
			if f.active && !f.extended && f.extendableBy(content.AttackRollAgainstEnemy) {
				f.extended = true
				out = append(out, FeatureExtended{Creature: c.ID, Feature: f.ID})
			}
		}
	}
	if !hits {
		return out
	}

	// Damage: roll the damage dice (twice as many on a Critical Hit) and add
	// the modifier; never below 0 (SRD "Playing the Game", Damage Rolls, Critical Hits).
	n := a.DiceCount
	if crit {
		n *= 2
	}
	h := &hit{s: s, attacker: c, target: t, attack: a, advantage: adv, disadvantage: dis,
		critical: crit, rolls: s.rollDice(n, a.DiceSides)}
	for _, f := range c.features {
		for _, e := range f.effects {
			e.onHit(h)
		}
	}
	out = append(out, h.events...)
	return append(out, s.applyDamage(h)...)
}

// applyDamage totals a hit's damage by type and deals it to the target.
// Bonuses come first, then Resistance halves each resisted type, rounding
// down (SRD "Playing the Game", Resistance and Vulnerability).
func (s *Session) applyDamage(h *hit) []Event {
	t, a := h.target, h.attack
	byType := map[string]int{a.DamageType: sum(h.rolls) + a.DamageBonus}
	types := []string{a.DamageType}
	for _, x := range h.extras {
		if _, seen := byType[x.Type]; !seen {
			types = append(types, x.Type)
		}
		byType[x.Type] += sum(x.Rolls) + x.Bonus
	}
	amount, resisted := 0, false
	for _, typ := range types {
		part := max(byType[typ], 0)
		in := &incoming{s: s, target: t, damageType: typ}
		for _, f := range t.features {
			for _, e := range f.effects {
				e.onDamageTaken(in)
			}
		}
		if in.resisted {
			part /= 2
			resisted = true
		}
		amount += part
	}
	t.hp = max(t.hp-amount, 0)
	out := []Event{DamageDealt{Target: t.ID, Rolls: h.rolls, Bonus: a.DamageBonus, Extras: h.extras,
		Resisted: resisted, Amount: amount, Type: a.DamageType, HPAfter: t.hp}}
	if t.down() {
		out = append(out, CreatureDowned{Creature: t.ID})
		// At 0 Hit Points the creature is Unconscious, so Incapacitated.
		for _, f := range t.features {
			if f.active && f.endsOn(content.Incapacitated) {
				out = append(out, s.endFeature(t, f, EndedIncapacitated))
			}
		}
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
	c := s.active()
	var out []Event
	if !c.out() {
		out = s.expireFeatures(c)
	}
	out = append(out, TurnEnded{Creature: c.ID})
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

// inReach reports whether a creature standing at from reaches p with a.
func inReach(a MeleeAttack, from, p board.Pos) bool {
	return board.Distance(from, p)*board.SquareFeet <= a.ReachFt
}

// reachable maps each square the creature can end its move in to its cost in
// feet, given movement already spent this turn. Each step to an adjacent square
// (orthogonal or diagonal) costs 1 square, 2 for Difficult Terrain; diagonals
// can't cross a wall corner (SRD "Playing the Game", Grid). An ally's space can
// be passed through at normal cost but not ended in; a hostile creature's space
// can't be entered ("Playing the Game", Moving around Other Creatures). Passing
// hostile creatures two sizes apart or Incapacitated is not modeled yet.
func (s *Session) reachable(c *creature) map[board.Pos]int {
	budget := (c.SpeedFt + s.dashedFt - s.movedFt) / board.SquareFeet
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
		cs := CreatureState{Name: c.Name, Team: c.Team, Pos: c.Pos,
			HP: c.hp, MaxHP: c.MaxHP, AC: c.AC, Down: c.down(),
			Gone: c.gone, ReactionAvailable: !c.out() && !c.reactionUsed,
			Dodging: c.dodging && !c.out()}
		for _, f := range c.features {
			if f.active {
				cs.ActiveFeatures = append(cs.ActiveFeatures, f.ID)
			}
		}
		st.Creatures[id] = cs
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
	la.MovementLeftFt = c.SpeedFt + s.dashedFt - s.movedFt
	la.Action, la.Reaction = !s.acted, !c.reactionUsed
	la.Dash, la.Disengage, la.Dodge = !s.acted, !s.acted, !s.acted
	la.NimbleEscape = c.NimbleEscape && !s.bonused
	for p, cost := range s.reachable(c) {
		la.Moves = append(la.Moves, MoveOption{To: p, CostFt: cost})
	}
	slices.SortFunc(la.Moves, func(a, b MoveOption) int {
		return cmp.Or(cmp.Compare(a.CostFt, b.CostFt), cmp.Compare(a.To.Y, b.To.Y), cmp.Compare(a.To.X, b.To.X))
	})
	for _, id := range s.order {
		t := s.creatures[id]
		if t.Team == c.Team || t.out() {
			continue
		}
		if !s.acted && inReach(c.Attack, c.Pos, t.Pos) {
			la.Attacks = append(la.Attacks, AttackOption{Target: id, Weapon: c.Attack.Name})
		}
		if !s.acted && board.Distance(c.Pos, t.Pos)*board.SquareFeet <= board.SquareFeet {
			la.Help = append(la.Help, id)
		}
		if c.OffHand != nil && s.lightAttack && !s.bonused && inReach(*c.OffHand, c.Pos, t.Pos) {
			la.OffHandAttacks = append(la.OffHandAttacks, AttackOption{Target: id, Weapon: c.OffHand.Name})
		}
	}
	if !s.bonused {
		for _, f := range c.features {
			switch {
			case f.Activation == nil:
			case f.active && f.extendableBy(content.BonusActionExtension):
				la.Features = append(la.Features, FeatureOption{Feature: f.ID, Extend: true, UsesLeft: f.usesLeft})
			case !f.active && f.usesLeft > 0:
				la.Features = append(la.Features, FeatureOption{Feature: f.ID, UsesLeft: f.usesLeft})
			}
		}
	}
	la.BonusAction = la.NimbleEscape || len(la.OffHandAttacks) > 0 || len(la.Features) > 0
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
