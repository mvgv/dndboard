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
	// Reactions decides Opportunity Attacks; see ReactionRule.
	Reactions ReactionRule
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
}

type DamageDealt struct {
	Target  ID
	Rolls   []int
	Bonus   int
	Amount  int
	Type    string
	HPAfter int
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
	Leave                         bool
	EndTurn                       bool
}
