#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

RAW_VERSION="${1:-}"
if [[ -z "${RAW_VERSION}" ]]; then
  echo "Usage: $0 <VERSION> [DIST_DIR]" >&2
  exit 1
fi

VERSION="${RAW_VERSION#v}"
if [[ -z "${VERSION}" ]]; then
  echo "Error: Invalid VERSION '${RAW_VERSION}'" >&2
  exit 1
fi

DIST_DIR="${2:-${ROOT_DIR}/dist}"
CHECKSUMS_FILE="${DIST_DIR}/checksums.txt"
FORMULA_FILE="${ROOT_DIR}/Formula/agents.rb"
TAP_REPO="${TAP_REPO:-nilbot/homebrew-tap}"
TAP_TOKEN="${HOMEBREW_TAP_TOKEN:-${GH_TOKEN:-}}"

if [[ ! -f "${CHECKSUMS_FILE}" ]]; then
  echo "Error: Checksums file not found at ${CHECKSUMS_FILE}" >&2
  exit 1
fi

extract_sha() {
  local pattern="$1"
  local sha
  sha="$(grep -E "${pattern}" "${CHECKSUMS_FILE}" 2>/dev/null | awk '{print $1}' | head -n 1)"
  echo "${sha}" | tr '[:upper:]' '[:lower:]'
}

validate_sha() {
  local platform="$1"
  local sha="$2"
  if [[ ! "${sha}" =~ ^[0-9a-f]{64}$ ]]; then
    echo "Error: Invalid or missing SHA-256 digest for ${platform}: '${sha}'" >&2
    exit 1
  fi
}

DARWIN_ARM64_SHA="$(extract_sha 'agents_.*_darwin_arm64\.tar\.gz')"
DARWIN_AMD64_SHA="$(extract_sha 'agents_.*_darwin_amd64\.tar\.gz')"
LINUX_ARM64_SHA="$(extract_sha 'agents_.*_linux_arm64\.tar\.gz')"
LINUX_AMD64_SHA="$(extract_sha 'agents_.*_linux_amd64\.tar\.gz')"

validate_sha "darwin/arm64" "${DARWIN_ARM64_SHA}"
validate_sha "darwin/amd64" "${DARWIN_AMD64_SHA}"
validate_sha "linux/arm64" "${LINUX_ARM64_SHA}"
validate_sha "linux/amd64" "${LINUX_AMD64_SHA}"

mkdir -p "$(dirname "${FORMULA_FILE}")"

cat <<FORMULA_EOF > "${FORMULA_FILE}"
# typed: false
# frozen_string_literal: true

class Agents < Formula
  desc "Development harness and standalone agent tool"
  homepage "https://github.com/nilbot/dotfiles"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/nilbot/dotfiles/releases/download/v${VERSION}/agents_v${VERSION}_darwin_arm64.tar.gz"
      sha256 "${DARWIN_ARM64_SHA}"
    end
    on_intel do
      url "https://github.com/nilbot/dotfiles/releases/download/v${VERSION}/agents_v${VERSION}_darwin_amd64.tar.gz"
      sha256 "${DARWIN_AMD64_SHA}"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/nilbot/dotfiles/releases/download/v${VERSION}/agents_v${VERSION}_linux_arm64.tar.gz"
      sha256 "${LINUX_ARM64_SHA}"
    end
    on_intel do
      url "https://github.com/nilbot/dotfiles/releases/download/v${VERSION}/agents_v${VERSION}_linux_amd64.tar.gz"
      sha256 "${LINUX_AMD64_SHA}"
    end
  end

  def install
    bin.install "agents"
  end

  test do
    assert_match "agents", shell_output("#{bin}/agents version")
  end
end
FORMULA_EOF

echo "Generated ${FORMULA_FILE} for v${VERSION}"

if [[ -n "${TAP_TOKEN}" ]]; then
  echo "Pushing Formula/agents.rb to ${TAP_REPO} via GitHub Contents REST API..."
  FILE_SHA="$(GH_TOKEN="${TAP_TOKEN}" gh api "repos/${TAP_REPO}/contents/Formula/agents.rb" --jq '.sha' 2>/dev/null || echo "")"
  FILE_SHA="$(echo "${FILE_SHA}" | tr -d '\r\n')"

  CONTENT_B64="$( (base64 -i "${FORMULA_FILE}" 2>/dev/null || base64 -w 0 "${FORMULA_FILE}" 2>/dev/null || base64 "${FORMULA_FILE}") | tr -d '\r\n' )"

  API_ARGS=(
    --method PUT
    -H "Accept: application/vnd.github+json"
    "repos/${TAP_REPO}/contents/Formula/agents.rb"
    -f "message=feat(agents): update formula to v${VERSION}"
    -f "content=${CONTENT_B64}"
  )
  if [[ -n "${FILE_SHA}" ]]; then
    API_ARGS+=(-f "sha=${FILE_SHA}")
  fi

  GH_TOKEN="${TAP_TOKEN}" gh api "${API_ARGS[@]}"
  echo "Successfully synced Formula/agents.rb to ${TAP_REPO}"
else
  echo "[dry-run] HOMEBREW_TAP_TOKEN not set; generated Formula/agents.rb locally."
fi
