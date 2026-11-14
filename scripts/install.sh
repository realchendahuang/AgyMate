#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-0.1.0}"
LDFLAGS="-s -w -X github.com/realchendahuang/AgyMate/internal/version.Version=${VERSION}"
PREFIX_BIN="${HOME}/.local/bin"
APP_NAME="AgyMate.app"
APP_DEST="/Applications/${APP_NAME}"
BUNDLE_ID="com.realchendahuang.agymate"

echo "==> Building AgyMate CLI & Desktop (${VERSION})"
cd "${ROOT_DIR}"
mkdir -p bin "${PREFIX_BIN}"

CGO_ENABLED=1 go build -trimpath -ldflags "${LDFLAGS}" -o "bin/agymate" ./cmd/agymate
install -m 755 "bin/agymate" "${PREFIX_BIN}/agymate"
codesign --force --sign - "${PREFIX_BIN}/agymate" >/dev/null 2>&1 || true
echo "    Installed CLI: ${PREFIX_BIN}/agymate"

CGO_ENABLED=1 \
CGO_CFLAGS="-mmacosx-version-min=12.0" \
CGO_LDFLAGS="-mmacosx-version-min=12.0" \
MACOSX_DEPLOYMENT_TARGET=12.0 \
go build -trimpath -ldflags "${LDFLAGS}" -o "bin/agymate-desktop" ./cmd/agymate-desktop

STAGE_DIR="$(mktemp -d)"
trap 'rm -rf "${STAGE_DIR}"' EXIT
BUNDLE_STAGE="${STAGE_DIR}/${APP_NAME}"
mkdir -p "${BUNDLE_STAGE}/Contents/MacOS" "${BUNDLE_STAGE}/Contents/Resources"

install -m 755 "bin/agymate-desktop" "${BUNDLE_STAGE}/Contents/MacOS/agymate-desktop"

if [[ -f "cmd/agymate-desktop/icon.png" ]] && command -v iconutil >/dev/null 2>&1 && command -v sips >/dev/null 2>&1; then
  ICONSET="${STAGE_DIR}/icon.iconset"
  mkdir -p "${ICONSET}"
  for sz in 16 32 128 256 512; do
    sips -z "${sz}" "${sz}" "cmd/agymate-desktop/icon.png" --out "${ICONSET}/icon_${sz}x${sz}.png" >/dev/null
    sz2=$((sz * 2))
    if [[ "${sz2}" -le 512 ]]; then
      sips -z "${sz2}" "${sz2}" "cmd/agymate-desktop/icon.png" --out "${ICONSET}/icon_${sz}x${sz}@2x.png" >/dev/null
    else
      cp "cmd/agymate-desktop/icon.png" "${ICONSET}/icon_${sz}x${sz}@2x.png"
    fi
  done
  iconutil -c icns "${ICONSET}" -o "${BUNDLE_STAGE}/Contents/Resources/icon.icns"
fi

cat > "${BUNDLE_STAGE}/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>zh_CN</string>
  <key>CFBundleDisplayName</key>
  <string>AgyMate</string>
  <key>CFBundleExecutable</key>
  <string>agymate-desktop</string>
  <key>CFBundleIconFile</key>
  <string>icon.icns</string>
  <key>CFBundleIdentifier</key>
  <string>${BUNDLE_ID}</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>AgyMate</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>${VERSION}</string>
  <key>CFBundleVersion</key>
  <string>${VERSION}</string>
  <key>LSMinimumSystemVersion</key>
  <string>12.0</string>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
EOF

echo "==> Packaging & Signing ${APP_DEST}"
pkill -x agymate-desktop >/dev/null 2>&1 || true
sleep 0.3
rm -rf "${APP_DEST}"
ditto "${BUNDLE_STAGE}" "${APP_DEST}"
xattr -cr "${APP_DEST}"
codesign --force --deep --sign - "${APP_DEST}"
codesign --verify --deep --strict "${APP_DEST}"

echo "==> Done: ${APP_DEST} & ${PREFIX_BIN}/agymate"
