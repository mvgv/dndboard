package content

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Status is an entry's review status. Only Approved entries load into the game.
type Status string

const (
	Draft    Status = "draft"
	Approved Status = "approved"
)

// Damage is a weapon's damage: DiceCount d DiceSides of Type.
type Damage struct {
	DiceCount int
	DiceSides int
	Type      string
}

// Weapon is a weapon entry (SRD "Equipment").
type Weapon struct {
	ID      string
	Name    string
	Status  Status
	Source  string // where the rules text lives, e.g. an SRD section
	Damage  Damage
	ReachFt int
	// Properties are the weapon's properties, e.g. "finesse" (SRD "Equipment").
	Properties []string
	// Ranged: a Ranged weapon rather than a Melee one.
	Ranged bool
}

// Attack is a stat block's use of a weapon, with the modifiers already summed
// (ability modifier plus Proficiency Bonus where it applies).
type Attack struct {
	Weapon      string // Weapon ID
	Ability     string // "str" or "dex": the modifier used for attack and damage
	ToHit       int
	DamageBonus int
}

// StatBlock is a creature entry (monster or pre-built character).
type StatBlock struct {
	ID            string
	Name          string
	Status        Status
	Source        string
	AC            int
	MaxHP         int
	SpeedFt       int
	InitiativeMod int
	Attack        Attack
	// OffHand, when set, is the extra attack with a second Light weapon after
	// attacking with a Light one (SRD "Equipment", Light). Its damage bonus
	// leaves out a positive ability modifier.
	OffHand  *Attack
	Features []string // Feature IDs
}

// Compendium holds the approved entries of a validated document.
type Compendium struct {
	weapons    map[string]Weapon
	statBlocks map[string]StatBlock
	features   map[string]Feature
}

// Weapon returns the approved weapon with the given ID.
func (c *Compendium) Weapon(id string) (Weapon, bool) {
	w, ok := c.weapons[id]
	return w, ok
}

// StatBlock returns the approved stat block with the given ID.
func (c *Compendium) StatBlock(id string) (StatBlock, bool) {
	s, ok := c.statBlocks[id]
	return s, ok
}

// Feature returns the approved feature with the given ID.
func (c *Compendium) Feature(id string) (Feature, bool) {
	f, ok := c.features[id]
	return f, ok
}

// Wire types: pointers mark required fields whose zero value is legal, so a
// missing field is distinguishable from 0.
type (
	rawDocument struct {
		Weapons    []rawWeapon    `json:"weapons"`
		StatBlocks []rawStatBlock `json:"stat_blocks"`
		Features   []rawFeature   `json:"features"`
	}
	rawDamage struct {
		DiceCount *int    `json:"dice_count"`
		DiceSides *int    `json:"dice_sides"`
		Type      *string `json:"type"`
	}
	rawWeapon struct {
		ID         *string    `json:"id"`
		Name       *string    `json:"name"`
		Status     *string    `json:"status"`
		Source     *string    `json:"source"`
		Damage     *rawDamage `json:"damage"`
		ReachFt    *int       `json:"reach_ft"`
		Properties []string   `json:"properties"`
		Ranged     bool       `json:"ranged"`
	}
	rawAttack struct {
		Weapon      *string `json:"weapon"`
		Ability     *string `json:"ability"`
		ToHit       *int    `json:"to_hit"`
		DamageBonus *int    `json:"damage_bonus"`
	}
	rawStatBlock struct {
		ID            *string    `json:"id"`
		Name          *string    `json:"name"`
		Status        *string    `json:"status"`
		Source        *string    `json:"source"`
		AC            *int       `json:"ac"`
		MaxHP         *int       `json:"max_hp"`
		SpeedFt       *int       `json:"speed_ft"`
		InitiativeMod *int       `json:"initiative_mod"`
		Attack        *rawAttack `json:"attack"`
		OffHand       *rawAttack `json:"off_hand"`
		Features      []string   `json:"features"`
	}
)

// Parse validates a JSON document and returns its approved entries. It fails
// with every problem found: malformed or mistyped JSON, unknown fields,
// missing required fields, duplicate IDs, out-of-range values, values outside
// a vocabulary (damage types, weapon properties, effect triggers, conditions
// and outcomes), effects whose conditions or outcome don't fit their trigger,
// references to unknown IDs, and approved entries that reference unapproved
// ones.
func Parse(data []byte) (*Compendium, error) {
	var doc rawDocument
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("content: trailing data after document")
	}

	v := &validator{}
	weapons := map[string]Weapon{}
	for i, rw := range doc.Weapons {
		w, ok := v.weapon(i, rw)
		if !ok {
			continue
		}
		if _, dup := weapons[w.ID]; dup {
			v.errf("weapons[%d]: duplicate id %q", i, w.ID)
			continue
		}
		weapons[w.ID] = w
	}
	features := map[string]Feature{}
	for i, rf := range doc.Features {
		f, ok := v.feature(i, rf)
		if !ok {
			continue
		}
		if _, dup := features[f.ID]; dup {
			v.errf("features[%d]: duplicate id %q", i, f.ID)
			continue
		}
		features[f.ID] = f
	}
	blocks := map[string]StatBlock{}
	for i, rs := range doc.StatBlocks {
		s, ok := v.statBlock(i, rs, weapons, features)
		if !ok {
			continue
		}
		if _, dup := blocks[s.ID]; dup {
			v.errf("stat_blocks[%d]: duplicate id %q", i, s.ID)
			continue
		}
		blocks[s.ID] = s
	}
	if len(v.problems) > 0 {
		return nil, fmt.Errorf("content: invalid document:\n  %s", strings.Join(v.problems, "\n  "))
	}

	c := &Compendium{weapons: map[string]Weapon{}, statBlocks: map[string]StatBlock{}, features: map[string]Feature{}}
	for id, w := range weapons {
		if w.Status == Approved {
			c.weapons[id] = w
		}
	}
	for id, f := range features {
		if f.Status == Approved {
			c.features[id] = f
		}
	}
	for id, s := range blocks {
		if s.Status == Approved {
			c.statBlocks[id] = s
		}
	}
	return c, nil
}

type validator struct{ problems []string }

func (v *validator) errf(format string, args ...any) {
	v.problems = append(v.problems, fmt.Sprintf(format, args...))
}

func (v *validator) str(where, field string, p *string) string {
	if p == nil || *p == "" {
		v.errf("%s: missing required field %q", where, field)
		return ""
	}
	return *p
}

func (v *validator) num(where, field string, p *int, min int) int {
	if p == nil {
		v.errf("%s: missing required field %q", where, field)
		return 0
	}
	if *p < min {
		v.errf("%s: %q = %d, must be at least %d", where, field, *p, min)
	}
	return *p
}

func (v *validator) status(where string, p *string) Status {
	s := v.str(where, "status", p)
	switch Status(s) {
	case Draft, Approved:
		return Status(s)
	case "":
	default:
		v.errf("%s: unknown status %q (want %q or %q)", where, s, Draft, Approved)
	}
	return ""
}

func (v *validator) weapon(i int, r rawWeapon) (Weapon, bool) {
	before := len(v.problems)
	where := fmt.Sprintf("weapons[%d]", i)
	w := Weapon{
		ID:     v.str(where, "id", r.ID),
		Name:   v.str(where, "name", r.Name),
		Status: v.status(where, r.Status),
		Source: v.str(where, "source", r.Source),
		Ranged: r.Ranged,
	}
	if w.ID != "" {
		where = fmt.Sprintf("weapon %q", w.ID)
	}
	w.ReachFt = v.num(where, "reach_ft", r.ReachFt, 0)
	if r.Damage == nil {
		v.errf("%s: missing required field %q", where, "damage")
	} else {
		w.Damage = Damage{
			DiceCount: v.num(where, "damage.dice_count", r.Damage.DiceCount, 1),
			DiceSides: v.num(where, "damage.dice_sides", r.Damage.DiceSides, 2),
			Type:      v.str(where, "damage.type", r.Damage.Type),
		}
		if w.Damage.Type != "" {
			oneOf(v, where, "damage type", w.Damage.Type, DamageTypes)
		}
	}
	for _, p := range r.Properties {
		oneOf(v, where, "weapon property", p, WeaponProperties)
	}
	w.Properties = r.Properties
	return w, len(v.problems) == before
}

func (v *validator) statBlock(i int, r rawStatBlock, weapons map[string]Weapon, features map[string]Feature) (StatBlock, bool) {
	before := len(v.problems)
	where := fmt.Sprintf("stat_blocks[%d]", i)
	s := StatBlock{
		ID:     v.str(where, "id", r.ID),
		Name:   v.str(where, "name", r.Name),
		Status: v.status(where, r.Status),
		Source: v.str(where, "source", r.Source),
	}
	if s.ID != "" {
		where = fmt.Sprintf("stat block %q", s.ID)
	}
	s.AC = v.num(where, "ac", r.AC, 1)
	s.MaxHP = v.num(where, "max_hp", r.MaxHP, 1)
	s.SpeedFt = v.num(where, "speed_ft", r.SpeedFt, 0)
	s.InitiativeMod = v.num(where, "initiative_mod", r.InitiativeMod, -100)
	if r.Attack == nil {
		v.errf("%s: missing required field %q", where, "attack")
	} else {
		s.Attack = v.attack(where, "attack", r.Attack, s.Status, weapons)
	}
	if r.OffHand != nil {
		off := v.attack(where, "off_hand", r.OffHand, s.Status, weapons)
		s.OffHand = &off
		main, mainOK := weapons[s.Attack.Weapon]
		second, secondOK := weapons[off.Weapon]
		switch {
		case !mainOK || !secondOK:
		case !slices.Contains(main.Properties, "light") || !slices.Contains(second.Properties, "light"):
			v.errf("%s: off_hand needs Light weapons in both hands", where)
		case main.ID == second.ID:
			v.errf("%s: off_hand must use a different weapon than attack", where)
		}
	}
	for _, id := range r.Features {
		f, ok := features[id]
		switch {
		case !ok:
			v.errf("%s: references unknown feature %q", where, id)
		case s.Status == Approved && f.Status != Approved:
			v.errf("%s: approved but references %s feature %q", where, f.Status, id)
		}
	}
	s.Features = r.Features
	return s, len(v.problems) == before
}

func (v *validator) attack(where, field string, r *rawAttack, status Status, weapons map[string]Weapon) Attack {
	a := Attack{
		Weapon:      v.str(where, field+".weapon", r.Weapon),
		Ability:     v.str(where, field+".ability", r.Ability),
		ToHit:       v.num(where, field+".to_hit", r.ToHit, -100),
		DamageBonus: v.num(where, field+".damage_bonus", r.DamageBonus, -100),
	}
	if id := a.Weapon; id != "" {
		w, ok := weapons[id]
		switch {
		case !ok:
			v.errf("%s: %s.weapon references unknown id %q", where, field, id)
		case status == Approved && w.Status != Approved:
			v.errf("%s: approved but references %s weapon %q", where, w.Status, id)
		}
	}
	if a.Ability != "" {
		oneOf(v, where, "attack ability", a.Ability, Abilities)
	}
	return a
}
