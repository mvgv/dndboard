package table

import (
	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/session"
)

// Approach is a stand-in Brain until the deterministic tactical brain (#10):
// it attacks the first valid target, preferring one still standing over one
// at 0 Hit Points, otherwise moves to the reachable square closest to a
// hostile creature, otherwise ends its turn. It only ever picks from the legal
// actions.
type Approach struct{}

func (Approach) Choose(st session.State, la session.LegalActions) session.Intent {
	me := la.Creature
	if len(la.Attacks) > 0 {
		target := la.Attacks[0].Target
		for _, a := range la.Attacks {
			if !st.Creatures[a.Target].Down {
				target = a.Target
				break
			}
		}
		return session.Attack{Actor: me, Target: target}
	}
	self := st.Creatures[me]
	nearest := func(p board.Pos) int {
		best := -1
		for _, id := range st.Order {
			c := st.Creatures[id]
			if c.Team == self.Team || c.Down || c.Gone {
				continue
			}
			if d := board.Distance(p, c.Pos); best < 0 || d < best {
				best = d
			}
		}
		return best
	}
	best, to := nearest(self.Pos), board.Pos{}
	moved := false
	for _, m := range la.Moves { // cheapest first, so ties keep the shorter move
		if d := nearest(m.To); d < best {
			best, to, moved = d, m.To, true
		}
	}
	if moved {
		return session.Move{Actor: me, To: to}
	}
	return session.EndTurn{Actor: me}
}
