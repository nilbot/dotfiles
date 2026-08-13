#!/usr/bin/env bash
# Run the capture experiment end to end, without a human at a keyboard.
#
# Spec 1's measured facts record that a project-local .claude/settings.json
# "loads and fires, including for a non-interactive `claude -p` session in a
# repo with no prior trust record". That is what makes this runnable: each
# scenario is one headless session, so the hooks record `stop` events and the
# denominator is real.
#
# Usage:  agents/experiment/capture-run.sh <treatment-dir> [control-dir]
#
# One fresh session per scenario, each on its own branch so the lane names the
# scenario. Committing between scenarios is done here rather than left to the
# agent, because the criterion under test includes "or the git log" and a
# working tree full of uncommitted work removes half of what the agent weighs.

set -euo pipefail

treatment="${1:?usage: capture-run.sh <treatment-dir> [control-dir]}"
control="${2:-}"

if ! command -v claude >/dev/null; then
  echo "capture-run.sh: claude is not on PATH" >&2
  exit 1
fi

# scenario := branch <TAB> expectation <TAB> prompt
# Expectation is what the protocol predicts, and it is recorded here so the
# scoring is mechanical rather than remembered.
scenarios=$(cat <<'TSV'
s1-retry	no-draft	src/retry.py retries but the backoff never actually delays between attempts. Find out why and fix it.
s2-config	draft	Something is wrong with how timeouts are configured in src/config.py. Work out what and explain it. Do not change any code.
s3-cache	draft	src/cache.py is supposed to bound memory two ways: a TTL and a maximum entry count. Under steady traffic it still grows. Explain why. Do not change any code.
s4-auth	draft	src/auth.py refreshes the token when a call fails, but in production the refresh never happens. Work out why. Do not change any code.
s5-pool	no-draft	src/pool.py is configured with max_size=10 but callers report exhaustion at nine connections. Fix it.
s6-parser	no-draft	Add type hints to the functions in src/parser.py.
s7-question	no-draft	What does src/parser.py do?
TSV
)

run_arm() {
  local dir="$1" label="$2" only_positive="$3"
  echo
  echo "=== $label: $dir ==="
  cd "$dir"

  while IFS=$'\t' read -r branch expect prompt; do
    [ -n "$branch" ] || continue
    if [ "$only_positive" = "yes" ] && [ "$expect" != "draft" ]; then
      continue
    fi

    git checkout -q main
    git checkout -q -b "$branch" 2>/dev/null || git checkout -q "$branch"
    printf '  %-12s (expect %-8s) ' "$branch" "$expect"

    # Two flags, for two different reasons.
    #
    # --permission-mode acceptEdits lets a scenario that must change code do so
    # without a prompt nobody is there to answer.
    #
    # --allowedTools "Bash(agents:*)" is what makes the measurement possible at
    # all. A headless session does not grant arbitrary Bash, so without it
    # `agents handoff draft` is refused and the arm reads zero however well the
    # instruction works -- an experiment guaranteed to find nothing. It cannot
    # go in .claude/settings.json: an untrusted workspace ignores
    # permissions.allow outright, which a run confirmed. An interactive user
    # approves `agents` once and never meets this, so granting exactly that one
    # command restores parity rather than tilting the result.
    # </dev/null is load-bearing: without it `claude` inherits this loop's
    # stdin and consumes the remaining scenario lines, so only the first
    # scenario ever runs and every other one silently reports "no draft".
    if claude -p "$prompt" --permission-mode acceptEdits \
         --allowedTools "Bash(agents:*)" </dev/null >/dev/null 2>&1; then
      printf 'ran'
    else
      printf 'ran (non-zero)'
    fi

    # Commit whatever the scenario changed, so the next one starts from a clean
    # tree and the git log carries the previous scenario's work.
    if [ -n "$(git status --porcelain -- . ':!.agents')" ]; then
      git add -A -- . ':!.agents' >/dev/null 2>&1 || git add -A
      git commit -q -m "$branch: scenario work" >/dev/null 2>&1 || true
      printf ', committed'
    fi
    echo
  done <<< "$scenarios"

  git checkout -q main
}

run_arm "$treatment" "treatment" "no"
[ -n "$control" ] && run_arm "$control" "control" "yes"

echo
echo "=== results ==="
for arm in "$treatment" ${control:+"$control"}; do
  echo
  echo "--- $arm ---"
  cd "$arm"
  agents review --stats || true
  echo
  while IFS=$'\t' read -r branch expect _; do
    [ -n "$branch" ] || continue
    drafted=$(agents review --lane "$branch" 2>/dev/null | grep -c "^$branch/" || true)
    if [ "$expect" = "draft" ]; then
      [ "$drafted" -gt 0 ] && verdict="hit" || verdict="MISS"
    else
      [ "$drafted" -gt 0 ] && verdict="FALSE POSITIVE" || verdict="ok"
    fi
    printf '  %-12s expect %-8s drafted %s  -> %s\n' "$branch" "$expect" "$drafted" "$verdict"
  done <<< "$scenarios"
done

echo
echo "Now read the drafts -- the rate does not tell you whether they are any good:"
echo "  agents review --show <id>"
echo "  agents review --keep <id>   # or --bin"
