package content

import (
	"fmt"
	"slices"
	"strings"
)

// Feature is a class feature, feat or trait: declarative effects the rules
// engine interprets, or a "requires implementation" flag meaning the behavior
// is hand-coded in the engine under the feature's ID.
type Feature struct {
	ID     string
	Name   string
	Status Status
	Source string
	// RequiresImplementation: the feature doesn't fit the effect format, so
	// it carries no Effects or Activation and the engine supplies its code.
	RequiresImplementation bool
	// Activation, when set, makes the Effects apply only while the feature is
	// active. Without it the Effects always apply.
	Activation *Activation
	Effects    []Effect
}

// Activation describes a feature its holder turns on, such as Rage.
type Activation struct {
	Cost       Cost
	Uses       int      // uses before a rest restores them
	Lasts      Duration // how long one activation lasts before extensions
	ExtendedBy []Extension
	MaxRounds  int // 0: no cap; otherwise it ends after this many rounds
	EndsOn     []Ending
}

type (
	Cost      string
	Duration  string
	Extension string
	Ending    string
)

const (
	BonusAction Cost = "bonus_action"

	// UntilEndOfNextTurn: lasts until the end of the holder's next turn, and
	// each extension pushes that to the end of the following turn.
	UntilEndOfNextTurn Duration = "until_end_of_next_turn"

	// AttackRollAgainstEnemy: making an attack roll against an enemy on the
	// holder's turn extends the activation.
	AttackRollAgainstEnemy Extension = "attack_roll_against_enemy"
	// BonusActionExtension: taking a Bonus Action to extend it.
	BonusActionExtension Extension = "bonus_action"

	// Incapacitated: the activation ends when the holder has the
	// Incapacitated condition.
	Incapacitated Ending = "incapacitated"
)

// Effect is one behavior: when Trigger happens and every Condition holds,
// apply Outcome, at most as often as Frequency allows.
type Effect struct {
	Trigger    Trigger
	Conditions []Condition
	Frequency  Frequency
	Outcome    Outcome
}

type (
	Trigger   string
	Frequency string
)

const (
	// AttackHit: the holder hits a creature with an attack.
	AttackHit Trigger = "attack_hit"
	// DamageTaken: the holder is about to take damage.
	DamageTaken Trigger = "damage_taken"

	Always Frequency = "always"
	// OncePerTurn: at most once in any one turn, whoever's turn it is.
	OncePerTurn Frequency = "once_per_turn"
)

// ConditionKind names what a Condition tests.
type ConditionKind string

const (
	// WeaponProperty: the attack's weapon has property Value.
	WeaponProperty ConditionKind = "weapon_property"
	// RangedWeapon: whether the attack's weapon is a Ranged weapon equals Flag.
	// Weapons carry no ranged flag until ranged attacks land, so every weapon
	// is a Melee one; the condition exists to encode rules text faithfully.
	RangedWeapon ConditionKind = "ranged_weapon"
	// AttackAbility: the attack uses ability Value ("str" or "dex").
	AttackAbility ConditionKind = "attack_ability"
	// Roll: the attack roll has Advantage (Value "advantage") or doesn't have
	// Disadvantage (Value "not_disadvantage").
	Roll ConditionKind = "roll"
	// AllyNearTarget: an ally of the holder, other than the holder and not
	// Incapacitated, is within Feet of the target.
	AllyNearTarget ConditionKind = "ally_near_target_ft"
	// DamageTypeIs: the damage is one of DamageTypes.
	DamageTypeIs ConditionKind = "damage_type"
	// AnyOf and AllOf combine the conditions in Of.
	AnyOf ConditionKind = "any_of"
	AllOf ConditionKind = "all_of"
)

// Roll values.
const (
	Advantage       = "advantage"
	NotDisadvantage = "not_disadvantage"
)

// Condition is one test an Effect needs to pass. Kind says which field holds
// its argument.
type Condition struct {
	Kind        ConditionKind
	Value       string
	Flag        bool
	Feet        int
	DamageTypes []string
	Of          []Condition
}

// OutcomeKind names what an Outcome does.
type OutcomeKind string

const (
	// ExtraDamage adds Dice to a hit's damage (doubled on a Critical Hit).
	ExtraDamage OutcomeKind = "extra_damage"
	// DamageBonus adds Bonus to a hit's damage.
	DamageBonus OutcomeKind = "damage_bonus"
	// Resistance halves the damage (round down).
	Resistance OutcomeKind = "resistance"
)

// WeaponDamageType as an extra damage type means "the weapon's damage type".
const WeaponDamageType = "weapon"

// Outcome is what an Effect does. Kind says which field holds its argument.
type Outcome struct {
	Kind  OutcomeKind
	Dice  Damage
	Bonus int
}

// Vocabularies.
var (
	// DamageTypes are the Ruleset's damage types (SRD 5.2 "Rules Glossary").
	DamageTypes = []string{"Acid", "Bludgeoning", "Cold", "Fire", "Force", "Lightning",
		"Necrotic", "Piercing", "Poison", "Psychic", "Radiant", "Slashing", "Thunder"}
	// WeaponProperties are the weapon properties (SRD 5.2 "Equipment").
	WeaponProperties = []string{"ammunition", "finesse", "heavy", "light", "loading",
		"reach", "thrown", "two-handed", "versatile"}
	// Abilities an attack can use.
	Abilities = []string{"str", "dex"}

	triggers    = []Trigger{AttackHit, DamageTaken}
	frequencies = []Frequency{Always, OncePerTurn}
	// What each trigger can test and do.
	triggerConditions = map[Trigger][]ConditionKind{
		AttackHit:   {WeaponProperty, RangedWeapon, AttackAbility, Roll, AllyNearTarget, AnyOf, AllOf},
		DamageTaken: {DamageTypeIs, AnyOf, AllOf},
	}
	triggerOutcomes = map[Trigger][]OutcomeKind{
		AttackHit:   {ExtraDamage, DamageBonus},
		DamageTaken: {Resistance},
	}
)

type (
	rawFeature struct {
		ID                     *string        `json:"id"`
		Name                   *string        `json:"name"`
		Status                 *string        `json:"status"`
		Source                 *string        `json:"source"`
		RequiresImplementation bool           `json:"requires_implementation"`
		Activation             *rawActivation `json:"activation"`
		Effects                []rawEffect    `json:"effects"`
	}
	rawActivation struct {
		Cost       *string  `json:"cost"`
		Uses       *int     `json:"uses"`
		Lasts      *string  `json:"lasts"`
		ExtendedBy []string `json:"extended_by"`
		MaxRounds  *int     `json:"max_rounds"`
		EndsOn     []string `json:"ends_on"`
	}
	rawEffect struct {
		Trigger    *string        `json:"trigger"`
		Conditions []rawCondition `json:"conditions"`
		Frequency  *string        `json:"frequency"`
		Outcome    *rawOutcome    `json:"outcome"`
	}
	rawCondition struct {
		WeaponProperty *string        `json:"weapon_property"`
		RangedWeapon   *bool          `json:"ranged_weapon"`
		AttackAbility  *string        `json:"attack_ability"`
		Roll           *string        `json:"roll"`
		AllyNearTarget *int           `json:"ally_near_target_ft"`
		DamageType     []string       `json:"damage_type"`
		AnyOf          []rawCondition `json:"any_of"`
		AllOf          []rawCondition `json:"all_of"`
	}
	rawOutcome struct {
		ExtraDamage *rawDamage `json:"extra_damage"`
		DamageBonus *int       `json:"damage_bonus"`
		Resistance  *bool      `json:"resistance"`
	}
)

// oneOf checks a value against a vocabulary.
func oneOf[T ~string](v *validator, where, what string, s T, allowed []T) bool {
	if slices.Contains(allowed, s) {
		return true
	}
	v.errf("%s: unknown %s %q", where, what, s)
	return false
}

func (v *validator) feature(i int, r rawFeature) (Feature, bool) {
	before := len(v.problems)
	where := fmt.Sprintf("features[%d]", i)
	f := Feature{
		ID:                     v.str(where, "id", r.ID),
		Name:                   v.str(where, "name", r.Name),
		Status:                 v.status(where, r.Status),
		Source:                 v.str(where, "source", r.Source),
		RequiresImplementation: r.RequiresImplementation,
	}
	if f.ID != "" {
		where = fmt.Sprintf("feature %q", f.ID)
	}
	if f.RequiresImplementation {
		if len(r.Effects) > 0 {
			v.errf("%s: requires implementation and must not declare effects", where)
		}
		if r.Activation != nil {
			v.errf("%s: requires implementation and must not declare an activation", where)
		}
		return f, len(v.problems) == before
	}
	if len(r.Effects) == 0 {
		v.errf("%s: needs at least one effect, or requires_implementation", where)
	}
	if r.Activation != nil {
		f.Activation = v.activation(where, r.Activation)
	}
	for j, re := range r.Effects {
		f.Effects = append(f.Effects, v.effect(fmt.Sprintf("%s: effects[%d]", where, j), re))
	}
	return f, len(v.problems) == before
}

func (v *validator) activation(where string, r *rawActivation) *Activation {
	a := &Activation{
		Cost:  Cost(v.str(where, "activation.cost", r.Cost)),
		Uses:  v.num(where, "activation.uses", r.Uses, 1),
		Lasts: Duration(v.str(where, "activation.lasts", r.Lasts)),
	}
	if a.Cost != "" {
		oneOf(v, where, "activation cost", a.Cost, []Cost{BonusAction})
	}
	if a.Lasts != "" {
		oneOf(v, where, "duration", a.Lasts, []Duration{UntilEndOfNextTurn})
	}
	if r.MaxRounds != nil {
		a.MaxRounds = v.num(where, "activation.max_rounds", r.MaxRounds, 1)
	}
	for _, e := range r.ExtendedBy {
		if oneOf(v, where, "extension", Extension(e), []Extension{AttackRollAgainstEnemy, BonusActionExtension}) {
			a.ExtendedBy = append(a.ExtendedBy, Extension(e))
		}
	}
	for _, e := range r.EndsOn {
		if oneOf(v, where, "ending", Ending(e), []Ending{Incapacitated}) {
			a.EndsOn = append(a.EndsOn, Ending(e))
		}
	}
	return a
}

func (v *validator) effect(where string, r rawEffect) Effect {
	e := Effect{
		Trigger:   Trigger(v.str(where, "trigger", r.Trigger)),
		Frequency: Frequency(v.str(where, "frequency", r.Frequency)),
	}
	triggerOK := e.Trigger != "" && oneOf(v, where, "trigger", e.Trigger, triggers)
	if e.Frequency != "" {
		oneOf(v, where, "frequency", e.Frequency, frequencies)
	}
	for k, rc := range r.Conditions {
		c := v.condition(fmt.Sprintf("%s: conditions[%d]", where, k), rc)
		if triggerOK {
			v.conditionFits(where, c, e.Trigger)
		}
		e.Conditions = append(e.Conditions, c)
	}
	if r.Outcome == nil {
		v.errf("%s: missing required field %q", where, "outcome")
		return e
	}
	e.Outcome = v.outcome(where, r.Outcome)
	if triggerOK && e.Outcome.Kind != "" && !slices.Contains(triggerOutcomes[e.Trigger], e.Outcome.Kind) {
		v.errf("%s: outcome %q does not apply to trigger %q", where, e.Outcome.Kind, e.Trigger)
	}
	return e
}

func (v *validator) conditionFits(where string, c Condition, t Trigger) {
	if c.Kind == "" {
		return
	}
	if !slices.Contains(triggerConditions[t], c.Kind) {
		v.errf("%s: condition %q does not apply to trigger %q", where, c.Kind, t)
	}
	for _, sub := range c.Of {
		v.conditionFits(where, sub, t)
	}
}

func (v *validator) condition(where string, r rawCondition) Condition {
	var set []ConditionKind
	note := func(present bool, k ConditionKind) {
		if present {
			set = append(set, k)
		}
	}
	note(r.WeaponProperty != nil, WeaponProperty)
	note(r.RangedWeapon != nil, RangedWeapon)
	note(r.AttackAbility != nil, AttackAbility)
	note(r.Roll != nil, Roll)
	note(r.AllyNearTarget != nil, AllyNearTarget)
	note(r.DamageType != nil, DamageTypeIs)
	note(r.AnyOf != nil, AnyOf)
	note(r.AllOf != nil, AllOf)
	if len(set) != 1 {
		v.errf("%s: condition must set exactly one of %s", where, kindList(triggerConditions[AttackHit], DamageTypeIs))
		return Condition{}
	}
	c := Condition{Kind: set[0]}
	switch c.Kind {
	case WeaponProperty:
		c.Value = *r.WeaponProperty
		oneOf(v, where, "weapon property", c.Value, WeaponProperties)
	case RangedWeapon:
		c.Flag = *r.RangedWeapon
	case AttackAbility:
		c.Value = *r.AttackAbility
		oneOf(v, where, "attack ability", c.Value, Abilities)
	case Roll:
		c.Value = *r.Roll
		oneOf(v, where, "roll", c.Value, []string{Advantage, NotDisadvantage})
	case AllyNearTarget:
		c.Feet = *r.AllyNearTarget
		if c.Feet < 5 || c.Feet%5 != 0 {
			v.errf("%s: %q = %d, must be a positive multiple of 5", where, AllyNearTarget, c.Feet)
		}
	case DamageTypeIs:
		if len(r.DamageType) == 0 {
			v.errf("%s: %q needs at least one damage type", where, DamageTypeIs)
		}
		for _, t := range r.DamageType {
			oneOf(v, where, "damage type", t, DamageTypes)
		}
		c.DamageTypes = r.DamageType
	case AnyOf, AllOf:
		subs := r.AnyOf
		if c.Kind == AllOf {
			subs = r.AllOf
		}
		if len(subs) == 0 {
			v.errf("%s: %q needs at least one condition", where, c.Kind)
		}
		for k, rs := range subs {
			c.Of = append(c.Of, v.condition(fmt.Sprintf("%s.%s[%d]", where, c.Kind, k), rs))
		}
	}
	return c
}

func kindList(kinds []ConditionKind, more ...ConditionKind) string {
	var names []string
	for _, k := range append(slices.Clone(kinds), more...) {
		names = append(names, string(k))
	}
	return strings.Join(names, ", ")
}

func (v *validator) outcome(where string, r *rawOutcome) Outcome {
	n := 0
	for _, present := range []bool{r.ExtraDamage != nil, r.DamageBonus != nil, r.Resistance != nil} {
		if present {
			n++
		}
	}
	if n != 1 {
		v.errf("%s: outcome must set exactly one of extra_damage, damage_bonus, resistance", where)
		return Outcome{}
	}
	switch {
	case r.ExtraDamage != nil:
		d := Damage{
			DiceCount: v.num(where, "outcome.extra_damage.dice_count", r.ExtraDamage.DiceCount, 1),
			DiceSides: v.num(where, "outcome.extra_damage.dice_sides", r.ExtraDamage.DiceSides, 2),
			Type:      v.str(where, "outcome.extra_damage.type", r.ExtraDamage.Type),
		}
		if d.Type != "" && d.Type != WeaponDamageType {
			oneOf(v, where, "damage type", d.Type, DamageTypes)
		}
		return Outcome{Kind: ExtraDamage, Dice: d}
	case r.DamageBonus != nil:
		return Outcome{Kind: DamageBonus, Bonus: *r.DamageBonus}
	default:
		if !*r.Resistance {
			v.errf("%s: %q must be true when set", where, Resistance)
		}
		return Outcome{Kind: Resistance}
	}
}
