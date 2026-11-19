package session_test

import (
	"slices"
	"testing"

	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
)

// Help (SRD 5.2 "Rules Glossary", Help [Action]): "Assist an Attack Roll. You
// momentarily distract an enemy within 5 feet of you, giving Advantage to the
// next attack roll by one of your allies against that enemy. This benefit
// expires at the start of your next turn."
func TestHelpGivesAdvantageToTheNextAllyAttackRoll(t *testing.T) {
	// f1 15+1, f2 10+1, g1 5+2, g2 3+2.
	s := start(t, dice.Fixed(15, 10, 5, 3,
		1, 2, // f2 with Advantage: keeps 2, misses
		2, 2, // g1 leaves both reaches: f1's and f2's Opportunity Attacks, one d20 each, miss
	), fighter("f1", 3, 2), fighter("f2", 3, 3), goblin("g1", 4, 2), goblin("g2", 10, 1))

	if la := s.LegalActions(); !slices.Equal(la.Help, []session.ID{"g1"}) || !la.Dodge {
		t.Fatalf("help targets: %+v", la)
	}
	if r := rejection(t, s, session.Help{Actor: "f1", Target: "g2"}); r.Code != session.OutOfReach {
		t.Fatalf("help a far enemy: %v", r.Code)
	}
	if r := rejection(t, s, session.Help{Actor: "f1", Target: "f2"}); r.Code != session.InvalidTarget {
		t.Fatalf("help against an ally: %v", r.Code)
	}
	evs := submit(t, s, session.Help{Actor: "f1", Target: "g1"})
	if h, _ := find[session.Helped](evs); h != (session.Helped{Creature: "f1", Target: "g1"}) {
		t.Fatalf("help: %#v", evs)
	}
	if r := rejection(t, s, session.Attack{Actor: "f1", Target: "g1"}); r.Code != session.ActionUsed {
		t.Fatalf("help spends the action: %v", r.Code)
	}
	endTurn(t, s)

	if a := attackRolls(submit(t, s, session.Attack{Actor: "f2", Target: "g1"}))[0]; !a.Advantage || a.D20 != 2 || !slices.Equal(a.D20s, []int{1, 2}) {
		t.Fatalf("helped attack: %+v", a)
	}
	endTurn(t, s)

	// The Advantage was used: the next attack rolls against g1 are normal.
	for _, a := range attackRolls(submit(t, s, session.Move{Actor: "g1", To: pos(5, 3)})) {
		if a.Advantage || len(a.D20s) != 1 {
			t.Fatalf("Opportunity Attack after the help was used: %+v", a)
		}
	}
}

// Dodge (SRD 5.2 "Rules Glossary", Dodge [Action]): "until the start of your
// next turn, any attack roll made against you has Disadvantage if you can see
// the attacker".
func TestDodgeImposesDisadvantageUntilTheDodgersNextTurn(t *testing.T) {
	s := start(t, dice.Fixed(15, 5,
		18, 3, // fighter with Disadvantage: keeps 3, misses
		3, //     next round, one d20
	), goblin("goblin", 4, 2), fighter("fighter", 3, 2))
	if _, ok := find[session.Dodged](submit(t, s, session.Dodge{Actor: "goblin"})); !ok {
		t.Fatal("no Dodged event")
	}
	if !s.State().Creatures["goblin"].Dodging || s.LegalActions().Action {
		t.Fatal("dodge did not take effect or spend the action")
	}
	endTurn(t, s)
	if a := attackRolls(submit(t, s, session.Attack{Actor: "fighter", Target: "goblin"}))[0]; !a.Disadvantage || a.D20 != 3 || a.Hit {
		t.Fatalf("attack on a dodging goblin: %+v", a)
	}
	endTurn(t, s)
	if s.State().Creatures["goblin"].Dodging {
		t.Fatal("dodge outlasted the start of the goblin's turn")
	}
	endTurn(t, s)
	if a := attackRolls(submit(t, s, session.Attack{Actor: "fighter", Target: "goblin"}))[0]; a.Disadvantage || len(a.D20s) != 1 {
		t.Fatalf("attack after the dodge: %+v", a)
	}
}

// Light (SRD 5.2 "Equipment", Weapon properties): "When you take the Attack
// action on your turn and attack with a Light weapon, you can make one extra
// attack as a Bonus Action later on the same turn. That extra attack must be
// made with a different Light weapon, and you don't add your ability modifier
// to the extra attack's damage unless that modifier is negative."
func TestOffHandAttackFollowsALightWeaponAttack(t *testing.T) {
	s := start(t, dice.Fixed(15, 10, 5, 2, 2), rogue("rogue", 3, 2), goblin("goblin", 4, 2), fighter("fighter", 10, 1))
	if la := s.LegalActions(); len(la.OffHandAttacks) != 0 || la.BonusAction {
		t.Fatalf("off-hand before the Attack action: %+v", la)
	}
	if r := rejection(t, s, session.OffHandAttack{Actor: "rogue", Target: "goblin"}); r.Code != session.NoSuchOption {
		t.Fatalf("off-hand first: %v", r.Code)
	}
	submit(t, s, session.Attack{Actor: "rogue", Target: "goblin"}) // misses
	la := s.LegalActions()
	if !la.BonusAction || !slices.Equal(la.OffHandAttacks, []session.AttackOption{{Target: "goblin", Weapon: "Dagger"}}) {
		t.Fatalf("off-hand after the Attack action: %+v", la)
	}
	a := attackRolls(submit(t, s, session.OffHandAttack{Actor: "rogue", Target: "goblin"}))[0]
	if a.Weapon != "Dagger" {
		t.Fatalf("off-hand attack: %+v", a)
	}
	if r := rejection(t, s, session.OffHandAttack{Actor: "rogue", Target: "goblin"}); r.Code != session.BonusActionUsed {
		t.Fatalf("second off-hand: %v", r.Code)
	}
	endTurn(t, s) // rogue
	endTurn(t, s) // goblin
	if r := rejection(t, s, session.OffHandAttack{Actor: "fighter", Target: "goblin"}); r.Code != session.NoSuchOption {
		t.Fatalf("fighter without an off-hand weapon: %v", r.Code)
	}
}
