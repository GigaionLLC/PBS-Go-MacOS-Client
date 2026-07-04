#!/usr/bin/env bash
# Build a distributable "Proxmox Backup.app" with the pbmac CLI bundled inside.
# Ad-hoc signed only (no Apple Developer account / notarization required) — the
# app runs on Apple Silicon; on first launch the user right-clicks -> Open.
#
# Usage:  bash macos/build-app.sh   (needs: Xcode, Go, xcodegen)
set -euo pipefail
cd "$(dirname "$0")"

command -v go        >/dev/null || { echo "Go not found (install from go.dev)"; exit 1; }
command -v xcodegen  >/dev/null || { echo "xcodegen not found (brew install xcodegen)"; exit 1; }
command -v xcodebuild >/dev/null || { echo "Xcode command-line tools not found"; exit 1; }

echo "==> generating Xcode project"
xcodegen generate

echo "==> building (Release, arm64)"
rm -rf build dist
xcodebuild \
  -project PBMac.xcodeproj \
  -target PBMac \
  -configuration Release \
  -derivedDataPath build \
  CODE_SIGN_IDENTITY="-" CODE_SIGNING_REQUIRED=NO CODE_SIGNING_ALLOWED=YES \
  build

APP=$(find build/Build/Products/Release -maxdepth 1 -name '*.app' | head -1)
[ -n "$APP" ] || { echo "no .app produced"; exit 1; }

# Re-sign the whole bundle (including the embedded pbmac) ad-hoc so the seal is
# consistent and it launches without notarization.
echo "==> ad-hoc signing $APP"
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP" && echo "signature ok"

mkdir -p dist
cp -R "$APP" dist/
NAME=$(basename "$APP")
echo
echo "built: macos/dist/$NAME"
echo "embedded CLI: dist/$NAME/Contents/Resources/pbmac"
echo
echo "First launch (unsigned/un-notarized): right-click the app in Finder -> Open,"
echo "or run:  xattr -dr com.apple.quarantine 'dist/$NAME'"
