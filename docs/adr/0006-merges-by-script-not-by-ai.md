# Merges are made by a script with checkable criteria, never by the AI

Tickets are implemented and reviewed by unattended Claude Code sessions, but merging into `main` is done by `scripts/conductor.sh`, a deterministic script, and only when every criterion is checkable: CI green on the PR head, an independent reviewer session's verdict `pass` for that exact commit, no conflicts, and a self-declared Merge Danger of a two-way door with no or low blast radius. Everything else is labeled `ready-to-merge` for the owner. Claude sessions are denied `gh pr merge` in `.claude/settings.json`. We chose this over letting an agent merge (its judgment is the thing under review) and over the owner merging everything (the owner becomes the bottleneck for parallel tickets).

## Consequences

- No CI, no automatic merge: the conductor refuses to merge a PR without checks.
- A new commit on a PR invalidates its review; the conductor asks for a new one.
