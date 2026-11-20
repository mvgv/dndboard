#!/usr/bin/env bash
# Run one GitHub issue unattended with Claude Code, in its own git worktree.
#
# Usage:
#   scripts/run-ticket.sh <issue> [--model sonnet|opus] [--detach] [--feedback-file <path>]
#
#   --detach          start in window t<issue> of the tmux session "tickets" and return
#   --model           Claude model alias (default: sonnet)
#   --feedback-file   resume the ticket's conversation once with this message (CI failure,
#                     review findings, merge conflict), then exit
#
# Environment:
#   MAX_ATTEMPTS  Claude runs before giving up on reaching a PR (default 6)
#   WAIT_MIN      minutes to wait between runs, e.g. for usage limits (default 30)
#   MAX_LIMIT_WAITS  usage-limit waits tolerated, not counted as attempts (default 24, ~12 h)
#   MAX_FEEDBACK  feedback rounds per ticket before asking a human (default 3)
#
# Implement mode is done when an open PR exists for the issue's branch. Each run that
# ends without a PR waits WAIT_MIN and resumes the same conversation. If the agent
# labels the issue needs-human, or attempts run out, the script labels the issue
# needs-human and stops. The tmux window closes when the script exits.
# Logs: <workspace>/logs/ticket-<issue>.log. Worktrees: <workspace>/wt/<branch>.
set -euo pipefail

usage() { echo "usage: $0 <issue> [--model sonnet|opus] [--detach] [--feedback-file <path>]" >&2; exit 2; }
[[ $# -ge 1 && $1 =~ ^[0-9]+$ ]] || usage
N=$1; shift
MODEL=sonnet
DETACH=0
FEEDBACK_FILE=
while [[ $# -gt 0 ]]; do
  case $1 in
    --model) MODEL=${2:?--model needs a value}; shift 2 ;;
    --detach) DETACH=1; shift ;;
    --feedback-file) FEEDBACK_FILE=${2:?--feedback-file needs a path}; shift 2 ;;
    *) usage ;;
  esac
done
MAX_ATTEMPTS=${MAX_ATTEMPTS:-6}
WAIT_MIN=${WAIT_MIN:-30}
MAX_LIMIT_WAITS=${MAX_LIMIT_WAITS:-24}
MAX_FEEDBACK=${MAX_FEEDBACK:-3}

MAIN=$(git -C "$(dirname "$0")" worktree list --porcelain | awk '/^worktree /{print $2; exit}')
WS=$(dirname "$MAIN")
mkdir -p "$WS/logs" "$WS/wt" "$WS/state"
LOG=$WS/logs/ticket-$N.log

if [[ $DETACH == 1 ]]; then
  command -v tmux >/dev/null || { echo "tmux not found" >&2; exit 1; }
  tmux has-session -t tickets 2>/dev/null || tmux new-session -d -s tickets -n home
  extra=""
  [[ -n $FEEDBACK_FILE ]] && extra="--feedback-file '$FEEDBACK_FILE'"
  tmux new-window -d -t tickets -n "t$N" \
    "MAX_ATTEMPTS=$MAX_ATTEMPTS WAIT_MIN=$WAIT_MIN MAX_LIMIT_WAITS=$MAX_LIMIT_WAITS MAX_FEEDBACK=$MAX_FEEDBACK '$MAIN/scripts/run-ticket.sh' $N --model $MODEL $extra 2>>'$LOG'"
  echo "Started issue #$N in tmux session 'tickets', window t$N. Log: $LOG"
  exit 0
fi

log() { echo "[$(date '+%F %T')] $*" | tee -a "$LOG"; }

REPO=$(cd "$MAIN" && gh repo view --json nameWithOwner -q .nameWithOwner)

has_pr() { [[ $(gh pr list -R "$REPO" --head "$BRANCH" --state open --json number --jq length) -gt 0 ]]; }
has_label() { gh issue view "$N" -R "$REPO" --json labels --jq '.labels[].name' | grep -qx "$1"; }
set_labels() { gh issue edit "$N" -R "$REPO" "$@" >/dev/null || true; }
give_up() {
  set_labels --remove-label in-progress --add-label needs-human
  gh issue comment "$N" -R "$REPO" --body "$1 Branch \`$BRANCH\`; see \`logs/ticket-$N.log\` on the VPS." >/dev/null
  log "$1 Labeled needs-human."
  # Forget the conductor's once-per-commit actions for this PR, so that when the
  # owner removes needs-human the conductor sends its feedback again.
  local pr
  pr=$(gh pr list -R "$REPO" --head "$BRANCH" --state open --json number --jq '.[0].number // empty' 2>/dev/null || true)
  [[ -n $pr ]] && rm -f "$WS"/state/*-pr"$pr"-*
  rm -f "$(git -C "$WT" rev-parse --git-dir 2>/dev/null)/run-ticket-feedback-rounds" 2>/dev/null || true
  exit 1
}
LIMIT_RE="hit your .*limit|usage limit reached"
limited() { tail -n 20 "$1" | grep -qiE "$LIMIT_RE"; }
mark_limited() { touch "$WS/state/usage-limited"; log "Usage limit hit; marked $WS/state/usage-limited."; }

title=$(gh issue view "$N" -R "$REPO" --json title -q .title)
slug=$(printf '%s' "$title" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-+//' | cut -c1-40 | sed -E 's/-+$//')
BRANCH="$N-$slug"
# Reuse an existing branch for this issue, whatever slug it was created with.
existing=$(git -C "$MAIN" for-each-ref --format='%(refname:short)' "refs/heads/$N-*" | head -1)
[[ -n $existing ]] && BRANCH=$existing
WT=$WS/wt/$BRANCH

# ---- Feedback mode: one resumed run on an existing ticket conversation ----
if [[ -n $FEEDBACK_FILE ]]; then
  [[ -f $FEEDBACK_FILE ]] || { log "Feedback file $FEEDBACK_FILE not found."; exit 1; }
  [[ -d $WT ]] || { log "No worktree $WT for #$N; cannot resume."; exit 1; }
  cd "$WT"
  rounds_file=$(git rev-parse --git-dir)/run-ticket-feedback-rounds
  if grep -q '^KIND: conflict$' "$FEEDBACK_FILE"; then
    rounds=$(cat "$rounds_file" 2>/dev/null || echo 0)
    log "Conflict resolution on #$N (not counted as a feedback round; model $MODEL)"
  else
    rounds=$(( $(cat "$rounds_file" 2>/dev/null || echo 0) + 1 ))
    (( rounds <= MAX_FEEDBACK )) || give_up "Feedback round $rounds exceeds $MAX_FEEDBACK without converging."
    log "Feedback round $rounds/$MAX_FEEDBACK on #$N (model $MODEL)"
  fi
  RUN_OUT=$(mktemp)
  waits=0
  while true; do
    set +e
    claude --model "$MODEL" --continue -p "$(grep -v '^KIND: ' "$FEEDBACK_FILE")" 2>&1 | tee "$RUN_OUT" | tee -a "$LOG"
    status=${PIPESTATUS[0]}
    set -e
    log "Claude exited with status $status"
    limited "$RUN_OUT" || break
    mark_limited
    waits=$((waits + 1))
    (( waits <= MAX_LIMIT_WAITS )) || give_up "Usage limit still hit after $MAX_LIMIT_WAITS waits of $WAIT_MIN min."
    log "Usage limit: waiting $WAIT_MIN minutes before retrying the feedback ($waits/$MAX_LIMIT_WAITS)."
    sleep $((WAIT_MIN * 60))
  done
  grep -q '^KIND: conflict$' "$FEEDBACK_FILE" || echo "$rounds" > "$rounds_file"
  rm -f "$FEEDBACK_FILE" "$RUN_OUT"
  exit 0
fi

# ---- Implement mode ----
open_blockers=$(gh api "repos/$REPO/issues/$N/dependencies/blocked_by" \
  --jq '[.[] | select(.state == "open") | "#\(.number)"] | join(" ")')
if [[ -n $open_blockers ]]; then
  log "Issue #$N is blocked by $open_blockers; not starting."
  exit 1
fi

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

PROMPT_FIRST="Implement GitHub issue #$N end to end following the ticket-flow skill. You are running unattended on branch $BRANCH in its own worktree: nobody can answer questions, so work through to an open pull request."
PROMPT_CONT="Continue GitHub issue #$N from where you stopped, following the ticket-flow skill. Start by checking git status, the branch's commits and whether a pull request already exists."

set_labels --add-label in-progress

i=1
waits=0
while (( i <= MAX_ATTEMPTS )); do
  has_pr && break
  if [[ -f $MARK ]]; then
    args=(--continue -p "$PROMPT_CONT")
  else
    args=(-p "$PROMPT_FIRST")
    touch "$MARK"
  fi
  log "Attempt $i/$MAX_ATTEMPTS on #$N (model $MODEL)"
  set +e
  RUN_OUT=$(mktemp)
  claude --model "$MODEL" "${args[@]}" 2>&1 | tee "$RUN_OUT" | tee -a "$LOG"
  status=${PIPESTATUS[0]}
  set -e
  log "Claude exited with status $status"
  if limited "$RUN_OUT"; then
    rm -f "$RUN_OUT"
    mark_limited
    waits=$((waits + 1))
    (( waits <= MAX_LIMIT_WAITS )) || give_up "Usage limit still hit after $MAX_LIMIT_WAITS waits of $WAIT_MIN min."
    log "Usage limit: waiting $WAIT_MIN minutes; this run does not count as an attempt ($waits/$MAX_LIMIT_WAITS)."
    sleep $((WAIT_MIN * 60))
    continue
  fi
  rm -f "$RUN_OUT"

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
  i=$((i + 1))
done

if has_pr; then
  set_labels --remove-label in-progress
  log "Done: $(gh pr list -R "$REPO" --head "$BRANCH" --state open --json url --jq '.[0].url')"
  exit 0
fi

give_up "Unattended run stopped after $MAX_ATTEMPTS attempts without a pull request."
