#!/usr/bin/env bash
# Builds the macOS app and its disk image from the binaries that `make release` wrote.
#
# Usage: scripts/macos/package.sh <version>
#
# Input:  dist/clanker-darwin-arm64 and dist/clanker-darwin-amd64
# Output: dist/SRC.dmg, with "Site Reliability Clanker.app" for both processor types.
#
# With CODESIGN_IDENTITY and NOTARY_PROFILE in the environment, the script signs the app
# and the image with that Developer ID identity, sends both to the Apple notary service
# with that keychain profile, staples the tickets and asks Gatekeeper to accept them.
# With neither, it signs ad hoc: the image is for a test on this machine only.
# docs/MACOS-RELEASE.md has the one-time setup.
set -euo pipefail

APP_NAME="Site Reliability Clanker"
VOLUME_NAME="Site Reliability Clanker"
MIN_MACOS="13.0"

if [ "$#" -ne 1 ]; then
  echo "usage: scripts/macos/package.sh <version>" >&2
  exit 2
fi
version="$1"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

# The command line tools of Apple cannot link the Swift code for Intel without a warning.
# The toolchain of Xcode can. This setting is for this script only.
xcode="/Applications/Xcode.app/Contents/Developer"
if [ -z "${DEVELOPER_DIR:-}" ] && [ -d "$xcode" ]; then
  export DEVELOPER_DIR="$xcode"
fi

work="build/macos"
app="$work/$APP_NAME.app"
dmg="dist/SRC.dmg"
identity="${CODESIGN_IDENTITY:-}"
profile="${NOTARY_PROFILE:-}"

fail() {
  echo "package: $*" >&2
  exit 1
}

# Removes a file or a folder of an earlier build. The files go to the Trash.
discard() {
  if [ -e "$1" ]; then
    trash "$1"
  fi
}

check_inputs() {
  local name
  for name in clanker-darwin-arm64 clanker-darwin-amd64; do
    [ -f "dist/$name" ] || fail "dist/$name is missing. Run 'make release VERSION=$version'."
  done
  for name in uv trash xcrun lipo iconutil codesign; do
    command -v "$name" >/dev/null || fail "the program '$name' is not installed."
  done
  if [ -n "$identity" ] && [ -z "$profile" ]; then
    fail "CODESIGN_IDENTITY is set and NOTARY_PROFILE is not. Set both, or neither."
  fi
  if [ -z "$identity" ] && [ -n "$profile" ]; then
    fail "NOTARY_PROFILE is set and CODESIGN_IDENTITY is not. Set both, or neither."
  fi
  if [ -n "$identity" ]; then
    security find-identity -v -p codesigning | grep -F -q "$identity" ||
      fail "the identity of CODESIGN_IDENTITY is not in the keychain. See" \
        "docs/MACOS-RELEASE.md."
    xcrun notarytool history --keychain-profile "$profile" >/dev/null 2>&1 ||
      fail "notarytool cannot use the profile of NOTARY_PROFILE. See docs/MACOS-RELEASE.md."
  fi
}

# The version in the bundle must be three numbers. A build that is not a release gets 0.0.0.
short_version() {
  if [[ "$version" =~ ^v?([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
    echo "${BASH_REMATCH[1]}"
  else
    echo "0.0.0"
  fi
}

build_app() {
  local arch
  discard "$app"
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
  lipo -create dist/clanker-darwin-arm64 dist/clanker-darwin-amd64 \
    -output "$app/Contents/MacOS/clanker"
  for arch in arm64 x86_64; do
    xcrun swiftc -O -swift-version 5 -target "$arch-apple-macos$MIN_MACOS" \
      -o "$work/shell-$arch" macos/App/*.swift
  done
  lipo -create "$work/shell-arm64" "$work/shell-x86_64" -output "$app/Contents/MacOS/SRC"
  sed "s/@SHORT_VERSION@/$(short_version)/g" macos/Info.plist >"$app/Contents/Info.plist"
  plutil -lint -s "$app/Contents/Info.plist"

  xcrun swiftc -O -swift-version 5 macos/art/icon.swift -o "$work/icon"
  discard "$work/AppIcon.iconset"
  "$work/icon" "$work/AppIcon.iconset"
  iconutil -c icns "$work/AppIcon.iconset" -o "$app/Contents/Resources/AppIcon.icns"
}

# Signs the program inside the app first, then the app. The hardened runtime is on, as
# the notary service requires. No entitlement is necessary.
sign_app() {
  local sign=(codesign --force --options runtime)
  if [ -n "$identity" ]; then
    sign+=(--timestamp --sign "$identity")
  else
    sign+=(--timestamp=none --sign -)
  fi
  "${sign[@]}" --identifier io.github.statik.clanker.cli "$app/Contents/MacOS/clanker"
  "${sign[@]}" "$app"
  codesign --verify --strict --verbose=2 "$app"
  if [ -n "$identity" ]; then
    codesign --display --verbose=2 "$app" 2>&1 | grep -q "Authority=Developer ID Application" ||
      fail "the app is not signed with a 'Developer ID Application' certificate. The" \
        "notary service accepts no other type. See docs/MACOS-RELEASE.md."
  fi
}

# Sends a file to the notary service and waits. Prints the log of the service on a refusal.
notarize() {
  local file="$1" reply id status
  echo "package: the notary service examines $file. This can take some minutes."
  reply="$(xcrun notarytool submit "$file" --keychain-profile "$profile" --wait \
    --output-format json)" || true
  id="$(printf '%s' "$reply" | plutil -extract id raw -o - -- - 2>/dev/null || true)"
  status="$(printf '%s' "$reply" | plutil -extract status raw -o - -- - 2>/dev/null || true)"
  if [ "$status" != "Accepted" ]; then
    echo "package: the notary service did not accept $file. Its answer:" >&2
    echo "$reply" >&2
    if [ -n "$id" ]; then
      xcrun notarytool log "$id" --keychain-profile "$profile" >&2 || true
    fi
    exit 1
  fi
}

notarize_app() {
  local zip="$work/app.zip"
  discard "$zip"
  ditto -c -k --keepParent "$app" "$zip"
  notarize "$zip"
  xcrun stapler staple "$app" ||
    fail "could not staple the ticket to the app. The service accepted the app, so the" \
      "ticket is possibly not published yet. Wait some minutes and run the script again."
}

build_dmg() {
  local venv="$work/venv" requirements="scripts/macos/dmg/requirements.txt"
  xcrun swiftc -O -swift-version 5 macos/art/dmg-background.swift -o "$work/dmg-background"
  "$work/dmg-background" "$work/background.tiff"
  if ! cmp -s "$requirements" "$venv/.requirements"; then
    discard "$venv"
    uv venv --quiet --python 3.13 "$venv"
    uv pip install --quiet --python "$venv/bin/python" --require-hashes -r "$requirements"
    cp "$requirements" "$venv/.requirements"
  fi
  discard "$dmg"
  "$venv/bin/dmgbuild" -s scripts/macos/dmg/settings.py -D app="$app" \
    -D background="$work/background.tiff" \
    -D volume_icon="$app/Contents/Resources/AppIcon.icns" "$VOLUME_NAME" "$dmg"
}

# Asks Gatekeeper what a person who downloads the image gets.
verify_release() {
  xcrun stapler validate "$app"
  xcrun stapler validate "$dmg"
  spctl --assess --type execute --verbose=2 "$app"
  spctl --assess --type open --context context:primary-signature --verbose=2 "$dmg"
}

check_inputs
mkdir -p "$work"
build_app
sign_app
if [ -n "$identity" ]; then
  notarize_app
fi
build_dmg
if [ -n "$identity" ]; then
  codesign --force --timestamp --sign "$identity" "$dmg"
  notarize "$dmg"
  xcrun stapler staple "$dmg" || fail "could not staple the ticket to $dmg. Run the script again."
  verify_release
  echo "package: wrote $dmg, signed and notarized."
else
  echo "package: wrote $dmg with an ad hoc signature and no notarization."
  echo "package: this image is NOT for distribution. Gatekeeper refuses it on other Macs."
fi
