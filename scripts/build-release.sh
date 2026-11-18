#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.1.0}"
BUILD_ID="${2:-$(git rev-parse --short HEAD 2>/dev/null || echo dev)}"
OUT_DIR="${3:-dist/release}"

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

TARGETS=(
  "darwin arm64 tar.gz"
  "darwin amd64 tar.gz"
  "linux arm64 tar.gz"
  "linux amd64 tar.gz"
  "windows arm64 zip"
  "windows amd64 zip"
)

LDFLAGS="-s -w -X main.version=${VERSION} -X main.buildID=${BUILD_ID}"

for entry in "${TARGETS[@]}"; do
  read -r goos goarch ext <<< "$entry"
  stage="$(mktemp -d)"
  bin="agy-statusline"
  if [[ "$goos" == "windows" ]]; then
    bin="agy-statusline.exe"
  fi

  echo "Building ${goos}/${goarch}..."
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "$LDFLAGS" -o "${stage}/${bin}" ./cmd/agy-statusline

  cp LICENSE README.md "$stage/"
  archive="agy-statusline_${VERSION}_${goos}_${goarch}.${ext}"

  if [[ "$ext" == "zip" ]]; then
    (cd "$stage" && zip -q -r "${OLDPWD}/${OUT_DIR}/${archive}" .)
  else
    COPYFILE_DISABLE=1 tar -C "$stage" -czf "${OUT_DIR}/${archive}" .
  fi
  rm -rf "$stage"
done

(
  cd "$OUT_DIR"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 agy-statusline_* > SHA256SUMS
  else
    sha256sum agy-statusline_* > SHA256SUMS
  fi
)

echo "Release artifacts ready in ${OUT_DIR}:"
ls -lh "$OUT_DIR"
