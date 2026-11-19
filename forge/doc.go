// Package forge is the offline Forge (ADR 0005): it turns source PDFs into
// Compendium files in the content format. The command is cmd/forge.
//
// The first slice handles SRD 5.2.1 monster stat blocks:
//
//  1. ReadPDF extracts each page's text, column by column.
//  2. Source.StatBlock cuts one stat block's text out of the pages.
//  3. Claude extracts its fields with structured output (a forced tool call).
//  4. Extract checks every value against the source text (warnings) and
//     validates the entry with package content (problems).
//  5. Work.Review shows each entry beside its source text and records the
//     reviewer's approvals and corrections in the work file.
//  6. Publish writes a versioned content document; only approved entries load
//     in the game.
//
// Tests replay recorded Claude responses (Replay) and never call the API;
// Record captures new ones from the live API.
package forge
