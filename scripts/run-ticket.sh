#!/usr/bin/env bash
# Run one GitHub issue unattended with Claude Code, in its own git worktree.
#
# Usage:
#   scripts/run-ticket.sh <issue> [--model sonnet|opus] [--detach]
#
#   --detach   start in a new window of the tmux session "tickets" and return
#   --model    Claude model alias (default: sonnet)
#
# Environment:
#   MAX_ATTEMPTS  Claude runs before giving up (default 6)
#   WAIT_MIN      minutes to wait between runs, e.g. for usage limits (default 30)
#
# The run is done when an open PR exists for the issue's branch. Each run that
# ends without a PR waits WAIT_MIN and resumes the same conversation. If the
# agent labels the issue needs-human, or attempts run out, the script labels the
# issue needs-human and stops. The tmux window closes when the script exits.
# Logs: <workspace>/logs/ticket-<issue>.log. Worktrees: <workspace>/wt/<branch>.
set -euo pipefail

usage() { echo "usage: $0 <issue> [--model sonnet|opus] [--detach]" >&2; exit 2; }
[[ $# -ge 1 && $1 =~ ^[0-9]+$ ]] || usage
N=$1; shift
MODEL=sonnet
DETACH=0
while [[ $# -gt 0 ]]; do
  case $1 in
    --model) MODEL=${2:?--model needs a value}; shift 2 ;;
    --detach) DETACH=1; shift ;;
    *) usage ;;
  esac
done
MAX_ATTEMPTS=${MAX_ATTEMPTS:-6}
WAIT_MIN=${WAIT_MIN:-30}

MAIN=$(git -C "$(dirname "$0")" worktree list --porcelain | awk '/^worktree /{print $2; exit}')
WS=$(dirname "$MAIN")
mkdir -p "$WS/logs" "$WS/wt"
LOG=$WS/logs/ticket-$N.log

if [[ $DETACH == 1 ]]; then
  command -v tmux >/dev/null || { echo "tmux not found" >&2; exit 1; }
  tmux has-session -t tickets 2>/dev/null || tmux new-session -d -s tickets -n home
  tmux new-window -d -t tickets -n "t$N" \
    "MAX_ATTEMPTS=$MAX_ATTEMPTS WAIT_MIN=$WAIT_MIN '$MAIN/scripts/run-ticket.sh' $N --model $MODEL"
  echo "Started issue #$N in tmux session 'tickets', window t$N. Log: $LOG"
  exit 0
fi

log() { echo "[$(date '+%F %T')] $*" | tee -a "$LOG"; }

REPO=$(cd "$MAIN" && gh repo view --json nameWithOwner -q .nameWithOwner)

open_blockers=$(gh api "repos/$REPO/issues/$N/dependencies/blocked_by" \
  --jq '[.[] | select(.state == "open") | "#\(.number)"] | join(" ")')
if [[ -n $open_blockers ]]; then
  log "Issue #$N is blocked by $open_blockers; not starting."
  exit 1
fi

title=$(gh issue view "$N" -R "$REPO" --json title -q .title)
slug=$(printf '%s' "$title" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-+//' | cut -c1-40 | sed -E 's/-+$//')
BRANCH="$N-$slug"
WT=$WS/wt/$BRANCH

if [[ ! -d $WT ]]; then
  git -C "$MAIN" fetch -q origin
  if git -C "$MAIN" show-ref --verify --quiet "refs/heads/$BRANCH"; then
    git -C "$MAIN" worktree add -q "$WT" "$BRANCH"
  else
    git -C "$MAIN" worktree add -q -b "$BRANCH" "$WT" origin/main
  fi
  log "Created worktree $WT on branch $BRANCH"
fi
cd "$WT"
MARK=$(git rev-parse --git-dir)/run-ticket-started

has_pr() { [[ $(gh pr list -R "$REPO" --head "$BRANCH" --state open --json number --jq length) -gt 0 ]]; }
has_label() { gh issue view "$N" -R "$REPO" --json labels --jq '.labels[].name' | grep -qx "$1"; }
set_labels() { gh issue edit "$N" -R "$REPO" "$@" >/dev/null; }

PROMPT_FIRST="Implement GitHub issue #$N end to end following the ticket-flow skill. You are running unattended on branch $BRANCH in its own worktree: nobody can answer questions, so work through to an open pull request."
PROMPT_CONT="Continue GitHub issue #$N from where you stopped, following the ticket-flow skill. Start by checking git status, the branch's commits and whether a pull request already exists."

set_labels --add-label in-progress

for ((i = 1; i <= MAX_ATTEMPTS; i++)); do
  has_pr && break
  if [[ -f $MARK ]]; then
    args=(--continue -p "$PROMPT_CONT")
  else
    args=(-p "$PROMPT_FIRST")
    touch "$MARK"
  fi
  log "Attempt $i/$MAX_ATTEMPTS on #$N (model $MODEL)"
  set +e
  claude --model "$MODEL" "${args[@]}" 2>&1 | tee -a "$LOG"
  status=${PIPESTATUS[0]}
  set -e
  log "Claude exited with status $status"

  has_pr && break
  if has_label needs-human; then
    set_labels --remove-label in-progress
    log "The agent asked for a human on #$N; stopping."
    exit 1
  fi
  if (( i < MAX_ATTEMPTS )); then
    log "No PR yet; waiting $WAIT_MIN minutes before resuming."
    sleep $((WAIT_MIN * 60))
  fi
done

if has_pr; then
  set_labels --remove-label in-progress
  log "Done: $(gh pr list -R "$REPO" --head "$BRANCH" --state open --json url --jq '.[0].url')"
  exit 0
fi

set_labels --remove-label in-progress --add-label needs-human
gh issue comment "$N" -R "$REPO" --body "Unattended run stopped after $MAX_ATTEMPTS attempts without a pull request. Branch \`$BRANCH\`; see \`logs/ticket-$N.log\` on the VPS." >/dev/null
log "Gave up on #$N after $MAX_ATTEMPTS attempts; labeled needs-human."
exit 1
