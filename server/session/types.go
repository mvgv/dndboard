package session

import (
	"fmt"

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

// EndTurn ends the actor's turn.
type EndTurn struct{ Actor ID }

func (m Move) actor() ID    { return m.Actor }
func (a Attack) actor() ID  { return a.Actor }
func (e EndTurn) actor() ID { return e.Actor }

// Code classifies a rejected intent.
type Code int

const (
	NotYourTurn Code = iota + 1
	Unreachable
	OutOfReach
	InvalidTarget
	ActionUsed
	CombatOver
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
}

// State is the visible state of the session.
type State struct {
	Round     int
	Active    ID
	Order     []ID
	Creatures map[ID]CreatureState
	Over      bool
	Winner    Team
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
	Moves    []MoveOption
	Attacks  []AttackOption
	EndTurn  bool
}
