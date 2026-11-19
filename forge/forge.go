package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mvgv/dndboard/content"
)

// Extractor reads the structured fields out of one stat block's text.
// *Claude is the implementation.
type Extractor interface {
	ExtractStatBlock(ctx context.Context, ex Excerpt) (Extraction, error)
}

// Work is the Forge's working file between runs: every extracted entry with
// its source text, what Claude read, the reviewer's corrections and verdict.
// Publish turns it into a content document.
type Work struct {
	Source  string  `json:"source"` // e.g. "SRD 5.2.1"
	Entries []Entry `json:"entries"`
}

// Review statuses of an Entry in the Work file.
const (
	Pending  = "pending"
	Approved = "approved"
)

// Entry is one stat block under review.
type Entry struct {
	ID         string     `json:"id"`
	Pages      []int      `json:"pages"`
	SourceText string     `json:"source_text"`
	Extracted  Extraction `json:"extracted"`  // Claude's answer, kept as is
	StatBlock  Extraction `json:"stat_block"` // Extracted with the corrections applied
	// Warnings are checks against the source text that failed, and parts of
	// the stat block content format v0 can't hold. The reviewer reads them.
	Warnings []string `json:"warnings,omitempty"`
	// Problems are content validation failures. An entry with problems can't
	// be approved or published until corrected.
	Problems    []string     `json:"problems,omitempty"`
	Review      string       `json:"review"`
	Corrections []Correction `json:"corrections,omitempty"`
}

// Correction is one field the reviewer changed.
type Correction struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// Extract finds each named stat block in src, has ex extract it, and checks
// the result, returning the entries pending review.
func Extract(ctx context.Context, src Source, sourceName string, names []string, ex Extractor) (*Work, error) {
	w := &Work{Source: sourceName}
	// Find every stat block before the first (paid) Claude call, so a
	// duplicate or missing name fails for free.
	seen := map[string]bool{}
	excerpts := make([]Excerpt, len(names))
	for i, name := range names {
		if seen[slug(name)] {
			return nil, fmt.Errorf("forge: stat block %q requested twice", name)
		}
		seen[slug(name)] = true
		excerpt, err := src.StatBlock(name)
		if err != nil {
			return nil, err
		}
		excerpts[i] = excerpt
	}
	for i, excerpt := range excerpts {
		x, err := ex.ExtractStatBlock(ctx, excerpt)
		if err != nil {
			return nil, fmt.Errorf("forge: %s: %w", names[i], err)
		}
		e := Entry{
			ID:         slug(names[i]),
			Pages:      excerpt.Pages,
			SourceText: excerpt.Text,
			Extracted:  x,
			StatBlock:  x,
			Review:     Pending,
		}
		e.check(w.Source)
		w.Entries = append(w.Entries, e)
	}
	return w, nil
}

// check recomputes the entry's warnings and problems from its current fields.
func (e *Entry) check(sourceName string) {
	e.Warnings, e.Problems = nil, nil
	x := e.StatBlock
	text := normalize(e.SourceText)
	want := func(what, pattern string) {
		if !regexp.MustCompile(pattern).MatchString(text) {
			e.Warnings = append(e.Warnings, fmt.Sprintf("%s not found in the source text", what))
		}
	}
	q := regexp.QuoteMeta
	want(fmt.Sprintf("name %q", x.Name), `(?m)^`+q(x.Name)+`$`)
	want(fmt.Sprintf("AC %d", x.AC), `\bAC `+strconv.Itoa(x.AC)+`\b`)
	want(fmt.Sprintf("HP %d", x.HP), `\bHP `+strconv.Itoa(x.HP)+`\b`)
	want(fmt.Sprintf("Speed %d ft.", x.SpeedFt), `\bSpeed `+strconv.Itoa(x.SpeedFt)+` ft\.`)
	want(fmt.Sprintf("Initiative %s", signed(x.InitiativeMod)), `\bInitiative `+q(signed(x.InitiativeMod))+`\b`)
	a := x.Attack
	want(fmt.Sprintf("melee attack %q", a.Name), `\b`+q(a.Name)+`\. Melee (or Ranged )?Attack Roll`)
	want(fmt.Sprintf("%s to hit %s, reach %d ft.", a.Name, signed(a.ToHit), a.ReachFt),
		q(a.Name)+`\. Melee (or Ranged )?Attack Roll: `+q(signed(a.ToHit))+`, reach `+strconv.Itoa(a.ReachFt)+` ft\.`)
	want(fmt.Sprintf("%s damage %s %s", a.Name, damage(a), a.DamageType),
		q(a.Name)+`\. Melee [^()]*Hit: \d+ \(`+q(damage(a))+`\) `+q(a.DamageType)+` damage`)
	for _, o := range x.Omitted {
		e.Warnings = append(e.Warnings, fmt.Sprintf("not in content format v0, the game will play without it: %s", o))
	}

	doc, err := json.Marshal(document(1, []Entry{*e}, sourceName, false))
	if err == nil {
		_, err = content.Parse(doc)
	}
	if err != nil {
		for _, line := range strings.Split(err.Error(), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasSuffix(line, "invalid document:") {
				e.Problems = append(e.Problems, line)
			}
		}
	}
}

// normalize undoes the PDF's line wrapping and typographic minus signs so
// values can be matched against the source text.
func normalize(s string) string {
	s = strings.ReplaceAll(s, "−", "-")
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "<!-- page ") {
			continue
		}
		lines = append(lines, l)
	}
	// Keep line starts for the name check, but join wrapped lines too.
	return strings.Join(lines, "\n") + "\n" + strings.Join(lines, " ")
}

func signed(n int) string {
	if n < 0 {
		return strconv.Itoa(n)
	}
	return "+" + strconv.Itoa(n)
}

func damage(a ExtractionAttack) string {
	d := fmt.Sprintf("%dd%d", a.DiceCount, a.DiceSides)
	switch {
	case a.DamageBonus > 0:
		return fmt.Sprintf("%s + %d", d, a.DamageBonus)
	case a.DamageBonus < 0:
		return fmt.Sprintf("%s - %d", d, -a.DamageBonus)
	}
	return d
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}
