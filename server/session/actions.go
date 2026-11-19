package session

import (
	"fmt"
	"slices"

	"github.com/mvgv/dndboard/server/board"
)

// The standard actions beyond Attack, Dash and Disengage (SRD 5.2 "Rules
// Glossary": Dodge, Help, Hide, Ready, Search, Utilize). Rulings where the text
// leaves room are listed in docs/adr or the pull request that introduced them:
//   - Help (ability check) accepts only the skills the session can roll
//     (Perception for Search, Stealth for Hide) and reaches allies within 5 feet.
//   - Hide needs a Heavily Obscured square; Cover is not modeled. Hidden
//     creatures can't be targeted, are not seen by Opportunity Attacks or Ready
//     triggers, and make attack rolls with Advantage. Only attacking or being
//     found by Search ends hiding (sounds and Verbal spells are not modeled).
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

type object struct{ ObjectSpec }

// rollD20 rolls a d20 test; Advantage and Disadvantage cancel out.
func (s *Session) rollD20(adv, dis bool) (int, RollMode) {
	d := s.dice.Roll(20)
	switch {
	case adv == dis:
		return d, Normal
	case adv:
		return max(d, s.dice.Roll(20)), Advantage
	}
	return min(d, s.dice.Roll(20)), Disadvantage
}

// rollCheck rolls an ability check for c, spending a Help it was given for skill.
func (s *Session) rollCheck(c *creature, skill string) (d20 int, mode RollMode) {
	adv := false
	for i, h := range s.helpChecks {
		if h.ally == c.ID && h.skill == skill {
			s.helpChecks = slices.Delete(s.helpChecks, i, i+1)
			adv = true
			break
		}
	}
	return s.rollD20(adv, false)
}

// consumeHelpAttack spends one Help given to c's team against t, if any.
func (s *Session) consumeHelpAttack(c, t *creature) bool {
	for i, h := range s.helpAttacks {
		if h.target == t.ID && s.creatures[h.helper].Team == c.Team {
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
		case t.out() || t.hidden:
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
		d20, mode := s.rollCheck(c, Stealth)
		total := d20 + c.StealthMod
		ok := total >= hideDC
		if ok {
			c.hidden, c.hideDC = true, total
		}
		out = []Event{Hid{Creature: c.ID, D20: d20, Modifier: c.StealthMod, Total: total, Mode: mode, Hidden: ok}}
	case Search:
		d20, mode := s.rollCheck(c, Perception)
		total := d20 + c.PerceptionMod
		var found []ID
		for _, id := range s.order {
			if o := s.creatures[id]; o.Team != c.Team && o.hidden && total >= o.hideDC {
				o.hidden = false
				found = append(found, id)
			}
		}
		out = []Event{Searched{Creature: c.ID, D20: d20, Modifier: c.PerceptionMod, Total: total, Mode: mode, Found: found}}
	case Utilize:
		o, ok := s.objects[in.Object]
		if !ok {
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
		if _, ok := s.objects[in.Response.Object]; !ok {
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
	if mover.hidden {
		return nil // nobody perceives it
	}
	var out []Event
	for _, id := range s.order {
		r := s.creatures[id]
		if s.over || mover.out() {
			break
		}
		if r.ready == nil || r.Team == mover.Team || r.out() || r.reactionUsed {
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
			cost, ok := s.reachable(r, r.SpeedFt)[resp.To]
			if !ok {
				fizzle("%v is not reachable", resp.To)
				continue
			}
			ev, _ := s.relocate(r, resp.To, cost, false)
			out = append(out, ev...)
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
		switch sp.Kind {
		case Door, Chest:
			if wall {
				return fmt.Errorf("session: %s %q is on a wall at %v", sp.Kind, sp.ID, sp.Pos)
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
		s.objects[sp.ID] = &object{sp}
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
	case "pull":
		o.Open = !o.Open
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
	if s.acted {
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
		} else if !o.hidden {
			la.HelpAttack = append(la.HelpAttack, id)
		}
	}
	for _, id := range s.objectOrder {
		o := s.objects[id]
		if board.Distance(c.Pos, o.Pos) > 1 {
			continue
		}
		if e := s.utilizeEffect(o); e != "" {
			la.Utilize = append(la.Utilize, UtilizeOption{Object: id, Effect: e})
		}
	}
}
