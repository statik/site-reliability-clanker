#!/usr/bin/env node
// Step 3 of the video build: record the pictures.
//
// For each part of the timeline, Playwright opens video/explainer.html and records the
// screen for exactly as long as the narration of that part. When a sentence starts, the
// script gives the page the cue of the sentence, and the page shows the picture of that
// cue. The script also draws one transparent picture for each caption, in the site's
// typeface, for the burned-in captions.
//
// Writes out/build/raw-<part>.webm, out/build/caption-NN.png and out/build/recordings.json.

import { readFileSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright";

import { serveSite } from "../lib/serve.mjs";
import { TIMELINE, WORK } from "./script.mjs";

const PORT = 4176;
// Playwright records the window at its own size. A 1440 x 810 window keeps the text large
// enough to read in a video; the assembly step scales it to 1920 x 1080.
const VIEWPORT = { width: 1440, height: 810 };
const CAPTION_SIZE = { width: 1920, height: 1080 };
// The picture of a sentence starts to change this long before the voice starts. The first
// picture of a part starts with the part. The last picture leaves this long before the
// part ends, so that the parts join on the plain background.
const AHEAD_SECONDS = 0.25;
const LEAVE_SECONDS = 0.25;

function cue(page, name, sentenceSeconds) {
  return page.evaluate(([cueName, length]) => {
    document.documentElement.style.setProperty("--say", `${length}s`);
    document.documentElement.dataset.cue = cueName;
  }, [name, sentenceSeconds]);
}

async function record(browser, part, base) {
  const context = await browser.newContext({ viewport: VIEWPORT, recordVideo: { dir: WORK, size: VIEWPORT } });
  const created = Date.now();
  const page = await context.newPage();
  // The page stops with an error when the data lacks what it shows. That must stop the
  // build, not leave an empty picture in the video.
  const failures = [];
  page.on("pageerror", (error) => failures.push(error.message));
  await page.goto(`${base}/video/explainer.html`, { waitUntil: "load" });
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(300);
  const ready = await page.evaluate(() => document.documentElement.dataset.ready === "true");
  if (!ready && failures.length === 0) failures.push("the page did not finish loading its data");
  const began = Date.now();
  const until = (offset) => page.waitForTimeout(Math.max(0, began + offset * 1000 - Date.now()));
  if (failures.length === 0) {
    for (const sentence of part.sentences) {
      await until(sentence === part.sentences[0] ? 0 : sentence.offset - AHEAD_SECONDS);
      await cue(page, sentence.cue, sentence.length);
    }
    await until(part.duration - LEAVE_SECONDS);
    await cue(page, "", 0);
    await until(part.duration + 0.4);
  }
  const video = page.video();
  await context.close();
  if (failures.length > 0) throw new Error(`part "${part.id}": ${failures[0]}`);
  const file = join(WORK, `raw-${part.id}.webm`);
  renameSync(await video.path(), file);
  return { file, leadIn: (began - created) / 1000 };
}

async function drawCaptions(browser, parts, base) {
  const context = await browser.newContext({ viewport: CAPTION_SIZE });
  const page = await context.newPage();
  await page.goto(`${base}/video/caption.html`, { waitUntil: "load" });
  await page.evaluate(() => document.fonts.ready);
  const captions = parts.flatMap((part) => part.sentences).flatMap((sentence) => sentence.captions);
  for (const caption of captions) {
    const lines = await page.evaluate((text) => {
      const box = document.getElementById("caption");
      box.textContent = text;
      const lineHeight = Number.parseFloat(getComputedStyle(box).lineHeight);
      const padding = Number.parseFloat(getComputedStyle(box).paddingTop) * 2;
      return Math.round((box.getBoundingClientRect().height - padding) / lineHeight);
    }, caption.lines.join("\n"));
    if (lines !== caption.lines.length) {
      throw new Error(`the caption "${caption.lines.join(" / ")}" is drawn on ${lines} lines, not ${caption.lines.length}`);
    }
    const file = join(WORK, `caption-${String(caption.number).padStart(2, "0")}.png`);
    await page.screenshot({ path: file, omitBackground: true });
  }
  await context.close();
}

async function main() {
  const timeline = JSON.parse(readFileSync(TIMELINE, "utf8"));
  const server = await serveSite(PORT);
  const browser = await chromium.launch();
  const recordings = {};
  try {
    for (const part of timeline.parts) {
      recordings[part.id] = await record(browser, part, server.url);
      console.log(`  ${part.id}: recorded (${part.duration.toFixed(1)} s)`);
    }
    await drawCaptions(browser, timeline.parts, server.url);
  } finally {
    await browser.close();
    await server.close();
  }
  writeFileSync(join(WORK, "recordings.json"), `${JSON.stringify(recordings, null, 1)}\n`);
}

main().catch((error) => {
  console.error(`record: ${error.message}`);
  process.exitCode = 1;
});
