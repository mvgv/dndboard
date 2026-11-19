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
//     minus Exhaustion. It is checked at session start and whenever anything
//     moves or a Door changes, for Player Characters only, against every
//     hidden trap and secret door whose square the character sees.
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

// Discovered: a hidden trap or secret door was found, by passive Perception or
// by a Search.
type Discovered struct {
	Finder  ID
	Kind    string // "trap" or "secret door"
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
	if s.effectiveLight(c, p) == Dim || c.checkDisadvantage() {
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
			out = append(out, Discovered{Finder: c.ID, Kind: "trap", ID: id, Pos: t.Pos, Passive: passive})
		}
	}
	for _, id := range s.objectOrder {
		o := s.objects[id]
		if o.Secret && !o.found && s.seesPos(c, o.Pos) && found(o.Pos, o.DC) {
			o.found = true
			out = append(out, Discovered{Finder: c.ID, Kind: "secret door", ID: id, Pos: o.Pos, Passive: passive})
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
	v := View{Viewer: viewer, Round: s.round, Active: s.active().ID, Over: s.over, Winner: s.winner,
		Creatures: map[ID]CreatureState{}, Objects: map[ID]ObjectState{}, Traps: map[ID]TrapState{}}
	c, ok := s.creatures[viewer]
	if !ok {
		return v
	}
	full := s.State()
	for id, o := range s.creatures {
		if s.sees(c, o) {
			v.Creatures[id] = full.Creatures[id]
		}
	}
	for id, o := range s.objects {
		if o.known() && s.seesPos(c, o.Pos) {
			v.Objects[id] = full.Objects[id]
		}
	}
	// Objects and traps once found stay known even out of sight.
	for id, o := range s.objects {
		if o.Secret && o.found {
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

// EventsFor filters events for a Player Character's client: an event about a
// creature the viewer does not perceive is dropped, and discoveries are only
// sent to the party that made them.
func (s *Session) EventsFor(viewer ID, evs []Event) []Event {
	c, ok := s.creatures[viewer]
	if !ok {
		return nil
	}
	seen := func(id ID) bool {
		o, ok := s.creatures[id]
		return !ok || s.sees(c, o)
	}
	var out []Event
	for _, orig := range evs {
		keep, ev := true, orig
		switch e := orig.(type) {
		case Discovered:
			keep = seen(e.Finder)
		case Moved:
			keep = seen(e.Creature)
		case Hid:
			keep = seen(e.Creature)
		case InitiativeRolled:
			keep = seen(e.Creature)
		case TurnStarted:
			keep = seen(e.Creature)
		case TurnEnded:
			keep = seen(e.Creature)
		case Searched:
			keep = seen(e.Creature)
			e.Found = slices.DeleteFunc(slices.Clone(e.Found), func(id ID) bool { return !seen(id) })
			ev = e
		case AttackRolled:
			keep = seen(e.Attacker) || seen(e.Target)
		case DamageDealt:
			keep = seen(e.Target)
		case CreatureDowned:
			keep = seen(e.Creature)
		case Dashed:
			keep = seen(e.Creature)
		case Disengaged:
			keep = seen(e.Creature)
		case Dodged:
			keep = seen(e.Creature)
		case Helped:
			keep = seen(e.Creature)
		case HelpedCheck:
			keep = seen(e.Creature)
		case Readied:
			keep = seen(e.Creature)
		case ReadyTriggered:
			keep = seen(e.Creature)
		case ReadyFizzled:
			keep = seen(e.Creature)
		case ReadyLapsed:
			keep = seen(e.Creature)
		case OpportunityAttackMade:
			keep = seen(e.Attacker) || seen(e.Target)
		case CreatureLeft:
			keep = seen(e.Creature)
		case ObjectUsed:
			keep = seen(e.Creature)
		}
		if keep {
			out = append(out, ev)
		}
	}
	return out
}
