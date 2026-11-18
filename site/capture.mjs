#!/usr/bin/env node
// Take real screenshots of the running demo into screens/.
//
//   node capture.mjs            screenshots of everything that is up
//
// Targets: the S.R.C. dashboard (http://127.0.0.1:7070), Paperboat
// (http://localhost:8088), the mail inbox (http://127.0.0.1:8025) and the webhook sink
// (http://127.0.0.1:7071). A target that is not up is skipped with a message and listed
// in screens/manifest.json, so the gallery says what is missing. Run it again when the
// target is up. The script never sends a request that changes the control plane: every
// browser context blocks each request that is not a GET.

import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { chromium } from "playwright";

import { SITE_DIR } from "./lib/serve.mjs";
import {
  DASHBOARD, MAIL, PAPERBOAT, SINK, dashboardIsUp, dashboardNav, makeReadOnly, openDashboard,
  workItemToShow,
} from "./lib/demo.mjs";

const SCREENS = join(SITE_DIR, "screens");
const VIEWPORT = { width: 1440, height: 900 };
// A full-page picture is cut at this height. The page of a work item is about 3500 high.
const MAX_HEIGHT = 4000;
const DASHBOARD_USER = "alice";

const DASHBOARD_VIEWS = [
  { slug: "overview", area: "Overview", title: "Overview", note: "The health of each environment, and the changes that wait for a decision." },
  { slug: "timeline", area: "Timeline", title: "Timeline", note: "The work of the past weeks by layer, and the maintenance that is coming." },
  { slug: "work-item", area: null },
  { slug: "topology", area: "Topology", title: "Topology", note: "The parts of the environment and the connections between them." },
  { slug: "access", area: "Access", title: "Access", note: "The grants that are open now, and every grant that ended." },
  { slug: "audit", area: "Audit", title: "Audit", note: "The event log, with a search and the check of its hash chain." },
];

// What the picture of a work item page shows depends on the demo: a change that waits for
// a decision, or one that was decided.
const WORK_ITEM_WAITING = {
  title: "Approval page",
  note: "A change that waits for a decision: the summary, the impact, the tier, the plan, and the buttons to decide.",
};
const WORK_ITEM_DECIDED = {
  title: "Work item page, after the decision",
  note: "A finished change: the summary, the impact, the tier, the plan, and who approved it. No change waited for a decision when this picture was taken.",
};

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

async function openWorkItem(page) {
  const item = await workItemToShow();
  await page.goto(`${DASHBOARD}/work/${item.id}`, { waitUntil: "load" });
  await page.getByRole("heading").first().waitFor({ state: "visible" });
  await page.waitForTimeout(1500);
  return item.waiting ? WORK_ITEM_WAITING : WORK_ITEM_DECIDED;
}

async function captureDashboard(browser) {
  const group = "dashboard";
  const probe = await browser.newContext({ viewport: VIEWPORT });
  const up = await dashboardIsUp(await probe.newPage());
  await probe.close();
  if (!up) {
    const reason = `The dashboard did not answer at ${DASHBOARD}/ when the capture ran.`;
    console.log(`skip: dashboard. ${reason} Run this script again when it is up.`);
    for (const view of DASHBOARD_VIEWS) skipped.push({ group, title: view.title ?? "Work item page", reason });
    return;
  }
  const context = await browser.newContext({
    viewport: VIEWPORT, extraHTTPHeaders: { "X-Clanker-User": DASHBOARD_USER },
  });
  await makeReadOnly(context);
  await context.addInitScript((user) => {
    window.localStorage.setItem("clanker.user", user);
  }, DASHBOARD_USER);
  const page = await context.newPage();
  await page.goto(`${DASHBOARD}/`, { waitUntil: "load" });
  await dashboardNav(page).waitFor({ state: "visible" });
  for (const view of DASHBOARD_VIEWS) {
    try {
      let words = view;
      if (view.area) await openDashboard(page, view.area);
      else words = await openWorkItem(page);
      await shoot(page, `dashboard-${view.slug}.png`, {
        group, title: words.title, note: words.note, alt: `The S.R.C. dashboard: ${words.title.toLowerCase()}.`,
      }, !view.area);
    } catch (error) {
      console.log(`skip: dashboard ${view.title ?? "work item page"}: ${error.message.split("\n")[0]}`);
      skipped.push({ group, title: view.title ?? "Work item page", reason: "The view did not load in time." });
    }
  }
  await context.close();
}

async function capturePaperboat(browser) {
  const group = "paperboat";
  if (!(await isUp(`${PAPERBOAT}/api/healthz`))) {
    const reason = `Paperboat did not answer at ${PAPERBOAT} when the capture ran.`;
    console.log(`skip: paperboat. ${reason}`);
    skipped.push({ group, title: "Inventory, rentals, deliveries, support desk", reason });
    return;
  }
  const pages = [
    ["inventory", "/", "Live inventory board", "Fish by tank, with status, age and condition. It updates from a live stream."],
    ["rentals", "/rentals", "Rentals", "Who has which fish, and until when."],
    ["deliveries", "/deliveries", "Deliveries", "Deliveries and collections, each with a manifest on the shared file system."],
    ["support", "/support", "Support desk", "Tickets from customers, with each interaction."],
  ];
  const context = await browser.newContext({ viewport: VIEWPORT });
  await makeReadOnly(context);
  const page = await context.newPage();
  for (const [slug, path, title, note] of pages) {
    await page.goto(`${PAPERBOAT}${path}`, { waitUntil: "load" });
    await page.getByRole("main").first().waitFor({ state: "visible" }).catch(() => {});
    await page.waitForTimeout(1200);
    await shoot(page, `paperboat-${slug}.png`, {
      group, title, note, alt: `Paperboat, the fish rental application: ${title.toLowerCase()}.`,
    });
  }
  await context.close();
}

async function captureNotifications(browser) {
  const group = "notifications";
  const context = await browser.newContext({ viewport: VIEWPORT });
  await makeReadOnly(context);
  const page = await context.newPage();
  if (await isUp(MAIL)) {
    await page.goto(MAIL, { waitUntil: "load" });
    await page.getByText("Approval needed").first().waitFor({ state: "visible", timeout: 8000 }).catch(() => {});
    await shoot(page, "mail-inbox.png", {
      group, title: "Mail inbox", note: "The emails that S.R.C. sent to the approvers during the month.",
      alt: "A mail inbox with the notification emails from S.R.C., most of them requests for approval.",
    });
    const message = page.getByText("Approval needed").first();
    if (await message.isVisible().catch(() => false)) {
      await message.click();
      await page.waitForTimeout(2000);
      await shoot(page, "mail-approval.png", {
        group, title: "An approval email", note: "What an approver receives: the change in one sentence, and a link to decide.",
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
      group, title: "Webhook sink", note: "A local stand-in for Slack, Microsoft Teams and Discord, with the real payloads.",
      alt: "The webhook sink page: a list of the chat notifications that S.R.C. sent.",
    };
    // The sink keeps its payloads in memory. After a restart it is empty, and an empty
    // page is not worth a picture, so an earlier picture stays.
    const empty = (await page.locator("body").innerText()).includes(" 0 payloads");
    if (empty && existsSync(join(SCREENS, "webhook-sink.png"))) {
      console.log("keep: webhook sink. It has no payloads now, so the earlier picture stays.");
      const note = `${details.note} This picture is from an earlier run: the sink was empty when the other pictures were taken.`;
      shots.push({ file: "webhook-sink.png", width: VIEWPORT.width, height: VIEWPORT.height, ...details, note });
    } else if (empty) {
      console.log("skip: webhook sink. It has no payloads yet.");
      skipped.push({ group, title: "Webhook sink", reason: "The webhook sink had no payloads when the capture ran." });
    } else {
      await shoot(page, "webhook-sink.png", details);
    }
  } else {
    console.log(`skip: webhook sink. Nothing answered at ${SINK}.`);
    skipped.push({ group, title: "Webhook sink", reason: `The webhook sink did not answer at ${SINK}.` });
  }
  await context.close();
}

async function main() {
  await mkdir(SCREENS, { recursive: true });
  const browser = await chromium.launch();
  try {
    await captureDashboard(browser);
    await capturePaperboat(browser);
    await captureNotifications(browser);
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
