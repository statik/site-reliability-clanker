#!/usr/bin/env bash
# Renders the app icon from the icon of the website (site/img/icon.svg).
# iOS puts its own mask on an app icon, so the picture is a full square in the ground colour
# of the icon, without the rounded corners of the SVG, and the figure is at 80% of its size
# so that the mask does not cut the wands.
# Needs rsvg-convert (brew install librsvg) and magick (brew install imagemagick).
set -euo pipefail

cd "$(dirname "$0")/.."
SOURCE="../site/img/icon.svg"
OUT="App/Assets.xcassets/AppIcon.appiconset/AppIcon.png"
GROUND="#1B354A"

for tool in rsvg-convert magick; do
  if ! command -v "$tool" >/dev/null; then
    echo "$tool is not installed. Install librsvg and imagemagick with brew." >&2
    exit 1
  fi
done
if ! grep -q "<rect width=\"64\" height=\"64\" rx=\"10\" fill=\"$GROUND\"/>" "$SOURCE"; then
  echo "$SOURCE does not have the ground rectangle that this script expects." >&2
  exit 1
fi

WORK="$(mktemp -d)"
trap 'rm -r "$WORK"' EXIT

{
  echo '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">'
  echo "<rect width=\"64\" height=\"64\" fill=\"$GROUND\"/>"
  echo '<g transform="translate(32 32.6) scale(0.8) translate(-32 -31.4)">'
  grep -v -e '<svg' -e '</svg>' -e '<rect ' "$SOURCE"
  echo '</g></svg>'
} >"$WORK/icon.svg"

rsvg-convert --width 1024 --height 1024 "$WORK/icon.svg" --output "$WORK/icon.png"
magick "$WORK/icon.png" -background "$GROUND" -alpha remove -alpha off "$OUT"
echo "$OUT"
