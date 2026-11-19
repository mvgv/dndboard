package session

import (
	"fmt"
	"slices"

	"github.com/mvgv/dndboard/server/board"
)

// Visibility and hidden information (SRD 5.2 "Rules Glossary": Bright Light, Dim
// Light, Darkness, Darkvision, Lightly and Heavily Obscured, Blinded, Passive
// Perception, Search). Readings taken where the text leaves room:
//   - A viewer sees a square when it is not Blinded, a straight line between
//     square centers reaches it without crossing a wall or a closed Door (the
//     target square itself may be one), and the square is not Heavily Obscured
//     for it: Darkness, or terrain marked Obscured. The viewer's own square is
//     always seen.
//   - Light: a square is as bright as the best of the ambient light and every
//     light source whose line reaches it (bright radius, then dim radius).
//     Darkvision within its range raises Dim to Bright and Darkness to Dim.
//   - Vision is per Player Character: allies do not share what they see.
//   - Passive Perception is 10 + the Perception bonus, minus 5 for Disadvantage
//     (Dim Light on the hidden thing's square, or Poisoned/Frightened) and
//     minus Exhaustion (the +5 for Advantage is not modeled: nothing grants it
//     yet; Frightened is taken as Disadvantage without checking line of sight to
//     its source, as checkDisadvantage does). It is checked at session start and
//     after every intent, answer or timeout, for Player Characters only, against
//     every hidden trap and secret door whose square the character sees.
//   - Search is a Wisdom (Perception) check, so it has Disadvantage against
//     things on Dim Light squares (Lightly Obscured): those get their own check.
//   - Heavily Obscured terrain ('%') hides the squares inside it and blocks
//     sight through it, which Darkness (a light level) does not: a lit square
//     behind a dark one is still seen.
//   - A hidden creature (Hide or HiddenDC) is never sent to anyone but its own
//     team until found; unseen creatures are not sent either.

// Light is how bright a square is.
type Light int

const (
	// Bright is normal illumination, the zero value.
	Bright Light = iota
	// Dim Light is Lightly Obscured.
	Dim
	// Dark is Darkness, Heavily Obscured.
	Dark
)

// LightSource lights the squares around Pos that its line reaches: Bright
// within BrightFt, then Dim for DimFt more.
type LightSource struct {
	Pos      board.Pos
	BrightFt int
	DimFt    int
}

// TrapSpec is a trap hidden until found; DC is the Wisdom (Perception) check
// total needed to notice it. Springing traps is applied by the caller as an
// Effect.
type TrapSpec struct {
	ID  ID
	Pos board.Pos
	DC  int
}

// TrapState is what a Player Character knows of a discovered trap.
type TrapState struct{ Pos board.Pos }

// DiscoveryKind is what a Discovered event found.
type DiscoveryKind string

const (
	FoundTrap       DiscoveryKind = "trap"
	FoundSecretDoor DiscoveryKind = "secret door"
)

// Discovered: a hidden trap or secret door was found, by passive Perception or
// by a Search.
type Discovered struct {
	Finder  ID
	Kind    DiscoveryKind
	ID      ID
	Pos     board.Pos
	Passive bool
}

func (Discovered) event() {}

// View is the state one Player Character perceives: the only state sent to its
// client. State is the server's own, complete view and never leaves the server.
type View struct {
	Viewer    ID
	Round     int
	Active    ID
	Creatures map[ID]CreatureState
	Objects   map[ID]ObjectState
	Traps     map[ID]TrapState
	// Visible lists the squares the viewer sees, sorted by row then column.
	Visible []board.Pos
	Over    bool
	Winner  Team
}

func (s *Session) placeTraps(specs []TrapSpec) error {
	s.traps = map[ID]*trap{}
	for _, sp := range specs {
		if _, dup := s.traps[sp.ID]; sp.ID == "" || dup {
			return fmt.Errorf("session: trap needs a unique ID, got %q", sp.ID)
		}
		if cost, ok := s.board.EntryCost(sp.Pos); !ok || cost == 0 {
			return fmt.Errorf("session: trap %q is on a wall at %v", sp.ID, sp.Pos)
		}
		s.traps[sp.ID] = &trap{TrapSpec: sp}
		s.trapOrder = append(s.trapOrder, sp.ID)
	}
	return nil
}

type trap struct {
	TrapSpec
	found bool
}

// lightAt is the brightness of p before the viewer's Darkvision.
func (s *Session) lightAt(p board.Pos) Light {
	l := s.ambient
	for _, src := range s.lights {
		d := board.Distance(src.Pos, p) * board.SquareFeet
		var got Light
		switch {
		case d <= src.BrightFt:
			got = Bright
		case d <= src.BrightFt+src.DimFt:
			got = Dim
		default:
			continue
		}
		if got < l && s.clearLine(src.Pos, p) {
			l = got
		}
	}
	return l
}

// clearLine reports whether nothing opaque lies strictly between a and b: walls
// and closed Doors block, and so does Obscured terrain.
func (s *Session) clearLine(a, b board.Pos) bool {
	line := board.Line(a, b)
	if len(line) < 3 {
		return true
	}
	for _, p := range line[1 : len(line)-1] {
		if s.board.At(p) == board.Wall || s.board.At(p) == board.Obscured || s.closedDoor(p) {
			return false
		}
	}
	return true
}

func (s *Session) closedDoor(p board.Pos) bool {
	for _, o := range s.objects {
		if o.Pos == p && o.Kind == Door && !o.Open {
			return true
		}
	}
	return false
}

// effectiveLight is p's brightness as c perceives it.
func (s *Session) effectiveLight(c *creature, p board.Pos) Light {
	l := s.lightAt(p)
	if c.DarkvisionFt > 0 && board.Distance(c.Pos, p)*board.SquareFeet <= c.DarkvisionFt && l > Bright {
		l--
	}
	return l
}

// seesPos reports whether c sees square p.
func (s *Session) seesPos(c *creature, p board.Pos) bool {
	if p == c.Pos {
		return true
	}
	if c.has(CondBlinded) || !s.board.In(p) {
		return false
	}
	if s.board.At(p) == board.Obscured || s.effectiveLight(c, p) == Dark {
		return false
	}
	return s.clearLine(c.Pos, p)
}

// sees reports whether viewer perceives other: same team always, otherwise it
// must not be hidden and its square must be seen.
func (s *Session) sees(viewer, other *creature) bool {
	if viewer == other || viewer.Team == other.Team {
		return true
	}
	return !other.hidden() && s.seesPos(viewer, other.Pos)
}

// targetable reports whether c may pick t as a target: t is not hidden and c
// sees its square. A Blinded attacker may still attack, with Disadvantage
// (attackMode), so blindness alone does not rule a target out.
func (s *Session) targetable(c, t *creature) bool {
	return !t.hidden() && (c.has(CondBlinded) || s.seesPos(c, t.Pos))
}

// passivePerception is c's passive Wisdom (Perception) score for noticing
// something at p.
func (s *Session) passivePerception(c *creature, p board.Pos) int {
	score := 10 + c.PerceptionMod - c.d20Penalty()
	if s.dimFor(c, p) || c.checkDisadvantage() {
		score -= 5
	}
	return score
}

// detectPassive reveals the traps and secret doors that a Player Character's
// passive Perception notices.
func (s *Session) detectPassive() []Event {
	var out []Event
	for _, id := range s.order {
		c := s.creatures[id]
		if !c.PlayerCharacter || c.out() {
			continue
		}
		out = append(out, s.discover(c, true, func(p board.Pos, dc int) bool {
			return s.passivePerception(c, p) >= dc
		})...)
	}
	return out
}

// discover finds every undiscovered trap and secret door c sees that meets
// found.
func (s *Session) discover(c *creature, passive bool, found func(p board.Pos, dc int) bool) []Event {
	var out []Event
	for _, id := range s.trapOrder {
		t := s.traps[id]
		if !t.found && s.seesPos(c, t.Pos) && found(t.Pos, t.DC) {
			t.found = true
			out = append(out, Discovered{Finder: c.ID, Kind: FoundTrap, ID: id, Pos: t.Pos, Passive: passive})
		}
	}
	for _, id := range s.objectOrder {
		o := s.objects[id]
		if o.Secret && !o.found && s.seesPos(c, o.Pos) && found(o.Pos, o.DC) {
			o.found = true
			out = append(out, Discovered{Finder: c.ID, Kind: FoundSecretDoor, ID: id, Pos: o.Pos, Passive: passive})
		}
	}
	return out
}

// known reports whether Board object o may be shown: everything but an
// undiscovered secret door.
func (o *object) known() bool { return !o.Secret || o.found }

// ViewFor returns what the Player Character viewer perceives. An unknown viewer
// gets an empty view.
func (s *Session) ViewFor(viewer ID) View {
	v := View{Viewer: viewer, Round: s.round, Over: s.over, Winner: s.winner,
		Creatures: map[ID]CreatureState{}, Objects: map[ID]ObjectState{}, Traps: map[ID]TrapState{}}
	c, ok := s.creatures[viewer]
	if !ok {
		return v
	}
	// Whose turn it is names a creature, so it is empty while the active
	// creature is unseen or hidden.
	if a := s.active(); s.sees(c, a) {
		v.Active = a.ID
	}
	full := s.State()
	for id, o := range s.creatures {
		if s.sees(c, o) {
			v.Creatures[id] = full.Creatures[id]
		}
	}
	// A found secret door stays known even out of sight.
	for id, o := range s.objects {
		if o.known() && (o.Secret || s.seesPos(c, o.Pos)) {
			v.Objects[id] = full.Objects[id]
		}
	}
	for id, t := range s.traps {
		if t.found {
			v.Traps[id] = TrapState{Pos: t.Pos}
		}
	}
	w, h := s.board.Size()
	for y := range h {
		for x := range w {
			if p := (board.Pos{X: x, Y: y}); s.seesPos(c, p) {
				v.Visible = append(v.Visible, p)
			}
		}
	}
	return v
}

// EventsFor filters events for a Player Character's client. It is default-deny:
// an event is sent only when every creature it names is perceived by the viewer
// (so an unlisted event type is dropped), a source or target the viewer does
// not perceive is cleared where the event stays useful, and an event about an
// undiscovered secret door is dropped. Discoveries go to everyone on the
// finder's team, since what a Player Character finds is known to the Party.
// Visibility is judged against the session's current state, not the state when
// the event happened, so filter the events a Submit returns before submitting
// again.
func (s *Session) EventsFor(viewer ID, evs []Event) []Event {
	c, ok := s.creatures[viewer]
	if !ok {
		return nil
	}
	seen := func(ids ...ID) bool {
		for _, id := range ids {
			if o, ok := s.creatures[id]; !ok || !s.sees(c, o) {
				return false
			}
		}
		return true
	}
	// unseen clears an optional creature reference the viewer does not perceive.
	unseen := func(id ID) ID {
		if id != "" && !seen(id) {
			return ""
		}
		return id
	}
	var out []Event
	for _, orig := range evs {
		switch e := orig.(type) {
		case Discovered:
			if f, ok := s.creatures[e.Finder]; ok && f.Team == c.Team {
				out = append(out, e)
			}
		case Searched:
			if seen(e.Creature) {
				e.Found = slices.DeleteFunc(slices.Clone(e.Found), func(id ID) bool { return !seen(id) })
				out = append(out, e)
			}
		case ConditionApplied:
			if seen(e.Creature) {
				e.Source = unseen(e.Source)
				out = append(out, e)
			}
		case ReactionPrompted:
			// The prompt is for its creature's owner; the target it names is
			// cleared when that creature does not perceive it.
			if e.Creature == viewer || seen(e.Creature, e.Target) {
				if e.Creature == viewer {
					e.Target = unseen(e.Target)
				}
				out = append(out, e)
			}
		case ObjectUsed:
			if o, ok := s.objects[e.Object]; ok && o.known() && seen(e.Creature) {
				out = append(out, e)
			}
		case ObjectChanged:
			if o, ok := s.objects[e.Object]; ok && o.known() {
				out = append(out, e)
			}
		case CombatEnded:
			out = append(out, e)
		default:
			if ids, ok := involved(orig); ok && seen(ids...) {
				out = append(out, orig)
			}
		}
	}
	return out
}

// involved lists the creatures an event names; ok is false for event types
// EventsFor does not know, which are never sent.
func involved(e Event) (ids []ID, ok bool) {
	switch e := e.(type) {
	case InitiativeRolled:
		return []ID{e.Creature}, true
	case TurnStarted:
		return []ID{e.Creature}, true
	case TurnEnded:
		return []ID{e.Creature}, true
	case Moved:
		return []ID{e.Creature}, true
	case AttackRolled:
		return []ID{e.Attacker, e.Target}, true
	case DamageDealt:
		return []ID{e.Target}, true
	case CreatureDowned:
		return []ID{e.Creature}, true
	case Dashed:
		return []ID{e.Creature}, true
	case Disengaged:
		return []ID{e.Creature}, true
	case OpportunityAttackMade:
		return []ID{e.Attacker, e.Target}, true
	case CreatureLeft:
		return []ID{e.Creature}, true
	case Dodged:
		return []ID{e.Creature}, true
	case Helped:
		return []ID{e.Creature, e.Target}, true
	case HelpedCheck:
		return []ID{e.Creature, e.Ally}, true
	case Hid:
		return []ID{e.Creature}, true
	case Revealed:
		return []ID{e.Creature}, true
	case Readied:
		return []ID{e.Creature}, true
	case ReadyTriggered:
		return []ID{e.Creature, e.Cause}, true
	case ReadyFizzled:
		return []ID{e.Creature}, true
	case ReadyLapsed:
		return []ID{e.Creature}, true
	case ReactionDeclined:
		return []ID{e.Creature}, true
	case ReactionTimedOut:
		return []ID{e.Creature}, true
	case ConditionRemoved:
		return []ID{e.Creature}, true
	case ConditionResisted:
		return []ID{e.Creature}, true
	case SaveRolled:
		return []ID{e.Creature}, true
	case CheckRolled:
		return []ID{e.Creature}, true
	case DeathSaveRolled:
		return []ID{e.Creature}, true
	case DeathSaveFailed:
		return []ID{e.Creature}, true
	case CreatureStabilized:
		return []ID{e.Creature}, true
	case CreatureDied:
		return []ID{e.Creature}, true
	case HealingReceived:
		return []ID{e.Creature}, true
	case StoodUp:
		return []ID{e.Creature}, true
	}
	return nil, false
}

// dimFor reports whether p is Dim Light (Lightly Obscured) for c.
func (s *Session) dimFor(c *creature, p board.Pos) bool { return s.effectiveLight(c, p) == Dim }

// searchTargets reports whether the undiscovered traps and secret doors c sees
// lie on Dim Light squares, and whether any lie elsewhere.
func (s *Session) searchTargets(c *creature) (dim, lit bool) {
	check := func(p board.Pos) {
		if s.seesPos(c, p) {
			if s.dimFor(c, p) {
				dim = true
			} else {
				lit = true
			}
		}
	}
	for _, id := range s.trapOrder {
		if t := s.traps[id]; !t.found {
			check(t.Pos)
		}
	}
	for _, id := range s.objectOrder {
		if o := s.objects[id]; o.Secret && !o.found {
			check(o.Pos)
		}
	}
	return dim, lit
}
