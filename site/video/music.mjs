#!/usr/bin/env node
// Step 2 of the video build: synthesize the music bed.
//
// A small offline synthesizer plays the score in js/score.mjs (the same notes as the
// soundtrack button on the walkthrough page) for the length of the video and writes
// out/build/music.wav. A soft pad, a low bass, a plucked melody with an echo, and quiet
// filtered noise like water. No samples and no downloads; the noise uses a fixed seed, so
// every build gives the same file.

import { readFileSync } from "node:fs";
import { join } from "node:path";

import { BEATS_PER_BAR, BEAT_SECONDS, frequency, notesForBar } from "../js/score.mjs";
import { SAMPLE_RATE, TIMELINE, WORK } from "./script.mjs";
import { writeWav } from "./wav.mjs";

const TWO_PI = Math.PI * 2;
const FADE_IN_SECONDS = 2;
const FADE_OUT_SECONDS = 3.5;
const PEAK = 0.6;

const VOICES = {
  pad: { attack: 0.9, release: 1.6, harmonics: [1, 0, 0.18, 0, 0.05], detune: 0.004, pan: 0.25 },
  bass: { attack: 0.04, release: 0.5, harmonics: [1, 0.12], detune: 0, pan: 0 },
  pluck: { attack: 0.004, release: 1.1, harmonics: [1, 0.25, 0.06], detune: 0, pan: -0.2 },
};

function envelope(time, held, shape, plucked) {
  if (time < shape.attack) return time / shape.attack;
  if (plucked) return Math.exp(-(time - shape.attack) * (5 / shape.release));
  if (time < held) return 1;
  return Math.max(0, 1 - (time - held) / shape.release);
}

function addNote(left, right, note, startSeconds) {
  const shape = VOICES[note.voice];
  const plucked = note.voice === "pluck";
  const held = note.beats * BEAT_SECONDS;
  const length = Math.floor((held + shape.release) * SAMPLE_RATE);
  const first = Math.floor(startSeconds * SAMPLE_RATE);
  const base = frequency(note.midi);
  const tunings = shape.detune > 0 ? [1 - shape.detune, 1 + shape.detune] : [1];
  for (let index = 0; index < length && first + index < left.length; index += 1) {
    const time = index / SAMPLE_RATE;
    let value = 0;
    for (const tuning of tunings) {
      shape.harmonics.forEach((weight, harmonic) => {
        if (weight > 0) value += weight * Math.sin(TWO_PI * base * tuning * (harmonic + 1) * time);
      });
    }
    value *= (note.level / tunings.length) * envelope(time, held, shape, plucked);
    left[first + index] += value * (1 - shape.pan) * 0.5;
    right[first + index] += value * (1 + shape.pan) * 0.5;
  }
}

function addEcho(channel, delaySeconds, feedback, repeats) {
  const delay = Math.floor(delaySeconds * SAMPLE_RATE);
  const dry = Float32Array.from(channel);
  for (let repeat = 1; repeat <= repeats; repeat += 1) {
    const gain = feedback ** repeat;
    for (let index = delay * repeat; index < channel.length; index += 1) {
      channel[index] += dry[index - delay * repeat] * gain;
    }
  }
}

// Low-passed noise with a slow swell: water under the music.
function addWater(left, right) {
  let seed = 20261116;
  const random = () => {
    seed = (seed * 1664525 + 1013904223) >>> 0;
    return seed / 4294967296 - 0.5;
  };
  let low = 0;
  for (let index = 0; index < left.length; index += 1) {
    low += (random() - low) * 0.06;
    const swell = 0.6 + 0.4 * Math.sin((TWO_PI * 0.11 * index) / SAMPLE_RATE);
    left[index] += low * swell * 0.22;
    right[index] += low * (1.2 - swell) * 0.22;
  }
}

function main() {
  const { duration } = JSON.parse(readFileSync(TIMELINE, "utf8"));
  const frames = Math.ceil(duration * SAMPLE_RATE);
  const pluckLeft = new Float32Array(frames);
  const pluckRight = new Float32Array(frames);
  const left = new Float32Array(frames);
  const right = new Float32Array(frames);
  const barSeconds = BEATS_PER_BAR * BEAT_SECONDS;
  for (let bar = 0; bar * barSeconds < duration; bar += 1) {
    for (const note of notesForBar(bar)) {
      const start = bar * barSeconds + note.beat * BEAT_SECONDS;
      if (note.voice === "pluck") addNote(pluckLeft, pluckRight, note, start);
      else addNote(left, right, note, start);
    }
  }
  addEcho(pluckLeft, BEAT_SECONDS * 0.75, 0.4, 3);
  addEcho(pluckRight, BEAT_SECONDS * 1.5, 0.4, 2);
  addWater(left, right);
  let peak = 0;
  for (let index = 0; index < frames; index += 1) {
    left[index] += pluckLeft[index];
    right[index] += pluckRight[index];
    peak = Math.max(peak, Math.abs(left[index]), Math.abs(right[index]));
  }
  for (let index = 0; index < frames; index += 1) {
    const time = index / SAMPLE_RATE;
    const fade = Math.min(1, time / FADE_IN_SECONDS, (duration - time) / FADE_OUT_SECONDS);
    const gain = (PEAK / peak) * Math.max(0, fade);
    left[index] *= gain;
    right[index] *= gain;
  }
  writeWav(join(WORK, "music.wav"), [left, right], SAMPLE_RATE);
  console.log(`music: ${duration.toFixed(1)} s written to out/build/music.wav`);
}

main();
