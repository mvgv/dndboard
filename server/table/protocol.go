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
	// Dead: died outright or failed three Death Saving Throws.
	Dead bool `json:"dead"`
	// Stable: at 0 Hit Points without making Death Saving Throws.
	Stable             bool `json:"stable"`
	DeathSaveSuccesses int  `json:"death_save_successes"`
	DeathSaveFailures  int  `json:"death_save_failures"`
	// Conditions, including implied ones, sorted by name.
	Conditions      []string `json:"conditions"`
	ExhaustionLevel int      `json:"exhaustion_level"`
	Gone            bool     `json:"gone"`
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
	// Stand rights a Prone creature; Stabilize lists Dying creatures within 5 feet.
	Stand        bool     `json:"stand"`
	Stabilize    []string `json:"stabilize"`
	NimbleEscape bool     `json:"nimble_escape"`
	Leave        bool     `json:"leave"`
	EndTurn      bool     `json:"end_turn"`
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
	// Dice: D20 for D20 Tests and Initiative, Rolls for damage dice. Mode is
	// "advantage" or "disadvantage" when one applied.
	D20      *int   `json:"d20,omitempty"`
	Modifier *int   `json:"modifier,omitempty"`
	Total    *int   `json:"total,omitempty"`
	Mode     string `json:"mode,omitempty"`
	AC       *int   `json:"ac,omitempty"`
	Hit      *bool  `json:"hit,omitempty"`
	Critical *bool  `json:"critical,omitempty"`
	Rolls    []int  `json:"rolls,omitempty"`
	Sides    *int   `json:"dice_sides,omitempty"` // of each damage die
	Bonus    *int   `json:"bonus,omitempty"`
	// Raw is the damage before Resistance, Vulnerability and Immunity;
	// Adjustment names the one that applied.
	Raw        *int   `json:"raw,omitempty"`
	Amount     *int   `json:"amount,omitempty"`
	Damage     string `json:"damage_type,omitempty"`
	Adjustment string `json:"adjustment,omitempty"`
	HPAfter    *int   `json:"hp_after,omitempty"`
	// Saving throws and ability checks.
	Ability  string `json:"ability,omitempty"`
	DC       *int   `json:"dc,omitempty"`
	Success  *bool  `json:"success,omitempty"`
	AutoFail bool   `json:"auto_fail,omitempty"`
	// Death Saving Throw tallies after the event.
	Successes int `json:"successes,omitempty"`
	Failures  int `json:"failures,omitempty"`
	// Conditions: Level is the Exhaustion level after the change; Source names
	// the charmer, the source of fear or the grappler.
	Condition string `json:"condition,omitempty"`
	Level     int    `json:"level,omitempty"`
	Source    string `json:"source,omitempty"`
	// BonusAction: Disengage taken as a Bonus Action (Nimble Escape).
	BonusAction bool   `json:"bonus_action,omitempty"`
	Winner      string `json:"winner,omitempty"`
	// TimeoutS: seconds a Reaction Prompt waits for its answer.
	TimeoutS int `json:"timeout_s,omitempty"`
}

// Intent is sent from a client: what a Player asks a Player Character to do.
// Kind is one of "move" (To), "attack" (Target), "dash", "disengage",
// "stand", "stabilize" (Target), "nimble_escape", "leave" or "end_turn".
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
	case "stand":
		return session.Stand{Actor: a}, nil
	case "stabilize":
		return session.Stabilize{Actor: a, Target: session.ID(in.Target)}, nil
	case "nimble_escape":
		return session.NimbleEscape{Actor: a}, nil
	case "leave":
		return session.Leave{Actor: a}, nil
	case "end_turn":
		return session.EndTurn{Actor: a}, nil
	}
	return nil, &session.Rejection{Code: session.NoSuchOption, Reason: "unknown intent " + quote(in.Kind)}
}

// quote quotes client text for a rejection reason, cut to a few characters.
func quote(s string) string {
	const maxRunes = 32
	if r := []rune(s); len(r) > maxRunes {
		s = string(r[:maxRunes]) + "…"
	}
	return `"` + s + `"`
}

// wireEvents flattens session events. pc reports whether a creature is a
// Player Character, whose rolls everyone sees; other creatures' rolls are
// hidden. sides gives the damage die of a creature's attack.
func wireEvents(evs []session.Event, pc func(session.ID) bool, sides func(session.ID) int) []Event {
	out := make([]Event, 0, len(evs))
	// roller is the attacker of the event just before: an attack's damage
	// follows its attack roll at once, and damage after anything else (an
	// Effect) has no roller.
	var roller session.ID
	for _, e := range evs {
		attacker := roller
		roller = ""
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
				w.Mode = string(e.Mode)
			} else {
				w.Hidden = true
			}
			out = append(out, w)
		case session.DamageDealt:
			// The adjustment is the target's, seen in play; the dice are the roller's.
			w := Event{Type: "damage_dealt", Target: string(e.Target), Amount: new(e.Amount),
				Damage: e.Type, Adjustment: e.Adjustment, HPAfter: new(e.HPAfter)}
			if pc(attacker) {
				w.Rolls, w.Sides, w.Bonus, w.Raw = e.Rolls, new(sides(attacker)), new(e.Bonus), new(e.Raw)
			} else {
				w.Hidden = true
			}
			out = append(out, w)
		case session.SaveRolled:
			out = append(out, d20Test("save_rolled", e.Creature, e.Ability, e.D20, e.Modifier, e.Total,
				e.DC, e.Success, e.AutoFail, e.Mode, pc))
		case session.CheckRolled:
			out = append(out, d20Test("check_rolled", e.Creature, e.Ability, e.D20, e.Modifier, e.Total,
				e.DC, e.Success, e.AutoFail, e.Mode, pc))
		case session.DeathSaveRolled:
			w := Event{Type: "death_save_rolled", Creature: string(e.Creature),
				Successes: e.Successes, Failures: e.Failures}
			if pc(e.Creature) {
				w.D20 = new(e.D20)
			} else {
				w.Hidden = true
			}
			out = append(out, w)
		case session.DeathSaveFailed:
			out = append(out, Event{Type: "death_save_failed", Creature: string(e.Creature),
				Successes: e.Successes, Failures: e.Failures})
		case session.CreatureStabilized:
			out = append(out, Event{Type: "creature_stabilized", Creature: string(e.Creature)})
		case session.CreatureDied:
			out = append(out, Event{Type: "creature_died", Creature: string(e.Creature)})
		case session.HealingReceived:
			out = append(out, Event{Type: "healing_received", Creature: string(e.Creature),
				Amount: new(e.Amount), HPAfter: new(e.HPAfter)})
		case session.StoodUp:
			out = append(out, Event{Type: "stood_up", Creature: string(e.Creature), CostFt: e.CostFt})
		case session.ConditionApplied:
			out = append(out, Event{Type: "condition_applied", Creature: string(e.Creature),
				Condition: string(e.Condition), Source: string(e.Source), Level: e.Level})
		case session.ConditionRemoved:
			out = append(out, Event{Type: "condition_removed", Creature: string(e.Creature),
				Condition: string(e.Condition), Level: e.Level})
		case session.ConditionResisted:
			out = append(out, Event{Type: "condition_resisted", Creature: string(e.Creature),
				Condition: string(e.Condition)})
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

// d20Test flattens a saving throw or ability check; a Hidden one keeps only
// its outcome.
func d20Test(typ string, who session.ID, ab session.Ability, d20, mod, total, dc int,
	success, autoFail bool, mode session.RollMode, pc func(session.ID) bool) Event {
	w := Event{Type: typ, Creature: string(who), Ability: string(ab), Success: new(success), AutoFail: autoFail}
	if pc(who) {
		w.D20, w.Modifier, w.Total, w.DC, w.Mode = new(d20), new(mod), new(total), new(dc), string(mode)
	} else {
		w.Hidden = true
	}
	return w
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
		conds := make([]string, 0, len(c.Conditions))
		for _, k := range c.Conditions {
			conds = append(conds, string(k))
		}
		w.Creatures = append(w.Creatures, Creature{
			ID: string(id), Name: c.Name, Team: string(c.Team), CreatureType: c.Type,
			PlayerCharacter: pc(id), X: c.Pos.X, Y: c.Pos.Y, HP: c.HP, MaxHP: c.MaxHP,
			AC: c.AC, Down: c.Down, Dead: c.Dead, Stable: c.Stable,
			DeathSaveSuccesses: c.DeathSaveSuccesses, DeathSaveFailures: c.DeathSaveFailures,
			Conditions: conds, ExhaustionLevel: c.ExhaustionLevel, Gone: c.Gone,
		})
	}
	return w
}

func wireLegal(la session.LegalActions) *Legal {
	w := &Legal{
		Creature: string(la.Creature), Action: la.Action, BonusAction: la.BonusAction,
		Reaction: la.Reaction, MovementLeftFt: la.MovementLeftFt,
		Moves: []Move{}, Attacks: []Target{}, Stabilize: []string{},
		Dash: la.Dash, Disengage: la.Disengage, Stand: la.Stand, NimbleEscape: la.NimbleEscape,
		Leave: la.Leave, EndTurn: la.EndTurn,
	}
	for _, id := range la.Stabilize {
		w.Stabilize = append(w.Stabilize, string(id))
	}
	for _, m := range la.Moves {
		w.Moves = append(w.Moves, Move{X: m.To.X, Y: m.To.Y, CostFt: m.CostFt})
	}
	for _, a := range la.Attacks {
		w.Attacks = append(w.Attacks, Target{Target: string(a.Target), Weapon: a.Weapon})
	}
	return w
}
