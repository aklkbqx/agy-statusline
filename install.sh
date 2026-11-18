#!/usr/bin/env sh
set -eu

REPO="aklkbqx/agy-statusline"
INSTALL_DIR="${AGY_STATUSLINE_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${AGY_STATUSLINE_VERSION:-}"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Error: required command '$1' not found" >&2
    exit 1
  fi
}

need_cmd curl
need_cmd tar

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$OS" in
  darwin|linux) ;;
  *) echo "Error: unsupported OS '$OS'" >&2; exit 1 ;;
esac
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "Error: unsupported architecture '$ARCH'" >&2; exit 1 ;;
esac

if [ -z "$VERSION" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' | head -n 1)"
fi
if [ -z "$VERSION" ]; then
  echo "Error: could not resolve latest release version" >&2
  exit 1
fi

ARCHIVE="agy-statusline_${VERSION}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/v${VERSION}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Downloading agy-statusline v${VERSION} (${OS}/${ARCH})..."
curl -fsSL --proto '=https' --tlsv1.2 "${BASE_URL}/${ARCHIVE}" -o "${TMP_DIR}/${ARCHIVE}"
curl -fsSL --proto '=https' --tlsv1.2 "${BASE_URL}/SHA256SUMS" -o "${TMP_DIR}/SHA256SUMS"

EXPECTED="$(awk -v f="$ARCHIVE" '$2 == f {print $1}' "${TMP_DIR}/SHA256SUMS")"
if [ -z "$EXPECTED" ]; then
  echo "Error: checksum entry for ${ARCHIVE} missing in SHA256SUMS" >&2
  exit 1
fi

if command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "${TMP_DIR}/${ARCHIVE}" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "${TMP_DIR}/${ARCHIVE}" | awk '{print $1}')"
else
  echo "Error: neither shasum nor sha256sum found for SHA-256 verification" >&2
  exit 1
fi

if [ "$ACTUAL" != "$EXPECTED" ]; then
  echo "Error: checksum mismatch (expected ${EXPECTED}, got ${ACTUAL})" >&2
  exit 1
fi

tar -C "$TMP_DIR" -xzf "${TMP_DIR}/${ARCHIVE}"
mkdir -p "$INSTALL_DIR"
install -m 755 "${TMP_DIR}/agy-statusline" "${INSTALL_DIR}/agy-statusline"

echo "✓ Installed ${INSTALL_DIR}/agy-statusline"
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) echo "Add ${INSTALL_DIR} to your PATH, then run:" ;;
esac
echo "  agy-statusline install"
