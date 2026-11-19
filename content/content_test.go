package content_test

import (
	"strings"
	"testing"

	"github.com/mvgv/dndboard/content"
	"github.com/mvgv/dndboard/content/srd"
)

const weapon = `{"id":"club","name":"Club","status":"approved","source":"s",
 "damage":{"dice_count":1,"dice_sides":4,"type":"Bludgeoning"},"reach_ft":5}`

func block(status, weaponID string) string {
	return `{"id":"thug","name":"Thug","status":"` + status + `","source":"s","ac":11,"max_hp":5,
 "speed_ft":30,"initiative_mod":0,"attack":{"weapon":"` + weaponID + `","ability":"str","to_hit":0,"damage_bonus":0}}`
}

func doc(weapons, blocks string) string {
	return `{"weapons":[` + weapons + `],"stat_blocks":[` + blocks + `]}`
}

func TestParseValid(t *testing.T) {
	c, err := content.Parse([]byte(doc(weapon, block("approved", "club"))))
	if err != nil {
		t.Fatal(err)
	}
	sb, ok := c.StatBlock("thug")
	if !ok || sb.Attack.Weapon != "club" || sb.InitiativeMod != 0 {
		t.Fatalf("stat block = %+v ok=%v", sb, ok)
	}
	if w, ok := c.Weapon("club"); !ok || w.Damage.DiceSides != 4 {
		t.Fatalf("weapon = %+v ok=%v", w, ok)
	}
}

func TestParseRejects(t *testing.T) {
	noName := strings.Replace(weapon, `"name":"Club",`, "", 1)
	noDamage := `{"id":"club","name":"Club","status":"approved","source":"s","reach_ft":5}`
	noInit := strings.Replace(block("approved", "club"), `"initiative_mod":0,`, "", 1)
	draftWeapon := strings.Replace(weapon, "approved", "draft", 1)
	cases := map[string]struct{ in, want string }{
		"missing string":        {doc(noName, ""), `missing required field "name"`},
		"missing object":        {doc(noDamage, ""), `missing required field "damage"`},
		"missing zero number":   {doc(weapon, noInit), `missing required field "initiative_mod"`},
		"wrong type":            {strings.Replace(doc(weapon, ""), `"reach_ft":5`, `"reach_ft":"5"`, 1), "reach_ft"},
		"unknown field":         {strings.Replace(doc(weapon, ""), `"reach_ft":5`, `"reach_ft":5,"bogus":1`, 1), "bogus"},
		"unknown weapon id":     {doc(weapon, block("approved", "sword")), `unknown id "sword"`},
		"unknown id, draft":     {doc(weapon, block("draft", "sword")), `unknown id "sword"`},
		"bad status":            {doc(weapon, block("maybe", "club")), `unknown status "maybe"`},
		"duplicate id":          {doc(weapon+","+weapon, ""), `duplicate id "club"`},
		"out of range":          {strings.Replace(doc(weapon, ""), `"dice_sides":4`, `"dice_sides":0`, 1), "dice_sides"},
		"not json":              {`{`, "content:"},
		"approved on draft ref": {doc(draftWeapon, block("approved", "club")), `approved but references draft weapon "club"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := content.Parse([]byte(tc.in))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestOnlyApprovedEntriesLoad(t *testing.T) {
	stick := strings.Replace(strings.Replace(weapon, "approved", "draft", 1), `"club"`, `"stick"`, 1)
	rookie := strings.Replace(block("draft", "stick"), `"thug"`, `"rookie"`, 1)
	c, err := content.Parse([]byte(doc(weapon+","+stick, block("approved", "club")+","+rookie)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Weapon("stick"); ok {
		t.Error("draft weapon loaded")
	}
	if _, ok := c.StatBlock("rookie"); ok {
		t.Error("draft stat block loaded")
	}
	if _, ok := c.StatBlock("thug"); !ok {
		t.Error("approved stat block missing")
	}
}

func TestOffHandAttackNeedsTwoDifferentLightWeapons(t *testing.T) {
	light := func(id string) string {
		return strings.Replace(strings.Replace(weapon, `"club"`, `"`+id+`"`, 1), `"reach_ft":5`, `"reach_ft":5,"properties":["light"]`, 1)
	}
	withOffHand := func(main, off string) string {
		b := strings.Replace(block("approved", main), `"attack"`,
			`"off_hand":{"weapon":"`+off+`","ability":"dex","to_hit":0,"damage_bonus":0},"attack"`, 1)
		return doc(weapon+","+light("dagger")+","+light("sickle"), b)
	}
	c, err := content.Parse([]byte(withOffHand("sickle", "dagger")))
	if err != nil {
		t.Fatal(err)
	}
	if sb, _ := c.StatBlock("thug"); sb.OffHand == nil || sb.OffHand.Weapon != "dagger" {
		t.Fatalf("off hand = %+v", sb.OffHand)
	}
	for in, want := range map[string]string{
		withOffHand("club", "dagger"):   "off_hand needs Light weapons",
		withOffHand("dagger", "dagger"): "off_hand must use a different weapon",
		withOffHand("sickle", "wand"):   `off_hand.weapon references unknown id "wand"`,
	} {
		if _, err := content.Parse([]byte(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want containing %q", err, want)
		}
	}
}

func TestEmbeddedSRDContentLoads(t *testing.T) {
	c, err := srd.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"fighter-1", "goblin-warrior"} {
		if _, ok := c.StatBlock(id); !ok {
			t.Errorf("stat block %q missing", id)
		}
	}
}
