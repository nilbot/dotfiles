#!/usr/bin/env bash
# Run the capture experiment headlessly, both arms, and score the matrix.
#
# An earlier version of this header claimed headless runs under-measure the
# positive half, because in a one-shot run the agent's answer IS the deliverable
# so there is no "before moving on" left to record. RETRACTED: on 2026-08-13 this
# runner drafted on 3 of 3 A scenarios headlessly.
#
# The variable was the tool grant, not the harness. Those earlier runs allowed
# only `Bash(agents:*)`, so the agent could read files and nothing else; it
# drafted about the sandbox rather than the code. Granting python3 and grep lets
# it investigate and reach a measured conclusion, and then it drafts. Keep that
# in mind before blaming a population difference for a zero.
#
# Protocol and results:
# docs/archive/analysis/2026-08-12-capture-instruction-experiment.md
#
# Usage:  agents/experiment/capture-run.sh <treatment-dir> [control-dir]

set -euo pipefail

treatment="${1:?usage: capture-run.sh <treatment-dir> [control-dir]}"
control="${2:-}"

if ! command -v claude >/dev/null; then
  echo "capture-run.sh: claude is not on PATH" >&2
  exit 1
fi

# arm <TAB> branch <TAB> expectation <TAB> session-id, appended as scenarios run.
#
# Scoring joins on the session id because the harness assigns it and the agent
# cannot override it. Branch name is not usable: the instruction tells the agent
# to pass `--lane <lane>` and it picks its own value, so a draft landed on lane
# "retry" while the branch was "s1-retry", and every scenario scored zero
# against a queue that visibly held a draft.
SESSION_MAP=$(mktemp)
trap 'rm -f "$SESSION_MAP"' EXIT

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

    # --permission-mode acceptEdits lets a scenario that must change code do so
    # without a prompt nobody is there to answer.
    #
    # --allowedTools is what makes the measurement possible at all. A headless
    # session grants no arbitrary Bash, so without it `agents handoff draft` is
    # refused and the arm reads zero however well the instruction works. It
    # cannot go in .claude/settings.json: an untrusted workspace ignores
    # permissions.allow outright. python3 and grep are granted too, because
    # granting only `agents` left the agent unable to verify its own work, and
    # it drafted about the sandbox rather than about the code.
    #
    # </dev/null is load-bearing: without it `claude` inherits this loop's stdin
    # and eats the remaining scenario lines, so only the first ever runs and
    # every other one silently reports "no draft".
    out=$(claude -p "$prompt" --permission-mode acceptEdits \
            --allowedTools "Bash(agents:*)" "Bash(python3:*)" "Bash(grep:*)" \
            --output-format json </dev/null 2>/dev/null || true)
    sid=$(printf '%s' "$out" | python3 -c 'import sys, json
try:
    print(json.load(sys.stdin).get("session_id", ""))
except Exception:
    print("")' 2>/dev/null || true)
    printf '%s\t%s\t%s\t%s\n' "$dir" "$branch" "$expect" "$sid" >> "$SESSION_MAP"
    if [ -n "$sid" ]; then printf 'ran'; else printf 'ran (no session id)'; fi

    # Commit so the next scenario starts clean and the git log carries the
    # previous scenario's work -- the criterion includes "or the git log".
    if [ -n "$(git status --porcelain)" ]; then
      git add -A >/dev/null 2>&1 || true
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
  hits=0; misses=0; fps=0; tns=0
  while IFS=$'\t' read -r dir branch expect sid; do
    [ "$dir" = "$arm" ] || continue
    if [ -z "$sid" ]; then
      printf '  %-12s expect %-8s NO SESSION ID -> cannot score\n' "$branch" "$expect"
      continue
    fi
    drafted=$(agents review 2>/dev/null | grep -c -- "-$sid-" || true)
    if [ "$expect" = "draft" ]; then
      if [ "$drafted" -gt 0 ]; then verdict="hit"; hits=$((hits + 1))
      else verdict="MISS"; misses=$((misses + 1)); fi
    else
      if [ "$drafted" -gt 0 ]; then verdict="FALSE POSITIVE"; fps=$((fps + 1))
      else verdict="ok"; tns=$((tns + 1)); fi
    fi
    printf '  %-12s expect %-8s drafted %s  -> %s\n' "$branch" "$expect" "$drafted" "$verdict"
  done < "$SESSION_MAP"
  echo
  printf '  hits %d  misses %d  false positives %d  true negatives %d\n' "$hits" "$misses" "$fps" "$tns"
done

cat <<'NOTE'

A "false positive" here is a scored verdict against a rubric that has been wrong
before. On 2026-08-13 both scored false positives turned out to be sound drafts:
a fix's justification -- what was ruled out, what was left alone -- is not
carried by its diff, so drafting alongside a self-explanatory fix is not by
itself a failure. Do not report the matrix without reading what it scored.

Read the drafts; the rate does not say whether they are any good:
  agents review --show <id>
  agents review --keep <id>   # or --bin
NOTE
