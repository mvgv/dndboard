package session

import (
	"fmt"
	"time"

	"github.com/mvgv/dndboard/server/board"
)

// ID names a creature in a session.
type ID string

// Team groups creatures; creatures on different teams are hostile.
type Team string

// MeleeAttack is a creature's melee weapon attack, with modifiers already
// summed (ability modifier plus Proficiency Bonus where it applies).
type MeleeAttack struct {
	Name        string
	ToHit       int
	DiceCount   int
	DiceSides   int
	DamageBonus int
	DamageType  string
	ReachFt     int
}

// ReactionRule says how a creature's Reaction to an Opportunity Attack
// opportunity is decided.
type ReactionRule int

const (
	// ReactAlways attacks without prompting. It is the zero value, and the
	// rule monsters use: their reactions are decided without prompting.
	ReactAlways ReactionRule = iota
	// ReactNever never reacts, without prompting.
	ReactNever
	// ReactAsk pauses the game and prompts the owner, who has the session's
	// reaction timeout to answer; no answer means no reaction. It is the
	// setting for a Player Character without an automatic rule. Monsters'
	// specs must never use it: their reactions are not prompted.
	ReactAsk
)

// CreatureSpec is a creature placed on the Board when the session starts.
type CreatureSpec struct {
	ID            ID
	Name          string
	Team          Team
	Pos           board.Pos
	AC            int
	MaxHP         int
	SpeedFt       int
	InitiativeMod int
	Attack        MeleeAttack
	// Group names a set of identical creatures that share one Initiative roll.
	Group string
	// Surprised gives Disadvantage on the Initiative roll.
	Surprised bool
	// NimbleEscape lets the creature take Disengage as a Bonus Action (SRD
	// "Monsters", Nimble Escape; Hide is not modeled yet).
	NimbleEscape bool
	// PlayerCharacter marks a Player Character: at 0 Hit Points it falls
	// Unconscious and makes Death Saving Throws instead of dying outright (SRD
	// "Playing the Game", Dropping to 0 Hit Points). Monsters die the instant
	// they drop to 0.
	PlayerCharacter bool
	// Resistances, Vulnerabilities and Immunities name damage types (SRD
	// "Playing the Game", Resistance and Vulnerability).
	Resistances, Vulnerabilities, Immunities []string
	// ConditionImmunities lists conditions the creature cannot gain.
	ConditionImmunities []Condition
	// Conditions the creature starts the combat with (no source).
	Conditions []Condition
	// SaveMods and CheckMods are the total modifiers to saving throws and ability
	// checks per ability; a missing ability means +0.
	SaveMods, CheckMods map[Ability]int
	// MedicineMod is the Wisdom (Medicine) check modifier, used to stabilize.
	MedicineMod int
	// Reactions decides Opportunity Attacks; see ReactionRule.
	Reactions ReactionRule
	// PerceptionMod and StealthMod are the modifiers of Wisdom (Perception) and
	// Dexterity (Stealth) checks, used by Search and Hide.
	PerceptionMod int
	StealthMod    int
	// DarkvisionFt is the range of Darkvision, 0 for none.
	DarkvisionFt int
	// HiddenDC, when positive, starts the creature hidden (an ambusher) with
	// that DC to find it.
	HiddenDC int
}

// ObjectKind is what a clickable Board object is.
type ObjectKind string

const (
	Door  ObjectKind = "door"
	Chest ObjectKind = "chest"
	Lever ObjectKind = "lever"
)

// ObjectSpec is a clickable object placed on the Board. A closed Door and any
// Chest fill their square (nobody walks through); a Lever is set in a wall
// square. A Lever's Controls names the Door it opens or closes.
type ObjectSpec struct {
	ID       ID
	Kind     ObjectKind
	Pos      board.Pos
	Open     bool // Door or Chest starts open; Lever starts pulled
	Controls ID
	// Secret makes a closed Door set in a wall: it is part of the wall, known
	// to no client, until found with a Wisdom (Perception) total of DC or more.
	Secret bool
	DC     int
}

// Skills that Search, Hide and Help (ability check) work with.
const (
	Perception = "Perception"
	Stealth    = "Stealth"
)

// TriggerKind is the perceivable circumstance a Readied action waits for.
type TriggerKind int

const (
	// EnemyEntersReach: a hostile creature moves into the Readier's melee reach.
	EnemyEntersReach TriggerKind = iota + 1
	// EnemyEntersSquare: a hostile creature moves onto the square at Pos.
	EnemyEntersSquare
)

// ReadyTrigger is the circumstance chosen when taking the Ready action.
type ReadyTrigger struct {
	Kind TriggerKind
	Pos  board.Pos // EnemyEntersSquare
}

// ResponseKind is what the Readier does when the trigger occurs.
type ResponseKind int

const (
	// RespondAttack: one melee attack against the creature that set off the trigger.
	RespondAttack ResponseKind = iota + 1
	// RespondMove: move up to Speed to the square To.
	RespondMove
	// RespondUtilize: take the Utilize action on Object.
	RespondUtilize
)

// ReadyResponse is the action chosen when taking the Ready action.
type ReadyResponse struct {
	Kind   ResponseKind
	To     board.Pos // RespondMove
	Object ID        // RespondUtilize
}

// Ability is one of the six abilities.
type Ability string

const (
	Strength     Ability = "Strength"
	Dexterity    Ability = "Dexterity"
	Constitution Ability = "Constitution"
	Intelligence Ability = "Intelligence"
	Wisdom       Ability = "Wisdom"
	Charisma     Ability = "Charisma"
)

// Condition is one of the 2024 conditions (SRD "Rules Glossary").
type Condition string

const (
	CondBlinded       Condition = "Blinded"
	CondCharmed       Condition = "Charmed"
	CondDeafened      Condition = "Deafened"
	CondExhaustion    Condition = "Exhaustion"
	CondFrightened    Condition = "Frightened"
	CondGrappled      Condition = "Grappled"
	CondIncapacitated Condition = "Incapacitated"
	CondInvisible     Condition = "Invisible"
	CondParalyzed     Condition = "Paralyzed"
	CondPetrified     Condition = "Petrified"
	CondPoisoned      Condition = "Poisoned"
	CondProne         Condition = "Prone"
	CondRestrained    Condition = "Restrained"
	CondStunned       Condition = "Stunned"
	CondUnconscious   Condition = "Unconscious"
)

// AllConditions lists every 2024 condition.
var AllConditions = []Condition{
	CondBlinded, CondCharmed, CondDeafened, CondExhaustion, CondFrightened,
	CondGrappled, CondIncapacitated, CondInvisible, CondParalyzed, CondPetrified,
	CondPoisoned, CondProne, CondRestrained, CondStunned, CondUnconscious,
}

// RollMode is how a D20 Test was rolled.
type RollMode string

const (
	Normal       RollMode = ""
	Advantage    RollMode = "advantage"
	Disadvantage RollMode = "disadvantage"
)

// Intent is what a Player (or a monster's tactics) asks the active creature to do.
type Intent interface{ actor() ID }

// Move moves the actor to a reachable square, spending movement.
type Move struct {
	Actor ID
	To    board.Pos
}

// Attack takes the Attack action: one melee attack roll against Target.
type Attack struct {
	Actor  ID
	Target ID
}

// Dash takes the Dash action: extra movement equal to Speed this turn.
type Dash struct{ Actor ID }

// Disengage takes the Disengage action: no Opportunity Attacks this turn.
type Disengage struct{ Actor ID }

// NimbleEscape takes Disengage as a Bonus Action (creatures with the trait).
type NimbleEscape struct{ Actor ID }

// Dodge takes the Dodge action.
type Dodge struct{ Actor ID }

// Help takes the Help action to assist an attack roll: the next attack roll by
// an ally against Target, an enemy within 5 feet of the actor, has Advantage.
type Help struct {
	Actor  ID
	Target ID
}

// HelpCheck takes the Help action to assist an ability check: Ally, within 5
// feet, has Advantage on its next check with Skill.
type HelpCheck struct {
	Actor ID
	Ally  ID
	Skill string
}

// Hide takes the Hide action.
type Hide struct{ Actor ID }

// Search takes the Search action: a Wisdom (Perception) check to find hidden creatures.
type Search struct{ Actor ID }

// Utilize takes the Utilize action on a Board object within 5 feet.
type Utilize struct {
	Actor  ID
	Object ID
}

// Ready takes the Ready action: hold Response until Trigger, then spend the Reaction.
type Ready struct {
	Actor    ID
	Trigger  ReadyTrigger
	Response ReadyResponse
}

// Stand rights a Prone actor, spending half its Speed.
type Stand struct{ Actor ID }

// Stabilize takes the Help action to stabilize a creature at 0 Hit Points: a
// DC 10 Wisdom (Medicine) check.
type Stabilize struct{ Actor, Target ID }

// Leave withdraws the actor from the combat (fled or surrendered) and ends its turn.
type Leave struct{ Actor ID }

// EndTurn ends the actor's turn.
type EndTurn struct{ Actor ID }

func (m Move) actor() ID         { return m.Actor }
func (a Attack) actor() ID       { return a.Actor }
func (d Dash) actor() ID         { return d.Actor }
func (d Disengage) actor() ID    { return d.Actor }
func (n NimbleEscape) actor() ID { return n.Actor }
func (s Stand) actor() ID        { return s.Actor }
func (s Stabilize) actor() ID    { return s.Actor }
func (l Leave) actor() ID        { return l.Actor }
func (d Dodge) actor() ID        { return d.Actor }
func (h Help) actor() ID         { return h.Actor }
func (h HelpCheck) actor() ID    { return h.Actor }
func (h Hide) actor() ID         { return h.Actor }
func (x Search) actor() ID       { return x.Actor }
func (u Utilize) actor() ID      { return u.Actor }
func (r Ready) actor() ID        { return r.Actor }
func (e EndTurn) actor() ID      { return e.Actor }

// Code classifies a rejected intent.
type Code int

const (
	NotYourTurn Code = iota + 1
	Unreachable
	OutOfReach
	InvalidTarget
	ActionUsed
	CombatOver
	BonusActionUsed
	NoSuchOption
	ObjectBlocked
	Incapacitated
	// AwaitingReaction: the game is paused until the pending prompt is answered.
	AwaitingReaction
	NoPendingReaction
)

func (c Code) String() string {
	switch c {
	case NotYourTurn:
		return "NotYourTurn"
	case Unreachable:
		return "Unreachable"
	case OutOfReach:
		return "OutOfReach"
	case InvalidTarget:
		return "InvalidTarget"
	case ActionUsed:
		return "ActionUsed"
	case CombatOver:
		return "CombatOver"
	case BonusActionUsed:
		return "BonusActionUsed"
	case NoSuchOption:
		return "NoSuchOption"
	case ObjectBlocked:
		return "ObjectBlocked"
	case Incapacitated:
		return "Incapacitated"
	case AwaitingReaction:
		return "AwaitingReaction"
	case NoPendingReaction:
		return "NoPendingReaction"
	}
	return fmt.Sprintf("Code(%d)", int(c))
}

// Rejection is returned for an illegal intent. The session state is unchanged.
type Rejection struct {
	Code   Code
	Reason string
}

func (r *Rejection) Error() string { return r.Code.String() + ": " + r.Reason }

func reject(c Code, format string, args ...any) *Rejection {
	return &Rejection{Code: c, Reason: fmt.Sprintf(format, args...)}
}

// Event is something that happened, in order. Events are the session's output.
type Event interface{ event() }

type InitiativeRolled struct {
	Creature ID
	D20      int
	Total    int
}

type TurnStarted struct {
	Creature ID
	Round    int
}

type Moved struct {
	Creature ID
	From, To board.Pos
	CostFt   int
}

type AttackRolled struct {
	Attacker, Target ID
	Weapon           string
	D20              int
	Modifier         int
	Total            int
	AC               int
	Hit              bool
	Critical         bool
	Mode             RollMode
}

type DamageDealt struct {
	Target ID
	Rolls  []int
	Bonus  int
	Raw    int // damage before Resistance, Vulnerability and Immunity
	Amount int // damage taken
	Type   string
	// Adjustment is "immunity", "resistance", "vulnerability" or
	// "resistance+vulnerability" when one applied.
	Adjustment string
	HPAfter    int
}

type CreatureDowned struct{ Creature ID }

type TurnEnded struct{ Creature ID }

type CombatEnded struct{ Winner Team }

type Dashed struct {
	Creature ID
	ExtraFt  int
}

type Disengaged struct {
	Creature ID
	Bonus    bool // taken with Nimble Escape rather than the action
}

// OpportunityAttackMade precedes the attack events of a Reaction attack.
type OpportunityAttackMade struct{ Attacker, Target ID }

// CreatureLeft: the creature fled or surrendered and is out of the combat.
type CreatureLeft struct{ Creature ID }

// Dodged: until the start of its next turn, attacks against the creature have
// Disadvantage and it has Advantage on Dexterity saving throws.
type Dodged struct{ Creature ID }

// Helped: the next attack roll by an ally against Target has Advantage.
type Helped struct{ Creature, Target ID }

// HelpedCheck: Ally has Advantage on its next check with Skill.
type HelpedCheck struct {
	Creature, Ally ID
	Skill          string
}

// Hid is the result of a Hide action's Dexterity (Stealth) check.
type Hid struct {
	Creature ID
	D20      int
	Modifier int
	Total    int
	Mode     RollMode
	Hidden   bool
}

// Revealed: the creature stopped being hidden (it made an attack roll).
type Revealed struct {
	Creature ID
	Reason   string
}

// Searched is the result of a Search action's Wisdom (Perception) check;
// Found lists the hidden enemies it discovered.
type Searched struct {
	Creature ID
	D20      int
	Modifier int
	Total    int
	Mode     RollMode
	Found    []ID
}

// Readied: the creature holds an action until its trigger.
type Readied struct {
	Creature ID
	Trigger  ReadyTrigger
	Response ReadyResponse
}

// ReadyTriggered: the trigger occurred and the Reaction was spent; the
// response events follow. Cause is the creature that set it off.
type ReadyTriggered struct{ Creature, Cause ID }

// ReadyFizzled: the held response could not be carried out.
type ReadyFizzled struct {
	Creature ID
	Reason   string
}

// ReadyLapsed: the creature's next turn started with the trigger unmet.
type ReadyLapsed struct{ Creature ID }

// ObjectUsed: a creature used a Board object (Open is its state afterwards).
type ObjectUsed struct {
	Creature ID
	Object   ID
	Kind     ObjectKind
	Open     bool
}

// ObjectChanged: a lever opened or closed the object it controls.
type ObjectChanged struct {
	Object ID
	Open   bool
}

func (Dodged) event()         {}
func (Helped) event()         {}
func (HelpedCheck) event()    {}
func (Hid) event()            {}
func (Revealed) event()       {}
func (Searched) event()       {}
func (Readied) event()        {}
func (ReadyTriggered) event() {}
func (ReadyFizzled) event()   {}
func (ReadyLapsed) event()    {}
func (ObjectUsed) event()     {}
func (ObjectChanged) event()  {}

// ReactionPrompted: the game is paused and Creature's owner is asked whether
// it makes an Opportunity Attack against Target, within Timeout.
type ReactionPrompted struct {
	Creature, Target ID
	Timeout          time.Duration
}

// ReactionDeclined: the owner chose not to react.
type ReactionDeclined struct{ Creature ID }

// ReactionTimedOut: the timeout passed without an answer; no reaction.
type ReactionTimedOut struct{ Creature ID }

func (ReactionPrompted) event() {}
func (ReactionDeclined) event() {}
func (ReactionTimedOut) event() {}

func (Dashed) event()                {}
func (Disengaged) event()            {}
func (OpportunityAttackMade) event() {}
func (CreatureLeft) event()          {}

func (InitiativeRolled) event() {}
func (TurnStarted) event()      {}
func (Moved) event()            {}
func (AttackRolled) event()     {}
func (DamageDealt) event()      {}
func (CreatureDowned) event()   {}
func (TurnEnded) event()        {}
func (CombatEnded) event()      {}

// CreatureState is what everyone sees of a creature.
type CreatureState struct {
	Name  string
	Team  Team
	Pos   board.Pos
	HP    int
	MaxHP int
	AC    int
	Down  bool
	// Dead: died outright or failed three Death Saving Throws.
	Dead bool
	// Stable: at 0 Hit Points without making Death Saving Throws.
	Stable                                bool
	DeathSaveSuccesses, DeathSaveFailures int
	// Conditions the creature has, including implied ones (Paralyzed implies
	// Incapacitated), sorted by name.
	Conditions []Condition
	// ExhaustionLevel is 0 without the Exhaustion condition.
	ExhaustionLevel int
	// Gone: fled or surrendered.
	Gone bool
	// ReactionAvailable: the creature has not used its Reaction this round.
	ReactionAvailable bool
	// Dodging: the Dodge action's benefits are active.
	Dodging bool
	// Hidden: the creature has the Invisible condition from the Hide action.
	Hidden bool
	// Readied: the creature holds an action for a trigger.
	Readied bool
}

// ObjectState is what everyone sees of a Board object.
type ObjectState struct {
	Kind ObjectKind
	Pos  board.Pos
	Open bool
}

// State is the visible state of the session.
type State struct {
	Round     int
	Active    ID
	Order     []ID
	Creatures map[ID]CreatureState
	Objects   map[ID]ObjectState
	Over      bool
	Winner    Team
	// Pending is the reaction prompt the game is paused on, if any.
	Pending *PendingReaction
}

// PendingReaction is an unanswered reaction prompt.
type PendingReaction struct {
	Creature, Target ID
	Remaining        time.Duration
}

// MoveOption is a square the active creature can reach this turn.
type MoveOption struct {
	To     board.Pos
	CostFt int
}

// AttackOption is a valid target for the active creature's attack.
type AttackOption struct {
	Target ID
	Weapon string
}

// LegalActions are the active creature's legal choices, precomputed by the
// server so the client only renders them (ADR 0004).
type LegalActions struct {
	Creature ID
	// Action-economy options still unspent this turn.
	Action, BonusAction, Reaction bool
	MovementLeftFt                int
	Moves                         []MoveOption
	Attacks                       []AttackOption
	Dash, Disengage               bool // standard actions, while the action is unspent
	Dodge, Hide, Search, Ready    bool // standard actions, while the action is unspent
	// HelpAttack lists the enemies within 5 feet; HelpCheck the allies within 5 feet.
	HelpAttack, HelpCheck []ID
	Utilize               []UtilizeOption
	Stand                 bool // right a Prone creature
	Stabilize             []ID // creatures at 0 Hit Points within 5 feet
	NimbleEscape          bool // Bonus Action, while unspent
	Leave                 bool
	EndTurn               bool
}

// UtilizeOption is a Board object the active creature can Utilize.
type UtilizeOption struct {
	Object ID
	Effect string // "open", "close" or "pull"
}
