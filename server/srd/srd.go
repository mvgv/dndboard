// Package srd holds hand-written SRD 5.2 creatures until the Forge produces
// the Compendium (build step 3). Each value cites the SRD entry it encodes.
package srd

import "github.com/mvgv/dndboard/server/session"

// Fighter is a level 1 Fighter (SRD 5.2 "Classes", Fighter).
//
//   - Str 16 (+3), Dex 12 (+1), Con 14 (+2); Proficiency Bonus +2 at level 1.
//   - Hit Points: D10 per Fighter level, so 10 + Con modifier = 12 at level 1.
//   - Chain Mail ("Equipment", Armor table): AC 16.
//   - Longsword ("Equipment", Martial Melee Weapons): 1d8 Slashing. Fighters
//     are proficient with Martial weapons; melee uses Strength ("Playing the
//     Game", Attack Roll Abilities): attack +3 +2 = +5, damage 1d8 + 3.
//   - Speed 30 ft.
func Fighter() session.CreatureSpec {
	return session.CreatureSpec{
		Name:          "Fighter",
		Team:          "party",
		AC:            16,
		MaxHP:         12,
		SpeedFt:       30,
		InitiativeMod: 1,
		Attack: session.MeleeAttack{
			Name: "Longsword", ToHit: 5, DiceCount: 1, DiceSides: 8,
			DamageBonus: 3, DamageType: "Slashing", ReachFt: 5,
		},
	}
}

// GoblinWarrior is the SRD 5.2 "Monsters" Goblin Warrior: AC 15, HP 10,
// Speed 30 ft., Initiative +2, Scimitar: Melee Attack Roll +4, reach 5 ft.,
// Hit: 5 (1d6 + 2) Slashing damage.
func GoblinWarrior() session.CreatureSpec {
	return session.CreatureSpec{
		Name:          "Goblin Warrior",
		Team:          "monsters",
		AC:            15,
		MaxHP:         10,
		SpeedFt:       30,
		InitiativeMod: 2,
		Attack: session.MeleeAttack{
			Name: "Scimitar", ToHit: 4, DiceCount: 1, DiceSides: 6,
			DamageBonus: 2, DamageType: "Slashing", ReachFt: 5,
		},
	}
}
