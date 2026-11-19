package session_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mvgv/dndboard/content"
	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
	"github.com/mvgv/dndboard/server/srd"
)

// Rogue level 1: Shortsword +5, 1d6+3 Piercing (Finesse, Light); off-hand
// Dagger +5, 1d4 Piercing; Sneak Attack 1d6. Initiative +3, AC 14, 10 HP.
func rogue(id string, x, y int) session.CreatureSpec {
	r := srd.Rogue()
	r.ID, r.Pos = session.ID(id), board.Pos{X: x, Y: y}
	return r
}

// Barbarian level 1 (Soldier): Greataxe +5, 1d12+3 Slashing (Strength); Rage
// (2 uses, +2 Rage Damage); Savage Attacker. Initiative +2, AC 14, 14 HP.
func barbarian(id string, x, y int) session.CreatureSpec {
	b := srd.Barbarian()
	b.ID, b.Pos = session.ID(id), board.Pos{X: x, Y: y}
	return b
}

func damageTo(t *testing.T, evs []session.Event, target session.ID) session.DamageDealt {
	t.Helper()
	for _, e := range evs {
		if d, ok := e.(session.DamageDealt); ok && d.Target == target {
			return d
		}
	}
	t.Fatalf("no damage to %s in %#v", target, evs)
	return session.DamageDealt{}
}

func attackRolls(evs []session.Event) []session.AttackRolled {
	var out []session.AttackRolled
	for _, e := range evs {
		if a, ok := e.(session.AttackRolled); ok {
			out = append(out, a)
		}
	}
	return out
}

func sneak(rolls ...int) []session.ExtraDamage {
	return []session.ExtraDamage{{Feature: "sneak-attack", Rolls: rolls, Type: "Piercing"}}
}

// Sneak Attack (SRD 5.2 "Classes", Rogue, Level 1: Sneak Attack): "Once per
// turn, you can deal an extra 1d6 damage to one creature you hit with an
// attack roll if you have Advantage on the roll and the attack uses a Finesse
// or a Ranged weapon. The extra damage's type is the same as the weapon's
// type. You don't need Advantage on the attack roll if at least one of your
// allies is within 5 feet of the target, the ally doesn't have the
// Incapacitated condition, and you don't have Disadvantage on the attack roll."
func TestSneakAttackWithAnAllyNextToTheTargetOncePerTurn(t *testing.T) {
	// rogue 15+3, fighter 10+1, goblin 5+2. The fighter stands next to the goblin.
	s := start(t, dice.Fixed(15, 10, 5,
		12, 1, 1, // rogue: Shortsword 17 hits AC 15; 1d6 1, Sneak Attack 1d6 1
		12, 1, //    off-hand Dagger 17 hits; 1d4 1, no second Sneak Attack this turn
		12, 1, 1, // goblin's turn: rogue's Opportunity Attack hits; 1d6 1, Sneak Attack 1d6 1
	), rogue("rogue", 3, 2), fighter("fighter", 5, 2), goblin("goblin", 4, 2))

	main := damageTo(t, submit(t, s, session.Attack{Actor: "rogue", Target: "goblin"}), "goblin")
	if !reflect.DeepEqual(main.Extras, sneak(1)) || main.Amount != 5 || main.HPAfter != 5 {
		t.Fatalf("first hit: %+v, want 1+3 plus Sneak Attack 1 = 5", main)
	}
	off := damageTo(t, submit(t, s, session.OffHandAttack{Actor: "rogue", Target: "goblin"}), "goblin")
	if len(off.Extras) != 0 || off.Amount != 1 || off.HPAfter != 4 {
		t.Fatalf("second hit in the same turn: %+v, want 1 and no Sneak Attack", off)
	}
	endTurn(t, s) // rogue
	endTurn(t, s) // fighter

	// A new turn (the goblin's): leaving the rogue's reach provokes an
	// Opportunity Attack, and Sneak Attack is available again.
	evs := submit(t, s, session.Move{Actor: "goblin", To: pos(5, 1)})
	oa := damageTo(t, evs, "goblin")
	if !reflect.DeepEqual(oa.Extras, sneak(1)) || oa.HPAfter != 0 {
		t.Fatalf("Opportunity Attack on the goblin's turn: %+v, want Sneak Attack again", oa)
	}
}

func TestSneakAttackNeedsAdvantageOrAnAllyNearTheTarget(t *testing.T) {
	// rogue 15+3, goblin 5+2, fighter 1+1 far away. Hit 17, 1d6 4.
	s := start(t, dice.Fixed(15, 5, 1, 12, 4), rogue("rogue", 3, 2), goblin("goblin", 4, 2), fighter("fighter", 10, 1))
	d := damageTo(t, submit(t, s, session.Attack{Actor: "rogue", Target: "goblin"}), "goblin")
	if len(d.Extras) != 0 || d.Amount != 7 {
		t.Fatalf("no Advantage, no ally near: %+v, want 4+3 and no Sneak Attack", d)
	}
}

func TestSneakAttackWithAdvantageFromHelp(t *testing.T) {
	// fighter 15+1, rogue 10+3, goblin 5+2.
	s := start(t, dice.Fixed(15, 10, 5,
		2,     // goblin's Opportunity Attack on the fighter misses (6 vs AC 16)
		3, 12, // rogue with Advantage: keeps 12, 17 hits
		2, 3, // 1d6 2, Sneak Attack 1d6 3
	), fighter("fighter", 3, 1), rogue("rogue", 4, 3), goblin("goblin", 4, 2))

	submit(t, s, session.Help{Actor: "fighter", Target: "goblin"})
	submit(t, s, session.Move{Actor: "fighter", To: pos(1, 1)}) // no ally within 5 ft of the goblin now
	endTurn(t, s)

	evs := submit(t, s, session.Attack{Actor: "rogue", Target: "goblin"})
	a := attackRolls(evs)[0]
	if !a.Advantage || !slices.Equal(a.D20s, []int{3, 12}) || a.D20 != 12 || !a.Hit {
		t.Fatalf("attack roll: %+v, want Advantage keeping 12", a)
	}
	if d := damageTo(t, evs, "goblin"); !reflect.DeepEqual(d.Extras, sneak(3)) || d.Amount != 8 {
		t.Fatalf("damage: %+v, want 2+3 plus Sneak Attack 3", d)
	}
}

func TestSneakAttackNotWithDisadvantageEvenWithAnAllyNear(t *testing.T) {
	// goblin 15+2, rogue 10+3, fighter 5+1. The goblin Dodges.
	s := start(t, dice.Fixed(15, 10, 5,
		18, 12, // rogue with Disadvantage: keeps 12, 17 hits
		4,
	), goblin("goblin", 4, 2), rogue("rogue", 3, 2), fighter("fighter", 5, 2))
	submit(t, s, session.Dodge{Actor: "goblin"})
	endTurn(t, s)
	evs := submit(t, s, session.Attack{Actor: "rogue", Target: "goblin"})
	if a := attackRolls(evs)[0]; !a.Disadvantage || a.D20 != 12 {
		t.Fatalf("attack roll: %+v, want Disadvantage keeping 12", a)
	}
	if d := damageTo(t, evs, "goblin"); len(d.Extras) != 0 || d.Amount != 7 {
		t.Fatalf("damage: %+v, want 4+3 and no Sneak Attack", d)
	}
}

func TestSneakAttackNeedsAFinesseOrRangedWeapon(t *testing.T) {
	r := rogue("rogue", 3, 2)
	r.Attack = srd.Fighter().Attack // Longsword: neither Finesse nor Ranged
	r.OffHand = nil
	s := start(t, dice.Fixed(15, 10, 5, 12, 4), r, fighter("fighter", 5, 2), goblin("goblin", 4, 2))
	if d := damageTo(t, submit(t, s, session.Attack{Actor: "rogue", Target: "goblin"}), "goblin"); len(d.Extras) != 0 {
		t.Fatalf("damage: %+v, want no Sneak Attack with a Longsword", d)
	}
}

// Critical Hits (SRD 5.2 "Playing the Game"): "If the attack involves other
// damage dice, such as from the Rogue's Sneak Attack feature, you also roll
// those dice twice."
func TestCriticalHitDoublesSneakAttackDice(t *testing.T) {
	s := start(t, dice.Fixed(15, 10, 5, 20, 1, 1, 1, 2),
		rogue("rogue", 3, 2), fighter("fighter", 5, 2), goblin("goblin", 4, 2))
	d := damageTo(t, submit(t, s, session.Attack{Actor: "rogue", Target: "goblin"}), "goblin")
	if !slices.Equal(d.Rolls, []int{1, 1}) || !reflect.DeepEqual(d.Extras, sneak(1, 2)) || d.Amount != 8 {
		t.Fatalf("critical: %+v, want 2d6 [1 1] + 3 + Sneak Attack 2d6 [1 2] = 8", d)
	}
}

// Rage (SRD 5.2 "Classes", Barbarian, Level 1: Rage): entered as a Bonus
// Action, a number of times per the Rages column (2 at level 1). While
// active: "Resistance to Bludgeoning, Piercing, and Slashing damage", and
// "When you make an attack using Strength ... and deal damage to the target,
// you gain a bonus to the damage" (+2 at level 1).
func TestRageGrantsDamageBonusAndResistance(t *testing.T) {
	// barbarian 15+2, goblin 5+2.
	s := start(t, dice.Fixed(15, 5,
		15, 1, 1, // Greataxe 20 hits; 1d12 1, Savage Attacker reroll 1
		15, 5, //    goblin's Scimitar 19 hits AC 14; 1d6 5 + 2 = 7 Slashing
	), barbarian("barb", 3, 2), goblin("goblin", 4, 2))

	la := s.LegalActions()
	if !la.BonusAction || !slices.Equal(la.Features, []session.FeatureOption{{Feature: "rage", UsesLeft: 2}}) {
		t.Fatalf("before raging: %+v", la)
	}
	evs := submit(t, s, session.UseFeature{Actor: "barb", Feature: "rage"})
	if got, _ := find[session.FeatureActivated](evs); got != (session.FeatureActivated{Creature: "barb", Feature: "rage", UsesLeft: 1}) {
		t.Fatalf("activation: %#v", evs)
	}
	if la := s.LegalActions(); la.BonusAction || len(la.Features) != 0 {
		t.Fatalf("Bonus Action spent: %+v", la)
	}
	if r := rejection(t, s, session.UseFeature{Actor: "barb", Feature: "rage"}); r.Code != session.BonusActionUsed {
		t.Fatalf("second Bonus Action: %v", r.Code)
	}
	if st := s.State().Creatures["barb"]; !slices.Equal(st.ActiveFeatures, []string{"rage"}) {
		t.Fatalf("visible state: %+v", st)
	}

	d := damageTo(t, submit(t, s, session.Attack{Actor: "barb", Target: "goblin"}), "goblin")
	want := []session.ExtraDamage{{Feature: "rage", Bonus: 2, Type: "Slashing"}}
	if !reflect.DeepEqual(d.Extras, want) || d.Amount != 6 || d.HPAfter != 4 {
		t.Fatalf("raging hit: %+v, want 1+3 plus Rage Damage 2 = 6", d)
	}
	endTurn(t, s)

	d = damageTo(t, submit(t, s, session.Attack{Actor: "goblin", Target: "barb"}), "barb")
	if !d.Resisted || d.Amount != 3 || d.HPAfter != 11 {
		t.Fatalf("hit on the raging barbarian: %+v, want 7 Slashing halved to 3", d)
	}
}

func TestRageDamageOnlyForStrengthAttacks(t *testing.T) {
	b := barbarian("barb", 3, 2)
	b.Attack = srd.Rogue().Attack // Shortsword with Dexterity
	s := start(t, dice.Fixed(15, 5, 15, 2, 1), b, goblin("goblin", 4, 2))
	submit(t, s, session.UseFeature{Actor: "barb", Feature: "rage"})
	if d := damageTo(t, submit(t, s, session.Attack{Actor: "barb", Target: "goblin"}), "goblin"); len(d.Extras) != 0 || d.Amount != 5 {
		t.Fatalf("Dexterity attack while raging: %+v, want 2+3 and no Rage Damage", d)
	}
}

// Rage duration: "The Rage lasts until the end of your next turn ... If your
// Rage is still active on your next turn, you can extend the Rage for another
// round by doing one of the following: Make an attack roll against an enemy.
// ... Take a Bonus Action to extend your Rage."
func TestRageLastsUntilTheEndOfTheNextTurnUnlessExtended(t *testing.T) {
	s := start(t, dice.Fixed(15, 5,
		2,     // round 2: the barbarian's attack misses, but extends the Rage
		15, 5, // round 4: goblin's Scimitar hits; 1d6 5 + 2 = 7, no longer resisted
	), barbarian("barb", 3, 2), goblin("goblin", 4, 2))
	ended := func(evs []session.Event) bool { _, ok := find[session.FeatureEnded](evs); return ok }

	submit(t, s, session.UseFeature{Actor: "barb", Feature: "rage"})
	if ended(submit(t, s, session.EndTurn{Actor: "barb"})) {
		t.Fatal("Rage ended at the end of the turn it started")
	}
	endTurn(t, s) // goblin

	// Round 2: an attack roll against an enemy extends it, hit or miss.
	if _, ok := find[session.FeatureExtended](submit(t, s, session.Attack{Actor: "barb", Target: "goblin"})); !ok {
		t.Fatal("attack roll did not extend the Rage")
	}
	if ended(submit(t, s, session.EndTurn{Actor: "barb"})) {
		t.Fatal("extended Rage ended")
	}
	endTurn(t, s)

	// Round 3: a Bonus Action extends it.
	if la := s.LegalActions(); !slices.Equal(la.Features, []session.FeatureOption{{Feature: "rage", Extend: true, UsesLeft: 1}}) {
		t.Fatalf("extend option: %+v", la.Features)
	}
	if _, ok := find[session.FeatureExtended](submit(t, s, session.UseFeature{Actor: "barb", Feature: "rage"})); !ok {
		t.Fatal("Bonus Action did not extend the Rage")
	}
	if ended(submit(t, s, session.EndTurn{Actor: "barb"})) {
		t.Fatal("extended Rage ended")
	}
	endTurn(t, s)

	// Round 4: not extended, so it ends with the turn.
	evs := submit(t, s, session.EndTurn{Actor: "barb"})
	if e, _ := find[session.FeatureEnded](evs); e != (session.FeatureEnded{Creature: "barb", Feature: "rage", Reason: session.EndedExpired}) {
		t.Fatalf("end of round 4: %#v", evs)
	}
	if st := s.State().Creatures["barb"]; len(st.ActiveFeatures) != 0 {
		t.Fatalf("still active: %+v", st)
	}
	if d := damageTo(t, submit(t, s, session.Attack{Actor: "goblin", Target: "barb"}), "barb"); d.Resisted || d.Amount != 7 {
		t.Fatalf("hit after the Rage: %+v, want the full 7", d)
	}
}

// "You can maintain a Rage for up to 10 minutes": 100 rounds of 6 seconds.
// Entered in round 1, it can last through the barbarian's turn in round 100
// but not into round 101.
func TestRageLastsAtMostTenMinutes(t *testing.T) {
	s := start(t, dice.Fixed(15, 5), barbarian("barb", 3, 2), goblin("goblin", 9, 2))
	submit(t, s, session.UseFeature{Actor: "barb", Feature: "rage"})
	for {
		evs := submit(t, s, session.EndTurn{Actor: "barb"})
		if e, ok := find[session.FeatureEnded](evs); ok {
			if r := s.State().Round; e.Reason != session.EndedMaxDuration || r != 100 {
				t.Fatalf("ended %#v in round %d, want max duration at the end of round 100", e, r)
			}
			return
		}
		endTurn(t, s) // goblin
		if s.State().Round > 100 {
			t.Fatal("Rage outlasted 10 minutes")
		}
		submit(t, s, session.UseFeature{Actor: "barb", Feature: "rage"}) // extend
	}
}

// "It ends early if you ... have the Incapacitated condition." A creature at 0
// Hit Points is Unconscious, which includes Incapacitated.
func TestRageEndsWhenTheBarbarianIsIncapacitated(t *testing.T) {
	b := barbarian("barb", 3, 2)
	b.MaxHP = 1
	s := start(t, dice.Fixed(15, 10, 5, 15, 1), b, goblin("goblin", 4, 2), fighter("fighter", 10, 1))
	submit(t, s, session.UseFeature{Actor: "barb", Feature: "rage"})
	endTurn(t, s)
	evs := submit(t, s, session.Attack{Actor: "goblin", Target: "barb"}) // 1+2 = 3, halved to 1
	if e, _ := find[session.FeatureEnded](evs); e != (session.FeatureEnded{Creature: "barb", Feature: "rage", Reason: session.EndedIncapacitated}) {
		t.Fatalf("downed while raging: %#v", evs)
	}
}

// "You can enter your Rage the number of times shown ... in the Rages column."
func TestRageUsesRunOut(t *testing.T) {
	s := start(t, dice.Fixed(15, 5), barbarian("barb", 3, 2), goblin("goblin", 9, 2))
	for want := 1; want >= 0; want-- {
		evs := submit(t, s, session.UseFeature{Actor: "barb", Feature: "rage"})
		if a, _ := find[session.FeatureActivated](evs); a.UsesLeft != want {
			t.Fatalf("uses left = %d, want %d", a.UsesLeft, want)
		}
		endTurn(t, s)
		endTurn(t, s)
		endTurn(t, s) // the Rage expires at the end of this turn
		endTurn(t, s)
	}
	if la := s.LegalActions(); len(la.Features) != 0 {
		t.Fatalf("rage offered with no uses left: %+v", la.Features)
	}
	if r := rejection(t, s, session.UseFeature{Actor: "barb", Feature: "rage"}); r.Code != session.NoUsesLeft {
		t.Fatalf("third Rage: %v", r.Code)
	}
	if r := rejection(t, s, session.UseFeature{Actor: "barb", Feature: "savage-attacker"}); r.Code != session.NoSuchOption {
		t.Fatalf("using a passive feature: %v", r.Code)
	}
}

// Savage Attacker (SRD 5.2 "Feats", Origin Feats) is flagged "requires
// implementation": "Once per turn when you hit a target with a weapon, you can
// roll the weapon's damage dice twice and use either roll against the
// target." The engine keeps the higher roll.
func TestHandCodedSavageAttackerRollsWeaponDiceTwice(t *testing.T) {
	for _, tc := range []struct{ first, second, kept int }{{3, 9, 9}, {9, 3, 9}} {
		s := start(t, dice.Fixed(15, 5, 15, tc.first, tc.second), barbarian("barb", 3, 2), goblin("goblin", 4, 2))
		evs := submit(t, s, session.Attack{Actor: "barb", Target: "goblin"})
		r, ok := find[session.DamageRerolled](evs)
		if !ok || r.Feature != "savage-attacker" || !slices.Equal(r.First, []int{tc.first}) || !slices.Equal(r.Second, []int{tc.second}) {
			t.Fatalf("reroll event: %#v", evs)
		}
		if d := damageTo(t, evs, "goblin"); !slices.Equal(d.Rolls, []int{tc.kept}) || d.Amount != tc.kept+3 {
			t.Fatalf("damage: %+v, want the %d kept", d, tc.kept)
		}
	}
}

func TestFeatureFlaggedRequiresImplementationNeedsEngineCode(t *testing.T) {
	b := barbarian("barb", 3, 2)
	b.Features = append(b.Features, content.Feature{ID: "wild-shape", Name: "Wild Shape", RequiresImplementation: true})
	_, err := session.New(session.Config{Board: mustBoard(t, hall), Creatures: []session.CreatureSpec{b, goblin("goblin", 4, 2)}})
	if err == nil || !strings.Contains(err.Error(), `"wild-shape"`) {
		t.Fatalf("err = %v, want the unimplemented feature named", err)
	}
}
