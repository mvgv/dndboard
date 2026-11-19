#!/usr/bin/env bash
# Independent review of a ticket PR by a fresh Claude Code session.
#
# Usage:
#   scripts/review-pr.sh <pr> [--model sonnet|opus] [--detach]
#
# Checks out the PR head in a throwaway worktree, runs a read-only Claude session
# with scripts/review-prompt.md, posts its report as a PR comment carrying the
# marker `<!-- dndboard-review sha=<sha> verdict=<pass|changes> -->`, and sets the
# review-passed / changes-requested labels. The conductor reads the marker for
# the PR's current head; a new commit means a new review.
# Logs: <workspace>/logs/review-<pr>.log.
set -euo pipefail

usage() { echo "usage: $0 <pr> [--model sonnet|opus] [--detach]" >&2; exit 2; }
[[ $# -ge 1 && $1 =~ ^[0-9]+$ ]] || usage
PR=$1; shift
MODEL=sonnet
DETACH=0
while [[ $# -gt 0 ]]; do
  case $1 in
    --model) MODEL=${2:?--model needs a value}; shift 2 ;;
    --detach) DETACH=1; shift ;;
    *) usage ;;
  esac
done

MAIN=$(git -C "$(dirname "$0")" worktree list --porcelain | awk '/^worktree /{print $2; exit}')
WS=$(dirname "$MAIN")
mkdir -p "$WS/logs" "$WS/wt" "$WS/state"
LOG=$WS/logs/review-$PR.log

if [[ $DETACH == 1 ]]; then
  command -v tmux >/dev/null || { echo "tmux not found" >&2; exit 1; }
  tmux has-session -t tickets 2>/dev/null || tmux new-session -d -s tickets -n home
  tmux new-window -d -t tickets -n "r$PR" "'$MAIN/scripts/review-pr.sh' $PR --model $MODEL"
  echo "Started review of PR #$PR in tmux session 'tickets', window r$PR. Log: $LOG"
  exit 0
fi

log() { echo "[$(date '+%F %T')] $*" | tee -a "$LOG"; }

REPO=$(cd "$MAIN" && gh repo view --json nameWithOwner -q .nameWithOwner)
read -r BRANCH SHA < <(gh pr view "$PR" -R "$REPO" --json headRefName,headRefOid --jq '.headRefName + " " + .headRefOid')
ISSUE=${BRANCH%%-*}
[[ $ISSUE =~ ^[0-9]+$ ]] || { log "PR #$PR branch $BRANCH is not a ticket branch."; exit 1; }

WT=$WS/wt/review-$PR
git -C "$MAIN" fetch -q origin "$BRANCH"
[[ -d $WT ]] && git -C "$MAIN" worktree remove --force "$WT"
git -C "$MAIN" worktree add -q --detach "$WT" "$SHA"
cleanup() { git -C "$MAIN" worktree remove --force "$WT" 2>/dev/null || true; }
trap cleanup EXIT

prompt=$(sed -e "s/{{PR}}/$PR/g" -e "s/{{ISSUE}}/$ISSUE/g" -e "s/{{SHA}}/$SHA/g" "$MAIN/scripts/review-prompt.md")
OUT=$(mktemp)
log "Reviewing PR #$PR (issue #$ISSUE) at ${SHA:0:7} with $MODEL"
cd "$WT"
set +e
claude --model "$MODEL" --disallowedTools Edit Write NotebookEdit -p "$prompt" > "$OUT" 2>>"$LOG"
status=$?
set -e
cat "$OUT" >> "$LOG"

verdict=$(tail -n 5 "$OUT" | sed -nE 's/^VERDICT:[[:space:]]*(pass|changes)[[:space:]]*$/\1/p' | tail -1)
if [[ -z $verdict ]]; then
  fails_file=$WS/state/review-fail-pr$PR-$SHA
  fails=$(( $(cat "$fails_file" 2>/dev/null || echo 0) + 1 ))
  echo "$fails" > "$fails_file"
  log "No VERDICT line (claude status $status, failure $fails for this commit)."
  if (( fails >= 2 )); then
    gh pr edit "$PR" -R "$REPO" --add-label needs-human >/dev/null || true
    gh pr comment "$PR" -R "$REPO" --body "Automatic review failed twice on ${SHA:0:7} without a verdict; see \`logs/review-$PR.log\` on the VPS." >/dev/null
  fi
  rm -f "$OUT"
  exit 1
fi

{
  cat "$OUT"
  echo
  echo "<!-- dndboard-review sha=$SHA verdict=$verdict -->"
} > "$OUT.body"
gh pr comment "$PR" -R "$REPO" --body-file "$OUT.body" >/dev/null
if [[ $verdict == pass ]]; then
  gh pr edit "$PR" -R "$REPO" --add-label review-passed --remove-label changes-requested >/dev/null || true
else
  gh pr edit "$PR" -R "$REPO" --add-label changes-requested --remove-label review-passed >/dev/null || true
fi
rm -f "$OUT" "$OUT.body"
log "Posted review of PR #$PR at ${SHA:0:7}: $verdict"
