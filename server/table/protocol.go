package table

import (
	"github.com/mvgv/dndboard/server/session"
)

// The wire protocol between the server and the client (ADR 0004), as JSON over
// WebSocket. The client only renders what it receives and sends Intents back.

// Message is sent from the server to clients.
//
//   - "welcome": to a client that joins: the Board, the events so far, the
//     visible state and legal actions.
//   - "update": to every client after an intent resolves: the new events,
//     the visible state and legal actions.
//   - "rejected": only to the client whose intent or message was refused.
type Message struct {
	Type   string  `json:"type"`
	Board  *Board  `json:"board,omitempty"`
	Events []Event `json:"events,omitempty"`
	State  *State  `json:"state,omitempty"`
	// Legal is the active creature's legal actions, present only while a
	// Player Character is active, the combat goes on and no Reaction Prompt
	// is pending.
	Legal  *Legal `json:"legal,omitempty"`
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Board is the grid as text rows: '.' floor, '#' wall, '~' Difficult Terrain.
type Board struct {
	Width  int      `json:"width"`
	Height int      `json:"height"`
	Rows   []string `json:"rows"`
}

// Square is a position on the Board.
type Square struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Creature is what every client sees of a creature.
type Creature struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Team            string `json:"team"`
	CreatureType    string `json:"creature_type"`
	PlayerCharacter bool   `json:"player_character"`
	X               int    `json:"x"`
	Y               int    `json:"y"`
	HP              int    `json:"hp"`
	MaxHP           int    `json:"max_hp"`
	AC              int    `json:"ac"`
	Down            bool   `json:"down"`
	Gone            bool   `json:"gone"`
}

// State is the visible state of the combat.
type State struct {
	Round     int        `json:"round"`
	Active    string     `json:"active"`
	Order     []string   `json:"order"`
	Creatures []Creature `json:"creatures"`
	Over      bool       `json:"over"`
	Winner    string     `json:"winner,omitempty"`
	// Pending is the Reaction Prompt the game is paused on, if any.
	Pending *Pending `json:"pending,omitempty"`
}

// Pending is an unanswered Reaction Prompt: Creature may make an Opportunity
// Attack against Target, with RemainingS seconds left to answer.
type Pending struct {
	Creature   string `json:"creature"`
	Target     string `json:"target"`
	RemainingS int    `json:"remaining_s"`
}

// Move is a reachable square and its movement cost.
type Move struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	CostFt int `json:"cost_ft"`
}

// Target is a valid target for an attack.
type Target struct {
	Target string `json:"target"`
	Weapon string `json:"weapon"`
}

// Legal is the active creature's legal actions.
type Legal struct {
	Creature       string   `json:"creature"`
	Action         bool     `json:"action"`
	BonusAction    bool     `json:"bonus_action"`
	Reaction       bool     `json:"reaction"`
	MovementLeftFt int      `json:"movement_left_ft"`
	Moves          []Move   `json:"moves"`
	Attacks        []Target `json:"attacks"`
	Dash           bool     `json:"dash"`
	Disengage      bool     `json:"disengage"`
	NimbleEscape   bool     `json:"nimble_escape"`
	Leave          bool     `json:"leave"`
	EndTurn        bool     `json:"end_turn"`
}

// Event is one session event, flattened. Type names the event; only the fields
// that event uses are set. Hidden marks a roll made behind the screen: a
// monster's dice faces and modifiers are left out and only the result is sent.
type Event struct {
	Type     string  `json:"type"`
	Creature string  `json:"creature,omitempty"`
	Attacker string  `json:"attacker,omitempty"`
	Target   string  `json:"target,omitempty"`
	Round    int     `json:"round,omitempty"`
	From     *Square `json:"from,omitempty"`
	To       *Square `json:"to,omitempty"`
	CostFt   int     `json:"cost_ft,omitempty"`
	ExtraFt  int     `json:"extra_ft,omitempty"`
	Weapon   string  `json:"weapon,omitempty"`
	Hidden   bool    `json:"hidden,omitempty"`
	// Dice: D20 for attack rolls and Initiative, Rolls for damage dice.
	D20      *int   `json:"d20,omitempty"`
	Modifier *int   `json:"modifier,omitempty"`
	Total    *int   `json:"total,omitempty"`
	AC       *int   `json:"ac,omitempty"`
	Hit      *bool  `json:"hit,omitempty"`
	Critical *bool  `json:"critical,omitempty"`
	Rolls    []int  `json:"rolls,omitempty"`
	Sides    *int   `json:"dice_sides,omitempty"` // of each damage die
	Bonus    *int   `json:"bonus,omitempty"`
	Amount   *int   `json:"amount,omitempty"`
	Damage   string `json:"damage_type,omitempty"`
	HPAfter  *int   `json:"hp_after,omitempty"`
	// BonusAction: Disengage taken as a Bonus Action (Nimble Escape).
	BonusAction bool   `json:"bonus_action,omitempty"`
	Winner      string `json:"winner,omitempty"`
	// TimeoutS: seconds a Reaction Prompt waits for its answer.
	TimeoutS int `json:"timeout_s,omitempty"`
}

// Intent is sent from a client: what a Player asks a Player Character to do.
// Kind is one of "move" (To), "attack" (Target), "dash", "disengage",
// "nimble_escape", "leave" or "end_turn".
type Intent struct {
	Kind   string  `json:"intent"`
	Actor  string  `json:"actor"`
	To     *Square `json:"to,omitempty"`
	Target string  `json:"target,omitempty"`
}

// ClientMessage is the envelope of everything a client sends; "intent" is the
// only type so far.
type ClientMessage struct {
	Type string `json:"type"`
	Intent
}

// sessionIntent converts a wire intent into a session intent.
func (in Intent) sessionIntent() (session.Intent, error) {
	a := session.ID(in.Actor)
	switch in.Kind {
	case "move":
		if in.To == nil {
			return nil, &session.Rejection{Code: session.NoSuchOption, Reason: "move needs a square"}
		}
		return session.Move{Actor: a, To: pos(*in.To)}, nil
	case "attack":
		return session.Attack{Actor: a, Target: session.ID(in.Target)}, nil
	case "dash":
		return session.Dash{Actor: a}, nil
	case "disengage":
		return session.Disengage{Actor: a}, nil
	case "nimble_escape":
		return session.NimbleEscape{Actor: a}, nil
	case "leave":
		return session.Leave{Actor: a}, nil
	case "end_turn":
		return session.EndTurn{Actor: a}, nil
	}
	return nil, &session.Rejection{Code: session.NoSuchOption, Reason: "unknown intent " + quote(in.Kind)}
}

func quote(s string) string {
	const max = 32
	if len(s) > max {
		s = s[:max] + "…"
	}
	return `"` + s + `"`
}

// wireEvents flattens session events. pc reports whether a creature is a
// Player Character, whose rolls everyone sees; other creatures' rolls are
// hidden. sides gives the damage die of a creature's attack.
func wireEvents(evs []session.Event, pc func(session.ID) bool, sides func(session.ID) int) []Event {
	out := make([]Event, 0, len(evs))
	var roller session.ID // the attacker whose damage follows
	for _, e := range evs {
		switch e := e.(type) {
		case session.InitiativeRolled:
			w := Event{Type: "initiative_rolled", Creature: string(e.Creature)}
			if pc(e.Creature) {
				w.D20, w.Total = new(e.D20), new(e.Total)
			} else {
				w.Hidden = true
			}
			out = append(out, w)
		case session.TurnStarted:
			out = append(out, Event{Type: "turn_started", Creature: string(e.Creature), Round: e.Round})
		case session.Moved:
			out = append(out, Event{Type: "moved", Creature: string(e.Creature),
				From: new(square(e.From)), To: new(square(e.To)), CostFt: e.CostFt})
		case session.AttackRolled:
			roller = e.Attacker
			w := Event{Type: "attack_rolled", Attacker: string(e.Attacker), Target: string(e.Target),
				Weapon: e.Weapon, Hit: new(e.Hit), Critical: new(e.Critical)}
			if pc(e.Attacker) {
				w.D20, w.Modifier, w.Total, w.AC = new(e.D20), new(e.Modifier), new(e.Total), new(e.AC)
			} else {
				w.Hidden = true
			}
			out = append(out, w)
		case session.DamageDealt:
			w := Event{Type: "damage_dealt", Target: string(e.Target), Amount: new(e.Amount),
				Damage: e.Type, HPAfter: new(e.HPAfter)}
			if pc(roller) {
				w.Rolls, w.Sides, w.Bonus = e.Rolls, new(sides(roller)), new(e.Bonus)
			} else {
				w.Hidden = true
			}
			out = append(out, w)
		case session.CreatureDowned:
			out = append(out, Event{Type: "creature_downed", Creature: string(e.Creature)})
		case session.TurnEnded:
			out = append(out, Event{Type: "turn_ended", Creature: string(e.Creature)})
		case session.CombatEnded:
			out = append(out, Event{Type: "combat_ended", Winner: string(e.Winner)})
		case session.Dashed:
			out = append(out, Event{Type: "dashed", Creature: string(e.Creature), ExtraFt: e.ExtraFt})
		case session.Disengaged:
			out = append(out, Event{Type: "disengaged", Creature: string(e.Creature), BonusAction: e.Bonus})
		case session.OpportunityAttackMade:
			out = append(out, Event{Type: "opportunity_attack", Attacker: string(e.Attacker), Target: string(e.Target)})
		case session.CreatureLeft:
			out = append(out, Event{Type: "creature_left", Creature: string(e.Creature)})
		case session.ReactionPrompted:
			out = append(out, Event{Type: "reaction_prompted", Creature: string(e.Creature),
				Target: string(e.Target), TimeoutS: int(e.Timeout.Seconds())})
		case session.ReactionDeclined:
			out = append(out, Event{Type: "reaction_declined", Creature: string(e.Creature)})
		case session.ReactionTimedOut:
			out = append(out, Event{Type: "reaction_timed_out", Creature: string(e.Creature)})
		}
	}
	return out
}

func wireState(st session.State, pc func(session.ID) bool) *State {
	w := &State{Round: st.Round, Active: string(st.Active), Over: st.Over, Winner: string(st.Winner)}
	if p := st.Pending; p != nil {
		w.Pending = &Pending{Creature: string(p.Creature), Target: string(p.Target),
			RemainingS: int(p.Remaining.Seconds())}
	}
	for _, id := range st.Order {
		c := st.Creatures[id]
		w.Order = append(w.Order, string(id))
		w.Creatures = append(w.Creatures, Creature{
			ID: string(id), Name: c.Name, Team: string(c.Team), CreatureType: c.Type,
			PlayerCharacter: pc(id), X: c.Pos.X, Y: c.Pos.Y, HP: c.HP, MaxHP: c.MaxHP,
			AC: c.AC, Down: c.Down, Gone: c.Gone,
		})
	}
	return w
}

func wireLegal(la session.LegalActions) *Legal {
	w := &Legal{
		Creature: string(la.Creature), Action: la.Action, BonusAction: la.BonusAction,
		Reaction: la.Reaction, MovementLeftFt: la.MovementLeftFt,
		Moves: []Move{}, Attacks: []Target{},
		Dash: la.Dash, Disengage: la.Disengage, NimbleEscape: la.NimbleEscape,
		Leave: la.Leave, EndTurn: la.EndTurn,
	}
	for _, m := range la.Moves {
		w.Moves = append(w.Moves, Move{X: m.To.X, Y: m.To.Y, CostFt: m.CostFt})
	}
	for _, a := range la.Attacks {
		w.Attacks = append(w.Attacks, Target{Target: string(a.Target), Weapon: a.Weapon})
	}
	return w
}
