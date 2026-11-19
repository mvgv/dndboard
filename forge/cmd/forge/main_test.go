package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvgv/dndboard/content"
	"github.com/mvgv/dndboard/forge"
)

func noEnv(string) string { return "" }

// review then publish twice through the command line: decisions persist in
// the work file and every publish bumps the document's version.
func TestReviewAndPublishBumpVersion(t *testing.T) {
	text, err := os.ReadFile("../../testdata/srd-pages.txt")
	if err != nil {
		t.Fatal(err)
	}
	src, err := forge.ParseSource(string(text))
	if err != nil {
		t.Fatal(err)
	}
	claude := &forge.Claude{HTTP: &http.Client{Transport: forge.Replay{Dir: "../../testdata/recorded"}}}
	w, err := forge.Extract(context.Background(), src, "SRD 5.2.1", []string{"Goblin Warrior", "Bandit"}, claude)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	work, out := filepath.Join(dir, "work.json"), filepath.Join(dir, "monsters.json")
	if err := writeWork(work, w); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := run([]string{"review", "-work", work}, strings.NewReader("a\ns\n"), &stdout, noEnv); err != nil {
		t.Fatal(err)
	}
	for v := 1; v <= 2; v++ {
		stdout.Reset()
		if err := run([]string{"publish", "-work", work, "-out", out}, nil, &stdout, noEnv); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		c, err := content.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		if c.Version() != v {
			t.Errorf("publish %d wrote version %d", v, c.Version())
		}
		if _, ok := c.StatBlock("goblin-warrior"); !ok {
			t.Error("approved goblin warrior missing")
		}
		if _, ok := c.StatBlock("bandit"); ok {
			t.Error("skipped bandit loaded")
		}
	}
	if !strings.Contains(stdout.String(), "version 2: 1 approved, 1 draft") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestExtractNeedsAPIKeyUnlessReplaying(t *testing.T) {
	err := run([]string{"extract", "-pdf", "x.pdf", "-work", "w.json"}, nil, &bytes.Buffer{}, noEnv)
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("err = %v", err)
	}
}
