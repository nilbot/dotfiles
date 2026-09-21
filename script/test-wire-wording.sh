#!/usr/bin/env bash
# Pin the two things `wire` says about itself: the lines it prints, and the
# comment that explains why it prints them.
#
# Why this test exists. `wire` used to print `wired <harness> -> <path>` for
# three config files it had not touched, because it still had the reporting of
# the version that wrote those files. Changing the return type stopped the lie
# at the source; this stops it coming back through a reworded string or a
# comment that drifts from the code. Neither was covered: the acceptance script
# greps for `wire ran` / `retired entry gone`, so any wording passed, and
# nothing read the docstring at all.
#
# Portable on purpose: no GNU-only flags, no `sed -i`, no `readlink -f`, no
# dependency on the installed `agents`. It builds the binary it tests, and runs
# on Linux CI as well as on a developer's macOS.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

pass=0
fail=0
ok()  { pass=$((pass + 1)); printf '  PASS  %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf '  FAIL  %s\n' "$1"; }

# expect <label> <expected> <actual>
expect() {
  if [ "$2" = "$3" ]; then
    ok "$1"
  else
    bad "$1"
    printf '        expected: %s\n' "$(printf '%s' "$2" | sed 's/^/          /')"
    printf '        actual:   %s\n' "$(printf '%s' "$3" | sed 's/^/          /')"
  fi
}

echo "building the binary under test"
if ! (cd "${ROOT_DIR}/agents" && go build -o "${WORK}/agents" .); then
  echo "  FAIL  the agents binary did not build" >&2
  exit 1
fi
AGENTS="${WORK}/agents"

# fresh_repo <name> — a git repository with nothing wired, printed on stdout.
fresh_repo() {
  local d="${WORK}/$1"
  mkdir -p "${d}"
  (cd "${d}" && git init -q .)
  printf '%s' "${d}"
}

# The three lines a run says when it found nothing of its own. This is the
# wording that replaced `wired <harness> -> <path>`.
NOTHING='claude-code: no entries of this tool'\''s to remove
codex: no entries of this tool'\''s to remove
antigravity: no entries of this tool'\''s to remove'

echo
echo "=== scenario 1: a repository that never carried our entries ==="
REPO="$(fresh_repo fresh)"
# `init` prints `initialized <absolute temp path>` first, then one line per
# harness, then the trust steps. That path varies per run, so drop line 1 by
# position rather than trying to match it.
OUT="$(cd "${REPO}" && "${AGENTS}" init 2>&1 | tail -n +2 | grep -E '^(claude-code|codex|antigravity):')"
expect "init says it removed nothing, once per harness" "${NOTHING}" "$OUT"

# The same repo after init, run through wire alone: same three lines.
OUT="$(cd "${REPO}" && "${AGENTS}" wire 2>&1)"
expect "wire alone says the same thing" "${NOTHING}" "$OUT"

# And it must not have written any of the files the old wording named.
for f in .claude/settings.json .codex/hooks.json .agents/hooks.json; do
  if [ -e "${REPO}/${f}" ]; then
    bad "wire reported nothing to remove but created ${f}"
  else
    ok "no config was created at ${f}"
  fi
done

echo
echo "=== scenario 2: entries an earlier version wrote, nothing else in the file ==="
REPO="$(fresh_repo stale)"
mkdir -p "${REPO}/.claude"
cat > "${REPO}/.claude/settings.json" <<'JSON'
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/opt/homebrew/bin/agents hook stop --harness claude-code"
          }
        ]
      }
    ]
  }
}
JSON
OUT="$(cd "${REPO}" && "${AGENTS}" wire 2>&1 | grep '^claude-code')"
expect "wire names the count and says the config went with it" \
  "claude-code: removed 1 entry and the config that held them" "$OUT"
if [ -e "${REPO}/.claude/settings.json" ]; then
  bad "the config held only our entry, so it should have been removed"
else
  ok "the config that held only our entry was removed"
fi

echo
echo "=== scenario 3: our entry beside content that is not ours ==="
REPO="$(fresh_repo mixed)"
mkdir -p "${REPO}/.claude"
cat > "${REPO}/.claude/settings.json" <<'JSON'
{
  "model": "opus",
  "hooks": {
    "Notification": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/my/own/notify.sh"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/opt/homebrew/bin/agents hook stop --harness claude-code"
          }
        ]
      }
    ]
  }
}
JSON
OUT="$(cd "${REPO}" && "${AGENTS}" wire 2>&1 | grep '^claude-code')"
expect "wire says what it took and what it left" \
  "claude-code: removed 1 entry, kept the rest of .claude/settings.json" "$OUT"
if grep -q '/my/own/notify.sh' "${REPO}/.claude/settings.json" 2>/dev/null \
   && grep -q '"model": "opus"' "${REPO}/.claude/settings.json" 2>/dev/null; then
  ok "the foreign hook and the unrelated key survived"
else
  bad "wire removed content that was not ours"
fi

echo
echo "=== scenario 4: the docstring still describes the code ==="
HARNESS="${ROOT_DIR}/agents/internal/harness/harness.go"
if [ ! -f "${HARNESS}" ]; then
  bad "cannot read ${HARNESS}"
else
  # The claims the comment exists to make. Each is a sentence a future edit
  # could drop while the code kept working, which is exactly the drift this
  # checks for.
  check_phrase() {
    if grep -qF "$1" "${HARNESS}"; then
      ok "the docstring still claims: $2"
    else
      bad "the docstring no longer claims: $2"
    fi
  }
  check_phrase "It exists because the command's name outlived the command's work." \
    "why the type exists"
  check_phrase "a caller cannot print a path it was not told was touched" \
    "that the type prevents the lie"
  check_phrase "Removed counts the entries of this tool's that the run deleted." \
    "what Removed means"
  check_phrase "It is always false when Removed is 0." \
    "the ConfigRemoved invariant"

  # And the false claim must not come back. The old message said a config was
  # generated; nothing generates one now.
  if grep -qE 'Wire writes that config, merging' "${HARNESS}"; then
    bad "the interface docstring claims Wire writes a config, which it does not"
  else
    ok "no docstring claims that Wire writes a config"
  fi
fi

echo
echo "==============================="
printf 'wire wording: %d passed, %d failed\n' "${pass}" "${fail}"
echo "==============================="
[ "${fail}" -eq 0 ]
