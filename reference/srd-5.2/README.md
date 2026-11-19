# SRD 5.2.1 text

The `ruleset` skill reads the D&D 2024 rules from this folder: the System Reference Document 5.2.1, one Markdown file per chapter, extracted with `pdftotext` from the official PDF (https://media.dndbeyond.com/compendium-images/srd/5.2/SRD_CC_v5.2.1.pdf). Each page is marked `<!-- page N -->`, matching the PDF's printed page numbers, so tests can cite the page.

Prose (rules, spells, features, the glossary) extracts cleanly. Tables and the ability-score grid of stat blocks come out column by column: read them carefully, and confirm against the PDF page when a value matters.

Licensed under CC-BY-4.0; the required attribution is in `ATTRIBUTION.md`. Only SRD content belongs here (ADR 0003): never Player's Handbook, Monster Manual or Adventure Book text.
