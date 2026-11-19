// Package tactics is how monsters take their turns on their own: a Brain looks
// at what everyone can see (the session's visible state and the active
// creature's Legal Actions) and picks an Intent. The rules engine stays the sole
// mechanical authority (ADR 0001): a brain only ever proposes, and an intent
// that is not among the Legal Actions is never submitted.
package tactics

import (
	"fmt"
	"slices"

	"github.com/mvgv/dndboard/server/session"
)

// Brain picks the next Intent of the active creature. ok is false when it has
// no opinion, which lets a Chain defer to the next brain. A brain must not use
// randomness beyond what its inputs hold, so the same state gives the same
// choice (the dice belong to the rules engine).
type Brain interface {
	Choose(st session.State, la session.LegalActions) (in session.Intent, ok bool)
}

// Chain tries the brains in order and returns the first legal choice, so a
// smarter brain (say, a language-model Narrator's) can sit in front of the
// deterministic one and fall back to it when it declines or proposes something
// illegal.
func Chain(brains ...Brain) Brain { return chain(brains) }

type chain []Brain

func (c chain) Choose(st session.State, la session.LegalActions) (session.Intent, bool) {
	for _, b := range c {
		if in, ok := b.Choose(st, la); ok && Legal(la, in) {
			return in, true
		}
	}
	return nil, false
}

// maxSteps bounds one turn; no sensible turn needs more intents than this.
const maxSteps = 64

// PlayTurn plays the active creature's whole turn with the brain, submitting
// intents until the turn passes to someone else, the combat ends, or the game
// pauses on a Reaction Prompt (the caller answers it and may call PlayTurn
// again to carry on). Whenever the brain has no legal choice the turn ends. It
// returns the events of the turn.
func PlayTurn(s *session.Session, b Brain) ([]session.Event, error) {
	who := s.State().Active
	var out []session.Event
	submit := func(in session.Intent) error {
		ev, err := s.Submit(in)
		out = append(out, ev...)
		return err
	}
	for range maxSteps {
		st := s.State()
		if st.Over || st.Active != who || st.Pending != nil {
			return out, nil
		}
		la := s.LegalActions()
		in, ok := b.Choose(st, la)
		if !ok || !Legal(la, in) {
			in = session.EndTurn{Actor: who}
		}
		if err := submit(in); err != nil {
			return out, fmt.Errorf("tactics: %s's %T was rejected: %w", who, in, err)
		}
	}
	if st := s.State(); !st.Over && st.Active == who && st.Pending == nil {
		if err := submit(session.EndTurn{Actor: who}); err != nil {
			return out, err
		}
	}
	return out, nil
}

// Legal reports whether the intent is one of the Legal Actions. It is the check
// that keeps every brain, however clever, inside the rules.
func Legal(la session.LegalActions, in session.Intent) bool {
	if in == nil {
		return false
	}
	switch in := in.(type) {
	case session.Move:
		return in.Actor == la.Creature && hasMove(la, in)
	case session.Attack:
		return in.Actor == la.Creature && contains(la.Attacks, func(o session.AttackOption) bool { return o.Target == in.Target })
	case session.Dash:
		return in.Actor == la.Creature && la.Dash
	case session.Disengage:
		return in.Actor == la.Creature && la.Disengage
	case session.NimbleEscape:
		return in.Actor == la.Creature && la.NimbleEscape
	case session.Dodge:
		return in.Actor == la.Creature && la.Dodge
	case session.Hide:
		return in.Actor == la.Creature && la.Hide
	case session.Search:
		return in.Actor == la.Creature && la.Search
	case session.Ready:
		return in.Actor == la.Creature && la.Ready
	case session.Help:
		return in.Actor == la.Creature && contains(la.HelpAttack, func(id session.ID) bool { return id == in.Target })
	case session.HelpCheck:
		return in.Actor == la.Creature && contains(la.HelpCheck, func(id session.ID) bool { return id == in.Ally })
	case session.Utilize:
		return in.Actor == la.Creature && contains(la.Utilize, func(o session.UtilizeOption) bool { return o.Object == in.Object })
	case session.Stand:
		return in.Actor == la.Creature && la.Stand
	case session.Stabilize:
		return in.Actor == la.Creature && contains(la.Stabilize, func(id session.ID) bool { return id == in.Target })
	case session.Leave:
		return in.Actor == la.Creature && la.Leave
	case session.EndTurn:
		return in.Actor == la.Creature && la.EndTurn
	}
	return false
}

func hasMove(la session.LegalActions, m session.Move) bool {
	return contains(la.Moves, func(o session.MoveOption) bool { return o.To == m.To })
}

func contains[T any](xs []T, f func(T) bool) bool { return slices.ContainsFunc(xs, f) }
