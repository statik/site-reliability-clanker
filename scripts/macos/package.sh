#!/usr/bin/env bash
# Builds the macOS app and its disk image from the binaries that `make release` wrote.
#
# Usage: scripts/macos/package.sh <version>
#
# Input:  dist/clanker-darwin-arm64 and dist/clanker-darwin-amd64
# Output: dist/SRC.dmg, with "Site Reliability Clanker.app" for both processor types.
#
# With SIGNING_TEAM (a team ID) and NOTARY_PROFILE (a keychain profile of notarytool) in
# the environment, Xcode signs the app with the Developer ID certificate that Apple keeps
# for the team, and the script sends the app and the image to the Apple notary service,
# staples the tickets and asks Gatekeeper to accept the app.
# With SIGNING_TEAM only, the app is signed and nothing is notarized: a test of the signing.
# With neither, the signature is ad hoc: the image is for a test on this machine only.
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
team="${SIGNING_TEAM:-}"
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
  if [ -z "$team" ] && [ -n "$profile" ]; then
    fail "NOTARY_PROFILE is set and SIGNING_TEAM is not. The notary service accepts only" \
      "an app with a Developer ID signature. Set SIGNING_TEAM too."
  fi
  if [ -n "$team" ] && [ ! -d "$xcode" ]; then
    fail "Xcode is not at /Applications/Xcode.app. Xcode does the Developer ID signature."
  fi
  if [ -n "$profile" ]; then
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

# Signs the program inside the app first, then the app, ad hoc and with the hardened
# runtime. For a release, Xcode replaces these signatures and keeps the runtime setting.
sign_ad_hoc() {
  local sign=(codesign --force --options runtime --timestamp=none --sign -)
  "${sign[@]}" --identifier io.github.statik.clanker.cli "$app/Contents/MacOS/clanker"
  "${sign[@]}" "$app"
  codesign --verify --strict --verbose=2 "$app"
}

# Puts the app into an Xcode archive and lets Xcode export it with the Developer ID
# method. Xcode asks Apple to sign with the certificate that Apple keeps for the team, so
# no private key is on this machine. The account in Xcode must have that right.
sign_with_xcode() {
  local archive="$work/SRC.xcarchive" options="$work/ExportOptions.plist"
  local export_dir="$work/export" short
  short="$(short_version)"
  discard "$archive"
  discard "$export_dir"
  mkdir -p "$archive/Products/Applications"
  ditto "$app" "$archive/Products/Applications/$APP_NAME.app"
  cat >"$archive/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
  <key>ApplicationProperties</key>
  <dict>
    <key>ApplicationPath</key><string>Applications/$APP_NAME.app</string>
    <key>CFBundleIdentifier</key><string>io.github.statik.clanker</string>
    <key>CFBundleShortVersionString</key><string>$short</string>
    <key>CFBundleVersion</key><string>$short</string>
    <key>SigningIdentity</key><string>-</string>
    <key>Team</key><string>$team</string>
  </dict>
  <key>ArchiveVersion</key><integer>2</integer>
  <key>CreationDate</key><date>$(date -u +%Y-%m-%dT%H:%M:%SZ)</date>
  <key>Name</key><string>SRC</string>
  <key>SchemeName</key><string>SRC</string>
</dict>
</plist>
PLIST
  cat >"$options" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
  <key>method</key><string>developer-id</string>
  <key>signingStyle</key><string>automatic</string>
  <key>teamID</key><string>$team</string>
</dict>
</plist>
PLIST
  if ! xcodebuild -exportArchive -archivePath "$archive" -exportPath "$export_dir" \
    -exportOptionsPlist "$options" -allowProvisioningUpdates >"$work/export.log" 2>&1; then
    tail -20 "$work/export.log" >&2
    fail "Xcode did not sign the app. Sign in to Xcode with an account that can use the" \
      "Developer ID certificate of the team. See docs/MACOS-RELEASE.md and $work/export.log."
  fi
  discard "$app"
  ditto "$export_dir/$APP_NAME.app" "$app"
  check_signature "$app"
  check_signature "$app/Contents/MacOS/clanker"
  codesign --verify --deep --strict "$app"
}

# Fails when a signed file is not what the notary service accepts.
check_signature() {
  local facts want
  facts="$(codesign --display --verbose=4 "$1" 2>&1)"
  for want in "Authority=Developer ID Application" "TeamIdentifier=$team" "Timestamp=" \
    "(runtime)"; do
    grep -F -q "$want" <<<"$facts" ||
      fail "the signature of $1 does not have '$want'."
  done
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

# Checks the tickets, and asks Gatekeeper about the app as a person gets it from the image.
# The image has no signature of its own: the key of the certificate is not on this
# machine. The ticket of the notary service is what Gatekeeper reads for the image.
verify_release() {
  local mount="$work/mount" copy="$work/from-image"
  xcrun stapler validate "$app"
  xcrun stapler validate "$dmg"
  discard "$copy"
  mkdir -p "$mount" "$copy"
  hdiutil attach -quiet -nobrowse -readonly -mountpoint "$mount" "$dmg"
  ditto "$mount/$APP_NAME.app" "$copy/$APP_NAME.app"
  hdiutil detach -quiet "$mount"
  spctl --assess --type execute --verbose=2 "$copy/$APP_NAME.app"
}

check_inputs
mkdir -p "$work"
build_app
sign_ad_hoc
if [ -n "$team" ]; then
  sign_with_xcode
fi
if [ -n "$profile" ]; then
  notarize_app
fi
build_dmg
if [ -n "$profile" ]; then
  notarize "$dmg"
  xcrun stapler staple "$dmg" || fail "could not staple the ticket to $dmg. Run the script again."
  verify_release
  echo "package: wrote $dmg, signed and notarized."
elif [ -n "$team" ]; then
  echo "package: wrote $dmg. The app has a Developer ID signature and no notarization."
  echo "package: this image is NOT for distribution. Set NOTARY_PROFILE for a release."
else
  echo "package: wrote $dmg with an ad hoc signature and no notarization."
  echo "package: this image is NOT for distribution. Gatekeeper refuses it on other Macs."
fi
