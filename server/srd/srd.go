// Package srd builds session creatures from the SRD 5.2 content loaded through
// the content format (content/srd).
package srd

import (
	"fmt"

	"github.com/mvgv/dndboard/content"
	contentsrd "github.com/mvgv/dndboard/content/srd"
	"github.com/mvgv/dndboard/server/session"
)

// Fighter is the level 1 Fighter stat block (id "fighter-1"), on the party team.
func Fighter() session.CreatureSpec { return mustCreature("fighter-1", "party") }

// GoblinWarrior is the SRD Goblin Warrior stat block, on the monsters team.
func GoblinWarrior() session.CreatureSpec { return mustCreature("goblin-warrior", "monsters") }

// Creature builds a CreatureSpec from an approved stat block and its weapon.
func Creature(c *content.Compendium, statBlockID string, team session.Team) (session.CreatureSpec, error) {
	sb, ok := c.StatBlock(statBlockID)
	if !ok {
		return session.CreatureSpec{}, fmt.Errorf("srd: no approved stat block %q", statBlockID)
	}
	w, ok := c.Weapon(sb.Attack.Weapon)
	if !ok {
		return session.CreatureSpec{}, fmt.Errorf("srd: no approved weapon %q", sb.Attack.Weapon)
	}
	return session.CreatureSpec{
		Name:          sb.Name,
		Team:          team,
		AC:            sb.AC,
		MaxHP:         sb.MaxHP,
		SpeedFt:       sb.SpeedFt,
		InitiativeMod: sb.InitiativeMod,
		Attack: session.MeleeAttack{
			Name: w.Name, ToHit: sb.Attack.ToHit,
			DiceCount: w.Damage.DiceCount, DiceSides: w.Damage.DiceSides,
			DamageBonus: sb.Attack.DamageBonus, DamageType: w.Damage.Type,
			ReachFt: w.ReachFt,
		},
	}, nil
}

func mustCreature(id string, team session.Team) session.CreatureSpec {
	c, err := contentsrd.Load()
	if err != nil {
		panic(err)
	}
	spec, err := Creature(c, id, team)
	if err != nil {
		panic(err)
	}
	return spec
}
