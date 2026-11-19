package session

import (
	"fmt"
	"slices"

	"github.com/mvgv/dndboard/content"
	"github.com/mvgv/dndboard/server/board"
)

// effect is one behavior of a feature. Declarative effects from the content
// format and hand-written ones (features flagged "requires implementation")
// implement it alike, so the engine resolves both through the same hooks.
type effect interface {
	// onHit runs when the holder hits with an attack, before the damage is
	// totalled. It may change the weapon dice or add extra damage.
	onHit(h *hit)
	// onDamageTaken runs for each damage type about to reach the holder.
	onDamageTaken(d *incoming)
}

// hit is a resolved hit whose damage is being assembled.
type hit struct {
	s                       *Session
	attacker, target        *creature
	attack                  MeleeAttack
	advantage, disadvantage bool // after cancelling
	critical                bool
	rolls                   []int // the weapon's damage dice, as kept
	extras                  []ExtraDamage
	events                  []Event // emitted before the damage
}

// incoming is the part of a hit's damage of one type, about to reach target.
type incoming struct {
	s          *Session
	target     *creature
	damageType string
	resisted   bool
}

// handCoded holds the engine code for features flagged "requires
// implementation", by feature ID.
var handCoded = map[string]func() effect{
	"savage-attacker": func() effect { return &savageAttacker{} },
}

// featureState is a creature's copy of a feature, with its uses and whether
// it is active.
type featureState struct {
	content.Feature
	effects  []effect
	usesLeft int
	active   bool
	extended bool // activated or extended during the holder's current turn
	since    int  // round it was activated
}

func newFeature(f content.Feature) (*featureState, error) {
	fs := &featureState{Feature: f}
	if a := f.Activation; a != nil {
		// useFeature and expireFeatures resolve exactly these words; a new
		// cost or duration must be taught to them before it can load.
		if a.Cost != content.BonusAction || a.Lasts != content.UntilEndOfNextTurn {
			return nil, fmt.Errorf("feature %q: the engine can't resolve activation cost %q lasting %q", f.ID, a.Cost, a.Lasts)
		}
		fs.usesLeft = a.Uses
	}
	if f.RequiresImplementation {
		code, ok := handCoded[f.ID]
		if !ok {
			return nil, fmt.Errorf("feature %q requires implementation, but the engine has no code for it", f.ID)
		}
		fs.effects = []effect{code()}
		return fs, nil
	}
	for _, e := range f.Effects {
		fs.effects = append(fs.effects, &declared{feature: fs, Effect: e})
	}
	return fs, nil
}

// live reports whether the feature's effects apply now.
func (f *featureState) live() bool { return f.Activation == nil || f.active }

func (f *featureState) extendableBy(e content.Extension) bool {
	return f.Activation != nil && slices.Contains(f.Activation.ExtendedBy, e)
}

func (f *featureState) endsOn(e content.Ending) bool {
	return f.Activation != nil && slices.Contains(f.Activation.EndsOn, e)
}

// declared interprets one declarative effect.
type declared struct {
	content.Effect
	feature  *featureState
	usedTurn int // Session.turnSeq when last applied; 0 when never
}

// use records an application against the frequency limit.
func (d *declared) use(s *Session) {
	if d.Frequency == content.OncePerTurn {
		d.usedTurn = s.turnSeq
	}
}

// ready reports whether the feature is live and the frequency allows another use.
func (d *declared) ready(s *Session) bool {
	return d.feature.live() && (d.Frequency != content.OncePerTurn || d.usedTurn != s.turnSeq)
}

func (d *declared) onHit(h *hit) {
	if d.Trigger != content.AttackHit || !d.ready(h.s) || !holdAll(d.Conditions, h.holds) {
		return
	}
	d.use(h.s)
	switch o := d.Outcome; o.Kind {
	case content.ExtraDamage:
		n := o.Dice.DiceCount
		if h.critical {
			n *= 2
		}
		typ := o.Dice.Type
		if typ == content.WeaponDamageType {
			typ = h.attack.DamageType
		}
		h.extras = append(h.extras, ExtraDamage{Feature: d.feature.ID, Rolls: h.s.rollDice(n, o.Dice.DiceSides), Type: typ})
	case content.DamageBonus:
		h.extras = append(h.extras, ExtraDamage{Feature: d.feature.ID, Bonus: o.Bonus, Type: h.attack.DamageType})
	}
}

func (d *declared) onDamageTaken(in *incoming) {
	if d.Trigger != content.DamageTaken || !d.ready(in.s) || !holdAll(d.Conditions, in.holds) {
		return
	}
	d.use(in.s)
	if d.Outcome.Kind == content.Resistance {
		in.resisted = true
	}
}

// holds evaluates one condition, combining AnyOf and AllOf and handing the
// rest to leaf.
func holds(c content.Condition, leaf func(content.Condition) bool) bool {
	switch c.Kind {
	case content.AnyOf:
		return slices.ContainsFunc(c.Of, func(sub content.Condition) bool { return holds(sub, leaf) })
	case content.AllOf:
		return holdAll(c.Of, leaf)
	}
	return leaf(c)
}

func holdAll(cs []content.Condition, leaf func(content.Condition) bool) bool {
	for _, c := range cs {
		if !holds(c, leaf) {
			return false
		}
	}
	return true
}

func (h *hit) holds(c content.Condition) bool {
	switch c.Kind {
	case content.WeaponProperty:
		return slices.Contains(h.attack.Properties, c.Value)
	case content.RangedWeapon:
		return !c.Flag // the session models melee weapon attacks only
	case content.AttackAbility:
		return h.attack.Ability == c.Value
	case content.Roll:
		switch c.Value {
		case content.Advantage:
			return h.advantage
		case content.NotDisadvantage:
			return !h.disadvantage
		}
	case content.AllyNearTarget:
		for _, o := range h.s.creatures {
			// A creature that is down is Unconscious, so Incapacitated.
			if o != h.attacker && o.Team == h.attacker.Team && !o.out() &&
				board.Distance(o.Pos, h.target.Pos)*board.SquareFeet <= c.Feet {
				return true
			}
		}
	}
	return false
}

func (in *incoming) holds(c content.Condition) bool {
	if c.Kind == content.DamageTypeIs {
		return slices.Contains(c.DamageTypes, in.damageType)
	}
	return false
}

// savageAttacker is Savage Attacker (SRD 5.2 "Feats", Origin Feats): "Once
// per turn when you hit a target with a weapon, you can roll the weapon's
// damage dice twice and use either roll against the target." It keeps the
// higher roll (the first on a tie). On a Critical Hit both rolls use the
// doubled dice.
type savageAttacker struct{ usedTurn int }

func (a *savageAttacker) onHit(h *hit) {
	if a.usedTurn == h.s.turnSeq {
		return
	}
	a.usedTurn = h.s.turnSeq
	second := h.s.rollDice(len(h.rolls), h.attack.DiceSides)
	h.events = append(h.events, DamageRerolled{Attacker: h.attacker.ID, Feature: "savage-attacker", First: h.rolls, Second: second})
	if sum(second) > sum(h.rolls) {
		h.rolls = second
	}
}

func (*savageAttacker) onDamageTaken(*incoming) {}

func (s *Session) rollDice(n, sides int) []int {
	rolls := make([]int, n)
	for i := range rolls {
		rolls[i] = s.dice.Roll(sides)
	}
	return rolls
}

func sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}
