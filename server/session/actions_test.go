package session_test

import (
	"slices"
	"testing"

	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
)

// Scenario tests for the standard actions. Sources are SRD 5.2 "Rules
// Glossary": Dash, Disengage, Dodge, Help, Hide, Ready, Search and Utilize.
// Fighter: AC 16, +5 to hit, longsword d8+3, Speed 30. Goblin Warrior: AC 15,
// 10 HP, +4 to hit, Speed 30. Both reach 5 ft. Every scenario starts with the
// initiative dice 15 (fighter, total 16) and 10 (goblin, total 12), so the
// fighter acts first.

var obscuredRoom = []string{
	"#########",
	"#.%.....#",
	"#.......#",
	"#########",
}

var corridor = []string{
	"#########",
	"#.......#",
	"#########",
}

func startOn(t *testing.T, rows []string, objs []session.ObjectSpec, d dice.Roller, cs ...session.CreatureSpec) *session.Session {
	t.Helper()
	s, err := session.New(session.Config{Board: mustBoard(t, rows), Creatures: cs, Objects: objs, Dice: d})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func scout(id string, x, y int) session.CreatureSpec {
	f := fighter(id, x, y)
	f.StealthMod, f.PerceptionMod = 2, 1
	return f
}

func all[E session.Event](evs []session.Event) []E {
	var out []E
	for _, e := range evs {
		if v, ok := e.(E); ok {
			out = append(out, v)
		}
	}
	return out
}

func utilizeOptions(s *session.Session) []session.UtilizeOption { return s.LegalActions().Utilize }

// "Dash: extra movement for the current turn equal to your Speed."
func TestDashDoublesMovement(t *testing.T) {
	s := start(t, dice.Fixed(15, 10), fighter("fighter", 1, 2), goblin("goblin", 11, 1))
	if la := s.LegalActions(); !la.Dash || la.MovementLeftFt != 30 {
		t.Fatalf("before Dash: dash=%v movement=%d", la.Dash, la.MovementLeftFt)
	}
	far := session.Move{Actor: "fighter", To: pos(10, 3)} // 45 ft away
	if rej := rejection(t, s, far); rej.Code != session.Unreachable {
		t.Fatalf("code = %v, want Unreachable", rej.Code)
	}
	submit(t, s, session.Dash{Actor: "fighter"})
	if la := s.LegalActions(); la.Dash || la.MovementLeftFt != 60 {
		t.Fatalf("after Dash: dash=%v movement=%d, want false and 60", la.Dash, la.MovementLeftFt)
	}
	submit(t, s, far)
	if rej := rejection(t, s, session.Dash{Actor: "fighter"}); rej.Code != session.ActionUsed {
		t.Fatalf("code = %v, want ActionUsed", rej.Code)
	}
}

// "If you take the Disengage action, your movement doesn't provoke Opportunity
// Attacks for the rest of the current turn."
func TestDisengagePreventsOpportunityAttacks(t *testing.T) {
	s := start(t, dice.Fixed(15, 10), fighter("fighter", 1, 2), goblin("goblin", 2, 2))
	submit(t, s, session.Disengage{Actor: "fighter"})
	ev := submit(t, s, session.Move{Actor: "fighter", To: pos(4, 1)})
	if len(all[session.OpportunityAttackMade](ev)) != 0 || len(all[session.Moved](ev)) != 1 {
		t.Fatalf("events = %+v, want a plain move", ev)
	}
}

// Legal actions list each standard action while the action is unspent.
func TestLegalActionsListStandardActions(t *testing.T) {
	s := start(t, dice.Fixed(15, 10), fighter("fighter", 1, 2), goblin("goblin", 2, 2))
	la := s.LegalActions()
	if !la.Dash || !la.Disengage || !la.Dodge || !la.Search || !la.Ready {
		t.Fatalf("standard actions missing: %+v", la)
	}
	if la.Hide {
		t.Fatal("Hide offered outside a Heavily Obscured square")
	}
	if !slices.Equal(la.HelpAttack, []session.ID{"goblin"}) || len(la.HelpCheck) != 0 {
		t.Fatalf("help = %v / %v", la.HelpAttack, la.HelpCheck)
	}
	submit(t, s, session.Dodge{Actor: "fighter"})
	la = s.LegalActions()
	if la.Action || la.Dash || la.Disengage || la.Dodge || la.Search || la.Ready || len(la.HelpAttack) != 0 {
		t.Fatalf("actions offered after the action was spent: %+v", la)
	}
}

// Dodge: "until the start of your next turn, any attack roll made against you
// has Disadvantage".
func TestDodgeGivesAttackersDisadvantageUntilNextTurn(t *testing.T) {
	// Goblin attack: d20s 18 and 3, Disadvantage keeps 3 (total 7 vs AC 16).
	s := start(t, dice.Fixed(15, 10, 18, 3), fighter("fighter", 1, 2), goblin("goblin", 2, 2))
	submit(t, s, session.Dodge{Actor: "fighter"})
	if !s.State().Creatures["fighter"].Dodging {
		t.Fatal("fighter should be Dodging")
	}
	endTurn(t, s)
	ev := submit(t, s, session.Attack{Actor: "goblin", Target: "fighter"})
	a, _ := find[session.AttackRolled](ev)
	if a.Mode != session.Disadvantage || a.D20 != 3 || a.Hit {
		t.Fatalf("attack = %+v, want a Disadvantage miss on 3", a)
	}
	endTurn(t, s)
	if s.State().Creatures["fighter"].Dodging {
		t.Fatal("Dodge should end at the start of the fighter's next turn")
	}
}

// Help (attack): "giving Advantage to the next attack roll by one of your
// allies against that enemy. This benefit expires at the start of your next turn."
func TestHelpGivesAnAllyAdvantageOnTheNextAttack(t *testing.T) {
	// f2 attacks with Advantage: d20s 3 and 12 (total 17 hits AC 15), d8 5.
	// In round 2 the Help is spent, so f2 rolls one die: 2 (total 7 misses).
	s := start(t, dice.Fixed(15, 14, 10, 3, 12, 5, 2),
		fighter("f1", 1, 2), fighter("f2", 1, 3), goblin("goblin", 2, 2))
	submit(t, s, session.Help{Actor: "f1", Target: "goblin"})
	endTurn(t, s)
	ev := submit(t, s, session.Attack{Actor: "f2", Target: "goblin"})
	a, _ := find[session.AttackRolled](ev)
	if a.Mode != session.Advantage || a.D20 != 12 || !a.Hit {
		t.Fatalf("attack = %+v, want an Advantage hit on 12", a)
	}
	endTurn(t, s) // f2
	endTurn(t, s) // goblin
	endTurn(t, s) // f1
	ev = submit(t, s, session.Attack{Actor: "f2", Target: "goblin"})
	if a, _ = find[session.AttackRolled](ev); a.Mode != session.Normal {
		t.Fatalf("second attack mode = %v, want Normal (the Help was spent)", a.Mode)
	}
}

func TestHelpExpiresAtTheStartOfTheHelpersNextTurn(t *testing.T) {
	// f2 does not attack in round 1; in round 2 it rolls a single die.
	s := start(t, dice.Fixed(15, 14, 10, 2),
		fighter("f1", 1, 2), fighter("f2", 1, 3), goblin("goblin", 2, 2))
	submit(t, s, session.Help{Actor: "f1", Target: "goblin"})
	endTurn(t, s) // f1
	endTurn(t, s) // f2
	endTurn(t, s) // goblin; f1's turn starts and its Help lapses
	endTurn(t, s) // f1
	ev := submit(t, s, session.Attack{Actor: "f2", Target: "goblin"})
	if a, _ := find[session.AttackRolled](ev); a.Mode != session.Normal {
		t.Fatalf("mode = %v, want Normal after the Help expired", a.Mode)
	}
}

func TestHelpNeedsAnEnemyWithin5Feet(t *testing.T) {
	s := start(t, dice.Fixed(15, 10), fighter("fighter", 1, 2), goblin("goblin", 5, 2))
	if rej := rejection(t, s, session.Help{Actor: "fighter", Target: "goblin"}); rej.Code != session.OutOfReach {
		t.Fatalf("code = %v, want OutOfReach", rej.Code)
	}
	if rej := rejection(t, s, session.Help{Actor: "fighter", Target: "fighter"}); rej.Code != session.InvalidTarget {
		t.Fatalf("code = %v, want InvalidTarget", rej.Code)
	}
}

// Help (ability check): "That ally has Advantage on the next ability check they
// make with the chosen skill." Dexterity (Stealth) is the check Hide makes.
func TestHelpGivesAnAllyAdvantageOnTheNextCheck(t *testing.T) {
	// f2 Hides on d20s 3 and 14: Advantage keeps 14, +2 = 16 >= DC 15.
	s := startOn(t, obscuredRoom, nil, dice.Fixed(15, 14, 10, 3, 14),
		scout("f1", 1, 1), scout("f2", 2, 1), goblin("goblin", 7, 2))
	if rej := rejection(t, s, session.HelpCheck{Actor: "f1", Ally: "f2", Skill: "Athletics"}); rej.Code != session.NoSuchOption {
		t.Fatalf("code = %v, want NoSuchOption", rej.Code)
	}
	submit(t, s, session.HelpCheck{Actor: "f1", Ally: "f2", Skill: session.Stealth})
	endTurn(t, s)
	ev := submit(t, s, session.Hide{Actor: "f2"})
	h, _ := find[session.Hid](ev)
	if h.Mode != session.Advantage || h.D20 != 14 || h.Total != 16 || !h.Hidden {
		t.Fatalf("hide = %+v", h)
	}
}

// Hide: "a DC 15 Dexterity (Stealth) check while you're Heavily Obscured ...
// you have the Invisible condition while hidden."
func TestHideSucceedsAtDC15AndHidesFromTargeting(t *testing.T) {
	s := startOn(t, obscuredRoom, nil, dice.Fixed(15, 10, 13), scout("fighter", 2, 1), goblin("goblin", 3, 1))
	if !s.LegalActions().Hide {
		t.Fatal("Hide should be legal on a Heavily Obscured square")
	}
	ev := submit(t, s, session.Hide{Actor: "fighter"}) // 13 + 2 = 15
	if h, _ := find[session.Hid](ev); !h.Hidden || h.Total != 15 {
		t.Fatalf("hide = %+v", h)
	}
	if !s.State().Creatures["fighter"].Hidden {
		t.Fatal("fighter should be hidden")
	}
	endTurn(t, s)
	if rej := rejection(t, s, session.Attack{Actor: "goblin", Target: "fighter"}); rej.Code != session.InvalidTarget {
		t.Fatalf("code = %v, want InvalidTarget for an unseen target", rej.Code)
	}
	if got := s.LegalActions().Attacks; len(got) != 0 {
		t.Fatalf("attacks = %v, want none against a hidden creature", got)
	}
}

func TestHideFailsBelowDC15AndSpendsTheAction(t *testing.T) {
	s := startOn(t, obscuredRoom, nil, dice.Fixed(15, 10, 12), scout("fighter", 2, 1), goblin("goblin", 3, 1))
	ev := submit(t, s, session.Hide{Actor: "fighter"}) // 12 + 2 = 14
	if h, _ := find[session.Hid](ev); h.Hidden {
		t.Fatalf("hide = %+v, want a failure", h)
	}
	if s.State().Creatures["fighter"].Hidden || s.LegalActions().Action {
		t.Fatal("a failed Hide leaves the fighter visible with the action spent")
	}
}

func TestHideNeedsAHeavilyObscuredSquare(t *testing.T) {
	s := startOn(t, obscuredRoom, nil, dice.Fixed(15, 10), scout("fighter", 1, 1), goblin("goblin", 7, 2))
	if rej := rejection(t, s, session.Hide{Actor: "fighter"}); rej.Code != session.NoSuchOption {
		t.Fatalf("code = %v, want NoSuchOption", rej.Code)
	}
	if !s.LegalActions().Action {
		t.Fatal("a rejected Hide must not spend the action")
	}
}

// Hide ends "if you make an attack roll"; the Invisible attacker has Advantage.
func TestHiddenAttackerHasAdvantageAndIsRevealed(t *testing.T) {
	// Hide 13 (15); goblin passes; attack d20s 4 and 17 (Advantage), d8 4.
	s := startOn(t, obscuredRoom, nil, dice.Fixed(15, 10, 13, 4, 17, 4), scout("fighter", 2, 1), goblin("goblin", 3, 1))
	submit(t, s, session.Hide{Actor: "fighter"})
	endTurn(t, s)
	endTurn(t, s)
	ev := submit(t, s, session.Attack{Actor: "fighter", Target: "goblin"})
	a, _ := find[session.AttackRolled](ev)
	if a.Mode != session.Advantage || a.D20 != 17 {
		t.Fatalf("attack = %+v, want Advantage keeping 17", a)
	}
	if _, ok := find[session.Revealed](ev); !ok || s.State().Creatures["fighter"].Hidden {
		t.Fatal("attacking should end hiding")
	}
}

// A creature that can't be seen provokes no Opportunity Attack.
func TestHiddenMoverProvokesNoOpportunityAttack(t *testing.T) {
	s := startOn(t, obscuredRoom, nil, dice.Fixed(15, 10, 13), scout("fighter", 2, 1), goblin("goblin", 3, 1))
	submit(t, s, session.Hide{Actor: "fighter"})
	endTurn(t, s)
	endTurn(t, s)
	ev := submit(t, s, session.Move{Actor: "fighter", To: pos(1, 2)}) // leaves the goblin's reach
	if len(all[session.OpportunityAttackMade](ev)) != 0 {
		t.Fatal("a hidden mover must not provoke an Opportunity Attack")
	}
}

// Search: "a Wisdom check to discern something that isn't obvious"; the Search
// table lists Perception for a concealed creature. The hidden creature's
// Stealth total is the DC.
func TestSearchFindsAHiddenCreatureAtItsDC(t *testing.T) {
	// Fighter hides on 14 (+2 = 16). Goblin (+1) searches: 14 -> 15 fails, then 15 -> 16 finds it.
	g := goblin("goblin", 3, 1)
	g.PerceptionMod = 1
	s := startOn(t, obscuredRoom, nil, dice.Fixed(15, 10, 14, 14, 15), scout("fighter", 2, 1), g)
	submit(t, s, session.Hide{Actor: "fighter"})
	endTurn(t, s)
	if !s.LegalActions().Search {
		t.Fatal("Search should be legal")
	}
	ev := submit(t, s, session.Search{Actor: "goblin"})
	if r, _ := find[session.Searched](ev); len(r.Found) != 0 || r.Total != 15 {
		t.Fatalf("search = %+v, want nothing found on 15", r)
	}
	endTurn(t, s) // goblin
	endTurn(t, s) // fighter
	ev = submit(t, s, session.Search{Actor: "goblin"})
	r, _ := find[session.Searched](ev)
	if !slices.Equal(r.Found, []session.ID{"fighter"}) || s.State().Creatures["fighter"].Hidden {
		t.Fatalf("search = %+v, want the fighter found and revealed", r)
	}
}

// Ready: "you take this action on your turn, which lets you act by taking a
// Reaction before the start of your next turn" once the trigger occurs.
func TestReadyAttackHoldsUntilTheTriggerAndSpendsTheReaction(t *testing.T) {
	// Held attack: d20 14 (+5 = 19 hits), d8 6 -> 9 damage.
	s := start(t, dice.Fixed(15, 10, 14, 6), fighter("fighter", 1, 2), goblin("goblin", 6, 2))
	submit(t, s, session.Ready{Actor: "fighter",
		Trigger:  session.ReadyTrigger{Kind: session.EnemyEntersReach},
		Response: session.ReadyResponse{Kind: session.RespondAttack}})
	if !s.State().Creatures["fighter"].Readied {
		t.Fatal("fighter should be holding an action")
	}
	if rej := rejection(t, s, session.Attack{Actor: "fighter", Target: "goblin"}); rej.Code != session.ActionUsed {
		t.Fatalf("code = %v, want ActionUsed: Ready spends the action", rej.Code)
	}
	endTurn(t, s)
	ev := submit(t, s, session.Move{Actor: "goblin", To: pos(4, 2)}) // not yet in reach
	if len(all[session.ReadyTriggered](ev)) != 0 {
		t.Fatal("the trigger has not occurred yet")
	}
	ev = submit(t, s, session.Move{Actor: "goblin", To: pos(2, 2)})
	if _, ok := find[session.ReadyTriggered](ev); !ok {
		t.Fatalf("events = %+v, want ReadyTriggered", ev)
	}
	if d, _ := find[session.DamageDealt](ev); d.Amount != 9 || d.HPAfter != 1 {
		t.Fatalf("damage = %+v, want 9 leaving 1 HP", d)
	}
	if st := s.State().Creatures["fighter"]; st.ReactionAvailable || st.Readied {
		t.Fatalf("fighter = %+v, want Reaction spent and nothing held", st)
	}
	// No Reaction left: leaving the fighter's reach provokes nothing.
	ev = submit(t, s, session.Move{Actor: "goblin", To: pos(3, 2)})
	if len(all[session.OpportunityAttackMade](ev)) != 0 {
		t.Fatal("the spent Reaction cannot make an Opportunity Attack")
	}
}

func TestReadyLapsesAtTheStartOfTheReadiersNextTurn(t *testing.T) {
	s := start(t, dice.Fixed(15, 10), fighter("fighter", 1, 2), goblin("goblin", 6, 2))
	submit(t, s, session.Ready{Actor: "fighter",
		Trigger:  session.ReadyTrigger{Kind: session.EnemyEntersReach},
		Response: session.ReadyResponse{Kind: session.RespondAttack}})
	endTurn(t, s)
	ev := submit(t, s, session.EndTurn{Actor: "goblin"})
	if _, ok := find[session.ReadyLapsed](ev); !ok {
		t.Fatalf("events = %+v, want ReadyLapsed", ev)
	}
	st := s.State().Creatures["fighter"]
	if st.Readied || !st.ReactionAvailable {
		t.Fatalf("fighter = %+v, want nothing held and a fresh Reaction", st)
	}
}

// "If the zombie steps next to me, I move away."
func TestReadyMoveRunsOnTheTrigger(t *testing.T) {
	// The goblin keeps its Reaction, so the fighter's move away provokes an
	// Opportunity Attack: d20 2 misses.
	s := start(t, dice.Fixed(15, 10, 2), fighter("fighter", 4, 2), goblin("goblin", 8, 2))
	submit(t, s, session.Ready{Actor: "fighter",
		Trigger:  session.ReadyTrigger{Kind: session.EnemyEntersReach},
		Response: session.ReadyResponse{Kind: session.RespondMove, To: pos(1, 2)}})
	endTurn(t, s)
	ev := submit(t, s, session.Move{Actor: "goblin", To: pos(5, 2)})
	moves := all[session.Moved](ev)
	if len(moves) != 2 || moves[1].Creature != "fighter" || moves[1].To != pos(1, 2) {
		t.Fatalf("moves = %+v, want the goblin's then the fighter's to 1,2", moves)
	}
	if len(all[session.OpportunityAttackMade](ev)) != 1 {
		t.Fatal("the fighter's move away still provokes an Opportunity Attack")
	}
}

// "If the cultist steps on the trapdoor, I'll pull the lever that opens it."
func TestReadyUtilizeLeverOnTheTrigger(t *testing.T) {
	objs := []session.ObjectSpec{
		{ID: "lever", Kind: session.Lever, Pos: pos(1, 0), Controls: "gate"},
		{ID: "gate", Kind: session.Door, Pos: pos(11, 2)},
	}
	s := startOn(t, hall, objs, dice.Fixed(15, 10), fighter("fighter", 1, 1), goblin("goblin", 7, 2))
	submit(t, s, session.Ready{Actor: "fighter",
		Trigger:  session.ReadyTrigger{Kind: session.EnemyEntersSquare, Pos: pos(5, 2)},
		Response: session.ReadyResponse{Kind: session.RespondUtilize, Object: "lever"}})
	endTurn(t, s)
	submit(t, s, session.Move{Actor: "goblin", To: pos(6, 2)}) // wrong square
	if s.State().Objects["gate"].Open {
		t.Fatal("the gate opened before the trigger")
	}
	ev := submit(t, s, session.Move{Actor: "goblin", To: pos(5, 2)})
	if c, ok := find[session.ObjectChanged](ev); !ok || c.Object != "gate" || !c.Open {
		t.Fatalf("events = %+v, want the gate opened", ev)
	}
	if s.State().Creatures["fighter"].ReactionAvailable {
		t.Fatal("the Reaction should be spent")
	}
}

func TestReadyResponseThatCannotBeCarriedOutFizzlesButSpendsTheReaction(t *testing.T) {
	objs := []session.ObjectSpec{{ID: "lever", Kind: session.Lever, Pos: pos(1, 0)}}
	s := startOn(t, hall, objs, dice.Fixed(15, 10), fighter("fighter", 3, 1), goblin("goblin", 7, 2))
	submit(t, s, session.Ready{Actor: "fighter",
		Trigger:  session.ReadyTrigger{Kind: session.EnemyEntersSquare, Pos: pos(5, 2)},
		Response: session.ReadyResponse{Kind: session.RespondUtilize, Object: "lever"}})
	endTurn(t, s)
	ev := submit(t, s, session.Move{Actor: "goblin", To: pos(5, 2)})
	if _, ok := find[session.ReadyFizzled](ev); !ok || s.State().Creatures["fighter"].ReactionAvailable {
		t.Fatalf("events = %+v, want ReadyFizzled with the Reaction spent", ev)
	}
}

// Utilize: "When an object requires an action for its use, you take the Utilize action."
func TestUtilizeOpensAndClosesADoor(t *testing.T) {
	door := []session.ObjectSpec{{ID: "door", Kind: session.Door, Pos: pos(4, 1)}}
	s := startOn(t, corridor, door, dice.Fixed(15, 10), fighter("fighter", 3, 1), goblin("goblin", 7, 1))
	if got := utilizeOptions(s); !slices.Equal(got, []session.UtilizeOption{{Object: "door", Effect: "open"}}) {
		t.Fatalf("options = %v", got)
	}
	for _, m := range s.LegalActions().Moves {
		if m.To.X >= 4 {
			t.Fatalf("closed door should block the corridor, but %v is reachable", m.To)
		}
	}
	ev := submit(t, s, session.Utilize{Actor: "fighter", Object: "door"})
	if u, _ := find[session.ObjectUsed](ev); !u.Open || !s.State().Objects["door"].Open {
		t.Fatalf("events = %+v, want the door opened", ev)
	}
	if rej := rejection(t, s, session.Utilize{Actor: "fighter", Object: "door"}); rej.Code != session.ActionUsed {
		t.Fatalf("code = %v, want ActionUsed", rej.Code)
	}
	endTurn(t, s)
	endTurn(t, s)
	if got := utilizeOptions(s); !slices.Equal(got, []session.UtilizeOption{{Object: "door", Effect: "close"}}) {
		t.Fatalf("options = %v", got)
	}
	submit(t, s, session.Move{Actor: "fighter", To: pos(4, 1)})
	if rej := rejection(t, s, session.Utilize{Actor: "fighter", Object: "door"}); rej.Code != session.ObjectBlocked {
		t.Fatalf("code = %v, want ObjectBlocked while standing in the doorway", rej.Code)
	}
	submit(t, s, session.Move{Actor: "fighter", To: pos(5, 1)})
	submit(t, s, session.Utilize{Actor: "fighter", Object: "door"})
	if s.State().Objects["door"].Open {
		t.Fatal("the door should be closed")
	}
}

func TestUtilizeOpensAChestOnce(t *testing.T) {
	chest := []session.ObjectSpec{{ID: "chest", Kind: session.Chest, Pos: pos(3, 2)}}
	s := startOn(t, hall, chest, dice.Fixed(15, 10), fighter("fighter", 2, 2), goblin("goblin", 9, 2))
	if slices.ContainsFunc(s.LegalActions().Moves, func(m session.MoveOption) bool { return m.To == pos(3, 2) }) {
		t.Fatal("a chest fills its square")
	}
	submit(t, s, session.Utilize{Actor: "fighter", Object: "chest"})
	if !s.State().Objects["chest"].Open {
		t.Fatal("the chest should be open")
	}
	endTurn(t, s)
	endTurn(t, s)
	if got := utilizeOptions(s); len(got) != 0 {
		t.Fatalf("options = %v, an open chest has nothing left to do", got)
	}
	if rej := rejection(t, s, session.Utilize{Actor: "fighter", Object: "chest"}); rej.Code != session.InvalidTarget {
		t.Fatalf("code = %v, want InvalidTarget", rej.Code)
	}
}

func TestUtilizePullsALeverThatTogglesItsDoor(t *testing.T) {
	objs := []session.ObjectSpec{
		{ID: "lever", Kind: session.Lever, Pos: pos(3, 0), Controls: "door"},
		{ID: "door", Kind: session.Door, Pos: pos(4, 1)},
	}
	s := startOn(t, corridor, objs, dice.Fixed(15, 10), fighter("fighter", 3, 1), goblin("goblin", 7, 1))
	if got := utilizeOptions(s); !slices.Contains(got, session.UtilizeOption{Object: "lever", Effect: "pull"}) {
		t.Fatalf("options = %v, want the lever", got)
	}
	ev := submit(t, s, session.Utilize{Actor: "fighter", Object: "lever"})
	if c, ok := find[session.ObjectChanged](ev); !ok || !c.Open || !s.State().Objects["door"].Open || !s.State().Objects["lever"].Open {
		t.Fatalf("events = %+v, want the lever pulled and the door opened", ev)
	}
	endTurn(t, s)
	endTurn(t, s)
	submit(t, s, session.Utilize{Actor: "fighter", Object: "lever"})
	if s.State().Objects["door"].Open || s.State().Objects["lever"].Open {
		t.Fatal("pulling the lever again should close the door")
	}
}

func TestUtilizeNeedsTheObjectWithin5Feet(t *testing.T) {
	door := []session.ObjectSpec{{ID: "door", Kind: session.Door, Pos: pos(4, 1)}}
	s := startOn(t, corridor, door, dice.Fixed(15, 10), fighter("fighter", 1, 1), goblin("goblin", 7, 1))
	if rej := rejection(t, s, session.Utilize{Actor: "fighter", Object: "door"}); rej.Code != session.OutOfReach {
		t.Fatalf("code = %v, want OutOfReach", rej.Code)
	}
	if rej := rejection(t, s, session.Utilize{Actor: "fighter", Object: "nope"}); rej.Code != session.InvalidTarget {
		t.Fatalf("code = %v, want InvalidTarget", rej.Code)
	}
}

func TestObjectPlacementIsValidated(t *testing.T) {
	for name, objs := range map[string][]session.ObjectSpec{
		"lever on floor":       {{ID: "l", Kind: session.Lever, Pos: pos(5, 2)}},
		"door in a wall":       {{ID: "d", Kind: session.Door, Pos: pos(0, 0)}},
		"chest under a hero":   {{ID: "c", Kind: session.Chest, Pos: pos(1, 2)}},
		"lever controls chest": {{ID: "l", Kind: session.Lever, Pos: pos(5, 0), Controls: "c"}, {ID: "c", Kind: session.Chest, Pos: pos(6, 2)}},
		"duplicate ID":         {{ID: "d", Kind: session.Door, Pos: pos(5, 2)}, {ID: "d", Kind: session.Door, Pos: pos(6, 2)}},
		"unknown kind":         {{ID: "x", Kind: "mirror", Pos: pos(5, 2)}},
	} {
		_, err := session.New(session.Config{Board: mustBoard(t, hall), Objects: objs,
			Creatures: []session.CreatureSpec{fighter("fighter", 1, 2), goblin("goblin", 9, 2)}})
		if err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// Dodge: "you make Dexterity saving throws with Advantage."
func TestDodgeGivesAdvantageOnDexteritySaves(t *testing.T) {
	// Save d20s 4 and 15: Advantage keeps 15.
	s := start(t, dice.Fixed(15, 10, 4, 15), fighter("fighter", 1, 2), goblin("goblin", 6, 2))
	submit(t, s, session.Dodge{Actor: "fighter"})
	ev, err := s.Apply(session.SavingThrow{Target: "fighter", Ability: session.Dexterity, DC: 13})
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := find[session.SaveRolled](ev); r.Mode != session.Advantage || r.D20 != 15 {
		t.Fatalf("save = %+v, want Advantage keeping 15", r)
	}
}

// "You lose these benefits if you have the Incapacitated condition."
func TestDodgeIsLostWhileIncapacitated(t *testing.T) {
	s := start(t, dice.Fixed(15, 10, 9), fighter("fighter", 1, 2), goblin("goblin", 2, 2))
	submit(t, s, session.Dodge{Actor: "fighter"})
	if _, err := s.Apply(session.InflictCondition{Target: "fighter", Condition: session.CondIncapacitated}); err != nil {
		t.Fatal(err)
	}
	if s.State().Creatures["fighter"].Dodging {
		t.Fatal("an Incapacitated creature loses Dodge")
	}
	// The goblin attacks with a single die (no Disadvantage): 9 + 4 = 13 < 16.
	endTurn(t, s)
	ev := submit(t, s, session.Attack{Actor: "goblin", Target: "fighter"})
	if a, _ := find[session.AttackRolled](ev); a.Mode != session.Normal {
		t.Fatalf("mode = %q, want a normal roll", a.Mode)
	}
}

// Hide gives the Invisible condition while hidden; it ends with the hiding.
func TestHideAppliesInvisibleUntilFound(t *testing.T) {
	g := goblin("goblin", 3, 1)
	g.PerceptionMod = 1
	// Hide 14 + 2 = 16; the goblin finds it with 15 + 1 = 16.
	s := startOn(t, obscuredRoom, nil, dice.Fixed(15, 10, 14, 15), scout("fighter", 2, 1), g)
	submit(t, s, session.Hide{Actor: "fighter"})
	if !slices.Contains(s.State().Creatures["fighter"].Conditions, session.CondInvisible) {
		t.Fatal("a hidden creature is Invisible")
	}
	endTurn(t, s)
	ev := submit(t, s, session.Search{Actor: "goblin"})
	if _, ok := find[session.ConditionRemoved](ev); !ok {
		t.Fatalf("events = %+v, want Invisible removed when found", ev)
	}
	if slices.Contains(s.State().Creatures["fighter"].Conditions, session.CondInvisible) {
		t.Fatal("the fighter should be visible again")
	}
}

func TestIncapacitatedCreatureCannotTakeStandardActions(t *testing.T) {
	s := start(t, dice.Fixed(15, 10), fighter("fighter", 1, 2), goblin("goblin", 2, 2))
	if _, err := s.Apply(session.InflictCondition{Target: "fighter", Condition: session.CondIncapacitated}); err != nil {
		t.Fatal(err)
	}
	if rej := rejection(t, s, session.Dodge{Actor: "fighter"}); rej.Code != session.Incapacitated {
		t.Fatalf("code = %v, want Incapacitated", rej.Code)
	}
	if la := s.LegalActions(); la.Dodge || la.Search || la.Ready {
		t.Fatalf("no standard action while Incapacitated: %+v", la)
	}
}
