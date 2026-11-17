#!/usr/bin/env node
// Step 1 of the video build: speak the narration and fix the timing.
//
// Each caption in narration.md is spoken into its own file, so the length of every
// sentence is measured, not estimated. The voice is ElevenLabs when ELEVEN_LABS_API_KEY and
// ELEVEN_LABS_VOICE_ID are set (in the environment or in the .env file of the repository),
// and the macOS `say` command if not. From those lengths this script writes:
//   out/build/timeline.json   every segment and caption with its start and end time
//   out/build/narration.wav   the whole narration track
//   out/explainer.srt         the captions as a sidecar file

import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import {
  GAP, LEAD, OUT, SAMPLE_RATE, TAIL, TIMELINE, WORK, readScript,
} from "./script.mjs";
import { readWav, writeWav } from "./wav.mjs";

const VOICE = process.env.NARRATION_VOICE ?? "Daniel";
const RATE_WORDS_PER_MINUTE = "172";

const ELEVENLABS_MODEL = "eleven_multilingual_v2";
const SPEECH_CACHE = join(OUT, "cache", "speech");

const ENV_FILE = join(dirname(fileURLToPath(import.meta.url)), "..", "..", ".env");
if (existsSync(ENV_FILE)) process.loadEnvFile(ENV_FILE);
const ELEVENLABS_KEY = process.env.ELEVEN_LABS_API_KEY;
const ELEVENLABS_VOICE = process.env.ELEVEN_LABS_VOICE_ID;
const CLONED = Boolean(ELEVENLABS_KEY && ELEVENLABS_VOICE);

function toWav(source, wav) {
  execFileSync("ffmpeg", [
    "-v", "error", "-y", "-i", source, "-ac", "1", "-ar", String(SAMPLE_RATE), "-c:a", "pcm_s16le", wav,
  ]);
  return readWav(wav);
}

function speakLocal(text, base) {
  const aiff = `${base}.aiff`;
  execFileSync("say", ["-v", VOICE, "-r", RATE_WORDS_PER_MINUTE, "-o", aiff, text]);
  return toWav(aiff, `${base}.wav`);
}

// A sentence that was spoken before comes from the cache, so a rebuild costs nothing.
async function speakCloned(text, base) {
  const key = createHash("sha256").update(`${ELEVENLABS_VOICE}\n${ELEVENLABS_MODEL}\n${text}`).digest("hex");
  const mp3 = join(SPEECH_CACHE, `${key.slice(0, 24)}.mp3`);
  if (!existsSync(mp3)) {
    const url = `https://api.elevenlabs.io/v1/text-to-speech/${ELEVENLABS_VOICE}?output_format=mp3_44100_128`;
    const response = await fetch(url, {
      method: "POST",
      headers: { "xi-api-key": ELEVENLABS_KEY, "Content-Type": "application/json", "User-Agent": "clanker-video-build" },
      body: JSON.stringify({ text, model_id: ELEVENLABS_MODEL }),
    });
    if (!response.ok) {
      throw new Error(`ElevenLabs answered ${response.status} for "${text}": ${(await response.text()).slice(0, 300)}`);
    }
    writeFileSync(mp3, Buffer.from(await response.arrayBuffer()));
  }
  return toWav(mp3, `${base}.wav`);
}

function stamp(totalSeconds) {
  const millis = Math.round(totalSeconds * 1000);
  const pad = (value, width = 2) => String(value).padStart(width, "0");
  return `${pad(Math.floor(millis / 3600000))}:${pad(Math.floor(millis / 60000) % 60)}:` +
    `${pad(Math.floor(millis / 1000) % 60)},${pad(millis % 1000, 3)}`;
}

async function main() {
  if (!CLONED) {
    const voices = execFileSync("say", ["-v", "?"], { encoding: "utf8" });
    if (!voices.split("\n").some((line) => line.startsWith(`${VOICE} `))) {
      throw new Error(`the voice "${VOICE}" is not installed. Set NARRATION_VOICE to one from: say -v '?'`);
    }
  }
  const voiceName = CLONED ? "ElevenLabs" : VOICE;
  mkdirSync(join(WORK, "speech"), { recursive: true });
  mkdirSync(SPEECH_CACHE, { recursive: true });
  const segments = readScript();
  const clips = [];
  let cursor = 0;
  let number = 0;
  for (const segment of segments) {
    segment.start = cursor;
    let offset = LEAD;
    for (const caption of segment.captions) {
      number += 1;
      const base = join(WORK, "speech", `caption-${String(number).padStart(2, "0")}`);
      // The spoken form spells out words for `say`. The cloned voice reads the caption as written.
      const audio = CLONED ? await speakCloned(caption.text, base) : speakLocal(caption.spoken, base);
      const length = audio.samples.length / audio.sampleRate;
      caption.number = number;
      caption.offset = offset;
      caption.start = segment.start + offset;
      caption.end = caption.start + length;
      clips.push({ at: caption.start, samples: audio.samples });
      offset += length + GAP;
    }
    // Whole frames, so that the video segments and the audio stay in step.
    segment.duration = Math.ceil((offset - GAP + TAIL) * 30) / 30;
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
  writeFileSync(TIMELINE, `${JSON.stringify({ voice: voiceName, duration: cursor, segments }, null, 1)}\n`);
  console.log(`narrate: ${number} sentences, ${segments.length} segments, ${cursor.toFixed(1)} s (voice ${voiceName})`);
  for (const segment of segments) {
    console.log(`  ${segment.start.toFixed(1).padStart(6)} s  ${segment.id.padEnd(10)} ${segment.duration.toFixed(1)} s`);
  }
}

main().catch((error) => {
  console.error(`narrate: ${error.message}`);
  process.exitCode = 1;
});
