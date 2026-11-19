package forge

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mvgv/dndboard/content"
)

// The content format's wire shape (see package content).
type (
	docDamage struct {
		DiceCount int    `json:"dice_count"`
		DiceSides int    `json:"dice_sides"`
		Type      string `json:"type"`
	}
	docWeapon struct {
		ID      string    `json:"id"`
		Name    string    `json:"name"`
		Status  string    `json:"status"`
		Source  string    `json:"source"`
		Damage  docDamage `json:"damage"`
		ReachFt int       `json:"reach_ft"`
	}
	docAttack struct {
		Weapon      string `json:"weapon"`
		ToHit       int    `json:"to_hit"`
		DamageBonus int    `json:"damage_bonus"`
	}
	docStatBlock struct {
		ID            string    `json:"id"`
		Name          string    `json:"name"`
		Status        string    `json:"status"`
		Source        string    `json:"source"`
		AC            int       `json:"ac"`
		MaxHP         int       `json:"max_hp"`
		SpeedFt       int       `json:"speed_ft"`
		InitiativeMod int       `json:"initiative_mod"`
		Attack        docAttack `json:"attack"`
	}
	contentDoc struct {
		Version    int            `json:"version"`
		Weapons    []docWeapon    `json:"weapons"`
		StatBlocks []docStatBlock `json:"stat_blocks"`
	}
)

// document maps entries to a content document. With reviewed false every
// entry is a draft (used to validate an entry on its own); otherwise approved
// entries are approved and the rest stay drafts.
func document(version int, entries []Entry, sourceName string, reviewed bool) contentDoc {
	d := contentDoc{Version: version, Weapons: []docWeapon{}, StatBlocks: []docStatBlock{}}
	for _, e := range entries {
		status := string(content.Draft)
		if reviewed && e.Review == Approved {
			status = string(content.Approved)
		}
		x := e.StatBlock
		source := e.source(sourceName)
		weaponID := e.ID + "-" + slug(x.Attack.Name)
		d.Weapons = append(d.Weapons, docWeapon{
			ID: weaponID, Name: x.Attack.Name, Status: status, Source: source,
			Damage:  docDamage{DiceCount: x.Attack.DiceCount, DiceSides: x.Attack.DiceSides, Type: x.Attack.DamageType},
			ReachFt: x.Attack.ReachFt,
		})
		d.StatBlocks = append(d.StatBlocks, docStatBlock{
			ID: e.ID, Name: x.Name, Status: status, Source: source,
			AC: x.AC, MaxHP: x.HP, SpeedFt: x.SpeedFt, InitiativeMod: x.InitiativeMod,
			Attack: docAttack{Weapon: weaponID, ToHit: x.Attack.ToHit, DamageBonus: x.Attack.DamageBonus},
		})
	}
	return d
}

func (e Entry) source(sourceName string) string {
	pages := make([]string, len(e.Pages))
	for i, p := range e.Pages {
		pages[i] = strconv.Itoa(p)
	}
	label := "page "
	if len(pages) > 1 {
		label = "pages "
	}
	return fmt.Sprintf("%s, %s, %s%s", sourceName, e.StatBlock.Name, label, strings.Join(pages, "-"))
}

// Publish writes the reviewed Work as a content document at the given
// version. Approved entries are published approved, pending ones as drafts
// (which the game doesn't load). Entries with content problems are left out
// and their IDs returned. The document is validated before it is returned.
func Publish(w *Work, version int) (doc []byte, left []string, err error) {
	var keep []Entry
	for _, e := range w.Entries {
		if len(e.Problems) > 0 {
			left = append(left, e.ID)
			continue
		}
		keep = append(keep, e)
	}
	doc, err = json.MarshalIndent(document(version, keep, w.Source, true), "", "  ")
	if err != nil {
		return nil, nil, err
	}
	if _, err := content.Parse(doc); err != nil {
		return nil, nil, fmt.Errorf("forge: published document is invalid: %w", err)
	}
	return append(doc, '\n'), left, nil
}

// NextVersion returns the version to publish over an existing document:
// one more than its version, or 1 when there is none.
func NextVersion(existing []byte) (int, error) {
	if existing == nil {
		return 1, nil
	}
	var d struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(existing, &d); err != nil {
		return 0, fmt.Errorf("forge: read existing document version: %w", err)
	}
	return d.Version + 1, nil
}
