package session

import (
	"fmt"
	"slices"

	"github.com/mvgv/dndboard/server/board"
)

// The standard actions beyond Attack, Dash and Disengage (SRD 5.2 "Rules
// Glossary": Dodge, Help, Hide, Ready, Search, Utilize). Where the text leaves
// room, this comment records the reading taken:
//   - Dodge gives Disadvantage on attacks against the creature and Advantage on
//     its Dexterity saving throws, until the start of its next turn; it is lost
//     while the creature is Incapacitated or has Speed 0. That the creature can
//     see the attacker is assumed.
//   - Help (ability check) accepts only the skills the session can roll
//     (Perception for Search, Stealth for Hide) and reaches allies within 5 feet.
//   - Hide needs a Heavily Obscured square; Cover is not modeled. A hidden
//     creature has the Invisible condition (so attackMode gives it Advantage and
//     its attackers Disadvantage), can't be chosen as a target, is not seen by
//     Opportunity Attacks or Ready triggers. Only attacking or being found by
//     Search ends hiding (sounds and Verbal spells are not modeled).
//   - A Readied response fires on its trigger without asking whether to ignore it.

// DC for a Dexterity (Stealth) check to Hide (SRD glossary, Hide).
const hideDC = 15

type readied struct {
	trigger  ReadyTrigger
	response ReadyResponse
}

type helpAttack struct{ helper, target ID }

type helpCheck struct {
	helper, ally ID
	skill        string
}

type object struct {
	ObjectSpec
	found bool // a Secret door that has been discovered
}

// hidden reports whether the creature is hidden: it has the Invisible condition
// from a successful Hide action.
func (c *creature) hidden() bool { return c.hideDC > 0 && c.own(CondInvisible) }

// dodgeActive: the Dodge benefits are lost while Incapacitated or at Speed 0.
func (c *creature) dodgeActive() bool {
	return c.dodging && !c.incapacitated() && c.speed() > 0
}

// reveal ends hiding: the creature stops being Invisible.
func (s *Session) reveal(c *creature, reason string) []Event {
	c.hideDC = 0
	return append([]Event{Revealed{Creature: c.ID, Reason: reason}}, s.clear(c, CondInvisible)...)
}

// rollCheck rolls an ability check for c, spending a Help it was given for
// skill. Poisoned and Frightened give Disadvantage; Exhaustion lowers the total
// through the caller's modifier (see d20Penalty).
func (s *Session) rollCheck(c *creature, skill string, extraDisadvantage bool) (d20 int, mode RollMode) {
	adv := false
	for i, h := range s.helpChecks {
		if h.ally == c.ID && h.skill == skill {
			s.helpChecks = slices.Delete(s.helpChecks, i, i+1)
			adv = true
			break
		}
	}
	return s.rollD20(adv, c.checkDisadvantage() || extraDisadvantage)
}

// consumeHelpAttack spends one Help given to c's team against t, if any.
func (s *Session) consumeHelpAttack(c, t *creature) bool {
	for i, h := range s.helpAttacks {
		// "One of your allies": the helper itself does not benefit.
		if h.target == t.ID && h.helper != c.ID && s.creatures[h.helper].Team == c.Team {
			s.helpAttacks = slices.Delete(s.helpAttacks, i, i+1)
			return true
		}
	}
	return false
}

// expireHelp drops the Help a creature gave: it lasts until the start of the
// helper's next turn.
func (s *Session) expireHelp(helper ID) {
	s.helpAttacks = slices.DeleteFunc(s.helpAttacks, func(h helpAttack) bool { return h.helper == helper })
	s.helpChecks = slices.DeleteFunc(s.helpChecks, func(h helpCheck) bool { return h.helper == helper })
}

func (s *Session) checkAction() *Rejection {
	if c := s.active(); c.incapacitated() {
		return reject(Incapacitated, "%s is Incapacitated and can't take actions", c.ID)
	}
	if s.acted {
		return reject(ActionUsed, "%s already took an action this turn", s.active().ID)
	}
	return nil
}

func (s *Session) standardAction(in Intent) ([]Event, error) {
	c := s.active()
	if rej := s.checkAction(); rej != nil {
		return nil, rej
	}
	var out []Event
	switch in := in.(type) {
	case Dodge:
		c.dodging = true
		out = []Event{Dodged{Creature: c.ID}}
	case Help:
		t, ok := s.creatures[in.Target]
		switch {
		case !ok:
			return nil, reject(InvalidTarget, "no creature %q", in.Target)
		case t.Team == c.Team:
			return nil, reject(InvalidTarget, "%s is not hostile", t.ID)
		case t.out() || t.hidden():
			return nil, reject(InvalidTarget, "%s is down, gone or can't be seen", t.ID)
		case board.Distance(c.Pos, t.Pos) > 1:
			return nil, reject(OutOfReach, "%s is beyond 5 feet of %s", t.ID, c.ID)
		}
		s.helpAttacks = append(s.helpAttacks, helpAttack{helper: c.ID, target: t.ID})
		out = []Event{Helped{Creature: c.ID, Target: t.ID}}
	case HelpCheck:
		a, ok := s.creatures[in.Ally]
		switch {
		case !ok || a == c || a.Team != c.Team || a.out():
			return nil, reject(InvalidTarget, "%q is not an ally to help", in.Ally)
		case board.Distance(c.Pos, a.Pos) > 1:
			return nil, reject(OutOfReach, "%s is beyond 5 feet of %s", a.ID, c.ID)
		case in.Skill != Perception && in.Skill != Stealth:
			return nil, reject(NoSuchOption, "no check with skill %q to help", in.Skill)
		}
		s.helpChecks = append(s.helpChecks, helpCheck{helper: c.ID, ally: a.ID, skill: in.Skill})
		out = []Event{HelpedCheck{Creature: c.ID, Ally: a.ID, Skill: in.Skill}}
	case Hide:
		if s.board.At(c.Pos) != board.Obscured {
			return nil, reject(NoSuchOption, "%s is not Heavily Obscured, so it can't Hide", c.ID)
		}
		d20, mode := s.rollCheck(c, Stealth, false)
		mod := c.StealthMod - c.d20Penalty()
		total := d20 + mod
		ok := total >= hideDC
		out = []Event{Hid{Creature: c.ID, D20: d20, Modifier: mod, Total: total, Mode: mode, Hidden: ok}}
		if ok {
			// "On a successful check, you have the Invisible condition while
			// hidden"; the total is the DC to find you.
			ev := s.inflict(c, CondInvisible, "")
			if c.own(CondInvisible) {
				c.hideDC = total
			}
			out = append(out, ev...)
		}
	case Search:
		// One Wisdom (Perception) check finds hidden creatures and the traps and
		// secret doors in sight on lit squares. What lies on a Dim Light square
		// is Lightly Obscured, so it takes its own check with Disadvantage.
		dim, lit := s.searchTargets(c)
		ambushers := slices.ContainsFunc(s.order, func(id ID) bool {
			o := s.creatures[id]
			return o.Team != c.Team && o.hidden()
		})
		search := func(dis, dimSquares, creatures bool) {
			d20, mode := s.rollCheck(c, Perception, dis)
			mod := c.PerceptionMod - c.d20Penalty()
			total := d20 + mod
			var found []ID
			var rest []Event
			if creatures {
				for _, id := range s.order {
					if o := s.creatures[id]; o.Team != c.Team && o.hidden() && total >= o.hideDC {
						rest = append(rest, s.reveal(o, "found by Search")...)
						found = append(found, id)
					}
				}
			}
			rest = append(rest, s.discover(c, false, func(p board.Pos, dc int) bool {
				return s.dimFor(c, p) == dimSquares && total >= dc
			})...)
			out = append(out, Searched{Creature: c.ID, D20: d20, Modifier: mod, Total: total, Mode: mode, Found: found})
			out = append(out, rest...)
		}
		if lit || ambushers || !dim {
			search(false, false, true)
		}
		if dim {
			search(true, true, false)
		}
	case Utilize:
		o, ok := s.objects[in.Object]
		if !ok || !o.known() {
			return nil, reject(InvalidTarget, "no object %q", in.Object)
		}
		ev, rej := s.useObject(c, o)
		if rej != nil {
			return nil, rej
		}
		out = ev
	case Ready:
		if err := s.validReady(in); err != nil {
			return nil, err
		}
		c.ready = &readied{trigger: in.Trigger, response: in.Response}
		out = []Event{Readied{Creature: c.ID, Trigger: in.Trigger, Response: in.Response}}
	}
	s.acted = true
	return out, nil
}

func (s *Session) validReady(in Ready) *Rejection {
	switch in.Trigger.Kind {
	case EnemyEntersReach:
	case EnemyEntersSquare:
		if _, ok := s.board.EntryCost(in.Trigger.Pos); !ok {
			return reject(NoSuchOption, "%v is not a square a creature can enter", in.Trigger.Pos)
		}
	default:
		return reject(NoSuchOption, "unknown Ready trigger")
	}
	switch in.Response.Kind {
	case RespondAttack:
	case RespondMove:
		if _, ok := s.board.EntryCost(in.Response.To); !ok {
			return reject(NoSuchOption, "%v is not a square a creature can enter", in.Response.To)
		}
	case RespondUtilize:
		if o, ok := s.objects[in.Response.Object]; !ok || !o.known() {
			return reject(InvalidTarget, "no object %q", in.Response.Object)
		}
	default:
		return reject(NoSuchOption, "unknown Ready response")
	}
	return nil
}

// fireReadied runs the Readied actions that mover's move from a square set off.
// A Reaction is spent whether or not the response can still be carried out.
func (s *Session) fireReadied(mover *creature, from board.Pos) []Event {
	if mover.hidden() {
		return nil // nobody perceives it
	}
	var out []Event
	for _, id := range s.order {
		r := s.creatures[id]
		if s.over || mover.out() || s.pending != nil {
			break
		}
		if r.ready == nil || r.Team == mover.Team || r.out() || r.reactionUsed || r.incapacitated() {
			continue
		}
		t := r.ready.trigger
		switch {
		case t.Kind == EnemyEntersReach && inReach(r, mover.Pos) && !inReach(r, from):
		case t.Kind == EnemyEntersSquare && mover.Pos == t.Pos && from != t.Pos:
		default:
			continue
		}
		resp := r.ready.response
		r.ready, r.reactionUsed = nil, true
		out = append(out, ReadyTriggered{Creature: r.ID, Cause: mover.ID})
		fizzle := func(format string, args ...any) {
			out = append(out, ReadyFizzled{Creature: r.ID, Reason: fmt.Sprintf(format, args...)})
		}
		switch resp.Kind {
		case RespondAttack:
			if !inReach(r, mover.Pos) {
				fizzle("%s is out of reach", mover.ID)
				continue
			}
			out = append(out, s.resolveAttack(r, mover)...)
		case RespondMove:
			if _, ok := s.reachable(r, r.SpeedFt)[resp.To]; !ok {
				fizzle("%v is not reachable", resp.To)
				continue
			}
			m := &pendingMove{mover: r.ID, reaction: true, path: s.pathTo(r, resp.To, r.SpeedFt), startPos: r.Pos}
			out = append(out, s.runMove(m)...)
		case RespondUtilize:
			ev, rej := s.useObject(r, s.objects[resp.Object])
			if rej != nil {
				fizzle("%s", rej.Reason)
				continue
			}
			out = append(out, ev...)
		}
	}
	return out
}

// placeObjects validates and stores the Board objects.
func (s *Session) placeObjects(specs []ObjectSpec) error {
	s.objects = map[ID]*object{}
	for _, sp := range specs {
		if sp.ID == "" {
			return fmt.Errorf("session: object without ID")
		}
		if _, dup := s.objects[sp.ID]; dup {
			return fmt.Errorf("session: duplicate object %q", sp.ID)
		}
		if !s.board.In(sp.Pos) {
			return fmt.Errorf("session: object %q is off the board at %v", sp.ID, sp.Pos)
		}
		for _, o := range s.objects {
			if o.Pos == sp.Pos {
				return fmt.Errorf("session: objects %q and %q share %v", sp.ID, o.ID, sp.Pos)
			}
		}
		wall := s.board.At(sp.Pos) == board.Wall
		if sp.Secret && (sp.Kind != Door || !wall || sp.Open) {
			return fmt.Errorf("session: secret door %q must be a closed door set in a wall at %v", sp.ID, sp.Pos)
		}
		switch sp.Kind {
		case Door, Chest:
			if wall && !sp.Secret {
				return fmt.Errorf("session: %s %q is on a wall at %v", sp.Kind, sp.ID, sp.Pos)
			}
			if sp.Secret {
				// Walkable once found; until then its closed state blocks.
				s.board = s.board.With(sp.Pos, board.Floor)
			}
			for _, c := range s.creatures {
				if c.Pos == sp.Pos && (sp.Kind == Chest || !sp.Open) {
					return fmt.Errorf("session: %s %q shares %v with %q", sp.Kind, sp.ID, sp.Pos, c.ID)
				}
			}
		case Lever:
			if !wall {
				return fmt.Errorf("session: lever %q must be set in a wall, not at %v", sp.ID, sp.Pos)
			}
		default:
			return fmt.Errorf("session: object %q has unknown kind %q", sp.ID, sp.Kind)
		}
		if sp.Controls != "" && sp.Kind != Lever {
			return fmt.Errorf("session: only a lever controls an object, not %q", sp.ID)
		}
		s.objects[sp.ID] = &object{ObjectSpec: sp}
		s.objectOrder = append(s.objectOrder, sp.ID)
	}
	for _, o := range s.objects {
		if o.Controls == "" {
			continue
		}
		if t, ok := s.objects[o.Controls]; !ok || t.Kind != Door {
			return fmt.Errorf("session: lever %q must control a door, not %q", o.ID, o.Controls)
		}
	}
	return nil
}

// blocked reports whether an object fills the square: a closed Door or a Chest.
func (s *Session) blocked(p board.Pos) bool {
	for _, o := range s.objects {
		if o.Pos == p && (o.Kind == Chest || (o.Kind == Door && !o.Open)) {
			return true
		}
	}
	return false
}

func (s *Session) occupied(p board.Pos) bool {
	for _, c := range s.creatures {
		if !c.out() && c.Pos == p {
			return true
		}
	}
	return false
}

func (s *Session) objectStates() map[ID]ObjectState {
	m := map[ID]ObjectState{}
	for id, o := range s.objects {
		m[id] = ObjectState{Kind: o.Kind, Pos: o.Pos, Open: o.Open}
	}
	return m
}

// utilizeEffect is what Utilize would do to o right now, or "" when nothing.
func (s *Session) utilizeEffect(o *object) string {
	switch o.Kind {
	case Door:
		if !o.Open {
			return "open"
		}
		if !s.occupied(o.Pos) {
			return "close"
		}
	case Chest:
		if !o.Open {
			return "open"
		}
	case Lever:
		return "pull"
	}
	return ""
}

// useObject is the Utilize action on a Board object: doors open and close,
// chests open, levers are pulled and toggle the door they control.
func (s *Session) useObject(c *creature, o *object) ([]Event, *Rejection) {
	if board.Distance(c.Pos, o.Pos) > 1 {
		return nil, reject(OutOfReach, "%s is beyond 5 feet of %s", o.ID, c.ID)
	}
	switch s.utilizeEffect(o) {
	case "":
		if o.Kind == Door {
			return nil, reject(ObjectBlocked, "a creature stands in the doorway of %s", o.ID)
		}
		return nil, reject(InvalidTarget, "%s is already open", o.ID)
	default:
		o.Open = !o.Open
	}
	out := []Event{ObjectUsed{Creature: c.ID, Object: o.ID, Kind: o.Kind, Open: o.Open}}
	if t := s.objects[o.Controls]; o.Kind == Lever && t != nil && s.utilizeEffect(t) != "" {
		t.Open = !t.Open
		out = append(out, ObjectChanged{Object: t.ID, Open: t.Open})
	}
	return out, nil
}

// standardActionOptions fills in the legal choices for the unspent action.
func (s *Session) standardActionOptions(c *creature, la *LegalActions) {
	if s.acted || c.incapacitated() {
		return
	}
	la.Dodge, la.Search, la.Ready = true, true, true
	la.Hide = s.board.At(c.Pos) == board.Obscured
	for _, id := range s.order {
		o := s.creatures[id]
		if o == c || o.out() || board.Distance(c.Pos, o.Pos) > 1 {
			continue
		}
		if o.Team == c.Team {
			la.HelpCheck = append(la.HelpCheck, id)
		} else if !o.hidden() {
			la.HelpAttack = append(la.HelpAttack, id)
		}
	}
	for _, id := range s.objectOrder {
		o := s.objects[id]
		if !o.known() || board.Distance(c.Pos, o.Pos) > 1 {
			continue
		}
		if e := s.utilizeEffect(o); e != "" {
			la.Utilize = append(la.Utilize, UtilizeOption{Object: id, Effect: e})
		}
	}
}
