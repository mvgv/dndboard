// Package srd embeds the SRD 5.2 content in the content format.
//
// monsters.json is written by the Forge (forge publish) from the SRD 5.2.1
// PDF after review; never edit it by hand, re-run the Forge instead.
// characters.json is hand-written: pre-built characters come from the
// character-creation rules, which the Forge does not process yet.
package srd

import (
	_ "embed"

	"github.com/mvgv/dndboard/content"
)

var (
	//go:embed characters.json
	characters []byte
	//go:embed monsters.json
	monsters []byte
)

// Characters parses the hand-written pre-built characters and returns their approved entries.
func Characters() (*content.Compendium, error) { return content.Parse(characters) }

// Monsters parses the Forge-generated monster stat blocks and returns their approved entries.
func Monsters() (*content.Compendium, error) { return content.Parse(monsters) }
