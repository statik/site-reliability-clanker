#!/usr/bin/env bash
# Generates the Xcode project, builds the app for the iOS Simulator and runs the unit tests.
set -euo pipefail

export DEVELOPER_DIR="${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}"
SIMULATOR="${SIMULATOR:-iPhone 17 Pro}"

cd "$(dirname "$0")"

if ! command -v xcodegen >/dev/null; then
  echo "xcodegen is not installed. Install it with: brew install xcodegen" >&2
  exit 1
fi

xcodegen generate --quiet

DERIVED="$PWD/.build"
PRODUCTS="$DERIVED/Build/Products/Debug-iphonesimulator"
APP="$PRODUCTS/SRC.app"

xcodebuild \
  -project SRC.xcodeproj \
  -scheme SRC \
  -configuration Debug \
  -destination "platform=iOS Simulator,name=${SIMULATOR}" \
  -derivedDataPath "$DERIVED" \
  -quiet \
  build-for-testing

# Run the test bundle in the simulator with the xctest agent of Xcode. "xcodebuild test" needs
# the first-launch packages of Xcode, which only an administrator can install; without them
# it waits for a test runner that never connects. The agent has no such requirement. The
# test bundle links the code of the app, which a Debug build keeps in a library in the app.
xcrun simctl bootstatus "$SIMULATOR" -b >/dev/null
SIMCTL_CHILD_DYLD_LIBRARY_PATH="$APP" xcrun simctl spawn "$SIMULATOR" \
  "$DEVELOPER_DIR/Platforms/iPhoneSimulator.platform/Developer/Library/Xcode/Agents/xctest" \
  "$APP/PlugIns/SRCTests.xctest"

echo "Build and tests passed. App: ${APP#"$PWD"/}"
