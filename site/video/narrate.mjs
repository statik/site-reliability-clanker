#!/usr/bin/env node
// Step 1 of the video build: speak the narration and fix the timing.
//
// Each sentence in narration.md is spoken into its own file by the ElevenLabs voice, so
// the length of every sentence is measured, not estimated. The voice needs
// ELEVEN_LABS_API_KEY and ELEVEN_LABS_VOICE_ID, in the environment or in the .env file of
// the repository. There is no other voice. The request holds the sentence and the name of
// the model, and nothing else. From the lengths this script writes:
//   out/build/timeline.json   every part, sentence and caption with its start and end time
//   out/build/narration.wav   the whole narration track
//   out/explainer.srt         the captions as a sidecar file
//
//   node video/narrate.mjs --check   read narration.md and check the captions; speak nothing

import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import {
  CAPTION_MIN_SECONDS, FPS, LEAD, OUT, SAMPLE_RATE, TIMELINE, WORK, readScript, wordCount,
} from "./script.mjs";
import { readWav, writeWav } from "./wav.mjs";

const ELEVENLABS_MODEL = "eleven_multilingual_v2";
const SPEECH_CACHE = join(OUT, "cache", "speech");
const ENV_FILE = join(dirname(fileURLToPath(import.meta.url)), "..", "..", ".env");

function voiceSettings() {
  if (existsSync(ENV_FILE)) process.loadEnvFile(ENV_FILE);
  const key = process.env.ELEVEN_LABS_API_KEY;
  const voice = process.env.ELEVEN_LABS_VOICE_ID;
  if (!key || !voice) {
    throw new Error("the narration needs the ElevenLabs voice. Put ELEVEN_LABS_API_KEY and " +
      "ELEVEN_LABS_VOICE_ID in the .env file at the root of the repository, or in the environment");
  }
  return { key, voice };
}

function toWav(source, wav) {
  execFileSync("ffmpeg", [
    "-v", "error", "-y", "-i", source, "-ac", "1", "-ar", String(SAMPLE_RATE), "-c:a", "pcm_s16le", wav,
  ]);
  return readWav(wav);
}

// A sentence that was spoken before comes from the cache, so a rebuild costs nothing.
async function speak(text, base, settings) {
  const key = createHash("sha256").update(`${settings.voice}\n${ELEVENLABS_MODEL}\n${text}`).digest("hex");
  const mp3 = join(SPEECH_CACHE, `${key.slice(0, 24)}.mp3`);
  if (!existsSync(mp3)) {
    const url = `https://api.elevenlabs.io/v1/text-to-speech/${settings.voice}?output_format=mp3_44100_128`;
    const response = await fetch(url, {
      method: "POST",
      headers: { "xi-api-key": settings.key, "Content-Type": "application/json", "User-Agent": "video-build" },
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

// A sentence with two captions shares its time between them by their number of characters.
function timeCaptions(sentence, pause, numberFrom) {
  const shownUntil = sentence.end + pause * 0.5;
  const characters = sentence.captions.reduce((sum, lines) => sum + lines.join(" ").length, 0);
  let start = sentence.start;
  return sentence.captions.map((lines, index) => {
    const last = index === sentence.captions.length - 1;
    const share = lines.join(" ").length / characters;
    const end = last ? shownUntil : start + (sentence.end - sentence.start) * share;
    if (end - start < CAPTION_MIN_SECONDS) {
      throw new Error(`the caption "${lines.join(" / ")}" would stay ${(end - start).toFixed(2)} s; ` +
        `the least is ${CAPTION_MIN_SECONDS} s. Give its part a longer pause in narration.md`);
    }
    const caption = { number: numberFrom + index, lines, start, end };
    start = end;
    return caption;
  });
}

async function main() {
  // readScript stops on a caption that does not fit two lines.
  const parts = readScript();
  if (process.argv.includes("--check")) {
    console.log(`narrate: ${parts.flatMap((part) => part.sentences).length} sentences; every caption fits`);
    return;
  }
  const settings = voiceSettings();
  mkdirSync(join(WORK, "speech"), { recursive: true });
  mkdirSync(SPEECH_CACHE, { recursive: true });
  const clips = [];
  const captions = [];
  let cursor = 0;
  let spoken = 0;
  let count = 0;
  for (const part of parts) {
    part.start = cursor;
    let offset = LEAD;
    for (const sentence of part.sentences) {
      count += 1;
      const base = join(WORK, "speech", `sentence-${String(count).padStart(2, "0")}`);
      const audio = await speak(sentence.text, base, settings);
      const length = audio.samples.length / audio.sampleRate;
      spoken += length;
      sentence.offset = offset;
      sentence.length = length;
      sentence.start = part.start + offset;
      sentence.end = sentence.start + length;
      clips.push({ at: sentence.start, samples: audio.samples });
      sentence.captions = timeCaptions(sentence, part.pause, captions.length + 1);
      captions.push(...sentence.captions);
      offset += length + part.pause;
    }
    // Whole frames, so that the pictures and the sound stay in step.
    part.duration = Math.ceil((offset - part.pause + part.hold) * FPS) / FPS;
    cursor += part.duration;
  }

  const track = new Float32Array(Math.ceil(cursor * SAMPLE_RATE));
  for (const clip of clips) {
    track.set(clip.samples.subarray(0, track.length - Math.floor(clip.at * SAMPLE_RATE)),
      Math.floor(clip.at * SAMPLE_RATE));
  }
  writeWav(join(WORK, "narration.wav"), [track], SAMPLE_RATE);

  const srt = captions.map((caption) =>
    `${caption.number}\n${stamp(caption.start)} --> ${stamp(caption.end)}\n${caption.lines.join("\n")}\n`);
  writeFileSync(join(OUT, "explainer.srt"), `${srt.join("\n")}`);
  const words = wordCount(parts);
  writeFileSync(TIMELINE, `${JSON.stringify({ duration: cursor, words, parts }, null, 1)}\n`);
  console.log(`narrate: ${count} sentences, ${words} words, ${cursor.toFixed(1)} s: ` +
    `${(words / cursor).toFixed(2)} words a second over the video, ${(words / spoken).toFixed(2)} while the voice speaks`);
  for (const part of parts) {
    console.log(`  ${part.start.toFixed(1).padStart(6)} s  ${part.id.padEnd(10)} ${part.duration.toFixed(1)} s`);
  }
}

main().catch((error) => {
  console.error(`narrate: ${error.message}`);
  process.exitCode = 1;
});
