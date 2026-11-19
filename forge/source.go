package forge

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Source is a source document's text, page by page.
type Source struct {
	Pages []Page
}

// Page is one page's text. Number is the PDF page number, which in the SRD
// 5.2.1 matches the printed page number.
type Page struct {
	Number int
	Text   string
}

var pageMarker = regexp.MustCompile(`(?m)^<!-- page (\d+) -->\n`)

// String renders the source as text with a "<!-- page N -->" line before
// each page, the same layout as reference/srd-5.2. ParseSource reads it back.
func (s Source) String() string {
	var b strings.Builder
	for _, p := range s.Pages {
		fmt.Fprintf(&b, "<!-- page %d -->\n%s", p.Number, p.Text)
	}
	return b.String()
}

// ParseSource reads text written by Source.String.
func ParseSource(text string) (Source, error) {
	idx := pageMarker.FindAllStringSubmatchIndex(text, -1)
	if len(idx) == 0 {
		return Source{}, fmt.Errorf("forge: source text has no page markers")
	}
	var s Source
	for i, m := range idx {
		n, _ := strconv.Atoi(text[m[2]:m[3]])
		end := len(text)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		s.Pages = append(s.Pages, Page{Number: n, Text: text[m[1]:end]})
	}
	return s, nil
}

// Excerpt is the source text of one stat block, as shown to Claude and to
// the reviewer.
type Excerpt struct {
	Name  string
	Pages []int
	Text  string
}

// A stat block starts with its name on a line of its own, followed by its
// size, type and alignment ("Small Fey (Goblinoid), Chaotic Neutral").
var sizeLine = regexp.MustCompile(`^(Tiny|Small|Medium|Large|Huge|Gargantuan)\b.*,`)

const maxExcerptLines = 80

// StatBlock finds the stat block called name and returns its text, up to the
// next stat block. Two-column pages can still put a neighbour's lines inside
// the excerpt; the extraction and the review are where those get caught.
func (s Source) StatBlock(name string) (Excerpt, error) {
	type line struct {
		page int
		text string
	}
	var lines []line
	for _, p := range s.Pages {
		for _, t := range strings.Split(strings.TrimRight(p.Text, "\n"), "\n") {
			if t = strings.TrimSpace(t); t != "" {
				lines = append(lines, line{p.Number, t})
			}
		}
	}
	heading := func(i int) bool { return i+1 < len(lines) && sizeLine.MatchString(lines[i+1].text) }

	start := -1
	for i := range lines {
		if lines[i].text == name && heading(i) {
			if start >= 0 {
				return Excerpt{}, fmt.Errorf("forge: stat block %q appears twice (pages %d and %d)", name, lines[start].page, lines[i].page)
			}
			start = i
		}
	}
	if start < 0 {
		return Excerpt{}, fmt.Errorf("forge: stat block %q not found", name)
	}

	ex := Excerpt{Name: name}
	var b strings.Builder
	for i := start; i < len(lines) && i < start+maxExcerptLines; i++ {
		if i > start && heading(i) {
			break
		}
		if n := len(ex.Pages); n == 0 || ex.Pages[n-1] != lines[i].page {
			ex.Pages = append(ex.Pages, lines[i].page)
			fmt.Fprintf(&b, "<!-- page %d -->\n", lines[i].page)
		}
		b.WriteString(lines[i].text)
		b.WriteByte('\n')
	}
	ex.Text = b.String()
	return ex, nil
}
