// Package srd embeds the hand-written SRD 5.2 content, in the content format,
// until the Forge produces the Compendium (build step 3).
package srd

import (
	_ "embed"

	"github.com/mvgv/dndboard/content"
)

//go:embed srd.json
var data []byte

// Load parses and validates the embedded document and returns its approved entries.
func Load() (*content.Compendium, error) { return content.Parse(data) }
