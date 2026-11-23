#!/usr/bin/env sh
# AgyMate zero-dependency prebuilt binary installer for macOS & Linux.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/realchendahuang/AgyMate/main/scripts/get.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/realchendahuang/AgyMate/main/scripts/get.sh | sh -s -- --minimal

set -eu

REPO="${AGYMATE_REPO:-realchendahuang/AgyMate}"
VERSION="${AGYMATE_VERSION:-latest}"
PREFIX_BIN="${AGYMATE_PREFIX_BIN:-${HOME}/.local/bin}"
SKIP_SETUP=0
SETUP_ARGS=""

for arg in "$@"; do
  case "$arg" in
    --no-setup) SKIP_SETUP=1 ;;
    *) SETUP_ARGS="${SETUP_ARGS} ${arg}" ;;
  esac
done

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command '$1' not found" >&2
    exit 1
  fi
}

need_cmd curl
need_cmd tar

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  darwin) OS="darwin" ;;
  linux)  OS="linux" ;;
  *)
    echo "error: unsupported OS '${OS}'. Download prebuilt binaries from https://github.com/${REPO}/releases" >&2
    exit 1
    ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "error: unsupported architecture '${ARCH}'" >&2
    exit 1
    ;;
esac

if [ "$VERSION" = "latest" ]; then
  API_URL="https://api.github.com/repos/${REPO}/releases/latest"
  VERSION="$(curl -fsSL "$API_URL" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
  if [ -z "$VERSION" ]; then
    echo "error: failed to resolve latest release tag from ${API_URL}" >&2
    exit 1
  fi
fi

VER_NUM="${VERSION#v}"
ARCHIVE="agymate_${VER_NUM}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "==> Downloading AgyMate ${VERSION} (${OS}/${ARCH})..."
curl -fsSL "${BASE_URL}/${ARCHIVE}" -o "${TMP_DIR}/${ARCHIVE}"
curl -fsSL "${BASE_URL}/checksums.txt" -o "${TMP_DIR}/checksums.txt"

echo "==> Verifying SHA256 checksum..."
EXPECTED_SHA="$(awk -v f="$ARCHIVE" '$2 == f {print $1}' "${TMP_DIR}/checksums.txt")"
if [ -z "$EXPECTED_SHA" ]; then
  echo "error: archive '${ARCHIVE}' not found in checksums.txt" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL_SHA="$(sha256sum "${TMP_DIR}/${ARCHIVE}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL_SHA="$(shasum -a 256 "${TMP_DIR}/${ARCHIVE}" | awk '{print $1}')"
else
  echo "error: neither sha256sum nor shasum found" >&2
  exit 1
fi
if [ "$EXPECTED_SHA" != "$ACTUAL_SHA" ]; then
  echo "error: checksum mismatch! expected=${EXPECTED_SHA} actual=${ACTUAL_SHA}" >&2
  exit 1
fi

tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "${TMP_DIR}"
mkdir -p "${PREFIX_BIN}"
TMP_BIN="${PREFIX_BIN}/.agymate.new.$$"
cp "${TMP_DIR}/agymate" "${TMP_BIN}"
chmod 755 "${TMP_BIN}"
if [ "$OS" = "darwin" ] && command -v codesign >/dev/null 2>&1; then
  codesign --force --sign - "${TMP_BIN}" >/dev/null 2>&1 || true
fi
mv -f "${TMP_BIN}" "${PREFIX_BIN}/agymate"
echo "    Installed: ${PREFIX_BIN}/agymate"

case ":${PATH}:" in
  *":${PREFIX_BIN}:"*) ;;
  *)
    echo ""
    echo "NOTE: ${PREFIX_BIN} is not in your PATH. Add it to your shell profile:"
    echo "  export PATH=\"${PREFIX_BIN}:\$PATH\""
    echo ""
    ;;
esac

if [ "$SKIP_SETUP" -eq 0 ]; then
  # shellcheck disable=SC2086
  "${PREFIX_BIN}/agymate" setup --bin "${PREFIX_BIN}/agymate" ${SETUP_ARGS}
else
  echo "Skipped setup (--no-setup). Run '${PREFIX_BIN}/agymate setup' whenever you are ready."
fi
