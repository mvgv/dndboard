package content_test

import (
	"strings"
	"testing"

	"github.com/mvgv/dndboard/content"
)

const sneakAttack = `{"id":"sneak-attack","name":"Sneak Attack","status":"approved","source":"s",
 "effects":[{"trigger":"attack_hit",
  "conditions":[
   {"any_of":[{"weapon_property":"finesse"},{"ranged_weapon":true}]},
   {"any_of":[{"roll":"advantage"},{"all_of":[{"ally_near_target_ft":5},{"roll":"not_disadvantage"}]}]}],
  "frequency":"once_per_turn","outcome":{"extra_damage":{"dice_count":1,"dice_sides":6,"type":"weapon"}}}]}`

const rage = `{"id":"rage","name":"Rage","status":"approved","source":"s",
 "activation":{"cost":"bonus_action","uses":2,"lasts":"until_end_of_next_turn",
  "extended_by":["attack_roll_against_enemy","bonus_action"],"max_rounds":100,"ends_on":["incapacitated"]},
 "effects":[
  {"trigger":"damage_taken","conditions":[{"damage_type":["Bludgeoning","Piercing","Slashing"]}],
   "frequency":"always","outcome":{"resistance":true}},
  {"trigger":"attack_hit","conditions":[{"attack_ability":"str"}],"frequency":"always","outcome":{"damage_bonus":2}}]}`

const handCoded = `{"id":"savage-attacker","name":"Savage Attacker","status":"approved","source":"s","requires_implementation":true}`

func featureDoc(features ...string) string {
	return `{"weapons":[` + weapon + `],"stat_blocks":[],"features":[` + strings.Join(features, ",") + `]}`
}

func TestParseFeatures(t *testing.T) {
	c, err := content.Parse([]byte(featureDoc(sneakAttack, rage, handCoded)))
	if err != nil {
		t.Fatal(err)
	}
	sa, ok := c.Feature("sneak-attack")
	if !ok || len(sa.Effects) != 1 || sa.Activation != nil || sa.RequiresImplementation {
		t.Fatalf("sneak attack = %+v ok=%v", sa, ok)
	}
	e := sa.Effects[0]
	if e.Trigger != content.AttackHit || e.Frequency != content.OncePerTurn || len(e.Conditions) != 2 ||
		e.Outcome.Kind != content.ExtraDamage || e.Outcome.Dice.DiceSides != 6 || e.Outcome.Dice.Type != content.WeaponDamageType {
		t.Fatalf("sneak attack effect = %+v", e)
	}
	alt := e.Conditions[1]
	if alt.Kind != content.AnyOf || len(alt.Of) != 2 || alt.Of[1].Kind != content.AllOf || alt.Of[1].Of[0].Feet != 5 {
		t.Fatalf("advantage-or-ally condition = %+v", alt)
	}
	r, _ := c.Feature("rage")
	if a := r.Activation; a == nil || a.Cost != content.BonusAction || a.Uses != 2 || a.MaxRounds != 100 ||
		len(a.ExtendedBy) != 2 || len(a.EndsOn) != 1 {
		t.Fatalf("rage activation = %+v", r.Activation)
	}
	if o := r.Effects[0].Outcome; o.Kind != content.Resistance {
		t.Fatalf("rage resistance outcome = %+v", o)
	}
	if o := r.Effects[1].Outcome; o.Kind != content.DamageBonus || o.Bonus != 2 {
		t.Fatalf("rage damage outcome = %+v", o)
	}
	if h, _ := c.Feature("savage-attacker"); !h.RequiresImplementation || len(h.Effects) != 0 {
		t.Fatalf("hand-coded feature = %+v", h)
	}
}

func TestStatBlockReferencesFeatures(t *testing.T) {
	withFeatures := strings.Replace(block("approved", "club"), `"attack"`, `"features":["rage"],"attack"`, 1)
	d := `{"weapons":[` + weapon + `],"stat_blocks":[` + withFeatures + `],"features":[` + rage + `]}`
	c, err := content.Parse([]byte(d))
	if err != nil {
		t.Fatal(err)
	}
	if sb, _ := c.StatBlock("thug"); len(sb.Features) != 1 || sb.Features[0] != "rage" {
		t.Fatalf("features = %v", sb.Features)
	}

	unknown := strings.Replace(d, `"features":["rage"]`, `"features":["frenzy"]`, 1)
	if _, err := content.Parse([]byte(unknown)); err == nil || !strings.Contains(err.Error(), `unknown feature "frenzy"`) {
		t.Fatalf("unknown feature ref: err = %v", err)
	}
	draft := strings.Replace(d, `"name":"Rage","status":"approved"`, `"name":"Rage","status":"draft"`, 1)
	if _, err := content.Parse([]byte(draft)); err == nil || !strings.Contains(err.Error(), `references draft feature "rage"`) {
		t.Fatalf("draft feature ref: err = %v", err)
	}
}

func TestParseRejectsInvalidEffects(t *testing.T) {
	sub := func(s, old, new string) string {
		if !strings.Contains(s, old) {
			t.Fatalf("fixture lacks %s", old)
		}
		return strings.Replace(s, old, new, 1)
	}
	sneakOutcome := `{"extra_damage":{"dice_count":1,"dice_sides":6,"type":"weapon"}}`
	finesseOrRanged := `{"any_of":[{"weapon_property":"finesse"},{"ranged_weapon":true}]}`
	cases := map[string]struct{ in, want string }{
		"no effects": {`{"id":"x","name":"X","status":"approved","source":"s"}`,
			"needs at least one effect"},
		"empty effects": {`{"id":"x","name":"X","status":"approved","source":"s","effects":[]}`,
			"needs at least one effect"},
		"hand-coded with effects": {sub(sneakAttack, `"source":"s",`, `"source":"s","requires_implementation":true,`),
			"requires implementation and must not declare effects"},
		"hand-coded with activation": {sub(handCoded, `"requires_implementation":true`,
			`"requires_implementation":true,"activation":{"cost":"bonus_action","uses":1,"lasts":"until_end_of_next_turn"}`),
			"requires implementation and must not declare an activation"},
		"unknown trigger":   {sub(sneakAttack, `"attack_hit"`, `"spell_cast"`), `unknown trigger "spell_cast"`},
		"missing trigger":   {sub(sneakAttack, `"trigger":"attack_hit",`, ``), `missing required field "trigger"`},
		"unknown frequency": {sub(sneakAttack, `"once_per_turn"`, `"twice_per_turn"`), `unknown frequency "twice_per_turn"`},
		"missing frequency": {sub(sneakAttack, `"frequency":"once_per_turn",`, ``), `missing required field "frequency"`},
		"missing outcome":   {sub(sneakAttack, `,"outcome":`+sneakOutcome, ``), `missing required field "outcome"`},
		"empty outcome":     {sub(sneakAttack, sneakOutcome, `{}`), "outcome must set exactly one of"},
		"two outcomes":      {sub(sneakAttack, `"type":"weapon"}`, `"type":"weapon"},"damage_bonus":1`), "outcome must set exactly one of"},
		"bad dice":          {sub(sneakAttack, `"dice_sides":6`, `"dice_sides":0`), "dice_sides"},
		"bad damage type":   {sub(sneakAttack, `"type":"weapon"`, `"type":"Shadow"`), `unknown damage type "Shadow"`},
		"outcome does not fit trigger": {sub(rage, `"outcome":{"damage_bonus":2}`, `"outcome":{"resistance":true}`),
			`outcome "resistance" does not apply to trigger "attack_hit"`},
		"false resistance": {sub(rage, `"resistance":true`, `"resistance":false`), `"resistance" must be true`},
		"empty condition":  {sub(sneakAttack, `{"weapon_property":"finesse"}`, `{}`), "condition must set exactly one of"},
		"two keys in one condition": {sub(sneakAttack, `{"weapon_property":"finesse"}`, `{"weapon_property":"finesse","roll":"advantage"}`),
			"condition must set exactly one of"},
		"unknown property": {sub(sneakAttack, `"finesse"`, `"sharp"`), `unknown weapon property "sharp"`},
		"unknown roll":     {sub(sneakAttack, `"roll":"advantage"`, `"roll":"lucky"`), `unknown roll "lucky"`},
		"unknown ability":  {sub(rage, `"attack_ability":"str"`, `"attack_ability":"cha"`), `unknown attack ability "cha"`},
		"distance not in squares": {sub(sneakAttack, `"ally_near_target_ft":5`, `"ally_near_target_ft":7`),
			"multiple of 5"},
		"empty any_of":      {sub(sneakAttack, finesseOrRanged, `{"any_of":[]}`), `"any_of" needs at least one condition`},
		"empty damage_type": {sub(rage, `["Bludgeoning","Piercing","Slashing"]`, `[]`), `"damage_type" needs at least one damage type`},
		"condition does not fit trigger": {sub(rage, `{"attack_ability":"str"}`, `{"damage_type":["Fire"]}`),
			`condition "damage_type" does not apply to trigger "attack_hit"`},
		"nested condition does not fit trigger": {sub(rage, `[{"damage_type":["Bludgeoning","Piercing","Slashing"]}]`, `[{"any_of":[{"roll":"advantage"}]}]`),
			`condition "roll" does not apply to trigger "damage_taken"`},
		"unknown condition field": {sub(sneakAttack, `{"weapon_property":"finesse"}`, `{"moon_phase":"full"}`), "moon_phase"},
		"unknown activation cost": {sub(rage, `"cost":"bonus_action"`, `"cost":"free"`), `unknown activation cost "free"`},
		"missing activation cost": {sub(rage, `"cost":"bonus_action",`, ``), `missing required field "activation.cost"`},
		"zero uses":               {sub(rage, `"uses":2`, `"uses":0`), "uses"},
		"unknown duration":        {sub(rage, `"lasts":"until_end_of_next_turn"`, `"lasts":"forever"`), `unknown duration "forever"`},
		"unknown extension":       {sub(rage, `"attack_roll_against_enemy"`, `"shouting"`), `unknown extension "shouting"`},
		"unknown ending":          {sub(rage, `"ends_on":["incapacitated"]`, `"ends_on":["sunset"]`), `unknown ending "sunset"`},
		"zero max rounds":         {sub(rage, `"max_rounds":100`, `"max_rounds":0`), "max_rounds"},
		"duplicate feature id":    {sneakAttack + "," + sneakAttack, `duplicate id "sneak-attack"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := content.Parse([]byte(featureDoc(tc.in)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestParseRejectsInvalidWeaponTraits(t *testing.T) {
	for in, want := range map[string]string{
		strings.Replace(weapon, `"Bludgeoning"`, `"Bludgeon"`, 1):                       `unknown damage type "Bludgeon"`,
		strings.Replace(weapon, `"reach_ft":5`, `"reach_ft":5,"properties":["sharp"]`, 1): `unknown weapon property "sharp"`,
	} {
		_, err := content.Parse([]byte(doc(in, "")))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want containing %q", err, want)
		}
	}
}

func TestAttackAbilityIsRequired(t *testing.T) {
	noAbility := strings.Replace(block("approved", "club"), `"ability":"str",`, "", 1)
	_, err := content.Parse([]byte(doc(weapon, noAbility)))
	if err == nil || !strings.Contains(err.Error(), `missing required field "attack.ability"`) {
		t.Fatalf("err = %v", err)
	}
	badAbility := strings.Replace(block("approved", "club"), `"ability":"str"`, `"ability":"wis"`, 1)
	if _, err := content.Parse([]byte(doc(weapon, badAbility))); err == nil || !strings.Contains(err.Error(), `unknown attack ability "wis"`) {
		t.Fatalf("err = %v", err)
	}
}
