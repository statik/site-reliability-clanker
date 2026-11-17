#!/usr/bin/env bash
# Boots a simulator, installs the app that build.sh made and launches it.
# Arguments after the script name go to the app, for example:
#   ./run.sh -initialScreen approvals -workItem WI-0020 -debugUser alice
set -euo pipefail

export DEVELOPER_DIR="${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}"
SIMULATOR="${SIMULATOR:-iPhone 17 Pro}"
BUNDLE_ID="dev.clanker.src"

cd "$(dirname "$0")"
APP=".build/Build/Products/Debug-iphonesimulator/SRC.app"

if [[ ! -d "$APP" ]]; then
  echo "The app is not built. Run ./build.sh first." >&2
  exit 1
fi

xcrun simctl bootstatus "$SIMULATOR" -b >/dev/null
xcrun simctl install "$SIMULATOR" "$APP"
xcrun simctl terminate "$SIMULATOR" "$BUNDLE_ID" >/dev/null 2>&1 || true
xcrun simctl launch "$SIMULATOR" "$BUNDLE_ID" "$@"

if [[ "${OPEN_SIMULATOR:-1}" == "1" ]]; then
  open -a Simulator
fi
