#!/usr/bin/env bash
# Build the explainer video: out/explainer.mp4 (1080p, H.264, captions burned in) and
# out/explainer.srt (the same captions as a sidecar file).
#
#   ./build-video.sh
#
# Everything is made again from narration.md, data/month.json and the drawn pictures in
# video/explainer.html. The build needs no running demo. The steps are small Node scripts
# in video/; this script runs them in order and checks the result.
#
# The voice is ElevenLabs: ELEVEN_LABS_API_KEY and ELEVEN_LABS_VOICE_ID must be in the
# environment or in the .env file of the repository. There is no other voice.
set -euo pipefail

cd "$(dirname "$0")"

WORK="out/build"
MIN_SECONDS=60
MAX_SECONDS=110

for tool in node pnpm ffmpeg ffprobe; do
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

mkdir -p "$WORK"
# Old recordings and segments must not leak into this build.
find "$WORK" -maxdepth 1 -type f \( -name '*.webm' -o -name '*.mp4' -o -name '*.png' \) -delete

echo "build-video: 1/3 narration, captions and music"
node video/narrate.mjs --check
node video/narrate.mjs
node video/music.mjs

echo "build-video: 2/3 recording the pictures"
node video/record.mjs

echo "build-video: 3/3 assembling"
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
# The intermediate pictures are large. The final files stay, and so do the two sound
# tracks: out/build/narration.wav (the voice alone) and out/build/music.wav (the bed).
find "$WORK" -maxdepth 1 -type f \( -name '*.webm' -o -name 'segment-*.mp4' -o -name 'pictures.mp4' \
  -o -name '*.png' \) -delete
find "$WORK/speech" -type f -delete
echo "build-video: done. Captions: out/explainer.srt"
