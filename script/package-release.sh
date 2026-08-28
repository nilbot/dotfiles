#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo "dev")}"
OUT_DIR="${2:-${ROOT_DIR}/dist}"
COMMIT="${COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo "none")}"
DATE="${DATE:-$(date -u +"%Y-%m-%dT%H:%M:%SZ")}"

mkdir -p "${OUT_DIR}"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

cp "${ROOT_DIR}/README.md" "${WORK_DIR}/README.md"

TARGETS=(
  "darwin/arm64"
  "linux/arm64"
  "linux/amd64"
)

echo "Packaging agents release ${VERSION} (commit: ${COMMIT}, date: ${DATE})..."

for target in "${TARGETS[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  echo "Building for ${os}/${arch}..."

  (
    cd "${ROOT_DIR}/agents"
    CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" go build \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o "${WORK_DIR}/agents" \
      .
  )

  archive_name="agents_${VERSION}_${os}_${arch}.tar.gz"
  tar -czf "${OUT_DIR}/${archive_name}" -C "${WORK_DIR}" agents README.md
  echo "Created ${OUT_DIR}/${archive_name}"
done

echo "Generating checksums..."
if command -v sha256sum >/dev/null 2>&1; then
  (cd "${OUT_DIR}" && rm -f checksums.txt && sha256sum agents_"${VERSION}"_*.tar.gz > checksums.txt)
elif command -v shasum >/dev/null 2>&1; then
  (cd "${OUT_DIR}" && rm -f checksums.txt && shasum -a 256 agents_"${VERSION}"_*.tar.gz > checksums.txt)
else
  echo "Error: neither sha256sum nor shasum found" >&2
  exit 1
fi

echo "Release packaging complete in ${OUT_DIR}"
