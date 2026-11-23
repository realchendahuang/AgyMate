#!/usr/bin/env bash
set -euo pipefail

# Source installer for developers cloning the AgyMate repository.
# Compiles a Pure Go (CGO_ENABLED=0) binary, installs it to ~/.local/bin/agymate,
# and delegates all setup/migration/plugin registration to `agymate setup`.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-0.4.0}"
GIT_COMMIT="$(git -C "${ROOT_DIR}" describe --tags --always --dirty 2>/dev/null || git -C "${ROOT_DIR}" rev-parse --short HEAD 2>/dev/null || echo dev)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-s -w -X github.com/realchendahuang/AgyMate/internal/version.Version=${VERSION} -X github.com/realchendahuang/AgyMate/internal/version.Commit=${GIT_COMMIT} -X github.com/realchendahuang/AgyMate/internal/version.BuildTime=${BUILD_TIME}"
PREFIX_BIN="${AGYMATE_PREFIX_BIN:-${HOME}/.local/bin}"
INSTALL_BIN="${PREFIX_BIN}/agymate"
MAGPIE_PLUGIN_DEV="${MAGPIE_PLUGIN_DEV:-0}"

cd "${ROOT_DIR}"

if [[ ! -f "internal/mcpapp/dist/app.html" ]]; then
  echo "==> [0/2] 构建内置 MCP App UI (internal/mcpapp/dist/app.html)"
  make build-mcp-app
fi

echo "==> [1/2] 编译并安装 AgyMate (${VERSION}+${GIT_COMMIT}, Pure Go)"
mkdir -p bin "${PREFIX_BIN}"
CGO_ENABLED=0 go build -trimpath -ldflags "${LDFLAGS}" -o "bin/agymate" ./cmd/agymate

TMP_CLI="${PREFIX_BIN}/.agymate.new.$$"
cp "bin/agymate" "${TMP_CLI}"
chmod 755 "${TMP_CLI}"
if [[ "$(uname -s)" == "Darwin" ]]; then
  codesign --force --sign - "${TMP_CLI}" >/dev/null 2>&1 || true
fi
mv -f "${TMP_CLI}" "${INSTALL_BIN}"
echo "    已安装二进制: ${INSTALL_BIN}"

echo "==> [2/2] 执行自包含部署 (agymate setup)"
SETUP_ARGS=(setup --bin "${INSTALL_BIN}")
if [[ "${MAGPIE_PLUGIN_DEV}" == "1" ]]; then
  SETUP_ARGS+=(--magpie-dev-dir "${ROOT_DIR}/magpie-plugin")
fi
if [[ "$#" -gt 0 ]]; then
  SETUP_ARGS+=("$@")
fi
"${INSTALL_BIN}" "${SETUP_ARGS[@]}"
