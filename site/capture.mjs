#!/usr/bin/env node
// Take real screenshots of the running demo into screens/, and with --video record the
// scripted click-throughs into out/segments/.
//
//   node capture.mjs            screenshots of everything that is up
//   node capture.mjs --video    also record the click-through videos
//
// Targets: the Clanker dashboard (http://127.0.0.1:7070), Paperboat
// (http://localhost:8088), the mail inbox (http://127.0.0.1:8025) and the webhook sink
// (http://127.0.0.1:7071). A target that is not up is skipped with a message and listed
// in screens/manifest.json, so the gallery says what is missing. Run it again when the
// target is up. The script never sends a request that changes the control plane.

import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir, rename, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { chromium } from "playwright";

import { SITE_DIR } from "./lib/serve.mjs";
import {
  DASHBOARD, MAIL, PAPERBOAT, SINK, TOURS, dashboardIsUp, dashboardNav, makeReadOnly,
  openDashboard, waitingWorkItem,
} from "./lib/tours.mjs";

const SCREENS = join(SITE_DIR, "screens");
const SEGMENTS = join(SITE_DIR, "out", "segments");
const VIEWPORT = { width: 1440, height: 900 };
const VIDEO_SIZE = { width: 1920, height: 1080 };
const MAX_HEIGHT = 2200;
const DASHBOARD_USER = "alice";
const TOUR_SECONDS = 20;

const DASHBOARD_VIEWS = [
  { slug: "overview", area: "Overview", title: "Overview", note: "Is each environment healthy, and what needs a decision now?" },
  { slug: "timeline", area: "Timeline", title: "Timeline", note: "What happened, when, and what is coming next." },
  { slug: "approval", area: null, title: "Approval page", note: "One sentence, the impact, the tier, what changes, and the decision." },
  { slug: "topology", area: "Topology", title: "Topology", note: "What exists in the environment and how it is connected." },
  { slug: "access", area: "Access", title: "Access", note: "Who and what holds which permission now, and the history of grants." },
  { slug: "audit", area: "Audit", title: "Audit", note: "The searchable, hash-chained event log." },
];

const shots = [];
const skipped = [];

async function isUp(url) {
  try {
    const response = await fetch(url, { method: "GET", signal: AbortSignal.timeout(4000) });
    return response.status < 500;
  } catch (error) {
    return false;
  }
}

async function shoot(page, file, details, fullPage = false) {
  const height = fullPage
    ? Math.min(MAX_HEIGHT, await page.evaluate(() => document.documentElement.scrollHeight))
    : VIEWPORT.height;
  await page.screenshot({
    path: join(SCREENS, file),
    fullPage,
    ...(fullPage ? { clip: { x: 0, y: 0, width: VIEWPORT.width, height } } : {}),
  });
  shots.push({ file, width: VIEWPORT.width, height, ...details });
  console.log(`  wrote screens/${file}`);
}

async function captureDashboard(browser) {
  const group = "dashboard";
  const probe = await browser.newContext({ viewport: VIEWPORT });
  const up = await dashboardIsUp(await probe.newPage());
  await probe.close();
  if (!up) {
    const reason = `The dashboard did not answer at ${DASHBOARD}/ when the capture ran.`;
    console.log(`skip: dashboard. ${reason} Run this script again when it is up.`);
    for (const view of DASHBOARD_VIEWS) skipped.push({ group, title: view.title, reason });
    return false;
  }
  for (const theme of ["light", "dark"]) {
    const context = await browser.newContext({
      viewport: VIEWPORT, colorScheme: theme, extraHTTPHeaders: { "X-Clanker-User": DASHBOARD_USER },
    });
    await makeReadOnly(context);
    await context.addInitScript(([user, carbon]) => {
      window.localStorage.setItem("clanker.user", user);
      window.localStorage.setItem("clanker.theme", carbon);
    }, [DASHBOARD_USER, theme === "dark" ? "g100" : "g10"]);
    const page = await context.newPage();
    await page.goto(`${DASHBOARD}/`, { waitUntil: "load" });
    await dashboardNav(page).waitFor({ state: "visible" });
    for (const view of DASHBOARD_VIEWS) {
      try {
        if (view.area) await openDashboard(page, view.area);
        else {
          await page.goto(`${DASHBOARD}/work/${await waitingWorkItem()}`, { waitUntil: "load" });
          await page.getByRole("heading").first().waitFor({ state: "visible" });
          await page.waitForTimeout(1500);
        }
        await shoot(page, `dashboard-${view.slug}-${theme}.png`, {
          group, theme, title: view.title, note: view.note,
          alt: `The ${view.title} view of the S.R.C. dashboard, ${theme} theme.`,
        }, view.slug === "approval");
      } catch (error) {
        console.log(`skip: dashboard ${view.title} (${theme}): ${error.message.split("\n")[0]}`);
        skipped.push({ group, title: `${view.title} (${theme})`, reason: "The view did not load in time." });
      }
    }
    await context.close();
  }
  return true;
}

async function capturePaperboat(browser) {
  const group = "paperboat";
  if (!(await isUp(`${PAPERBOAT}/api/healthz`))) {
    const reason = `Paperboat did not answer at ${PAPERBOAT} when the capture ran.`;
    console.log(`skip: paperboat. ${reason}`);
    skipped.push({ group, title: "Inventory, rentals, deliveries, support desk", reason });
    return false;
  }
  const pages = [
    ["inventory", "/", "Live inventory board", "Fish by tank, with status, age and condition. It updates from a live stream."],
    ["rentals", "/rentals", "Rentals", "Who has which fish, and until when."],
    ["deliveries", "/deliveries", "Deliveries", "Deliveries and collections, each with a manifest on the shared file system."],
    ["support", "/support", "Support desk", "Tickets from customers, with each interaction."],
  ];
  // Paperboat has one theme, so one set of pictures.
  for (const theme of ["light"]) {
    const context = await browser.newContext({ viewport: VIEWPORT, colorScheme: theme });
    await makeReadOnly(context);
    const page = await context.newPage();
    for (const [slug, path, title, note] of pages) {
      await page.goto(`${PAPERBOAT}${path}`, { waitUntil: "load" });
      await page.getByRole("main").first().waitFor({ state: "visible" }).catch(() => {});
      await page.waitForTimeout(1200);
      await shoot(page, `paperboat-${slug}-${theme}.png`, {
        group, theme, title, note, alt: `Paperboat, the fish rental application: ${title.toLowerCase()}.`,
      });
    }
    await context.close();
  }
  return true;
}

async function captureNotifications(browser) {
  const group = "notifications";
  const context = await browser.newContext({ viewport: VIEWPORT, colorScheme: "light" });
  const page = await context.newPage();
  if (await isUp(MAIL)) {
    await page.goto(MAIL, { waitUntil: "load" });
    await page.getByText("Approval needed").first().waitFor({ state: "visible", timeout: 8000 }).catch(() => {});
    await shoot(page, "mail-inbox-light.png", {
      group, theme: "light", title: "Mail inbox", note: "The emails that S.R.C. sent to the approvers during the month.",
      alt: "A mail inbox with the notification emails from S.R.C., most of them requests for approval.",
    });
    const message = page.getByText("Approval needed").first();
    if (await message.isVisible().catch(() => false)) {
      await message.click();
      await page.waitForTimeout(2000);
      await shoot(page, "mail-approval-light.png", {
        group, theme: "light", title: "An approval email", note: "What an approver receives: the change in one sentence, and a link to decide.",
        alt: "One notification email from S.R.C. that asks for an approval.",
      });
    }
  } else {
    console.log(`skip: mail inbox. Nothing answered at ${MAIL}.`);
    skipped.push({ group, title: "Mail inbox", reason: `The mail inbox did not answer at ${MAIL}.` });
  }
  if (await isUp(SINK)) {
    await page.goto(SINK, { waitUntil: "load" });
    await page.waitForTimeout(800);
    const details = {
      group, theme: "light", title: "Webhook sink", note: "A local stand-in for Slack, Microsoft Teams and Discord, with the real payloads.",
      alt: "The webhook sink page: a list of the chat notifications that S.R.C. sent.",
    };
    // The sink keeps its payloads in memory. After a restart it is empty, and an empty
    // page is not worth a picture, so an earlier picture stays.
    const empty = (await page.locator("body").innerText()).includes(" 0 payloads");
    if (empty && existsSync(join(SCREENS, "webhook-sink-light.png"))) {
      console.log("keep: webhook sink. It has no payloads now, so the earlier picture stays.");
      shots.push({ file: "webhook-sink-light.png", width: VIEWPORT.width, height: VIEWPORT.height, ...details });
    } else if (empty) {
      console.log("skip: webhook sink. It has no payloads yet.");
      skipped.push({ group, title: "Webhook sink", reason: "The webhook sink had no payloads when the capture ran." });
    } else {
      await shoot(page, "webhook-sink-light.png", details);
    }
  } else {
    console.log(`skip: webhook sink. Nothing answered at ${SINK}.`);
    skipped.push({ group, title: "Webhook sink", reason: `The webhook sink did not answer at ${SINK}.` });
  }
  await context.close();
}

async function recordTour(browser, name, setup = async () => {}) {
  const context = await browser.newContext({
    viewport: VIDEO_SIZE, colorScheme: "dark", recordVideo: { dir: SEGMENTS, size: VIDEO_SIZE },
  });
  await makeReadOnly(context);
  await setup(context);
  const page = await context.newPage();
  try {
    await TOURS[name](page, TOUR_SECONDS);
  } catch (error) {
    console.log(`skip: video ${name}: ${error.message.split("\n")[0]}`);
  }
  const video = page.video();
  await context.close();
  await rename(await video.path(), join(SEGMENTS, `tour-${name}.webm`));
  console.log(`  wrote out/segments/tour-${name}.webm`);
}

async function main() {
  await mkdir(SCREENS, { recursive: true });
  const browser = await chromium.launch();
  try {
    const dashboardUp = await captureDashboard(browser);
    const paperboatUp = await capturePaperboat(browser);
    await captureNotifications(browser);
    if (process.argv.includes("--video")) {
      await mkdir(SEGMENTS, { recursive: true });
      if (paperboatUp) await recordTour(browser, "paperboat");
      if (dashboardUp) {
        const asUser = (context) => context.addInitScript((user) => {
          window.localStorage.setItem("clanker.user", user);
          window.localStorage.setItem("clanker.theme", "g100");
        }, DASHBOARD_USER);
        for (const name of ["dashboard-approval", "dashboard-timeline", "dashboard-access"]) {
          await recordTour(browser, name, asUser);
        }
      } else {
        console.log("skip: dashboard videos. The dashboard is not up.");
      }
    }
  } finally {
    await browser.close();
  }
  const manifest = { captured_at: new Date().toISOString(), shots, skipped };
  await writeFile(join(SCREENS, "manifest.json"), `${JSON.stringify(manifest, null, 1)}\n`);
  console.log(`capture: ${shots.length} screenshots, ${skipped.length} views skipped`);
  execFileSync(process.execPath, [join(SITE_DIR, "build.mjs")], { stdio: "inherit" });
}

main().catch((error) => {
  console.error(`capture: ${error.message}`);
  process.exitCode = 1;
});
