---
name: ticket-flow
description: Use when implementing a GitHub issue (ticket) of this repo end to end, from branch to pull request.
---

# Ticket flow

Tickets are sub-issues of the MVP spec (#1): tracer-bullet slices with acceptance criteria and native blocking links. Read `GLOSSARY.md` and the ADRs in `docs/adr/` for the area you touch.

## Steps

1. **Load the ticket**: `gh issue view <n> --json number,title,body,labels`, and the spec #1 sections it relies on. Check its blockers are closed (`gh api repos/{owner}/{repo}/issues/<n>/dependencies/blocked_by --jq '[.[] | {number, state}]'`); stop if any is open.
2. **Branch**: when the current branch already starts with `<n>-` (a worktree made by `scripts/run-ticket.sh`), work on it. Otherwise branch `<n>-<short-slug>` from an up-to-date `main`. Add the `in-progress` label to the issue.
3. **Test first at the seam** (`tdd` skill). Tests talk only to the game-session seam (submit intent → events, visible state, legal actions; seeded dice; fake Narrator and AI) or the Forge seam (source document → validated output; recorded LLM responses). Ruleset mechanics follow the `ruleset` skill.
4. **Implement** until green, committing as you go.
5. **Check**, all passing:
   - Go: `gofmt -l .` prints nothing, `go vet ./...` clean, tests via `gotestsum --format dots` (fall back to `go test ./...`).
   - Client: `godot --headless --path client --quit` loads with no script errors.
   - Changes to login, networking, user input or Narrator output: run `/security-review` and fix findings.
6. **Record domain changes**: new or changed terms go to `GLOSSARY.md` (`domain-modeling` skill); hard-to-reverse decisions get an ADR.
7. **Open the PR** with the `pr` skill: push the branch, and start the body with `Closes #<n>`, mapping every acceptance criterion to the test that proves it. Leave merging to the owner.

Done when the PR is open, checks pass, and every acceptance criterion of the ticket has named evidence in the PR body.

## Unattended runs

Under `scripts/run-ticket.sh` nobody answers questions. Decide anything the spec, ADRs and ticket settle, and note the choice in the PR. For a decision only the owner can make, comment the question on the issue, add the `needs-human` label, and end the session.
