// Package srd builds session creatures from the SRD 5.2 content loaded through
// the content format (content/srd).
package srd

import (
	"fmt"
	"sync"

	"github.com/mvgv/dndboard/content"
	contentsrd "github.com/mvgv/dndboard/content/srd"
	"github.com/mvgv/dndboard/server/session"
)

// Fighter is the level 1 Fighter stat block (id "fighter-1"), on the party team.
func Fighter() session.CreatureSpec { return mustCreature("fighter-1", "party") }

// Rogue is the level 1 Rogue stat block (id "rogue-1"), with Sneak Attack, on
// the party team.
func Rogue() session.CreatureSpec { return mustCreature("rogue-1", "party") }

// Barbarian is the level 1 Barbarian stat block (id "barbarian-1"), with Rage
// and Savage Attacker, on the party team.
func Barbarian() session.CreatureSpec { return mustCreature("barbarian-1", "party") }

// GoblinWarrior is the SRD Goblin Warrior stat block, on the monsters team. Its
// Bonus Action, Nimble Escape (SRD 5.2 "Monsters", Goblin Warrior), is set here
// because the content format does not carry actions yet.
func GoblinWarrior() session.CreatureSpec {
	g := mustCreature("goblin-warrior", "monsters")
	g.NimbleEscape = true // Disengage; Hide is not modeled yet
	return g
}

// creature builds a CreatureSpec from an approved stat block, its weapons and
// its features.
func creature(c *content.Compendium, statBlockID string, team session.Team) (session.CreatureSpec, error) {
	sb, ok := c.StatBlock(statBlockID)
	if !ok {
		return session.CreatureSpec{}, fmt.Errorf("srd: no approved stat block %q", statBlockID)
	}
	main, err := attack(c, sb.Attack)
	if err != nil {
		return session.CreatureSpec{}, err
	}
	spec := session.CreatureSpec{
		Name:          sb.Name,
		Team:          team,
		AC:            sb.AC,
		MaxHP:         sb.MaxHP,
		SpeedFt:       sb.SpeedFt,
		InitiativeMod: sb.InitiativeMod,
		Attack:        main,
	}
	if sb.OffHand != nil {
		off, err := attack(c, *sb.OffHand)
		if err != nil {
			return session.CreatureSpec{}, err
		}
		spec.OffHand = &off
	}
	for _, id := range sb.Features {
		f, ok := c.Feature(id)
		if !ok {
			return session.CreatureSpec{}, fmt.Errorf("srd: no approved feature %q", id)
		}
		spec.Features = append(spec.Features, f)
	}
	return spec, nil
}

func attack(c *content.Compendium, a content.Attack) (session.MeleeAttack, error) {
	w, ok := c.Weapon(a.Weapon)
	if !ok {
		return session.MeleeAttack{}, fmt.Errorf("srd: no approved weapon %q", a.Weapon)
	}
	return session.MeleeAttack{
		Name: w.Name, ToHit: a.ToHit,
		DiceCount: w.Damage.DiceCount, DiceSides: w.Damage.DiceSides,
		DamageBonus: a.DamageBonus, DamageType: w.Damage.Type,
		ReachFt: w.ReachFt, Ability: a.Ability, Properties: w.Properties,
	}, nil
}

var compendium = sync.OnceValues(contentsrd.Load)

func mustCreature(id string, team session.Team) session.CreatureSpec {
	c, err := compendium()
	if err != nil {
		panic(err)
	}
	spec, err := creature(c, id, team)
	if err != nil {
		panic(err)
	}
	return spec
}
