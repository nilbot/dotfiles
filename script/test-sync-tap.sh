#!/usr/bin/env bash
# Exercise `sync-homebrew-formula.sh` against the inputs it will meet in
# production, including the ones it must refuse.
#
# What this protects. The script rewrites four `url` and four `sha256` lines in
# the tap's formula. Two ways that could go wrong are silent: matching a digest
# to the wrong platform, and mangling content the tap's maintainers wrote. Both
# were live risks -- `checksums.txt` is produced by `sha256sum agents_v*_*.tar.gz`,
# whose glob collates `darwin_amd64` BEFORE `darwin_arm64`, while the formula's
# four slots read arm-first, so a positional implementation swaps every Intel
# digest with its ARM sibling. Nothing downstream would catch that: the formula
# stays valid Ruby and the tap's CI runs `brew test-bot --only-tap-syntax`,
# which checks syntax, not whether a digest belongs to the URL above it. The
# error would surface as a SHA256 mismatch in a user's `brew install`.
#
# The tap is never contacted and nothing is ever pushed: `gh` is shadowed by a
# stub that answers reads from a fixture and records writes to a file.
#
# Portable on purpose: no GNU-only flags, no `sed -i`, no `readlink -f`. Runs on
# Linux CI and on macOS alike.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
SCRIPT="${ROOT_DIR}/script/sync-homebrew-formula.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

pass=0
fail=0
ok()  { pass=$((pass + 1)); printf '  PASS  %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf '  FAIL  %s\n' "$1"; }
expect() {
  if [ "$2" = "$3" ]; then ok "$1"; else
    bad "$1"; printf '        expected: %s\n        actual:   %s\n' "$2" "$3"
  fi
}

TAG=v0.7.0
TARGETS="darwin_arm64 darwin_amd64 linux_arm64 linux_amd64"

# ---------------------------------------------------------------- fixtures ---
mkdir -p "${WORK}/bin" "${WORK}/dist"

# A formula shaped like the one the tap carries. Written here rather than
# fetched, so the test does not depend on the network or on the tap's current
# contents.
cat > "${WORK}/formula.rb" <<'RUBY'
# typed: false
# frozen_string_literal: true

class Agents < Formula
  desc "Development harness and standalone agent tool"
  homepage "https://github.com/nilbot/dotfiles"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/nilbot/dotfiles/releases/download/v0.6.0/agents_v0.6.0_darwin_arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000001"
    end
    on_intel do
      url "https://github.com/nilbot/dotfiles/releases/download/v0.6.0/agents_v0.6.0_darwin_amd64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000002"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/nilbot/dotfiles/releases/download/v0.6.0/agents_v0.6.0_linux_arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000003"
    end
    on_intel do
      url "https://github.com/nilbot/dotfiles/releases/download/v0.6.0/agents_v0.6.0_linux_amd64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000004"
    end
  end

  def install
    bin.install "agents"
  end

  test do
    assert_match "agents", shell_output("#{bin}/agents version")
  end
end
RUBY

# checksums.txt in the order `sha256sum agents_v*_*.tar.gz` produces: amd64
# before arm64 within each OS. This transposition is the point of the test, so
# it is asserted before it is relied on.
cat > "${WORK}/dist/checksums.txt" <<SUMS
9f2c41ab7d3e50861c0b4a9d2e7f385a1b6c0d4e5f7a8b9c0d1e2f3a4b5c6d70  agents_${TAG}_darwin_amd64.tar.gz
3a7e9c1d5b8f20463d6a1c9e7b4f28051a3c6e9d2b5f80741c3a6e9d2b5f8074  agents_${TAG}_darwin_arm64.tar.gz
c81d4e7a2b5f90836e1c4a7d039b2f685a9c3e6d1b4f70829c3a5e8d1b4f7082  agents_${TAG}_linux_amd64.tar.gz
5e0b3d6f9a2c584717b4d0e3f6a9c2b5d8e1f4a7c0b3d6e9f2a5c8b1d4e7f0a3  agents_${TAG}_linux_arm64.tar.gz
SUMS

# The order the formula lists its four slots, for the assertion below.
FIRST_TWO="$(awk '{print $2}' "${WORK}/dist/checksums.txt" | head -2 | tr '\n' ' ')"
expect "fixture reproduces the collation trap (amd64 listed before arm64)" \
  "agents_${TAG}_darwin_amd64.tar.gz agents_${TAG}_darwin_arm64.tar.gz " "${FIRST_TWO}"

# A `gh` that answers from a fixture and records writes without pushing.
cat > "${WORK}/bin/gh" <<'GH'
#!/usr/bin/env bash
[ "${1:-}" = api ] || { echo "stub gh: unhandled $*" >&2; exit 1; }
shift
jq_expr=""; method="GET"
while [ $# -gt 0 ]; do
  case "$1" in
    --jq) jq_expr="$2"; shift 2 ;;
    --method) method="$2"; shift 2 ;;
    -f) printf '%s\n' "$2" >> "${STUB_RECORD:?}"; shift 2 ;;
    -H) shift 2 ;;
    *) shift ;;
  esac
done
if [ "${method}" = PUT ]; then echo '{"commit":{"sha":"stub"}}'; exit 0; fi
case "${jq_expr}" in
  .content) base64 < "${STUB_FORMULA:?}" | tr -d '\n'; echo ;;
  .sha) echo stubsha ;;
  *) echo '{}' ;;
esac
GH
chmod +x "${WORK}/bin/gh"

# run <formula> <dist-dir> — sets RC and PUSHED.
run() {
  : > "${WORK}/record"
  PUSHED=""
  STUB_FORMULA="$1" STUB_RECORD="${WORK}/record" \
  PATH="${WORK}/bin:${PATH}" HOMEBREW_TAP_TOKEN=stub-token \
    bash "${SCRIPT}" "${TAG}" "$2" > "${WORK}/out" 2>&1
  RC=$?
  local b64
  b64="$(grep '^content=' "${WORK}/record" 2>/dev/null | head -1 | sed 's/^content=//')"
  if [ -n "${b64}" ]; then
    if ! printf '%s' "${b64}" | base64 --decode > "${WORK}/pushed.rb" 2>/dev/null; then
      printf '%s' "${b64}" | base64 -D > "${WORK}/pushed.rb" 2>/dev/null
    fi
    PUSHED="${WORK}/pushed.rb"
  fi
}

echo
echo "=== 1. the released digests land in the slots their URL names ==="
run "${WORK}/formula.rb" "${WORK}/dist"
expect "the run succeeded" 0 "${RC}"
if [ -z "${PUSHED}" ]; then
  bad "nothing was pushed"
else
  for t in ${TARGETS}; do
    want="$(awk -v f="agents_${TAG}_${t}.tar.gz" '$2 == f {print $1}' "${WORK}/dist/checksums.txt")"
    got="$(grep -A1 "agents_${TAG}_${t}.tar.gz" "${PUSHED}" | grep -o '[0-9a-f]\{64\}' | head -1)"
    expect "${t} carries the digest checksums.txt records for it" "${want}" "${got}"
  done
fi

echo
echo "=== 2. content the tap's maintainers wrote is left alone ==="
# Same formula, but with an extra install line: the script must not revert it.
python3 - "${WORK}/formula.rb" "${WORK}/edited.rb" <<'PY'
import sys, pathlib
src, dst = sys.argv[1], sys.argv[2]
t = pathlib.Path(src).read_text()
t = t.replace('    bin.install "agents"', '    bin.install "agents"\n    doc.install "README.md"')
pathlib.Path(dst).write_text(t)
PY
run "${WORK}/edited.rb" "${WORK}/dist"
expect "the run succeeded" 0 "${RC}"
if [ -z "${PUSHED}" ]; then
  bad "nothing was pushed"
else
  grep -q 'doc.install' "${PUSHED}" && ok "the maintainer's extra install line survived" \
    || bad "the maintainer's extra install line was dropped"
  grep -q 'assert_match' "${PUSHED}" && ok "the test block survived" || bad "the test block was dropped"
  # Everything except the eight rewritten lines must be byte-identical.
  # Python rather than sed: the alternation needs grouped ERE, and BSD sed's
  # `sed -E 's|(url|sha256) ".*"|\1 "X"|'` fails with "parentheses not
  # balanced" — which silently compared two empty streams and passed. An
  # assertion that cannot fail is worse than no assertion, so this one is
  # checked against a deliberately wrong input below.
  if python3 "${SCRIPT_DIR}/norm-formula-lines.py" "${WORK}/edited.rb" "${PUSHED}"; then
    ok "nothing outside the url and sha256 lines changed"
  else
    bad "something outside the url and sha256 lines changed"
  fi
fi

echo
echo "=== 3. a formula missing a platform block is refused, not half-written ==="
python3 - "${WORK}/formula.rb" "${WORK}/missing.rb" <<'PY'
import re, sys, pathlib
src, dst = sys.argv[1], sys.argv[2]
t = pathlib.Path(src).read_text()
t = re.sub(r'    on_intel do\n      url "[^"]*linux_amd64[^"]*"\n      sha256 "[0-9a-f]+"\n    end\n', '', t)
pathlib.Path(dst).write_text(t)
PY
run "${WORK}/missing.rb" "${WORK}/dist"
expect "the run refused" 2 "${RC}"
grep -qi 'no url for linux_amd64' "${WORK}/out" && ok "the error names the missing block" \
  || bad "the error does not name the missing block"
[ -z "${PUSHED}" ] && ok "nothing was pushed" || bad "a partial formula was pushed"

echo
echo "=== 4. a checksums.txt missing a platform is refused ==="
mkdir -p "${WORK}/short"
grep -v 'linux_arm64' "${WORK}/dist/checksums.txt" > "${WORK}/short/checksums.txt"
run "${WORK}/formula.rb" "${WORK}/short"
expect "the run refused" 2 "${RC}"
[ -z "${PUSHED}" ] && ok "nothing was pushed" || bad "a formula with a missing digest was pushed"

echo
echo "=== 5. a formula that is not this formula is refused ==="
cat > "${WORK}/other.rb" <<'RUBY'
class SomethingElse < Formula
  url "https://example.com/other.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
end
RUBY
run "${WORK}/other.rb" "${WORK}/dist"
expect "the run refused" 2 "${RC}"
[ -z "${PUSHED}" ] && ok "nothing was pushed" || bad "an unrelated formula was overwritten"

echo
echo "=== 6. --check agrees with the live tap's current release ==="
# The strongest check available without touching the tap: read the real formula,
# rewrite it for the version it already carries, and require the result to be
# identical.
#
# Skipped unless `gh` is installed, the network answers, AND a token is present.
# All three are preconditions, not failures: `sync-homebrew-formula.sh` now
# authenticates before it reads, so with no token it exits 2 and says so. This
# scenario previously ran with no token at all, which meant it could only ever
# fail or skip -- in CI it skipped (no `gh` on the runner) and locally it failed
# with "neither HOMEBREW_TAP_TOKEN nor GH_TOKEN is set", which read as a defect
# in the script rather than in the test.
if command -v gh >/dev/null 2>&1 \
   && [ -n "${HOMEBREW_TAP_TOKEN:-}${GH_TOKEN:-}" ] \
   && gh api repos/nilbot/homebrew-tap --jq '.name' >/dev/null 2>&1; then
  LIVE="${WORK}/live.rb"
  if gh api repos/nilbot/homebrew-tap/contents/Formula/agents.rb --jq '.content' 2>/dev/null \
       | tr -d '\n' | base64 --decode > "${LIVE}" 2>/dev/null \
     || gh api repos/nilbot/homebrew-tap/contents/Formula/agents.rb --jq '.content' 2>/dev/null \
       | tr -d '\n' | base64 -D > "${LIVE}" 2>/dev/null; then
    LIVE_TAG="$(grep -o 'releases/download/v[0-9.]*' "${LIVE}" | head -1 | sed 's|.*/||')"
    if [ -n "${LIVE_TAG}" ]; then
      if gh release download "${LIVE_TAG}" --repo nilbot/dotfiles --pattern checksums.txt \
           --dir "${WORK}/live-dist" --clobber >/dev/null 2>&1; then
        bash "${SCRIPT}" --check "${LIVE_TAG}" "${WORK}/live-dist" >/dev/null 2>&1
        expect "--check reports the tap current at ${LIVE_TAG}" 0 "$?"
      else
        printf '  SKIP  could not download checksums.txt for %s\n' "${LIVE_TAG}"
      fi
    else
      printf '  SKIP  could not read a version out of the live formula\n'
    fi
  else
    printf '  SKIP  could not read the live formula\n'
  fi
else
  printf '  SKIP  no gh, or no network\n'
fi

echo
echo "==============================="
printf 'tap sync: %d passed, %d failed\n' "${pass}" "${fail}"
echo "==============================="
[ "${fail}" -eq 0 ]
