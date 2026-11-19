// Command forge turns source PDFs into Compendium files (ADR 0005).
//
//	forge extract -pdf SRD_CC_v5.2.1.pdf -work srd-work.json [-names "Goblin Warrior,Bandit"]
//	forge review  -work srd-work.json
//	forge publish -work srd-work.json -out content/srd/monsters.json
//
// extract reads the PDF, extracts each named stat block through Claude (the
// API key comes from ANTHROPIC_API_KEY) and writes a work file of entries
// pending review. review shows each entry beside its source text and records
// approvals and corrections in the work file. publish writes the reviewed
// entries as a versioned content document, one version above the file it
// replaces.
//
// extract -record DIR saves every Claude exchange for tests to replay;
// extract -replay DIR answers from such recordings without calling the API.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/mvgv/dndboard/forge"
)

// defaultNames is the first slice of SRD stat blocks the Forge processes.
const defaultNames = "Goblin Warrior,Bandit,Skeleton,Zombie"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "forge:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer, getenv func(string) string) error {
	if len(args) == 0 {
		return errors.New("usage: forge extract|review|publish [flags]")
	}
	cmd, args := args[0], args[1:]
	fl := flag.NewFlagSet("forge "+cmd, flag.ContinueOnError)
	fl.SetOutput(stdout)
	work := fl.String("work", "", "work file (required)")
	switch cmd {
	case "extract":
		pdf := fl.String("pdf", "", "source PDF (required)")
		names := fl.String("names", defaultNames, "comma-separated stat block names")
		source := fl.String("source", "SRD 5.2.1", "source name recorded on each entry")
		model := fl.String("model", forge.DefaultModel, "Claude model")
		record := fl.String("record", "", "save Claude exchanges to this directory")
		replay := fl.String("replay", "", "answer from recorded exchanges in this directory instead of the API")
		if err := fl.Parse(args); err != nil {
			return err
		}
		if *pdf == "" || *work == "" {
			return errors.New("extract needs -pdf and -work")
		}
		claude := &forge.Claude{APIKey: getenv("ANTHROPIC_API_KEY"), Model: *model}
		switch {
		case *replay != "":
			claude.HTTP = &http.Client{Transport: forge.Replay{Dir: *replay}}
		case claude.APIKey == "":
			return errors.New("ANTHROPIC_API_KEY is not set")
		case *record != "":
			claude.HTTP = &http.Client{Transport: forge.Record{Dir: *record}}
		}
		src, err := forge.ReadPDF(*pdf)
		if err != nil {
			return err
		}
		w, err := forge.Extract(context.Background(), src, *source, splitNames(*names), claude)
		if err != nil {
			return err
		}
		for _, e := range w.Entries {
			fmt.Fprintf(stdout, "%-20s %d warnings, %d problems\n", e.ID, len(e.Warnings), len(e.Problems))
		}
		return writeWork(*work, w)

	case "review":
		if err := fl.Parse(args); err != nil {
			return err
		}
		w, err := readWork(*work)
		if err != nil {
			return err
		}
		return w.Review(stdin, stdout, func() error { return writeWork(*work, w) })

	case "publish":
		out := fl.String("out", "", "content document to write (required)")
		if err := fl.Parse(args); err != nil {
			return err
		}
		if *work == "" || *out == "" {
			return errors.New("publish needs -work and -out")
		}
		w, err := readWork(*work)
		if err != nil {
			return err
		}
		existing, err := os.ReadFile(*out)
		if errors.Is(err, fs.ErrNotExist) {
			existing, err = nil, nil
		}
		if err != nil {
			return err
		}
		version, err := forge.NextVersion(existing)
		if err != nil {
			return err
		}
		doc, left, err := forge.Publish(w, version)
		if err != nil {
			return err
		}
		for _, id := range left {
			fmt.Fprintf(stdout, "left out %s: it has content problems\n", id)
		}
		if err := os.WriteFile(*out, doc, 0o644); err != nil {
			return err
		}
		approved := 0
		for _, e := range w.Entries {
			if e.Review == forge.Approved {
				approved++
			}
		}
		fmt.Fprintf(stdout, "wrote %s version %d: %d approved, %d draft\n", *out, version, approved, len(w.Entries)-approved-len(left))
		return nil
	}
	return fmt.Errorf("unknown command %q (want extract, review or publish)", cmd)
}

func splitNames(s string) []string {
	var out []string
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func readWork(path string) (*forge.Work, error) {
	if path == "" {
		return nil, errors.New("-work is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var w forge.Work
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &w, nil
}

func writeWork(path string, w *forge.Work) error {
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
