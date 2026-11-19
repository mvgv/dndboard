// Package srd builds session creatures from the SRD 5.2 content loaded through
// the content format (content/srd): the hand-written pre-built characters and
// the Forge-generated monster Compendium.
package srd

import (
	"fmt"
	"sync"

	"github.com/mvgv/dndboard/content"
	contentsrd "github.com/mvgv/dndboard/content/srd"
	"github.com/mvgv/dndboard/server/session"
)

// Fighter is the level 1 Fighter stat block (id "fighter-1"), on the party team.
func Fighter() session.CreatureSpec { return must(creature(characters, "fighter-1", "party")) }

// GoblinWarrior is the SRD Goblin Warrior stat block, on the monsters team. Its
// Bonus Action, Nimble Escape (SRD 5.2 "Monsters", Goblin Warrior), is set here
// because the content format does not carry traits yet (the Forge lists it as
// omitted in review).
func GoblinWarrior() session.CreatureSpec {
	g := must(Monster("goblin-warrior"))
	g.NimbleEscape = true // Disengage; Hide is not modeled yet
	return g
}

// Monster builds the approved monster stat block with the given ID, on the
// monsters team. Draft stat blocks are not available.
func Monster(id string) (session.CreatureSpec, error) { return creature(monsters, id, "monsters") }

// MonstersVersion is the version of the monster Compendium in use.
func MonstersVersion() int { return must(monsters()).Version() }

var (
	characters = sync.OnceValues(contentsrd.Characters)
	monsters   = sync.OnceValues(contentsrd.Monsters)
)

// creature builds a CreatureSpec from an approved stat block and its weapon.
func creature(load func() (*content.Compendium, error), statBlockID string, team session.Team) (session.CreatureSpec, error) {
	c, err := load()
	if err != nil {
		return session.CreatureSpec{}, err
	}
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

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
