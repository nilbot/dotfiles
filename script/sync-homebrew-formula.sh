#!/usr/bin/env bash
# Point the Homebrew tap's formula at a release, without carrying a copy of it.
#
# The formula lives in nilbot/homebrew-tap and is the only copy that exists.
# This script does not render a formula; it edits the one the tap already has,
# so the tap stays the source of truth and this repository keeps no duplicate
# that can drift from it.
#
# Why the digests are matched by filename and never by line order. `checksums.txt`
# is produced by `sha256sum agents_<version>_*.tar.gz`, whose glob sorts by
# collation, so it lists darwin_amd64 BEFORE darwin_arm64 -- while the formula's
# four slots read darwin_arm64, darwin_amd64, linux_arm64, linux_amd64. A
# positional paste therefore swaps every Intel digest with its ARM sibling, and
# nothing in either repository notices: the formula stays valid Ruby and the
# tap's CI runs `brew test-bot --only-tap-syntax`, which checks syntax, not
# whether a digest belongs to the URL above it. The error would surface as a
# SHA256 mismatch in a user's `brew install`. Matching on the filename in the
# URL makes the order in checksums.txt irrelevant by construction.
#
# Usage:
#   sync-homebrew-formula.sh <version> [dist-dir]   # rewrite the tap's formula
#   sync-homebrew-formula.sh --check <version> [dist-dir]
#                                                   # assert the tap is current
#
# In --check mode nothing is written and the exit code is the whole result:
#   0  the tap's formula already points at this version with these digests
#   1  it does not (a release exists that the tap does not carry)
#   2  the check could not be completed
#
# Environment:
#   HOMEBREW_TAP_TOKEN  required to write (Contents: Read and Write on the tap)
#   GH_TOKEN            used instead when the tap token is absent
#   TAP_REPO            default nilbot/homebrew-tap
set -euo pipefail

readonly TAP_REPO="${TAP_REPO:-nilbot/homebrew-tap}"
readonly FORMULA_PATH="Formula/agents.rb"
readonly CHECK_EXIT_STALE=1
readonly CHECK_EXIT_ERROR=2

usage() {
  echo "Usage: $0 [--check] <VERSION> [DIST_DIR]" >&2
  exit "${CHECK_EXIT_ERROR}"
}

mode="write"
if [[ "${1:-}" == "--check" ]]; then
  mode="check"
  shift
fi

RAW_VERSION="${1:-}"
[[ -n "${RAW_VERSION}" ]] || usage
# The tag, `v` included. It is not cosmetic: the release assets are named
# `agents_v0.6.0_darwin_arm64.tar.gz`, so the `v` is part of the filename that
# keys every digest lookup and part of the download URL.
TAG="v${RAW_VERSION#v}"
VERSION="${TAG#v}"
[[ -n "${VERSION}" ]] || usage

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
DIST_DIR="${2:-${ROOT_DIR}/dist}"
CHECKSUMS_FILE="${DIST_DIR}/checksums.txt"

# The four targets, in the order the formula lists them. Each entry is
# "<os>_<arch>", which is both the formula's nesting hint to a human reader and
# the filename fragment the digest is looked up by.
readonly TARGETS=(darwin_arm64 darwin_amd64 linux_arm64 linux_amd64)

require_checksums() {
  if [[ ! -f "${CHECKSUMS_FILE}" ]]; then
    echo "Error: checksums file not found at ${CHECKSUMS_FILE}" >&2
    exit "${CHECK_EXIT_ERROR}"
  fi
}

# digest_for echoes the sha256 recorded for one target, selected by the archive
# filename rather than by its position in the file.
digest_for() {
  local target="$1" wanted="agents_${TAG}_${target}.tar.gz" sha
  sha="$(awk -v f="${wanted}" '$2 == f {print $1}' "${CHECKSUMS_FILE}" | head -n 1)"
  sha="$(echo "${sha}" | tr '[:upper:]' '[:lower:]')"
  if [[ ! "${sha}" =~ ^[0-9a-f]{64}$ ]]; then
    echo "Error: no valid sha256 for ${wanted} in ${CHECKSUMS_FILE}" >&2
    exit "${CHECK_EXIT_ERROR}"
  fi
  echo "${sha}"
}

fetch_formula() {
  local out="$1" raw
  raw="$(gh api "repos/${TAP_REPO}/contents/${FORMULA_PATH}" --jq '.content' 2>/dev/null)"
  if [[ -z "${raw}" ]]; then
    # A missing tap token is the common cause, and so is an unauthenticated rate
    # limit. Both are failures to check, not evidence that the tap is current.
    echo "Error: could not read ${TAP_REPO}/${FORMULA_PATH}" >&2
    exit "${CHECK_EXIT_ERROR}"
  fi
  # Both spellings of the decode flag, because macOS ships a base64 whose long
  # option is `-D` while GNU's is `--decode`. The old script carried this same
  # pair; without it the script works in CI and fails on the maintainer's Mac.
  if ! printf '%s' "${raw}" | tr -d '\n' | base64 --decode > "${out}" 2>/dev/null \
       && ! printf '%s' "${raw}" | tr -d '\n' | base64 -D > "${out}" 2>/dev/null; then
    echo "Error: could not decode ${TAP_REPO}/${FORMULA_PATH}" >&2
    exit "${CHECK_EXIT_ERROR}"
  fi
  if [[ ! -s "${out}" ]]; then
    echo "Error: ${TAP_REPO}/${FORMULA_PATH} read back empty" >&2
    exit "${CHECK_EXIT_ERROR}"
  fi
}

# render rewrites the url and sha256 of every block, leaving all other prose
# (desc, license, install, test) exactly as the tap's maintainers wrote it.
render() {
  local src="$1" dest="$2"
  local target os arch old_url new_url digest
  cp "${src}" "${dest}"
  for target in "${TARGETS[@]}"; do
    os="${target%%_*}"
    arch="${target##*_}"
    digest="$(digest_for "${target}")"
    new_url="https://github.com/nilbot/dotfiles/releases/download/${TAG}/agents_${TAG}_${target}.tar.gz"

    # Exactly one url line per target, matched on the filename it points at.
    if ! grep -q "agents_v[0-9][^/]*_${target}\.tar\.gz" "${dest}"; then
      echo "Error: ${FORMULA_PATH} has no url for ${target}; refusing to guess" >&2
      exit "${CHECK_EXIT_ERROR}"
    fi
    old_url="$(grep -o "https://[^\"]*agents_v[^/]*_${target}\.tar\.gz" "${dest}" | head -n 1)"
    # Arguments reach python through the environment, not argv, because argv
    # here would mean `python3 - "$dest" ... <<'PY'` and a heredoc on stdin
    # cannot then be read by a loop that is itself fed from a command
    # substitution. The environment keeps the two independent.
    FORMULA_PATH_ARG="${dest}" OLD_URL="${old_url}" NEW_URL="${new_url}" DIGEST="${digest}" \
      python3 - <<'PY'
import os, re, sys
path = os.environ["FORMULA_PATH_ARG"]
old_url = os.environ["OLD_URL"]
new_url = os.environ["NEW_URL"]
digest = os.environ["DIGEST"]
text = open(path).read()
# Replace the digest only on the line immediately following the url, so a
# digest belonging to another block cannot be touched.
pattern = re.compile(r'(' + re.escape(old_url) + r'"\n\s*sha256 ")[0-9a-f]{64}(")')
text, n = pattern.subn(lambda m: m.group(1) + digest + m.group(2), text)
if n != 1:
    sys.exit(f"expected exactly one sha256 line under {old_url}, found {n}")
open(path, "w").write(text.replace(old_url, new_url))
PY
  done
}

check_ts="$(mktemp)"; render_ts="$(mktemp)"
trap 'rm -f "${check_ts}" "${render_ts}"' EXIT

require_checksums
fetch_formula "${check_ts}"
render "${check_ts}" "${render_ts}"

if [[ "${mode}" == "check" ]]; then
  if diff -q "${check_ts}" "${render_ts}" >/dev/null; then
    echo "tap formula already points at v${VERSION} with the released digests"
    exit 0
  fi
  echo "tap formula is NOT current for v${VERSION}:" >&2
  diff -u "${check_ts}" "${render_ts}" >&2 || true
  exit "${CHECK_EXIT_STALE}"
fi

if diff -q "${check_ts}" "${render_ts}" >/dev/null; then
  echo "tap formula already points at v${VERSION}; nothing to push"
  exit 0
fi

TOKEN="${HOMEBREW_TAP_TOKEN:-${GH_TOKEN:-}}"
if [[ -z "${TOKEN}" ]]; then
  echo "Error: neither HOMEBREW_TAP_TOKEN nor GH_TOKEN is set" >&2
  exit "${CHECK_EXIT_ERROR}"
fi

file_sha="$(gh api "repos/${TAP_REPO}/contents/${FORMULA_PATH}" --jq '.sha' 2>/dev/null || true)"
content_b64="$(base64 < "${render_ts}" | tr -d '\n')"

# shellcheck disable=SC2086 # ${file_sha:+-f sha=...} must word-split when set.
GH_TOKEN="${TOKEN}" gh api \
  --method PUT \
  -H "Accept: application/vnd.github+json" \
  "repos/${TAP_REPO}/contents/${FORMULA_PATH}" \
  -f message="feat(agents): update formula to v${VERSION}" \
  -f content="${content_b64}" \
  ${file_sha:+-f sha="${file_sha}"} \
  --jq '.commit.sha' >/dev/null

echo "synced ${FORMULA_PATH} to v${VERSION} in ${TAP_REPO}"
