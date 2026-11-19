package forge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mvgv/dndboard/content"
	"github.com/mvgv/dndboard/forge"
)

var slice = []string{"Goblin Warrior", "Bandit", "Skeleton", "Zombie"}

var srdPDF = flag.String("srd-pdf", "", "path to the SRD 5.2.1 PDF, for TestReadPDF")

// srdPages is the text the Forge reads from the SRD 5.2.1 PDF for the pages
// holding the first slice of stat blocks. TestReadPDF checks it against the
// PDF itself when run with -srd-pdf.
func srdPages(t *testing.T) forge.Source {
	t.Helper()
	text, err := os.ReadFile("testdata/srd-pages.txt")
	if err != nil {
		t.Fatal(err)
	}
	src, err := forge.ParseSource(string(text))
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// replayed is a Claude client answering only from recorded responses: it
// never reaches the live API.
func replayed() *forge.Claude {
	return &forge.Claude{HTTP: &http.Client{Transport: forge.Replay{Dir: "testdata/recorded"}}}
}

func extract(t *testing.T) *forge.Work {
	t.Helper()
	w, err := forge.Extract(context.Background(), srdPages(t), "SRD 5.2.1", slice, replayed())
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func entry(t *testing.T, w *forge.Work, id string) *forge.Entry {
	t.Helper()
	for i := range w.Entries {
		if w.Entries[i].ID == id {
			return &w.Entries[i]
		}
	}
	t.Fatalf("no entry %q", id)
	return nil
}

func hasWarning(e *forge.Entry, substr string) bool {
	return slices.ContainsFunc(e.Warnings, func(w string) bool { return strings.Contains(w, substr) })
}

// Values from the SRD 5.2.1 Monsters chapter: Bandit p. 261, Goblin Warrior
// p. 290, Skeleton pp. 325-326, Zombie pp. 343-344.
func TestExtractSRDStatBlocks(t *testing.T) {
	w := extract(t)
	want := map[string]forge.Extraction{
		"goblin-warrior": {Name: "Goblin Warrior", AC: 15, HP: 10, SpeedFt: 30, InitiativeMod: 2,
			Attack: forge.ExtractionAttack{Name: "Scimitar", ToHit: 4, ReachFt: 5, DiceCount: 1, DiceSides: 6, DamageBonus: 2, DamageType: "Slashing"}},
		"bandit": {Name: "Bandit", AC: 12, HP: 11, SpeedFt: 30, InitiativeMod: 1,
			Attack: forge.ExtractionAttack{Name: "Scimitar", ToHit: 3, ReachFt: 5, DiceCount: 1, DiceSides: 6, DamageBonus: 1, DamageType: "Slashing"}},
		"skeleton": {Name: "Skeleton", AC: 14, HP: 13, SpeedFt: 30, InitiativeMod: 3,
			Attack: forge.ExtractionAttack{Name: "Shortsword", ToHit: 5, ReachFt: 5, DiceCount: 1, DiceSides: 6, DamageBonus: 3, DamageType: "Piercing"}},
		"zombie": {Name: "Zombie", AC: 8, HP: 15, SpeedFt: 20, InitiativeMod: -2,
			Attack: forge.ExtractionAttack{Name: "Slam", ToHit: 3, ReachFt: 5, DiceCount: 1, DiceSides: 8, DamageBonus: 1, DamageType: "Bludgeoning"}},
	}
	if len(w.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(w.Entries), len(want))
	}
	for id, x := range want {
		e := entry(t, w, id)
		got := e.StatBlock
		got.Omitted = nil
		if got.Name != x.Name || got.AC != x.AC || got.HP != x.HP || got.SpeedFt != x.SpeedFt ||
			got.InitiativeMod != x.InitiativeMod || got.Attack != x.Attack {
			t.Errorf("%s = %+v, want %+v", id, got, x)
		}
		if len(e.Problems) > 0 {
			t.Errorf("%s fails validation: %v", id, e.Problems)
		}
		if e.Review != forge.Pending || !strings.Contains(e.SourceText, x.Name+"\n") {
			t.Errorf("%s: review %q, source text %q", id, e.Review, e.SourceText)
		}
	}

	// Each excerpt runs to the next stat block, across the page break.
	if sk := entry(t, w, "skeleton"); !slices.Equal(sk.Pages, []int{325, 326}) ||
		!strings.Contains(sk.SourceText, "Shortsword. Melee Attack Roll") || strings.Contains(sk.SourceText, "Warhorse") {
		t.Errorf("skeleton excerpt pages %v:\n%s", sk.Pages, sk.SourceText)
	}

	// The PDF garbles the Zombie's "Initiative −2 (8)"; the value Claude
	// derived can't be found in the text, so the reviewer is warned.
	if z := entry(t, w, "zombie"); !hasWarning(z, "Initiative -2 not found") {
		t.Errorf("zombie warnings = %v", z.Warnings)
	}
	if gw := entry(t, w, "goblin-warrior"); slices.ContainsFunc(gw.Warnings, func(s string) bool { return strings.Contains(s, "not found") }) {
		t.Errorf("goblin warrior has grounding warnings: %v", gw.Warnings)
	}
	// What the format can't hold is listed, never silently dropped.
	if z := entry(t, w, "zombie"); !hasWarning(z, "Undead Fortitude") {
		t.Errorf("zombie warnings = %v", z.Warnings)
	}
}

func TestChangedSourceHasNoRecording(t *testing.T) {
	src := srdPages(t)
	for i := range src.Pages {
		src.Pages[i].Text = strings.Replace(src.Pages[i].Text, "HP 11 (2d8 + 2)", "HP 12 (2d8 + 2)", 1)
	}
	_, err := forge.Extract(context.Background(), src, "SRD 5.2.1", []string{"Bandit"}, replayed())
	if err == nil || !strings.Contains(err.Error(), "no recorded response") {
		t.Fatalf("err = %v, want a missing recording", err)
	}
}

func TestMissingStatBlock(t *testing.T) {
	_, err := forge.Extract(context.Background(), srdPages(t), "SRD 5.2.1", []string{"Tarrasque"}, replayed())
	if err == nil || !strings.Contains(err.Error(), `"Tarrasque" not found`) {
		t.Fatalf("err = %v", err)
	}
}

// claudeAnswering serves one recorded-style Messages API response whose
// record_stat_block input is x.
func claudeAnswering(t *testing.T, x forge.Extraction) *forge.Claude {
	t.Helper()
	input, _ := json.Marshal(x)
	body, _ := json.Marshal(map[string]any{
		"type": "message", "role": "assistant", "stop_reason": "tool_use",
		"content": []any{map[string]any{"type": "tool_use", "id": "toolu_1", "name": "record_stat_block", "input": json.RawMessage(input)}},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "test-key" || r.Header.Get("anthropic-version") == "" {
			http.Error(rw, `{"error":{"type":"invalid_request_error","message":"bad request"}}`, http.StatusBadRequest)
			return
		}
		rw.Write(body)
	}))
	t.Cleanup(srv.Close)
	return &forge.Claude{APIKey: "test-key", BaseURL: srv.URL}
}

// fixedExtractor stands in for Claude at the Extractor seam.
type fixedExtractor struct{ x forge.Extraction }

func (f fixedExtractor) ExtractStatBlock(context.Context, forge.Excerpt) (forge.Extraction, error) {
	return f.x, nil
}

// Duplicates are caught before any extraction is paid for.
func TestDuplicateNameFailsBeforeExtraction(t *testing.T) {
	_, err := forge.Extract(context.Background(), srdPages(t), "SRD 5.2.1", []string{"Bandit", "Bandit"}, panicExtractor{})
	if err == nil || !strings.Contains(err.Error(), "requested twice") {
		t.Fatalf("err = %v", err)
	}
}

type panicExtractor struct{}

func (panicExtractor) ExtractStatBlock(context.Context, forge.Excerpt) (forge.Extraction, error) {
	panic("extractor called")
}

var bandit = forge.Extraction{Name: "Bandit", AC: 12, HP: 11, SpeedFt: 30, InitiativeMod: 1,
	Attack: forge.ExtractionAttack{Name: "Scimitar", ToHit: 3, ReachFt: 5, DiceCount: 1, DiceSides: 6, DamageBonus: 1, DamageType: "Slashing"}}

func TestWrongValueIsWarned(t *testing.T) {
	wrong := bandit
	wrong.HP = 12
	wrong.Attack.DamageBonus = 2
	w, err := forge.Extract(context.Background(), srdPages(t), "SRD 5.2.1", []string{"Bandit"}, fixedExtractor{wrong})
	if err != nil {
		t.Fatal(err)
	}
	e := &w.Entries[0]
	if !hasWarning(e, "HP 12 not found") || !hasWarning(e, "damage 1d6 + 2 Slashing not found") {
		t.Fatalf("warnings = %v", e.Warnings)
	}
}

func TestInvalidEntryCannotBeApprovedOrPublished(t *testing.T) {
	broken := bandit
	broken.Attack.DiceSides = 0
	w, err := forge.Extract(context.Background(), srdPages(t), "SRD 5.2.1", []string{"Bandit"}, claudeAnswering(t, broken))
	if err != nil {
		t.Fatal(err)
	}
	if e := w.Entries[0]; len(e.Problems) == 0 || !strings.Contains(e.Problems[0], "dice_sides") {
		t.Fatalf("problems = %v", e.Problems)
	}
	if err := w.Approve(0); err == nil {
		t.Fatal("approved an invalid entry")
	}
	_, left, err := forge.Publish(w, 1)
	if err != nil || !slices.Equal(left, []string{"bandit"}) {
		t.Fatalf("publish left %v, err %v", left, err)
	}

	// Correcting the field clears the problem.
	if err := w.Correct(0, "attack.dice_sides", "6"); err != nil {
		t.Fatal(err)
	}
	if err := w.Approve(0); err != nil {
		t.Fatal(err)
	}
	// Correcting an approved entry sends it back to review.
	if err := w.Correct(0, "attack.dice_sides", "0"); err != nil {
		t.Fatal(err)
	}
	if e := w.Entries[0]; e.Review != forge.Pending || len(e.Problems) == 0 {
		t.Fatalf("after correcting an approved entry: review %q, problems %v", e.Review, e.Problems)
	}
}

func TestReviewQueue(t *testing.T) {
	w := extract(t)
	saves := 0
	var out bytes.Buffer
	// goblin warrior: approve; bandit: bad command, correct hp, approve;
	// skeleton: skip; zombie: quit.
	in := strings.NewReader("a\nx\nc hp 12\nc attack.to_hit=4\na\ns\nq\n")
	if err := w.Review(in, &out, func() error { saves++; return nil }); err != nil {
		t.Fatal(err)
	}
	shown := out.String()

	// Each entry is shown beside its source text.
	for _, line := range []string{
		"<!-- page 290 -->                                        | name                 Goblin Warrior",
		"Goblin Warrior                                           | ac                   15",
		"AC 15 Initiative +2 (12)                                 | speed_ft             30",
		"── 3/4  skeleton  (SRD 5.2.1, Skeleton, pages 325-326) ──",
		"warning: not in content format v0, the game will play without it: Light Crossbow (ranged attack)",
	} {
		if !strings.Contains(shown, line) {
			t.Errorf("review output lacks %q:\n%s", line, shown)
		}
	}
	if !strings.Contains(shown, `unknown command "x"`) {
		t.Error("bad command not reported")
	}

	if e := entry(t, w, "goblin-warrior"); e.Review != forge.Approved || len(e.Corrections) != 0 {
		t.Errorf("goblin warrior = %s %v", e.Review, e.Corrections)
	}
	b := entry(t, w, "bandit")
	wantCorr := []forge.Correction{{Field: "hp", From: "11", To: "12"}, {Field: "attack.to_hit", From: "3", To: "4"}}
	if b.Review != forge.Approved || !slices.Equal(b.Corrections, wantCorr) || b.StatBlock.HP != 12 || b.Extracted.HP != 11 {
		t.Errorf("bandit = %s %v hp %d (extracted %d)", b.Review, b.Corrections, b.StatBlock.HP, b.Extracted.HP)
	}
	// A correction re-checks the entry against its source text.
	if !hasWarning(b, "HP 12 not found") {
		t.Errorf("bandit warnings after correction = %v", b.Warnings)
	}
	for _, id := range []string{"skeleton", "zombie"} {
		if e := entry(t, w, id); e.Review != forge.Pending {
			t.Errorf("%s = %s, want pending", id, e.Review)
		}
	}
	if saves != 5 { // approve, correct, correct, approve, skip
		t.Errorf("saved %d times, want 5", saves)
	}

	// A second pass shows only what is still pending.
	out.Reset()
	if err := w.Review(strings.NewReader("a\na\n"), &out, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "goblin-warrior") || !strings.Contains(out.String(), "── 1/2  skeleton") {
		t.Errorf("second pass:\n%s", out.String())
	}
}

func TestPublishVersionedCompendium(t *testing.T) {
	w := extract(t)
	if err := w.Approve(1); err != nil { // bandit
		t.Fatal(err)
	}
	doc, left, err := forge.Publish(w, 7)
	if err != nil || len(left) != 0 {
		t.Fatalf("left %v, err %v", left, err)
	}
	c, err := content.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	if c.Version() != 7 {
		t.Errorf("version = %d", c.Version())
	}
	sb, ok := c.StatBlock("bandit")
	if !ok || sb.Source != "SRD 5.2.1, Bandit, page 261" || sb.Attack.Weapon != "bandit-scimitar" {
		t.Errorf("bandit = %+v ok=%v", sb, ok)
	}
	if _, ok := c.StatBlock("goblin-warrior"); ok {
		t.Error("unapproved entry loaded")
	}

	if v, _ := forge.NextVersion(doc); v != 8 {
		t.Errorf("next version = %d, want 8", v)
	}
	if v, _ := forge.NextVersion(nil); v != 1 {
		t.Errorf("first version = %d, want 1", v)
	}
}

// The committed monster Compendium is exactly what the Forge publishes from
// the recorded extraction with the review decisions taken on it: Goblin
// Warrior and Bandit approved, Skeleton and Zombie left as drafts.
func TestCommittedMonstersAreForgeOutput(t *testing.T) {
	committed, err := os.ReadFile("../content/srd/monsters.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Version int }
	if err := json.Unmarshal(committed, &v); err != nil {
		t.Fatal(err)
	}
	w := extract(t)
	if err := w.Review(strings.NewReader("a\na\ns\ns\n"), &bytes.Buffer{}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	doc, _, err := forge.Publish(w, v.Version)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(doc, committed) {
		t.Errorf("content/srd/monsters.json differs from the Forge output; re-run forge publish:\n%s", doc)
	}
}

// TestReadPDF checks the page text fixture against the SRD 5.2.1 PDF
// (https://media.dndbeyond.com/compendium-images/srd/5.2/SRD_CC_v5.2.1.pdf),
// and that the Forge finds the slice in the whole PDF. The PDF isn't in the
// repository, so this runs only when pointed at it:
//
//	go test ./forge -run TestReadPDF -args -srd-pdf=SRD_CC_v5.2.1.pdf
func TestReadPDF(t *testing.T) {
	if *srdPDF == "" {
		t.Skip("-srd-pdf not set")
	}
	src, err := forge.ReadPDF(*srdPDF)
	if err != nil {
		t.Fatal(err)
	}
	fixture := srdPages(t)
	want := map[int]bool{}
	for _, p := range fixture.Pages {
		want[p.Number] = true
	}
	var picked forge.Source
	for _, p := range src.Pages {
		if want[p.Number] {
			picked.Pages = append(picked.Pages, p)
		}
	}
	if got := picked.String(); got != fixture.String() {
		t.Fatalf("PDF text differs from testdata/srd-pages.txt; regenerate it:\n%s", got)
	}
	w, err := forge.Extract(context.Background(), src, "SRD 5.2.1", slice, replayed())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range w.Entries {
		if len(e.Problems) > 0 {
			t.Errorf("%s: %v", e.ID, e.Problems)
		}
	}
}
