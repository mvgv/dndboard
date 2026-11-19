package forge

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// field is one correctable field of an extracted stat block.
type field struct {
	name string
	get  func(*Extraction) string
	set  func(*Extraction, string) error
}

func intField(name string, p func(*Extraction) *int) field {
	return field{
		name: name,
		get:  func(x *Extraction) string { return strconv.Itoa(*p(x)) },
		set: func(x *Extraction, v string) error {
			n, err := strconv.Atoi(strings.Replace(v, "−", "-", 1))
			if err != nil {
				return fmt.Errorf("%s must be a whole number, got %q", name, v)
			}
			*p(x) = n
			return nil
		},
	}
}

func strField(name string, p func(*Extraction) *string) field {
	return field{
		name: name,
		get:  func(x *Extraction) string { return *p(x) },
		set: func(x *Extraction, v string) error {
			*p(x) = v
			return nil
		},
	}
}

var fields = []field{
	strField("name", func(x *Extraction) *string { return &x.Name }),
	intField("ac", func(x *Extraction) *int { return &x.AC }),
	intField("hp", func(x *Extraction) *int { return &x.HP }),
	intField("speed_ft", func(x *Extraction) *int { return &x.SpeedFt }),
	intField("initiative_mod", func(x *Extraction) *int { return &x.InitiativeMod }),
	strField("attack.name", func(x *Extraction) *string { return &x.Attack.Name }),
	intField("attack.to_hit", func(x *Extraction) *int { return &x.Attack.ToHit }),
	intField("attack.reach_ft", func(x *Extraction) *int { return &x.Attack.ReachFt }),
	intField("attack.dice_count", func(x *Extraction) *int { return &x.Attack.DiceCount }),
	intField("attack.dice_sides", func(x *Extraction) *int { return &x.Attack.DiceSides }),
	intField("attack.damage_bonus", func(x *Extraction) *int { return &x.Attack.DamageBonus }),
	strField("attack.damage_type", func(x *Extraction) *string { return &x.Attack.DamageType }),
}

func lookupField(name string) (field, bool) {
	for _, f := range fields {
		if f.name == name {
			return f, true
		}
	}
	return field{}, false
}

// Correct sets one field of the entry, records the correction, and re-checks
// the entry against its source text and the content format. A corrected
// entry goes back to pending: it must be approved again as it now stands.
func (w *Work) Correct(i int, name, value string) error {
	f, ok := lookupField(name)
	if !ok {
		names := make([]string, len(fields))
		for k, f := range fields {
			names[k] = f.name
		}
		return fmt.Errorf("unknown field %q (fields: %s)", name, strings.Join(names, ", "))
	}
	e := &w.Entries[i]
	from := f.get(&e.StatBlock)
	if err := f.set(&e.StatBlock, value); err != nil {
		return err
	}
	e.Corrections = append(e.Corrections, Correction{Field: name, From: from, To: f.get(&e.StatBlock)})
	e.Review = Pending
	e.check(w.Source)
	return nil
}

// Approve marks the entry approved. Entries with content problems can't be.
func (w *Work) Approve(i int) error {
	e := &w.Entries[i]
	if len(e.Problems) > 0 {
		return fmt.Errorf("%s has problems; correct them first", e.ID)
	}
	e.Review = Approved
	return nil
}

const sourceColumn = 56

// Review walks the pending entries, showing each entry's source text beside
// its extracted fields with its warnings and problems, and reads the
// reviewer's commands from in:
//
//	a                  approve
//	c <field> <value>  correct a field (also "c field=value")
//	s                  skip, leaving it pending
//	q                  stop reviewing
//
// save is called after every approve or correct, so an interrupted review
// keeps its decisions.
func (w *Work) Review(in io.Reader, out io.Writer, save func() error) error {
	sc := bufio.NewScanner(in)
	pending := 0
	for _, e := range w.Entries {
		if e.Review == Pending {
			pending++
		}
	}
	n := 0
	for i := range w.Entries {
		if w.Entries[i].Review != Pending {
			continue
		}
		n++
		w.show(out, i, n, pending)
		for {
			fmt.Fprint(out, "[a]pprove  [c]orrect <field> <value>  [s]kip  [q]uit > ")
			if !sc.Scan() {
				fmt.Fprintln(out)
				return sc.Err()
			}
			cmd, arg, _ := strings.Cut(strings.TrimSpace(sc.Text()), " ")
			switch cmd {
			case "a":
				if err := w.Approve(i); err != nil {
					fmt.Fprintln(out, err)
					continue
				}
				fmt.Fprintf(out, "approved %s\n\n", w.Entries[i].ID)
			case "c":
				name, value, ok := strings.Cut(strings.TrimSpace(arg), "=")
				if !ok {
					name, value, ok = strings.Cut(strings.TrimSpace(arg), " ")
				}
				if !ok {
					fmt.Fprintln(out, "usage: c <field> <value>")
					continue
				}
				if err := w.Correct(i, strings.TrimSpace(name), strings.TrimSpace(value)); err != nil {
					fmt.Fprintln(out, err)
					continue
				}
				if err := save(); err != nil {
					return err
				}
				w.show(out, i, n, pending)
				continue
			case "s":
				fmt.Fprintln(out)
			case "q":
				return nil
			default:
				fmt.Fprintf(out, "unknown command %q\n", cmd)
				continue
			}
			if err := save(); err != nil {
				return err
			}
			break
		}
	}
	fmt.Fprintln(out, "review queue empty")
	return nil
}

func (w *Work) show(out io.Writer, i, n, total int) {
	e := w.Entries[i]
	fmt.Fprintf(out, "── %d/%d  %s  (%s) ──\n", n, total, e.ID, e.source(w.Source))

	var left []string
	for _, l := range strings.Split(strings.TrimRight(e.SourceText, "\n"), "\n") {
		left = append(left, wrap(l, sourceColumn)...)
	}
	right := []string{"ENTRY"}
	for _, f := range fields {
		right = append(right, fmt.Sprintf("%-20s %s", f.name, f.get(&e.StatBlock)))
	}
	if len(e.Corrections) > 0 {
		right = append(right, "", "CORRECTED")
		for _, c := range e.Corrections {
			right = append(right, fmt.Sprintf("%s: %s -> %s", c.Field, c.From, c.To))
		}
	}
	left = append([]string{"SOURCE TEXT"}, left...)
	for k := 0; k < max(len(left), len(right)); k++ {
		var l, r string
		if k < len(left) {
			l = left[k]
		}
		if k < len(right) {
			r = right[k]
		}
		fmt.Fprintf(out, "%s%s | %s\n", l, strings.Repeat(" ", sourceColumn-utf8.RuneCountInString(l)), r)
	}
	for _, p := range e.Problems {
		fmt.Fprintf(out, "PROBLEM: %s\n", p)
	}
	for _, wn := range e.Warnings {
		fmt.Fprintf(out, "warning: %s\n", wn)
	}
}

func wrap(s string, width int) []string {
	var out []string
	for utf8.RuneCountInString(s) > width {
		r := []rune(s)
		cut := width
		for k := width; k > width/2; k-- {
			if r[k] == ' ' {
				cut = k
				break
			}
		}
		out = append(out, string(r[:cut]))
		s = strings.TrimLeft(string(r[cut:]), " ")
	}
	return append(out, s)
}
