// Read narration.md into parts and sentences, wrap the captions, and keep the shared paths
// and timing constants of the video build.

import { readFileSync } from "node:fs";
import { join } from "node:path";

import { totals } from "../lib/month.mjs";
import { SITE_DIR } from "../lib/serve.mjs";

export const OUT = join(SITE_DIR, "out");
export const WORK = join(OUT, "build");
export const TIMELINE = join(WORK, "timeline.json");
export const FPS = 30;
export const SAMPLE_RATE = 48000;

// Seconds of quiet before the first sentence of a part. `pause:` and `hold:` in
// narration.md replace the other two for one part.
export const LEAD = 0.5;
const PAUSE = 0.5;
const HOLD = 0.7;

// What a caption may be: two lines of 42 characters, on the screen for one second or more.
export const CAPTION_LINE = 42;
export const CAPTION_LINES = 2;
export const CAPTION_MIN_SECONDS = 1;

// A space that a caption does not break at. The duration stays on one caption line, and
// so do two words with a ~ between them in narration.md.
const TIE = "\u00A0";

function heldInWords(totalSeconds) {
  const minutes = Math.floor(totalSeconds / 60);
  const rest = Math.round(totalSeconds - minutes * 60);
  const secondsPart = `${rest} ${rest === 1 ? "second" : "seconds"}`;
  if (minutes === 0) return secondsPart.replaceAll(" ", TIE);
  return `${minutes} ${minutes === 1 ? "minute" : "minutes"} and ${secondsPart}`.replaceAll(" ", TIE);
}

function placeholders() {
  const month = JSON.parse(readFileSync(join(SITE_DIR, "data", "month.json"), "utf8"));
  const sums = totals(month);
  return { days: String(sums.days), held: heldInWords(sums.held_seconds) };
}

function fill(text, values) {
  return text.replace(/\{(\w+)\}/g, (match, name) => {
    if (!(name in values)) throw new Error(`narration.md uses {${name}}, which is not a known value`);
    return values[name];
  });
}

// Two lines of about the same length read better than a full line and a short one.
function balanced(words) {
  let best = null;
  for (let cut = 1; cut < words.length; cut += 1) {
    const pair = [words.slice(0, cut).join(" "), words.slice(cut).join(" ")];
    if (pair.some((line) => line.length > CAPTION_LINE)) continue;
    const difference = Math.abs(pair[0].length - pair[1].length);
    if (best === null || difference < best.difference) best = { pair, difference };
  }
  return best?.pair ?? null;
}

// The captions of one sentence: each is one or two lines. A `|` in the sentence ends a
// caption, for a sentence that is too long for one.
function captionsOf(marked) {
  return marked.split("|").map((piece) => {
    const text = piece.trim();
    if (text.length <= CAPTION_LINE) return [text.replaceAll(TIE, " ")];
    const lines = balanced(text.split(/ +/))?.map((line) => line.replaceAll(TIE, " "));
    if (!lines) {
      throw new Error(`narration.md: "${text}" does not fit ${CAPTION_LINES} caption lines of ` +
        `${CAPTION_LINE} characters. Put a | in the sentence where a second caption must start`);
    }
    return lines;
  });
}

function number(value, name, part) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed < 0) {
    throw new Error(`narration.md: "${name}: ${value}" in the part "${part}" is not a number of seconds`);
  }
  return parsed;
}

export function readScript() {
  const values = placeholders();
  const lines = readFileSync(join(SITE_DIR, "narration.md"), "utf8").split("\n");
  const parts = [];
  for (const line of lines) {
    const heading = line.match(/^## (\S+)\s*$/);
    if (heading) {
      parts.push({ id: heading[1], pause: PAUSE, hold: HOLD, sentences: [] });
      continue;
    }
    const part = parts.at(-1);
    if (!part) continue;
    const setting = line.match(/^(pause|hold):\s*(\S+)\s*$/);
    if (setting) {
      part[setting[1]] = number(setting[2], setting[1], part.id);
      continue;
    }
    if (!line.startsWith("- ")) continue;
    const sentence = line.match(/^- \[([\w-]+)\]\s+(.+)$/);
    if (!sentence) throw new Error(`narration.md: this sentence has no [cue] for its picture: ${line}`);
    const marked = fill(sentence[2].trim(), values).replaceAll("~", TIE);
    const text = marked.replace(/\s*\|\s*/g, " ").replaceAll(TIE, " ");
    part.sentences.push({ cue: sentence[1], text, captions: captionsOf(marked) });
  }
  if (parts.length === 0) throw new Error("narration.md has no parts");
  for (const part of parts) {
    if (part.sentences.length === 0) throw new Error(`narration.md: the part "${part.id}" has no sentences`);
  }
  return parts;
}

export function wordCount(parts) {
  return parts.flatMap((part) => part.sentences)
    .reduce((sum, sentence) => sum + sentence.text.split(/\s+/).length, 0);
}
