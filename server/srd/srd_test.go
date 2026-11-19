package srd_test

import (
	"testing"

	"github.com/mvgv/dndboard/server/board"
	"github.com/mvgv/dndboard/server/dice"
	"github.com/mvgv/dndboard/server/session"
	"github.com/mvgv/dndboard/server/srd"
)

func submit(t *testing.T, s *session.Session, in session.Intent) []session.Event {
	t.Helper()
	ev, err := s.Submit(in)
	if err != nil {
		t.Fatalf("%T rejected: %v", in, err)
	}
	return ev
}

func find[E session.Event](evs []session.Event) (E, bool) {
	for _, e := range evs {
		if v, ok := e.(E); ok {
			return v, true
		}
	}
	var zero E
	return zero, false
}

// The Bandit comes from the Forge-generated Compendium (content/srd/monsters.json,
// SRD 5.2.1 Monsters, Bandit, page 261): AC 12, HP 11, Initiative +1,
// Scimitar: Melee Attack Roll +3, reach 5 ft., Hit 4 (1d6 + 1) Slashing damage.
// The Fighter (AC 16, HP 12, Longsword +5, 1d8 + 3) is hand-written.
func TestFighterBeatsForgedBandit(t *testing.T) {
	if v := srd.MonstersVersion(); v < 1 {
		t.Fatalf("monster Compendium version = %d", v)
	}
	b, err := board.Parse([]string{"#####", "#...#", "#####"})
	if err != nil {
		t.Fatal(err)
	}
	f := srd.Fighter()
	f.ID, f.Pos = "fighter", board.Pos{X: 1, Y: 1}
	bandit, err := srd.Monster("bandit")
	if err != nil {
		t.Fatal(err)
	}
	bandit.ID, bandit.Pos = "bandit", board.Pos{X: 2, Y: 1}
	s, err := session.New(session.Config{
		Board:     b,
		Creatures: []session.CreatureSpec{f, bandit},
		Dice: dice.Fixed(
			15, 10, // initiative: fighter 15+1=16, bandit 10+1=11
			12, 3, // fighter: 12+5=17 vs AC 12 hits, 3+3=6 -> bandit 5 HP
			15, 4, // bandit: 15+3=18 vs AC 16 hits, 4+1=5 -> fighter 7 HP
			10, 4, // fighter: 10+5=15 vs AC 12 hits, 4+3=7 -> bandit down
		),
	})
	if err != nil {
		t.Fatal(err)
	}

	ev := submit(t, s, session.Attack{Actor: "fighter", Target: "bandit"})
	if dmg, _ := find[session.DamageDealt](ev); dmg.HPAfter != 5 {
		t.Fatalf("bandit after first hit = %+v, want 5 HP of 11", dmg)
	}
	submit(t, s, session.EndTurn{Actor: "fighter"})

	ev = submit(t, s, session.Attack{Actor: "bandit", Target: "fighter"})
	roll, _ := find[session.AttackRolled](ev)
	if roll.Weapon != "Scimitar" || roll.Modifier != 3 || roll.Total != 18 || !roll.Hit {
		t.Fatalf("bandit attack = %+v", roll)
	}
	dmg, _ := find[session.DamageDealt](ev)
	if dmg.Bonus != 1 || dmg.Amount != 5 || dmg.Type != "Slashing" || dmg.HPAfter != 7 {
		t.Fatalf("bandit damage = %+v", dmg)
	}
	submit(t, s, session.EndTurn{Actor: "bandit"})

	ev = submit(t, s, session.Attack{Actor: "fighter", Target: "bandit"})
	if _, ok := find[session.CreatureDowned](ev); !ok {
		t.Fatalf("bandit not downed: %+v", ev)
	}
	if end, ok := find[session.CombatEnded](ev); !ok || end.Winner != "party" {
		t.Fatalf("combat end = %+v ok=%v", end, ok)
	}
}

// Draft entries never load: the Skeleton and Zombie were left unapproved in
// review (their vulnerabilities, immunities and Undead Fortitude are not in
// the content format yet).
func TestDraftMonstersAreUnavailable(t *testing.T) {
	for _, id := range []string{"skeleton", "zombie"} {
		if _, err := srd.Monster(id); err == nil {
			t.Errorf("draft %q loaded", id)
		}
	}
}
