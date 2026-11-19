package content

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
}

// Attack is a stat block's use of a weapon, with the modifiers already summed
// (ability modifier plus Proficiency Bonus where it applies).
type Attack struct {
	Weapon      string // Weapon ID
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
}

// Compendium holds the approved entries of a validated document.
type Compendium struct {
	version    int
	weapons    map[string]Weapon
	statBlocks map[string]StatBlock
}

// Version is the document's version. Every regeneration of a document bumps
// it, so a Campaign can record which version it was started with.
func (c *Compendium) Version() int { return c.version }

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

// Wire types: pointers mark required fields whose zero value is legal, so a
// missing field is distinguishable from 0.
type (
	rawDocument struct {
		Version    *int           `json:"version"`
		Weapons    []rawWeapon    `json:"weapons"`
		StatBlocks []rawStatBlock `json:"stat_blocks"`
	}
	rawDamage struct {
		DiceCount *int    `json:"dice_count"`
		DiceSides *int    `json:"dice_sides"`
		Type      *string `json:"type"`
	}
	rawWeapon struct {
		ID      *string    `json:"id"`
		Name    *string    `json:"name"`
		Status  *string    `json:"status"`
		Source  *string    `json:"source"`
		Damage  *rawDamage `json:"damage"`
		ReachFt *int       `json:"reach_ft"`
	}
	rawAttack struct {
		Weapon      *string `json:"weapon"`
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
	}
)

// Parse validates a JSON document and returns its approved entries. It fails
// with every problem found: malformed or mistyped JSON, unknown fields,
// missing required fields, duplicate IDs, out-of-range values, references to
// unknown IDs, and approved entries that reference unapproved ones.
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
	version := v.num("document", "version", doc.Version, 1)
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
	blocks := map[string]StatBlock{}
	for i, rs := range doc.StatBlocks {
		s, ok := v.statBlock(i, rs, weapons)
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

	c := &Compendium{version: version, weapons: map[string]Weapon{}, statBlocks: map[string]StatBlock{}}
	for id, w := range weapons {
		if w.Status == Approved {
			c.weapons[id] = w
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
	}
	return w, len(v.problems) == before
}

func (v *validator) statBlock(i int, r rawStatBlock, weapons map[string]Weapon) (StatBlock, bool) {
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
		s.Attack = Attack{
			Weapon:      v.str(where, "attack.weapon", r.Attack.Weapon),
			ToHit:       v.num(where, "attack.to_hit", r.Attack.ToHit, -100),
			DamageBonus: v.num(where, "attack.damage_bonus", r.Attack.DamageBonus, -100),
		}
		if id := s.Attack.Weapon; id != "" {
			w, ok := weapons[id]
			switch {
			case !ok:
				v.errf("%s: attack.weapon references unknown id %q", where, id)
			case s.Status == Approved && w.Status != Approved:
				v.errf("%s: approved but references %s weapon %q", where, w.Status, id)
			}
		}
	}
	return s, len(v.problems) == before
}
