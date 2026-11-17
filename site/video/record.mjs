#!/usr/bin/env node
// Step 3 of the video build: record the pictures.
//
// For each segment of the timeline, Playwright opens the segment's source and records
// the screen for exactly as long as the narration of that segment. A source that is not
// available (the dashboard is not up, or the Blender clip was not rendered) is replaced
// by the segment's fallback, with a message. The script also draws one transparent
// picture for each caption, in the site's typeface, for the burned-in captions.
//
// Writes out/build/raw-<segment>.webm, out/build/caption-NN.png and
// out/build/recordings.json.

import { existsSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright";

import { serveSite } from "../lib/serve.mjs";
import { TOURS, dashboardIsUp, makeReadOnly } from "../lib/tours.mjs";
import { TIMELINE, TITLE_CLIP, WORK } from "./script.mjs";

const PORT = 4176;
// Playwright records the window at its own size. A 1440 x 810 window keeps the text large
// enough to read in a video; the assembly step scales it to 1920 x 1080.
const VIEWPORT = { width: 1440, height: 810 };
const CAPTION_SIZE = { width: 1920, height: 1080 };
const DASHBOARD_USER = "alice";

function parse(source) {
  const [kind, ...rest] = source.split(":");
  return { kind, name: rest.join(":") };
}

function choose(segment, available) {
  const wanted = parse(segment.source);
  const ready =
    (wanted.kind === "blender" && available.title) ||
    (wanted.kind === "dashboard" && available.dashboard) ||
    wanted.kind === "site" || wanted.kind === "tour";
  if (ready) return wanted;
  if (!segment.fallback) throw new Error(`segment "${segment.id}": ${segment.source} is not available and it has no fallback`);
  const why = wanted.kind === "dashboard" ? "the dashboard is not up" : "the Blender title was not rendered";
  console.log(`  ${segment.id}: ${why}, so it shows ${segment.fallback}`);
  return parse(segment.fallback);
}

async function doCue(page, cue, base) {
  if (!cue) return;
  if (cue.startsWith("page:")) {
    await page.goto(`${base}/${cue.slice(5)}?theme=light`, { waitUntil: "load" });
  } else if (cue.startsWith("flow:")) {
    // A click from inside the page, so that the browser does not scroll to the list item.
    await page.evaluate((index) => {
      document.querySelectorAll("#flow-steps li")[index].click();
    }, Number(cue.slice(5)) - 1);
  } else {
    const found = await page.evaluate((selector) => {
      const target = document.querySelector(selector);
      target?.scrollIntoView({ behavior: "smooth", block: "start" });
      return target !== null;
    }, cue);
    if (!found) console.log(`  the cue [${cue}] matches nothing on this page; the picture stays`);
  }
}

async function playSite(page, segment, name, base) {
  await page.goto(`${base}/${name}?theme=light`, { waitUntil: "load" });
  await page.evaluate(() => document.fonts.ready);
  // The narration drives the system diagram, so stop its own autoplay.
  const play = page.locator(".flow-play[aria-pressed='true']");
  if (await play.count()) await play.click();
  await page.waitForTimeout(300);
  const sceneStart = Date.now();
  for (const caption of segment.captions) {
    const wait = sceneStart + (caption.offset - 0.25) * 1000 - Date.now();
    if (wait > 0) await page.waitForTimeout(wait);
    await doCue(page, caption.cue, base);
  }
  const rest = sceneStart + (segment.duration + 0.4) * 1000 - Date.now();
  if (rest > 0) await page.waitForTimeout(rest);
  return sceneStart;
}

async function record(browser, segment, chosen, base) {
  const context = await browser.newContext({
    viewport: VIEWPORT, colorScheme: "light", recordVideo: { dir: WORK, size: VIEWPORT },
  });
  await makeReadOnly(context);
  if (chosen.kind === "dashboard") {
    await context.addInitScript((user) => {
      window.localStorage.setItem("clanker.user", user);
      window.localStorage.setItem("clanker.theme", "g10");
    }, DASHBOARD_USER);
  }
  const created = Date.now();
  const page = await context.newPage();
  let sceneStart;
  if (chosen.kind === "site") {
    sceneStart = await playSite(page, segment, chosen.name, base);
  } else {
    const tour = TOURS[chosen.kind === "dashboard" ? `dashboard-${chosen.name}` : chosen.name];
    if (!tour) throw new Error(`segment "${segment.id}": there is no tour named ${chosen.name}`);
    sceneStart = Date.now() + 500;
    await tour(page, segment.duration + 1, segment.captions);
  }
  const video = page.video();
  await context.close();
  const file = join(WORK, `raw-${segment.id}.webm`);
  renameSync(await video.path(), file);
  return { file, leadIn: (sceneStart - created) / 1000 };
}

async function drawCaptions(browser, segments, base) {
  const context = await browser.newContext({ viewport: CAPTION_SIZE });
  const page = await context.newPage();
  await page.goto(`${base}/video/caption.html`, { waitUntil: "load" });
  await page.evaluate(() => document.fonts.ready);
  for (const caption of segments.flatMap((segment) => segment.captions)) {
    await page.evaluate((text) => {
      document.getElementById("caption").textContent = text;
    }, caption.text);
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
    const probe = await browser.newContext();
    const available = { title: existsSync(TITLE_CLIP), dashboard: await dashboardIsUp(await probe.newPage()) };
    await probe.close();
    console.log(`record: dashboard ${available.dashboard ? "is up" : "is not up"}, ` +
      `Blender title ${available.title ? "is rendered" : "is not rendered"}`);
    for (const segment of timeline.segments) {
      const chosen = choose(segment, available);
      if (chosen.kind === "blender") {
        recordings[segment.id] = { file: TITLE_CLIP, leadIn: 0, shows: segment.source };
        continue;
      }
      const result = await record(browser, segment, chosen, server.url);
      recordings[segment.id] = { ...result, shows: `${chosen.kind}:${chosen.name}` };
      console.log(`  ${segment.id}: recorded ${chosen.kind}:${chosen.name} (${segment.duration.toFixed(1)} s)`);
    }
    await drawCaptions(browser, timeline.segments, server.url);
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
