#!/usr/bin/env node
// Step 3 of the video build: record the pictures.
//
// For each segment of the timeline, Playwright opens the segment's source and records
// the screen for exactly as long as the narration of that segment. A dashboard that is
// not up is replaced by the segment's fallback, with a message. The script also draws one
// transparent picture for each caption, in the site's typeface, for the burned-in captions.
//
// With VIDEO_FOOTAGE=<directory>, the script does not contact the running applications.
// A `tour:` or `dashboard:` segment then takes its picture from <directory>/<segment>.mp4,
// a recording that was kept from an earlier run. The clip must be as long as the segment.
//
// Writes out/build/raw-<segment>.webm, out/build/caption-NN.png and
// out/build/recordings.json.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { chromium } from "playwright";

import { serveSite } from "../lib/serve.mjs";
import { TOURS, dashboardIsUp, makeReadOnly } from "../lib/tours.mjs";
import { TIMELINE, WORK } from "./script.mjs";

const PORT = 4176;
// Playwright records the window at its own size. A 1440 x 810 window keeps the text large
// enough to read in a video; the assembly step scales it to 1920 x 1080.
const VIEWPORT = { width: 1440, height: 810 };
const CAPTION_SIZE = { width: 1920, height: 1080 };
const DASHBOARD_USER = "alice";
// The captions are burned in over the bottom of the picture. A page of the site gets a
// plain band there while it is recorded, so that no caption lies on the text of the page.
// The drawn scenes keep their content above the band by their own layout.
const CAPTION_BAND = `body::after { content: ""; position: fixed; left: 0; right: 0; bottom: 0; z-index: 90;
  height: 172px; background: var(--dark); border-top: 1px solid var(--border-strong); }
  .motion-toggle { display: none; }`;
const FOOTAGE = process.env.VIDEO_FOOTAGE ? resolve(process.env.VIDEO_FOOTAGE) : "";
// A kept clip may differ from its segment by this many seconds; the assembly holds the
// last frame for the difference.
const FOOTAGE_SLACK_SECONDS = 0.25;

function parse(source) {
  const [kind, ...rest] = source.split(":");
  return { kind, name: rest.join(":") };
}

function isLive(kind) {
  return kind === "tour" || kind === "dashboard";
}

function choose(segment, dashboardUp) {
  const wanted = parse(segment.source);
  if (wanted.kind !== "dashboard" || dashboardUp) return wanted;
  if (!segment.fallback) throw new Error(`segment "${segment.id}": ${segment.source} is not available and it has no fallback`);
  console.log(`  ${segment.id}: the dashboard is not up, so it shows ${segment.fallback}`);
  return parse(segment.fallback);
}

function keptFootage(segment) {
  const file = join(FOOTAGE, `${segment.id}.mp4`);
  if (!existsSync(file)) {
    throw new Error(`segment "${segment.id}": VIDEO_FOOTAGE is set, but ${file} is missing`);
  }
  const length = Number(execFileSync("ffprobe", [
    "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", file,
  ], { encoding: "utf8" }));
  if (Math.abs(length - segment.duration) > FOOTAGE_SLACK_SECONDS) {
    throw new Error(`segment "${segment.id}": ${file} is ${length.toFixed(2)} s, but the narration of the ` +
      `segment is ${segment.duration.toFixed(2)} s. Record it again from the running demo, or cut the clip again`);
  }
  return { file, leadIn: 0, shows: segment.source };
}

function isScene(name) {
  return name.startsWith("video/");
}

async function doCue(page, cue, base) {
  if (!cue) return;
  if (cue.startsWith("page:")) {
    await page.goto(`${base}/${cue.slice(5)}?theme=dark`, { waitUntil: "load" });
    await page.addStyleTag({ content: CAPTION_BAND });
  } else if (cue.startsWith("beat:")) {
    // A drawn scene (video/scene.js) shows its next part when this value changes.
    await page.evaluate((beat) => {
      document.documentElement.dataset.beat = beat;
    }, cue.slice(5));
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
  await page.goto(`${base}/${name}?theme=dark`, { waitUntil: "load" });
  if (!isScene(name)) await page.addStyleTag({ content: CAPTION_BAND });
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
    viewport: VIEWPORT, colorScheme: "dark", recordVideo: { dir: WORK, size: VIEWPORT },
  });
  await makeReadOnly(context);
  if (chosen.kind === "dashboard") {
    await context.addInitScript((user) => {
      window.localStorage.setItem("clanker.user", user);
      window.localStorage.setItem("clanker.theme", "g100");
    }, DASHBOARD_USER);
  }
  const created = Date.now();
  const page = await context.newPage();
  // A drawn scene stops with an error when the data lacks what it shows. That must stop
  // the build, not leave an empty picture in the video.
  const failures = [];
  if (chosen.kind === "site") page.on("pageerror", (error) => failures.push(error.message));
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
  if (failures.length > 0) {
    throw new Error(`segment "${segment.id}": ${chosen.name} failed: ${failures[0]}`);
  }
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
    let dashboardUp = false;
    if (FOOTAGE) {
      console.log(`record: kept footage from ${FOOTAGE}; the running applications are not contacted`);
    } else {
      const probe = await browser.newContext();
      dashboardUp = await dashboardIsUp(await probe.newPage());
      await probe.close();
      console.log(`record: dashboard ${dashboardUp ? "is up" : "is not up"}`);
    }
    for (const segment of timeline.segments) {
      if (FOOTAGE && isLive(parse(segment.source).kind)) {
        recordings[segment.id] = keptFootage(segment);
        console.log(`  ${segment.id}: kept footage (${segment.duration.toFixed(1)} s)`);
        continue;
      }
      const chosen = choose(segment, dashboardUp);
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
