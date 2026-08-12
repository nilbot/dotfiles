#!/usr/bin/env bash
# Build a throwaway repository for measuring the capture instruction.
#
# The repository is deliberately small and deliberately has a real bug in it:
# the scenarios below have to be tasks where a genuine conclusion exists to be
# recorded, or the experiment measures nothing but the model's manners.
#
# Usage:  ./setup.sh <dir> [--no-instruction]
#
# --no-instruction scaffolds the same repo with the capture paragraph stripped
# from CLAUDE.md. That is the control arm: without it, a draft rate means
# nothing, because there is no way to tell the instruction working from the
# model doing it anyway.

set -euo pipefail

target="${1:?usage: setup.sh <dir> [--no-instruction]}"
arm="${2:-}"

if [ -e "$target" ]; then
  echo "setup.sh: $target already exists; pick a fresh path" >&2
  exit 1
fi

mkdir -p "$target"
cd "$target"
git init -q -b main
git config user.email "experiment@example.com"
git config user.name "Capture Experiment"
git config commit.gpgsign false

mkdir -p src

# A retry helper whose backoff is wrong in a way that takes reading to see:
# the sleep is outside the loop, so every attempt after the first retries
# immediately. Finding this is a conclusion worth recording; the fix is one
# line, so the git diff will not carry the reasoning.
cat > src/retry.py <<'PY'
import time


def fetch_with_retry(client, url, attempts=5, base_delay=0.5):
    """Fetch url, retrying on transient failures with exponential backoff."""
    delay = base_delay
    last = None
    time.sleep(delay)
    for attempt in range(attempts):
        try:
            return client.get(url)
        except TransientError as exc:
            last = exc
            delay = delay * 2
    raise last


class TransientError(Exception):
    pass
PY

cat > src/config.py <<'PY'
# Timeouts are in seconds everywhere except REQUEST_TIMEOUT_MS, which the
# vendor SDK requires in milliseconds. Mixing them silently produces a
# 30-microsecond timeout.
CONNECT_TIMEOUT = 5
READ_TIMEOUT = 30
REQUEST_TIMEOUT_MS = 30
PY

cat > README.md <<'MD'
# fetcher

A small HTTP fetch helper with retry and a vendor SDK integration.
MD

git add -A
git commit -q -m "initial import"

# `agents init` exits 1 (advisory) to report the trust steps a hook cannot take
# for itself. That is success here, so the exit code is tolerated deliberately
# rather than by dropping `set -e`.
agents init >/dev/null || true
agents wire >/dev/null || true

if [ "$arm" = "--no-instruction" ]; then
  # Control arm: same everything, minus the paragraph under test.
  python3 - <<'PY'
import pathlib, re
p = pathlib.Path("CLAUDE.md")
text = p.read_text()
start = text.find("When a stretch of work concludes")
if start != -1:
    end = text.find("\n\n", start)
    text = text[:start] + (text[end + 2:] if end != -1 else "")
p.write_text(text)
PY
  git add CLAUDE.md
  git commit -q -m "control arm: no capture instruction"
  echo "control arm (no capture instruction)"
else
  echo "treatment arm (capture instruction present)"
fi

git add -A
git commit -q -m "agents init" 2>/dev/null || true

echo "ready: $(pwd)"
echo
echo "Run the scenarios in SCENARIOS.md from inside this directory, then:"
echo "  agents review --stats"
