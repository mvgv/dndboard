package session

import (
	"fmt"
	"time"

	"github.com/mvgv/dndboard/content"
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
	// Ability is "str" or "dex", the modifier the attack uses.
	Ability string
	// Properties are the weapon's properties, e.g. "finesse" or "light".
	Properties []string
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
	// Reactions decides Opportunity Attacks; see ReactionRule.
	Reactions ReactionRule
	// OffHand is the extra Bonus Action attack with a second Light weapon,
	// after taking the Attack action with a Light one (SRD "Equipment", Light).
	OffHand *MeleeAttack
	// Features are the creature's approved features from the content format.
	// The engine interprets their declarative effects and runs hand-written
	// code for those flagged "requires implementation".
	Features []content.Feature
}

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

// Leave withdraws the actor from the combat (fled or surrendered) and ends its turn.
type Leave struct{ Actor ID }

// EndTurn ends the actor's turn.
type EndTurn struct{ Actor ID }

// Help takes the Help action to assist an attack roll: the next attack roll
// by one of the actor's allies against Target has Advantage.
type Help struct {
	Actor  ID
	Target ID
}

// Dodge takes the Dodge action: attack rolls against the actor have
// Disadvantage until the start of its next turn.
type Dodge struct{ Actor ID }

// OffHandAttack makes the extra Light-weapon attack as a Bonus Action.
type OffHandAttack struct {
	Actor  ID
	Target ID
}

// UseFeature activates a feature with an activation (e.g. Rage), or extends
// it when it is active and can be extended that way.
type UseFeature struct {
	Actor   ID
	Feature string // Feature ID
}

func (h Help) actor() ID          { return h.Actor }
func (d Dodge) actor() ID         { return d.Actor }
func (o OffHandAttack) actor() ID { return o.Actor }
func (u UseFeature) actor() ID    { return u.Actor }

func (m Move) actor() ID         { return m.Actor }
func (a Attack) actor() ID       { return a.Actor }
func (d Dash) actor() ID         { return d.Actor }
func (d Disengage) actor() ID    { return d.Actor }
func (n NimbleEscape) actor() ID { return n.Actor }
func (l Leave) actor() ID        { return l.Actor }
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
	// AwaitingReaction: the game is paused until the pending prompt is answered.
	AwaitingReaction
	NoPendingReaction
	NoUsesLeft
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
	case AwaitingReaction:
		return "AwaitingReaction"
	case NoPendingReaction:
		return "NoPendingReaction"
	case NoUsesLeft:
		return "NoUsesLeft"
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
	D20              int   // the d20 used
	D20s             []int // every d20 rolled: two with Advantage or Disadvantage
	Modifier         int
	Total            int
	AC               int
	Hit              bool
	Critical         bool
	// Advantage and Disadvantage after cancelling each other out.
	Advantage, Disadvantage bool
}

// DamageDealt: Rolls are the weapon's damage dice and Bonus its modifier;
// Extras come from features. Amount is what the target lost, after
// Resistance.
type DamageDealt struct {
	Target   ID
	Rolls    []int
	Bonus    int
	Extras   []ExtraDamage
	Resisted bool // some of the damage was halved by Resistance
	Amount   int
	Type     string
	HPAfter  int
}

// ExtraDamage is damage a feature adds to a hit: dice, a flat bonus, or both.
type ExtraDamage struct {
	Feature string // Feature ID
	Rolls   []int
	Bonus   int
	Type    string
}

// DamageRerolled: a feature rolled the weapon's damage dice twice; the kept
// roll is the one in the following DamageDealt.
type DamageRerolled struct {
	Attacker      ID
	Feature       string
	First, Second []int
}

type Helped struct{ Creature, Target ID }

type Dodged struct{ Creature ID }

type FeatureActivated struct {
	Creature ID
	Feature  string
	UsesLeft int
}

type FeatureExtended struct {
	Creature ID
	Feature  string
}

// Why an active feature ended.
const (
	EndedExpired       = "expired"      // its duration ran out without an extension
	EndedMaxDuration   = "max_duration" // it reached its longest duration
	EndedIncapacitated = "incapacitated"
)

type FeatureEnded struct {
	Creature ID
	Feature  string
	Reason   string
}

func (DamageRerolled) event()   {}
func (Helped) event()           {}
func (Dodged) event()           {}
func (FeatureActivated) event() {}
func (FeatureExtended) event()  {}
func (FeatureEnded) event()     {}

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
	// Gone: fled or surrendered.
	Gone bool
	// ReactionAvailable: the creature has not used its Reaction this round.
	ReactionAvailable bool
	// Dodging: attack rolls against the creature have Disadvantage.
	Dodging bool
	// ActiveFeatures are the IDs of its features currently active (e.g. Rage).
	ActiveFeatures []string
}

// State is the visible state of the session.
type State struct {
	Round     int
	Active    ID
	Order     []ID
	Creatures map[ID]CreatureState
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
	NimbleEscape                  bool // Bonus Action, while unspent
	// Help lists the enemies within 5 feet the creature can Help against,
	// while the action is unspent.
	Help  []ID
	Dodge bool // while the action is unspent
	// OffHandAttacks: Bonus Action attack targets after a Light-weapon attack.
	OffHandAttacks []AttackOption
	// Features the creature can activate or extend with its Bonus Action.
	Features []FeatureOption
	Leave    bool
	EndTurn  bool
}

// FeatureOption is a feature the active creature can use now.
type FeatureOption struct {
	Feature  string // Feature ID
	Extend   bool   // already active: using it extends it
	UsesLeft int
}
