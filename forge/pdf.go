package forge

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ReadPDF extracts the text of every page of a PDF. The SRD prints two
// columns per page, so each page's glyphs are split at the vertical midline
// and read column by column, top to bottom, which keeps a stat block's lines
// together instead of interleaving them with the neighbouring column.
func ReadPDF(path string) (Source, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return Source{}, fmt.Errorf("forge: open %s: %w", path, err)
	}
	defer f.Close()

	var src Source
	for n := 1; n <= r.NumPage(); n++ {
		p := r.Page(n)
		if p.V.IsNull() {
			continue
		}
		text, err := pageText(p)
		if err != nil {
			return Source{}, fmt.Errorf("forge: %s page %d: %w", path, n, err)
		}
		src.Pages = append(src.Pages, Page{Number: n, Text: text})
	}
	return src, nil
}

func pageText(p pdf.Page) (text string, err error) {
	// The pdf package panics on content streams it can't decode.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("unreadable content: %v", r)
		}
	}()
	width := p.V.Key("MediaBox").Index(2).Float64()
	var cols [2][]pdf.Text
	for _, g := range p.Content().Text {
		if strings.TrimSpace(g.S) == "" {
			continue
		}
		i := 0
		if g.X >= width/2 {
			i = 1
		}
		cols[i] = append(cols[i], g)
	}

	var b strings.Builder
	for _, col := range cols {
		sort.SliceStable(col, func(i, j int) bool { return col[i].Y > col[j].Y })
		var lines [][]pdf.Text
		for _, g := range col {
			if k := len(lines) - 1; k >= 0 && math.Abs(lines[k][0].Y-g.Y) < 0.5*g.FontSize {
				lines[k] = append(lines[k], g)
				continue
			}
			lines = append(lines, []pdf.Text{g})
		}
		for _, line := range lines {
			sort.SliceStable(line, func(i, j int) bool { return line[i].X < line[j].X })
			var end float64
			for i, g := range line {
				if i > 0 && g.X-end > 0.15*g.FontSize {
					b.WriteByte(' ')
				}
				b.WriteString(g.S)
				end = g.X + g.W
			}
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}
