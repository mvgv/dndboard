---
name: ruleset
description: Use when implementing, testing or reviewing any Ruleset mechanic — actions, attacks, damage, saves, checks, conditions, spells, class features, monsters, rests, death, character creation or level advancement.
---

# Ruleset

The Ruleset is D&D 2024 (SRD 5.2 baseline), and ADR 0001 makes the rules engine its sole authority, so every mechanic must match the rule text exactly. Memory of D&D blends the 2014 and 2024 rules; the text is the source of truth.

## Steps

1. **Find the rule text** in `reference/srd-5.2/` before writing code or a test for a mechanic. Read the whole entry, plus every term it relies on (a spell's conditions, an action's definitions).
2. **Name the source** in the scenario test: the SRD section and entry the test encodes, so a reviewer can check it against the text.
3. **Write the scenario test in Ruleset terms** through the game-session seam (e.g. "Fireball, 8d6, 20-foot radius, DC 15 Dexterity save: failed takes full damage, success takes half").
4. **Implement** until the scenario passes.

Done when every behavior in the change traces to a quoted SRD entry and has a passing scenario test.

## When the text is missing

- Mechanic outside SRD 5.2 (Player's Handbook-only content): stop and flag it in the PR as needing the Compendium (Forge) instead of writing it from memory.
- Ambiguous text: implement the most literal reading, and record the ambiguity and your reading in the PR.
- `reference/srd-5.2/` empty: stop and ask the owner to populate it (see its README).
