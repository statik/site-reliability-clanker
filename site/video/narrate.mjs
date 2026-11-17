#!/usr/bin/env node
// Step 1 of the video build: speak the narration and fix the timing.
//
// Each caption in narration.md is spoken by the macOS `say` command into its own file,
// so the length of every sentence is measured, not estimated. From those lengths this
// script writes:
//   out/build/timeline.json   every segment and caption with its start and end time
//   out/build/narration.wav   the whole narration track
//   out/explainer.srt         the captions as a sidecar file

import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import {
  GAP, LEAD, OUT, SAMPLE_RATE, TAIL, TIMELINE, WORK, readScript,
} from "./script.mjs";
import { readWav, writeWav } from "./wav.mjs";

const VOICE = process.env.NARRATION_VOICE ?? "Daniel";
const RATE_WORDS_PER_MINUTE = "172";
// The Blender title clip has a fixed length, so its segment is never shorter than this.
const MINIMUM_SECONDS = { "blender:title": 8 };

function speak(text, base) {
  const aiff = `${base}.aiff`;
  const wav = `${base}.wav`;
  execFileSync("say", ["-v", VOICE, "-r", RATE_WORDS_PER_MINUTE, "-o", aiff, text]);
  execFileSync("ffmpeg", [
    "-v", "error", "-y", "-i", aiff, "-ac", "1", "-ar", String(SAMPLE_RATE), "-c:a", "pcm_s16le", wav,
  ]);
  return readWav(wav);
}

function stamp(totalSeconds) {
  const millis = Math.round(totalSeconds * 1000);
  const pad = (value, width = 2) => String(value).padStart(width, "0");
  return `${pad(Math.floor(millis / 3600000))}:${pad(Math.floor(millis / 60000) % 60)}:` +
    `${pad(Math.floor(millis / 1000) % 60)},${pad(millis % 1000, 3)}`;
}

function main() {
  const voices = execFileSync("say", ["-v", "?"], { encoding: "utf8" });
  if (!voices.split("\n").some((line) => line.startsWith(`${VOICE} `))) {
    throw new Error(`the voice "${VOICE}" is not installed. Set NARRATION_VOICE to one from: say -v '?'`);
  }
  mkdirSync(join(WORK, "speech"), { recursive: true });
  const segments = readScript();
  const clips = [];
  let cursor = 0;
  let number = 0;
  for (const segment of segments) {
    segment.start = cursor;
    let offset = LEAD;
    for (const caption of segment.captions) {
      number += 1;
      const audio = speak(caption.spoken, join(WORK, "speech", `caption-${String(number).padStart(2, "0")}`));
      const length = audio.samples.length / audio.sampleRate;
      caption.number = number;
      caption.offset = offset;
      caption.start = segment.start + offset;
      caption.end = caption.start + length;
      clips.push({ at: caption.start, samples: audio.samples });
      offset += length + GAP;
    }
    const spokenLength = offset - GAP + TAIL;
    segment.duration = Math.max(spokenLength, MINIMUM_SECONDS[segment.source] ?? 0);
    // Whole frames, so that the video segments and the audio stay in step.
    segment.duration = Math.ceil(segment.duration * 30) / 30;
    cursor += segment.duration;
  }

  const track = new Float32Array(Math.ceil(cursor * SAMPLE_RATE));
  for (const clip of clips) {
    track.set(clip.samples.subarray(0, track.length - Math.floor(clip.at * SAMPLE_RATE)),
      Math.floor(clip.at * SAMPLE_RATE));
  }
  writeWav(join(WORK, "narration.wav"), [track], SAMPLE_RATE);

  const srt = segments.flatMap((segment) => segment.captions).map((caption) =>
    `${caption.number}\n${stamp(caption.start)} --> ${stamp(caption.end + GAP * 0.5)}\n${caption.text}\n`);
  writeFileSync(join(OUT, "explainer.srt"), `${srt.join("\n")}`);
  writeFileSync(TIMELINE, `${JSON.stringify({ voice: VOICE, duration: cursor, segments }, null, 1)}\n`);
  console.log(`narrate: ${number} sentences, ${segments.length} segments, ${cursor.toFixed(1)} s (voice ${VOICE})`);
  for (const segment of segments) {
    console.log(`  ${segment.start.toFixed(1).padStart(6)} s  ${segment.id.padEnd(10)} ${segment.duration.toFixed(1)} s`);
  }
}

main();
