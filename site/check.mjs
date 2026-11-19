#!/usr/bin/env node
// Check the site in a real browser and write full-page screenshots to out/check/.
//
//   node check.mjs
//
// For every page, at a desktop and a phone width, it
// fails on: a request to any host that is not this machine, a console error, a broken
// image, or a page that scrolls sideways. It then checks that the text is there without
// JavaScript, that the keys move through the walkthrough, that the presentation mode goes
// through every section and from page to page, and that reduced motion stops the autoplay.

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

// The state of the presentation mode: the page, the mark, and the sections whose top is
// at the top of the window, below the bars that stay there.
function presentState(page) {
  return page.evaluate(() => {
    let bars = 0;
    for (const bar of document.querySelectorAll(".shell, .rail")) {
      if (getComputedStyle(bar).position === "sticky") bars += bar.getBoundingClientRect().height;
    }
    const sections = Array.from(document.querySelectorAll("main section"))
      .filter((section) => section.parentElement.closest("section") === null);
    return {
      page: window.location.pathname.split("/").pop(),
      search: window.location.search,
      on: document.documentElement.classList.contains("present"),
      mark: document.querySelector(".present-mark")?.textContent ?? "",
      top: window.scrollY,
      sections: sections.length,
      aligned: sections.findIndex((section) =>
        Math.abs(section.getBoundingClientRect().top - bars) <= 3),
    };
  });
}

// Press the key until another page opens. Return the sections that were at the top.
async function pressThroughPage(page, key, label) {
  const start = await presentState(page);
  const seen = new Set(start.aligned >= 0 ? [start.aligned] : []);
  let before = start;
  for (let presses = 0; presses < 150; presses += 1) {
    await page.keyboard.press(key);
    await page.waitForTimeout(60);
    // The state cannot be read while another page loads.
    const now = await presentState(page).catch(() => null);
    if (now === null || now.page !== start.page) {
      await page.waitForURL((url) => !url.pathname.endsWith(start.page), { waitUntil: "load" });
      return { seen, sections: start.sections, last: before };
    }
    if (now.top === before.top) problems.push(`${label}: ${key} did not move at ${now.mark}`);
    if (now.aligned >= 0) seen.add(now.aligned);
    before = now;
  }
  problems.push(`${label}: ${key} did not reach another page`);
  return { seen, sections: start.sections, last: before };
}

async function presentation(browser, base) {
  const context = await browser.newContext({ viewport: VIEWPORTS.desktop, reducedMotion: "reduce" });
  const page = await context.newPage();
  watch(page, "presentation");
  const expect = (ok, text) => { if (!ok) problems.push(`presentation: ${text}`); };
  await page.goto(`${base}/index.html`);
  await page.keyboard.press("ArrowRight");
  expect((await presentState(page)).top === 0, "a key moved the page with the mode off");
  await page.keyboard.press("p");
  let state = await presentState(page);
  expect(state.on && state.search === "?presentation", "P did not turn the mode on");
  expect(state.mark.startsWith("1 / "), `the mark is "${state.mark}" at the top`);

  const order = ["index.html", "walkthrough.html", "architecture.html", "screens.html"];
  for (const name of order.slice(0, -1)) {
    const result = await pressThroughPage(page, "ArrowRight", name);
    expect(result.seen.size === result.sections,
      `${name}: ${result.seen.size} of ${result.sections} sections came to the top`);
    state = await presentState(page);
    expect(state.page === order[order.indexOf(name) + 1], `${name}: the next page is ${state.page}`);
    expect(state.on && state.top === 0, `${state.page} did not open at its top with the mode on`);
  }

  // Back from the top of a page: the page before it opens at its last slide.
  await page.keyboard.press("ArrowLeft");
  await page.waitForURL(/architecture\.html/, { waitUntil: "load" });
  state = await presentState(page);
  const [at, of] = state.mark.split(" / ");
  expect(state.page === "architecture.html" && at === of && state.on,
    `back from the last page gave ${state.page} at "${state.mark}"`);

  // On the walkthrough one key press is one step: its own keys do not also move the page.
  await page.goto(`${base}/walkthrough.html?presentation`);
  await page.keyboard.press("Space");
  await page.waitForTimeout(200);
  const step = await page.evaluate(() => document.querySelector(".step.is-active")?.dataset.step);
  expect(step === "0", `the first key on the walkthrough gave step ${step}`);
  await page.screenshot({ path: join(OUT, "presentation-desktop.png") });

  await page.keyboard.press("Escape");
  state = await presentState(page);
  expect(!state.on && state.search === "" && state.mark === "", "Escape did not turn the mode off");
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
    await presentation(browser, server.url);
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
