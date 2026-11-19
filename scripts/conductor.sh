#!/usr/bin/env bash
# The conductor: merges ticket PRs that pass every checkable gate and starts the
# next unblocked tickets (ADR 0006). Deterministic: it calls Claude only through
# run-ticket.sh and review-pr.sh. Meant to run every few minutes (systemd timer).
#
# Usage:   scripts/conductor.sh
# Environment:
#   MAX_PARALLEL        tickets running at once (default 2)
#   MAX_STARTS_PER_DAY  tickets started per day (default 6)
#   SPEC_ISSUE          parent issue whose sub-issues are the tickets (default 1)
#   DRY_RUN=1           log what it would do, change nothing
#
# Kill switch: touch <workspace>/STOP. Log: <workspace>/logs/conductor.log.
#
# Per open ticket PR (branch <issue>-...), skipping any with a running session:
#   conflict with main      -> feedback to the ticket session (once per commit)
#   CI failed               -> feedback with the failed checks (once per commit)
#   behind main             -> gh pr update-branch
#   no review for the head  -> independent review (review-pr.sh)
#   review asked changes    -> feedback with the review (once per commit)
#   review pass, CI green, mergeable:
#     Merge Danger two-way door and blast radius none/low -> merge
#     otherwise, or no CI at all                          -> label ready-to-merge
set -euo pipefail

MAIN=$(git -C "$(dirname "$0")" worktree list --porcelain | awk '/^worktree /{print $2; exit}')
WS=$(dirname "$MAIN")
STATE=$WS/state
mkdir -p "$WS/logs" "$WS/wt" "$STATE"
LOG=$WS/logs/conductor.log
MAX_PARALLEL=${MAX_PARALLEL:-2}
MAX_STARTS_PER_DAY=${MAX_STARTS_PER_DAY:-6}
SPEC_ISSUE=${SPEC_ISSUE:-1}
DRY_RUN=${DRY_RUN:-0}
SCRIPTS=$MAIN/scripts

log() { echo "[$(date '+%F %T')] $*" | tee -a "$LOG"; }
act() { if [[ $DRY_RUN == 1 ]]; then log "DRY-RUN: $*"; else "$@"; fi; }
once() { # once <key>: true the first time a key is seen
  [[ -e $STATE/$1 ]] && return 1
  [[ $DRY_RUN == 1 ]] || touch "$STATE/$1"
  return 0
}

if command -v flock >/dev/null; then
  exec 9>"$STATE/conductor.lock"
  flock -n 9 || exit 0
fi
if [[ -e $WS/STOP ]]; then
  log "STOP file present; idle."
  exit 0
fi
if [[ $DRY_RUN != 1 && $(git -C "$MAIN" branch --show-current) == main ]]; then
  git -C "$MAIN" pull -q --ff-only || log "Warning: could not update $MAIN"
fi

REPO=$(cd "$MAIN" && gh repo view --json nameWithOwner -q .nameWithOwner)

windows() { tmux list-windows -t tickets -F '#W' 2>/dev/null || true; }
busy() { windows | grep -qx "$1"; }
issue_labels() { gh issue view "$1" -R "$REPO" --json labels --jq '[.labels[].name] | join(" ")'; }
model_for() { [[ " $(issue_labels "$1") " == *" model:opus "* ]] && echo opus || echo sonnet; }
open_blockers() {
  gh api "repos/$REPO/issues/$1/dependencies/blocked_by" --jq '[.[] | select(.state == "open")] | length'
}
feedback() { # feedback <issue> <text>
  local f=$STATE/feedback-$1-$(date +%s).md
  printf '%s\n' "$2" > "$f"
  act "$SCRIPTS/run-ticket.sh" "$1" --model "$(model_for "$1")" --detach --feedback-file "$f"
}

# ---------------------------------------------------------------- PRs
FAIL_RE='FAILURE|ERROR|CANCELLED|TIMED_OUT|ACTION_REQUIRED|STARTUP_FAILURE'
prs=$(gh pr list -R "$REPO" --state open --limit 100 --json number,headRefName \
  --jq '.[] | select(.headRefName | test("^[0-9]+-")) | "\(.number) \(.headRefName)"')

while read -r PR BRANCH <&3; do
  [[ -n ${PR:-} ]] || continue
  N=${BRANCH%%-*}
  pv() { gh pr view "$PR" -R "$REPO" --json "$1" --jq "$2" 2>/dev/null; }

  if busy "t$N" || busy "r$PR"; then log "PR #$PR: session running; skip."; continue; fi
  labels=" $(pv labels '[.labels[].name] | join(" ")') $(issue_labels "$N") "
  if [[ $labels == *" needs-human "* ]]; then log "PR #$PR: needs-human; skip."; continue; fi

  SHA=$(pv headRefOid .headRefOid)
  mergeable=$(pv mergeable .mergeable)
  mstate=$(pv mergeStateStatus .mergeStateStatus)
  ci=$(pv statusCheckRollup ".statusCheckRollup | if length == 0 then \"none\"
    elif any(.[]; (.conclusion // .state // \"\") | test(\"$FAIL_RE\")) then \"failure\"
    elif any(.[]; ((.status // \"COMPLETED\") != \"COMPLETED\") or ((.state // \"\") | test(\"PENDING|EXPECTED\"))) then \"pending\"
    else \"success\" end")
  log "PR #$PR (#$N) ${SHA:0:7}: ci=$ci mergeable=$mergeable state=$mstate"

  if [[ $mergeable == CONFLICTING ]]; then
    once "conflict-pr$PR-$SHA" && feedback "$N" "PR #$PR has merge conflicts with main. Merge origin/main into $BRANCH, resolve the conflicts keeping the intent of both sides, run the checks, and push."
    continue
  fi
  if [[ $ci == failure ]]; then
    failed=$(pv statusCheckRollup ".statusCheckRollup | [.[] | select((.conclusion // .state // \"\") | test(\"$FAIL_RE\")) | \"- \(.name // .context): \(.detailsUrl // .targetUrl // \"\")\"] | join(\"\n\")")
    once "ci-pr$PR-$SHA" && feedback "$N" "CI failed on PR #$PR at commit ${SHA:0:7}:
$failed

Read the failing logs (gh run view <run-id> --log-failed), fix the cause, run the checks locally, and push."
    continue
  fi
  if [[ $mstate == BEHIND ]]; then
    once "update-pr$PR-$SHA" && act gh pr update-branch "$PR" -R "$REPO"
    continue
  fi

  review=$(gh api "repos/$REPO/issues/$PR/comments" --paginate \
    --jq "[.[] | select(.body | contains(\"dndboard-review sha=$SHA \"))] | last | .body // empty" | tail -c 200000)
  verdict=$(printf '%s' "$review" | sed -nE 's/.*dndboard-review sha=[0-9a-f]+ verdict=(pass|changes).*/\1/p' | tail -1)
  if [[ -z $verdict ]]; then
    act "$SCRIPTS/review-pr.sh" "$PR" --model "$(model_for "$N")" --detach
    continue
  fi
  if [[ $verdict == changes ]]; then
    once "review-pr$PR-$SHA" && feedback "$N" "The independent review of PR #$PR at commit ${SHA:0:7} asked for changes. Address every blocking finding, run the checks, push, and reply on the PR summarizing what changed.

$review"
    continue
  fi

  # verdict == pass
  if [[ $ci == none ]]; then
    once "noci-pr$PR-$SHA" && { log "PR #$PR: no CI checks, so no automatic merge (ADR 0006)."; act gh pr edit "$PR" -R "$REPO" --add-label ready-to-merge; }
    continue
  fi
  if [[ $ci != success || $mergeable != MERGEABLE ]]; then
    log "PR #$PR: waiting (ci=$ci mergeable=$mergeable)."
    continue
  fi
  body=$(pv body .body)
  door=$(printf '%s' "$body" | sed -nE 's/.*\*\*Door:\*\*[[:space:]]*([A-Za-z-]+).*/\1/p' | head -1 | tr '[:upper:]' '[:lower:]')
  blast=$(printf '%s' "$body" | sed -nE 's/.*\*\*Blast Radius:\*\*[[:space:]]*([A-Za-z]+).*/\1/p' | head -1 | tr '[:upper:]' '[:lower:]')
  if [[ $door == two-way && ( $blast == none || $blast == low ) ]]; then
    log "PR #$PR: all gates passed (door=$door blast=$blast); merging."
    act gh pr merge "$PR" -R "$REPO" --merge
    act gh api -X DELETE "repos/$REPO/git/refs/heads/$BRANCH" --silent || true
  else
    once "ready-pr$PR-$SHA" && { log "PR #$PR: gates passed but door=${door:-?} blast=${blast:-?}; leaving the merge to the owner."; act gh pr edit "$PR" -R "$REPO" --add-label ready-to-merge; }
  fi
done 3<<< "$prs"

# ------------------------------------------------- clean finished worktrees
for wt in "$WS"/wt/[0-9]*-*; do
  [[ -d $wt ]] || continue
  b=$(basename "$wt"); n=${b%%-*}
  if [[ $(gh issue view "$n" -R "$REPO" --json state -q .state) == CLOSED ]] && ! busy "t$n"; then
    log "Issue #$n closed; removing worktree $b."
    act git -C "$MAIN" worktree remove --force "$wt"
    act git -C "$MAIN" branch -D "$b"
  fi
done

# ------------------------------------------------------------- frontier
active=$(windows | grep -cE '^t[0-9]+$' || true)
today=$STATE/starts-$(date +%F)
started=$( { cat "$today" 2>/dev/null || true; } | wc -l)
pr_issues=$(gh pr list -R "$REPO" --state open --limit 100 --json headRefName --jq '.[].headRefName' | sed -nE 's/^([0-9]+)-.*/\1/p')
candidates=$(gh api "repos/$REPO/issues/$SPEC_ISSUE/sub_issues" --paginate \
  --jq '.[] | select(.state == "open") | select([.labels[].name] as $l | ($l | index("ready-for-agent")) and ($l | index("in-progress") | not) and ($l | index("needs-human") | not) and ($l | index("interactive") | not)) | .number' | sort -n)

for N in $candidates; do
  if (( active >= MAX_PARALLEL )); then break; fi
  if (( started >= MAX_STARTS_PER_DAY )); then log "Daily start cap ($MAX_STARTS_PER_DAY) reached."; break; fi
  grep -qx "$N" <<< "$pr_issues" && continue
  busy "t$N" && continue
  (( $(open_blockers "$N") == 0 )) || continue
  model=$(model_for "$N")
  log "Starting #$N with $model ($((active + 1))/$MAX_PARALLEL running, $((started + 1))/$MAX_STARTS_PER_DAY today)."
  act "$SCRIPTS/run-ticket.sh" "$N" --model "$model" --detach
  [[ $DRY_RUN == 1 ]] || echo "$N" >> "$today"
  active=$((active + 1)); started=$((started + 1))
done
