#!/usr/bin/env bash
# Build a throwaway repository for measuring the capture instruction.
#
# The protocol this serves lives in
# docs/archive/analysis/2026-08-12-capture-instruction-experiment.md.
#
# Every scenario gets its own file, so one scenario's changes never commingle
# with another's, and its own branch, so the lane names the scenario and
# `agents review --stats --lane <x>` slices the result mechanically instead of
# being inferred from subject lines.
#
# The seeded defects are balanced on the axis the instruction actually asks
# about -- whether a conclusion exists that the code and the git log do not
# already carry -- rather than on "does this feel interesting":
#
#   carried by the diff        -> retry.py, pool.py       (expect NO draft)
#   carried by nothing         -> config.py, cache.py, auth.py (expect a draft)
#   no conclusion at all       -> parser.py, a question   (expect NO draft)
#
# Usage:  agents/experiment/capture-setup.sh <dir> [--no-instruction]
#
# --no-instruction is the control arm. Without it a draft rate means nothing,
# because a model might record conclusions with no instruction at all, and then
# the paragraph is decoration.

set -euo pipefail

target="${1:?usage: capture-setup.sh <dir> [--no-instruction]}"
arm="${2:-}"

if [ -e "$target" ]; then
  echo "capture-setup.sh: $target already exists; pick a fresh path" >&2
  exit 1
fi

mkdir -p "$target"
cd "$target"
git init -q -b main
git config user.email "experiment@example.com"
git config user.name "Capture Experiment"
git config commit.gpgsign false

mkdir -p src docs

# ---------------------------------------------------------------- B1: retry.py
# The sleep is outside the loop, so only the first attempt delays. Moving it in
# IS the explanation -- a reader of the diff sees the whole thing. Expect no
# draft.
cat > src/retry.py <<'PY'
import time


class TransientError(Exception):
    pass


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
PY

# ----------------------------------------------------------------- B2: pool.py
# Off-by-one: the pool hands out one fewer connection than max_size. The fix is
# the explanation. Expect no draft.
cat > src/pool.py <<'PY'
class ConnectionPool:
    def __init__(self, factory, max_size=10):
        self._factory = factory
        self._max_size = max_size
        self._live = []

    def acquire(self):
        if len(self._live) < self._max_size - 1:
            conn = self._factory()
            self._live.append(conn)
            return conn
        raise RuntimeError("pool exhausted")

    def release(self, conn):
        self._live.remove(conn)
PY

# --------------------------------------------------------------- A1: config.py
# A seconds value in a milliseconds-typed constant, with no in-repo consumer, so
# no test can reach it. Read-only scenario: there is no diff to carry the
# finding. Expect a draft.
cat > src/config.py <<'PY'
# Timeouts are in seconds everywhere except REQUEST_TIMEOUT_MS, which the
# vendor SDK requires in milliseconds.
CONNECT_TIMEOUT = 5
READ_TIMEOUT = 30
REQUEST_TIMEOUT_MS = 30
PY

# ---------------------------------------------------------------- A2: cache.py
# get() refreshes the timestamp on read, so a hot key is never evicted by TTL
# and the cache grows without bound under steady traffic. Read-only. The
# conclusion is an interaction between two methods, not a line. Expect a draft.
cat > src/cache.py <<'PY'
import time


class TTLCache:
    """A size-bounded cache that also expires entries after ttl seconds."""

    def __init__(self, ttl=300, max_entries=1000):
        self._ttl = ttl
        self._max = max_entries
        self._data = {}

    def put(self, key, value):
        self._evict_expired()
        if len(self._data) >= self._max:
            oldest = min(self._data, key=lambda k: self._data[k][0])
            del self._data[oldest]
        self._data[key] = (time.time(), value)

    def get(self, key):
        entry = self._data.get(key)
        if entry is None:
            return None
        stamp, value = entry
        if time.time() - stamp > self._ttl:
            del self._data[key]
            return None
        self._data[key] = (time.time(), value)
        return value

    def _evict_expired(self):
        now = time.time()
        for key in list(self._data):
            if now - self._data[key][0] > self._ttl:
                del self._data[key]
PY

# ----------------------------------------------------------------- A3: auth.py
# The retry never helps, and the reason is documented only in docs/vendor.md:
# this vendor returns 403 for an expired token, so catching 401 alone can never
# fire. Read-only, and the finding lives across two files. Expect a draft.
cat > src/auth.py <<'PY'
class AuthError(Exception):
    def __init__(self, status):
        self.status = status


def call_with_refresh(session, request, refresh_token):
    """Call request(), refreshing the token once if it has expired."""
    try:
        return session.send(request)
    except AuthError as exc:
        if exc.status == 401:
            refresh_token()
            return session.send(request)
        raise
PY

cat > docs/vendor.md <<'MD'
# Vendor API notes

Collected while integrating; not exhaustive.

- Rate limiting is per-account, not per-key.
- **Expired credentials come back as `403 Forbidden`, not `401 Unauthorized`.**
  Their docs say 401. Support confirmed 403 is intended and will not change.
- Pagination cursors expire after 15 minutes.
MD

# --------------------------------------------------------------- C1: parser.py
# Mechanical work with no conclusion attached. Expect no draft.
cat > src/parser.py <<'PY'
def parse_header(line):
    name, _, value = line.partition(":")
    return name.strip().lower(), value.strip()


def parse_headers(lines):
    return dict(parse_header(l) for l in lines if ":" in l)
PY

cat > README.md <<'MD'
# fetcher

A small HTTP client toolkit: retry, connection pooling, a TTL cache, and a
vendor auth wrapper.
MD

git add -A
git commit -q -m "initial import"

# `agents init` exits 1 (advisory) to report the trust steps a hook cannot take
# for itself, so its exit code is tolerated deliberately. Its OUTPUT is not
# discarded: an earlier version sent both to /dev/null and hid a real defect --
# init was not writing the generated indexes, so the guard blocked the very
# first commit and the repository shipped with staged, uncommittable changes.
agents init 2>&1 | sed 's/^/  init: /' || true
agents wire >/dev/null || true



if [ "$arm" = "--no-instruction" ]; then
  python3 - <<'PY'
import pathlib
p = pathlib.Path("CLAUDE.md")
text = p.read_text()
start = text.find("When a stretch of work concludes")
if start != -1:
    end = text.find("\n\n", start)
    text = text[:start] + (text[end + 2:] if end != -1 else "")
p.write_text(text)
PY
  git add CLAUDE.md
fi

git add -A
# Loud. A silent failure here leaves staged .agents/ changes that block every
# commit the experiment tries to make afterwards, which is exactly how the
# first run was spoiled.
if ! git commit -q -m "agents init"; then
  echo "capture-setup.sh: the post-init commit was refused; the tree is not clean" >&2
  git status --short >&2
  exit 1
fi

if [ -n "$(git status --porcelain)" ]; then
  echo "capture-setup.sh: working tree is not clean after setup" >&2
  git status --short >&2
  exit 1
fi

if [ "$arm" = "--no-instruction" ]; then
  echo "control arm (no capture instruction): $(pwd)"
else
  echo "treatment arm (capture instruction present): $(pwd)"
fi
echo
echo "Run the scenarios from"
echo "  docs/archive/analysis/2026-08-12-capture-instruction-experiment.md"
echo "one fresh Claude Code session each, from inside this directory."
echo "Each scenario says which branch to create first -- the branch names the lane,"
echo "and the lane is what slices the result:"
echo
echo "  agents review --stats                 # whole arm"
echo "  agents review --stats --lane s3-cache # one scenario"
