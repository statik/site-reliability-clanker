// Read narration.md into segments and captions, and keep the shared paths and timing
// constants of the video build.

import { readFileSync } from "node:fs";
import { join } from "node:path";

import { duration as spokenDuration, numberWord } from "../lib/html.mjs";
import { seconds, totals } from "../lib/month.mjs";
import { SITE_DIR } from "../lib/serve.mjs";

export const OUT = join(SITE_DIR, "out");
export const WORK = join(OUT, "build");
export const TITLE_CLIP = join(WORK, "title.mp4");
export const TIMELINE = join(WORK, "timeline.json");
export const FPS = 30;
export const SAMPLE_RATE = 48000;

// Seconds of quiet before the first sentence of a segment, between sentences, and after
// the last one.
export const LEAD = 0.6;
export const GAP = 0.4;
export const TAIL = 0.9;

// How the voice says words that the captions spell another way.
const SPOKEN = [
  [/\bAI\b/g, "A.I."],
  [/\bAWS\b/g, "A.W.S."],
  [/\bClanker\b/g, "Clanker"],
];

function minutesInWords(totalSeconds) {
  const minutes = Math.floor(totalSeconds / 60);
  const rest = Math.round(totalSeconds - minutes * 60);
  if (minutes === 0) return `${rest} seconds`;
  return `${numberWord(minutes)} ${minutes === 1 ? "minute" : "minutes"} and ${rest} seconds`;
}

function readMonth() {
  return JSON.parse(readFileSync(join(SITE_DIR, "data", "month.json"), "utf8"));
}

function placeholders(month) {
  const sums = totals(month);
  const failed = month.events.find((event) => event.type === "verify.failed");
  const recovered = month.events.find(
    (event) => event.type === "verify.passed" && failed && event.seq > failed.seq,
  );
  return {
    days: String(sums.days),
    grants: String(sums.grants),
    events: String(sums.events),
    held: minutesInWords(sums.held_seconds),
    held_short: spokenDuration(sums.held_seconds),
    rollback_seconds: failed && recovered ? String(Math.round(seconds(failed.ts, recovered.ts))) : "a few",
  };
}

function fill(text, values) {
  return text.replace(/\{(\w+)\}/g, (match, name) => {
    if (!(name in values)) throw new Error(`narration.md uses {${name}}, which is not a known value`);
    return values[name];
  });
}

function spoken(text) {
  return SPOKEN.reduce((result, [pattern, replacement]) => result.replace(pattern, replacement), text);
}

export function readScript() {
  const month = readMonth();
  const values = placeholders(month);
  const happened = new Set(month.events.map((event) => event.type));
  const lines = readFileSync(join(SITE_DIR, "narration.md"), "utf8").split("\n");
  const segments = [];
  for (const line of lines) {
    const heading = line.match(/^## (\S+)\s*$/);
    if (heading) {
      segments.push({ id: heading[1], source: "", fallback: "", captions: [] });
      continue;
    }
    const segment = segments.at(-1);
    if (!segment) continue;
    const setting = line.match(/^(source|fallback):\s*(\S+)\s*$/);
    if (setting) {
      segment[setting[1]] = setting[2];
      continue;
    }
    const caption = line.match(/^- (?:\((if|unless) ([\w.]+)\)\s*)?(?:\[([^\]]+)\]\s*)?(.+)$/);
    if (caption) {
      const [, condition, eventType, cue, words] = caption;
      // A sentence about something that this run did not do is left out.
      if (condition === "if" && !happened.has(eventType)) continue;
      if (condition === "unless" && happened.has(eventType)) continue;
      const text = fill(words.trim(), values);
      segment.captions.push({ cue: cue ?? "", text, spoken: spoken(text) });
    }
  }
  for (const segment of segments) {
    if (!segment.source) throw new Error(`narration.md: the segment "${segment.id}" has no source`);
    if (segment.captions.length === 0) {
      throw new Error(`narration.md: the segment "${segment.id}" has no sentences`);
    }
  }
  if (segments.length === 0) throw new Error("narration.md has no segments");
  return segments;
}
