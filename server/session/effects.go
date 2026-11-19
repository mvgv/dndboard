package session

// Effect is something the rules engine applies outside a creature's own
// intents: traps, spells and the Narrator's rulings come in through Session.Apply.
type Effect interface{ effect() }

// InflictCondition gives Target a condition. Exhaustion adds one level. Source
// names the charmer, the source of fear or the grappler.
type InflictCondition struct {
	Target    ID
	Condition Condition
	Source    ID
}

// ClearCondition ends a condition on Target. Exhaustion loses one level.
type ClearCondition struct {
	Target    ID
	Condition Condition
}

// InflictDamage deals damage of a type to Target.
type InflictDamage struct {
	Target   ID
	Amount   int
	Type     string
	Critical bool
}

// Heal restores Hit Points to Target.
type Heal struct {
	Target ID
	Amount int
}

// SavingThrow makes Target roll a saving throw against DC.
type SavingThrow struct {
	Target  ID
	Ability Ability
	DC      int
}

// AbilityCheck makes Target roll an ability check against DC. A check that
// requires sight or hearing fails automatically while Blinded or Deafened.
type AbilityCheck struct {
	Target                   ID
	Ability                  Ability
	DC                       int
	NeedsSight, NeedsHearing bool
}

func (InflictCondition) effect() {}
func (ClearCondition) effect()   {}
func (InflictDamage) effect()    {}
func (Heal) effect()             {}
func (SavingThrow) effect()      {}
func (AbilityCheck) effect()     {}

// ConditionApplied: Level is the Exhaustion level after the change.
type ConditionApplied struct {
	Creature  ID
	Condition Condition
	Source    ID
	Level     int
}

// ConditionRemoved: Level is the Exhaustion level after the change.
type ConditionRemoved struct {
	Creature  ID
	Condition Condition
	Level     int
}

// ConditionResisted: the creature is immune to the condition.
type ConditionResisted struct {
	Creature  ID
	Condition Condition
}

type SaveRolled struct {
	Creature ID
	Ability  Ability
	D20      int
	Modifier int
	Total    int
	DC       int
	Success  bool
	AutoFail bool
	Mode     RollMode
}

type CheckRolled struct {
	Creature ID
	Ability  Ability
	D20      int
	Modifier int
	Total    int
	DC       int
	Success  bool
	AutoFail bool
	Mode     RollMode
}

// DeathSaveRolled carries the tallies after the roll.
type DeathSaveRolled struct {
	Creature            ID
	D20                 int
	Successes, Failures int
}

// DeathSaveFailed: a Death Saving Throw failure from damage at 0 Hit Points.
type DeathSaveFailed struct {
	Creature            ID
	Successes, Failures int
}

type CreatureStabilized struct{ Creature ID }

type CreatureDied struct{ Creature ID }

// HealingReceived: Amount is what was actually regained.
type HealingReceived struct {
	Creature ID
	Amount   int
	HPAfter  int
}

type StoodUp struct {
	Creature ID
	CostFt   int
}

func (ConditionApplied) event()   {}
func (ConditionRemoved) event()   {}
func (ConditionResisted) event()  {}
func (SaveRolled) event()         {}
func (CheckRolled) event()        {}
func (DeathSaveRolled) event()    {}
func (DeathSaveFailed) event()    {}
func (CreatureStabilized) event() {}
func (CreatureDied) event()       {}
func (HealingReceived) event()    {}
func (StoodUp) event()            {}
