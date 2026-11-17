#!/usr/bin/env node
// Check the site in a real browser and write full-page screenshots to out/check/.
//
//   node check.mjs
//
// For every page, at a desktop and a phone width, it
// fails on: a request to any host that is not this machine, a console error, a broken
// image, or a page that scrolls sideways. It then checks that the text is there without
// JavaScript, that the keys move through the walkthrough, and that reduced motion stops
// the autoplay.

import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { chromium } from "playwright";

import { SITE_DIR, serveSite } from "./lib/serve.mjs";

const PORT = 4174;
const PAGES = ["index", "walkthrough", "architecture", "screens"];
const VIEWPORTS = { desktop: { width: 1440, height: 900 }, phone: { width: 390, height: 844 } };
const OUT = join(SITE_DIR, "out", "check");

const problems = [];

function watch(page, label) {
  page.on("request", (request) => {
    const host = new URL(request.url()).hostname;
    if (host !== "127.0.0.1" && host !== "localhost" && !request.url().startsWith("data:")) {
      problems.push(`${label}: request to another host: ${request.url()}`);
    }
  });
  page.on("console", (message) => {
    if (message.type() === "error") problems.push(`${label}: console error: ${message.text()}`);
  });
  page.on("pageerror", (error) => problems.push(`${label}: page error: ${error.message}`));
  page.on("response", (response) => {
    if (response.status() >= 400) problems.push(`${label}: ${response.status()} ${response.url()}`);
  });
}

async function checkLayout(page, label) {
  const result = await page.evaluate(() => ({
    overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    broken: Array.from(document.images).filter((image) => image.complete && image.naturalWidth === 0)
      .map((image) => image.getAttribute("src")),
  }));
  if (result.overflow > 1) problems.push(`${label}: the page scrolls sideways by ${result.overflow}px`);
  for (const source of result.broken) problems.push(`${label}: broken image ${source}`);
}

async function screenshots(browser, base) {
  for (const [size, viewport] of Object.entries(VIEWPORTS)) {
    {
      const context = await browser.newContext({
        viewport, reducedMotion: "reduce",
      });
      for (const name of PAGES) {
        const label = `${name} ${size}`;
        const page = await context.newPage();
        watch(page, label);
        await page.goto(`${base}/${name}.html`, { waitUntil: "networkidle" });
        await page.evaluate(() => document.fonts.ready);
        // Load the lazy images, which a full-page screenshot does not scroll to.
        await page.evaluate(async () => {
          for (const image of document.images) image.loading = "eager";
          await Promise.all(Array.from(document.images).map((image) => image.decode().catch(() => {})));
        });
        await checkLayout(page, label);
        await page.screenshot({ path: join(OUT, `${name}-${size}.png`), fullPage: true });
        await page.close();
      }
      await context.close();
    }
  }
}

async function withoutJavaScript(browser, base) {
  const context = await browser.newContext({ javaScriptEnabled: false, viewport: VIEWPORTS.desktop });
  const page = await context.newPage();
  const expectations = {
    index: ["The model proposes. Code decides.", "Validated, not applied", "Permissions requested"],
    walkthrough: ["A blank slate", "The auditor asks for the evidence", "Held"],
    architecture: ["Access broker", "Rolled back", "Standing write access"],
    screens: ["Paperboat, the managed application"],
  };
  for (const [name, phrases] of Object.entries(expectations)) {
    await page.goto(`${base}/${name}.html`);
    const text = await page.locator("main").innerText();
    for (const phrase of phrases) {
      if (!text.includes(phrase)) problems.push(`${name} without JavaScript: missing text "${phrase}"`);
    }
    const hidden = await page.evaluate(() => Array.from(document.querySelectorAll(".reveal"))
      .filter((item) => getComputedStyle(item).opacity === "0").length);
    if (hidden > 0) problems.push(`${name} without JavaScript: ${hidden} parts are invisible`);
  }
  await page.screenshot({ path: join(OUT, "screens-no-js.png") });
  await context.close();
}

async function keyboard(browser, base) {
  const context = await browser.newContext({ viewport: VIEWPORTS.desktop, reducedMotion: "reduce" });
  const page = await context.newPage();
  watch(page, "walkthrough keys");
  await page.goto(`${base}/walkthrough.html`);
  const active = () => page.evaluate(() => document.querySelector(".step.is-active")?.id ?? "");
  const steps = await page.evaluate(() => Array.from(document.querySelectorAll(".step")).map((step) => step.id));
  const expectStep = async (key, wanted) => {
    await page.keyboard.press(key);
    await page.waitForFunction((id) => document.querySelector(".step.is-active")?.id === id, wanted,
      { timeout: 3000 }).catch(() => {});
    const got = await active();
    if (got !== wanted) problems.push(`walkthrough keys: after ${key} the step is "${got}", not "${wanted}"`);
  };
  await expectStep("ArrowRight", steps[0]);
  await expectStep("Space", steps[1]);
  await expectStep("ArrowRight", steps[2]);
  await expectStep("ArrowLeft", steps[1]);
  await expectStep("End", steps.at(-1));
  await expectStep("Home", steps[0]);
  const current = await page.evaluate(() => document.querySelector(".dot[aria-current='step']")?.dataset.step);
  if (current !== "0") problems.push("walkthrough keys: the first dot is not marked as the current step");
  await page.screenshot({ path: join(OUT, "walkthrough-step-desktop.png") });
  await context.close();
}

async function reducedMotion(browser, base) {
  const context = await browser.newContext({ viewport: VIEWPORTS.desktop, reducedMotion: "reduce" });
  const page = await context.newPage();
  await page.goto(`${base}/architecture.html`);
  const first = await page.evaluate(() => document.querySelector("#flow-steps .is-active")?.textContent);
  await page.waitForTimeout(3200);
  const later = await page.evaluate(() => ({
    stop: document.querySelector("#flow-steps .is-active")?.textContent,
    pressed: document.querySelector(".flow-play")?.getAttribute("aria-pressed"),
    running: document.getAnimations().filter((animation) => animation.playState === "running").length,
  }));
  if (later.stop !== first) problems.push("reduced motion: the flow figure advanced by itself");
  if (later.pressed !== "false") problems.push("reduced motion: the flow figure is not paused");
  if (later.running > 0) problems.push(`reduced motion: ${later.running} animations still run`);
  await page.goto(`${base}/index.html`);
  const label = await page.locator(".motion-toggle").textContent();
  if (label !== "Play motion") problems.push("reduced motion: the hero backdrop is not paused");
  await context.close();
}

async function main() {
  await mkdir(OUT, { recursive: true });
  const server = await serveSite(PORT);
  const browser = await chromium.launch();
  try {
    await screenshots(browser, server.url);
    await withoutJavaScript(browser, server.url);
    await keyboard(browser, server.url);
    await reducedMotion(browser, server.url);
  } finally {
    await browser.close();
    await server.close();
  }
  if (problems.length > 0) {
    console.error(`check: ${problems.length} problems\n  - ${[...new Set(problems)].join("\n  - ")}`);
    process.exitCode = 1;
    return;
  }
  console.log(`check: all pages pass. Screenshots are in ${OUT}`);
}

main().catch((error) => {
  console.error(`check: ${error.message}`);
  process.exitCode = 1;
});
