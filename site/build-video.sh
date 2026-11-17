#!/usr/bin/env bash
# Build the explainer video: out/explainer.mp4 (1080p, H.264, captions burned in) and
# out/explainer.srt (the same captions as a sidecar file).
#
#   ./build-video.sh
#
# Everything is made again from narration.md, data/month.json, the pages of this site and
# video/title.py. The steps are small Node scripts in video/; this script runs them in
# order and checks the result.
#
#   BLENDER=/path/to/Blender   use another Blender binary
#   NARRATION_VOICE=Daniel     use another voice of the macOS `say` command
set -euo pipefail

cd "$(dirname "$0")"

BLENDER="${BLENDER:-/Applications/Blender.app/Contents/MacOS/Blender}"
WORK="out/build"
CACHE="out/cache"
TITLE_FRAMES=240
FPS=30
MIN_SECONDS=120
MAX_SECONDS=180

for tool in node pnpm ffmpeg ffprobe say shasum; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "build-video: '$tool' is not installed or not on the PATH." >&2
    exit 1
  fi
done

if [ ! -d node_modules/playwright ]; then
  echo "build-video: installing the pinned dependencies"
  pnpm install --frozen-lockfile
fi
pnpm exec playwright install chromium >/dev/null

mkdir -p "$WORK" "$CACHE"
# Old recordings and segments must not leak into this build. The title cache stays.
find "$WORK" -maxdepth 1 -type f \( -name '*.webm' -o -name '*.mp4' -o -name '*.png' \) -delete

echo "build-video: 1/5 the pages, from the data"
node build.mjs

echo "build-video: 2/5 narration, captions and music"
node video/narrate.mjs
node video/music.mjs

echo "build-video: 3/5 the 3D title sequence"
if [ -x "$BLENDER" ]; then
  title_hash="$(shasum -a 256 video/title.py | cut -c1-16)"
  title_cache="$CACHE/title-$title_hash-$TITLE_FRAMES.mp4"
  if [ ! -f "$title_cache" ]; then
    frames_dir="$PWD/$WORK/title-frames"
    mkdir -p "$frames_dir"
    find "$frames_dir" -type f -name '*.png' -delete
    echo "build-video: rendering $TITLE_FRAMES frames with Blender (a few minutes, then cached)"
    "$BLENDER" -b -P video/title.py -- --out "$frames_dir" --frames "$TITLE_FRAMES" --fps "$FPS" \
      >"$WORK/blender.log" 2>&1 || {
      echo "build-video: Blender failed. The end of $WORK/blender.log:" >&2
      tail -n 20 "$WORK/blender.log" >&2
      exit 1
    }
    ffmpeg -v error -y -framerate "$FPS" -i "$frames_dir/frame-%04d.png" \
      -c:v libx264 -preset medium -crf 16 -pix_fmt yuv420p "$title_cache"
    find "$frames_dir" -type f -name '*.png' -delete
  else
    echo "build-video: the title is in the cache ($title_cache)"
  fi
  cp "$title_cache" "$WORK/title.mp4"
else
  echo "build-video: SKIP the 3D title. Blender is not at $BLENDER (set BLENDER=...)."
  echo "build-video: the title segment shows its fallback from narration.md."
fi

echo "build-video: 4/5 recording the pictures"
node video/record.mjs

echo "build-video: 5/5 assembling"
node video/assemble.mjs

output="out/explainer.mp4"
duration="$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$output")"
streams="$(ffprobe -v error -show_entries stream=codec_type,codec_name,width,height -of csv=p=0 "$output" | tr '\n' ' ')"
echo "build-video: $output is ${duration} s; streams: $streams"

case "$streams" in
*video*audio* | *audio*video*) ;;
*)
  echo "build-video: the result must have a video stream and an audio stream." >&2
  exit 1
  ;;
esac
case "$streams" in
*h264,video,1920,1080*) ;;
*)
  echo "build-video: the video stream is not H.264 at 1920x1080." >&2
  exit 1
  ;;
esac
whole="${duration%.*}"
if [ "$whole" -lt "$MIN_SECONDS" ] || [ "$whole" -gt "$MAX_SECONDS" ]; then
  echo "build-video: the video is ${duration} s; it must be $MIN_SECONDS to $MAX_SECONDS s. Edit narration.md." >&2
  exit 1
fi
# The intermediate files are large. The cached title and the final files stay.
find "$WORK" -maxdepth 1 -type f \( -name '*.webm' -o -name 'segment-*.mp4' -o -name 'pictures.mp4' \
  -o -name 'title.mp4' -o -name '*.wav' -o -name '*.png' \) -delete
find "$WORK/speech" -type f -delete
echo "build-video: done. Captions: out/explainer.srt"
