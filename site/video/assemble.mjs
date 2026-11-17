#!/usr/bin/env node
// Step 4 of the video build: put it together with ffmpeg.
//
// 1. Cut each recording to the length of its part (1080p, 30 frames a second, H.264). The
//    pictures enter and leave by themselves on the background colour, so the parts join
//    with no fade. Only the start and the end of the video fade.
// 2. Join the parts.
// 3. Lay each caption picture over its time, mix the narration over the music (the music
//    gets quieter while the voice speaks), and write out/explainer.mp4.
//
// This ffmpeg has no subtitle or text filter, so the captions are pictures, drawn by
// video/record.mjs in the site's typeface. out/explainer.srt has the same captions as text.

import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import { FPS, OUT, TIMELINE, WORK } from "./script.mjs";

const FADE_IN = 0.4;
const FADE_OUT = 0.8;
const MUSIC_LEVEL = 0.4;

function ffmpeg(args) {
  execFileSync("ffmpeg", ["-v", "error", "-y", ...args], { stdio: ["ignore", "inherit", "inherit"] });
}

function cutParts(timeline, recordings) {
  const files = [];
  timeline.parts.forEach((part, index) => {
    const recording = recordings[part.id];
    const file = join(WORK, `segment-${String(index + 1).padStart(2, "0")}-${part.id}.mp4`);
    const filters = [
      `fps=${FPS}`,
      "scale=1920:1080:flags=lanczos",
      // Hold the last frame if the recording is a little shorter than the narration.
      "tpad=stop_mode=clone:stop_duration=30",
      ...(index === 0 ? [`fade=t=in:st=0:d=${FADE_IN}`] : []),
      ...(index === timeline.parts.length - 1
        ? [`fade=t=out:st=${(part.duration - FADE_OUT).toFixed(3)}:d=${FADE_OUT}`] : []),
      "format=yuv420p",
    ];
    ffmpeg([
      "-ss", recording.leadIn.toFixed(3), "-i", recording.file,
      "-vf", filters.join(","), "-t", part.duration.toFixed(3),
      "-an", "-c:v", "libx264", "-preset", "medium", "-crf", "17", file,
    ]);
    files.push(file);
  });
  return files;
}

function join_(files) {
  const list = join(WORK, "segments.txt");
  writeFileSync(list, files.map((file) => `file '${file}'\n`).join(""));
  const joined = join(WORK, "pictures.mp4");
  ffmpeg(["-f", "concat", "-safe", "0", "-i", list, "-c", "copy", joined]);
  return joined;
}

function finish(timeline, pictures) {
  const captions = timeline.parts.flatMap((part) => part.sentences).flatMap((sentence) => sentence.captions);
  const inputs = ["-i", pictures];
  for (const caption of captions) {
    inputs.push("-i", join(WORK, `caption-${String(caption.number).padStart(2, "0")}.png`));
  }
  const narration = captions.length + 1;
  const music = captions.length + 2;
  inputs.push("-i", join(WORK, "narration.wav"), "-i", join(WORK, "music.wav"));

  const graph = [];
  let previous = "0:v";
  captions.forEach((caption, index) => {
    const from = caption.start.toFixed(3);
    const to = caption.end.toFixed(3);
    graph.push(`[${previous}][${index + 1}:v]overlay=enable='between(t,${from},${to})'[v${index + 1}]`);
    previous = `v${index + 1}`;
  });
  graph.push(
    `[${narration}:a]aformat=channel_layouts=stereo,volume=1.5,asplit=2[voice][key]`,
    `[${music}:a]volume=${MUSIC_LEVEL}[bed]`,
    "[bed][key]sidechaincompress=threshold=0.02:ratio=9:attack=30:release=700[ducked]",
    "[voice][ducked]amix=inputs=2:normalize=0,alimiter=limit=0.95[a]",
  );
  const output = join(OUT, "explainer.mp4");
  ffmpeg([
    ...inputs, "-filter_complex", graph.join(";"), "-map", `[${previous}]`, "-map", "[a]",
    "-t", timeline.duration.toFixed(3),
    "-c:v", "libx264", "-preset", "medium", "-crf", "19", "-pix_fmt", "yuv420p", "-r", String(FPS),
    "-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709",
    "-c:a", "aac", "-b:a", "192k", "-movflags", "+faststart", output,
  ]);
  return output;
}

function main() {
  const timeline = JSON.parse(readFileSync(TIMELINE, "utf8"));
  const recordings = JSON.parse(readFileSync(join(WORK, "recordings.json"), "utf8"));
  const pictures = join_(cutParts(timeline, recordings));
  const output = finish(timeline, pictures);
  console.log(`assemble: wrote ${output}`);
}

main();
