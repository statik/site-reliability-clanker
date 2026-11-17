// Loads every page of the dashboard in both themes and saves screenshots.
// Usage: start `pnpm preview`, then `pnpm screenshots`. BASE_URL overrides the address.
// It never changes server state: every write is blocked or answered by a mock.
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const BASE = process.env.BASE_URL ?? "http://127.0.0.1:4173";
const OUT = new URL("../screenshots/", import.meta.url).pathname;
const THEMES = [["light", "light"], ["dark", "dark"]];
const problems = [];

async function guard(context) {
  await context.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (request.method() === "GET") {
      return route.continue();
    }
    if (path.endsWith("/me/seen")) {
      // Answered here, so that the catch-up panel keeps its content between runs.
      return route.fulfill({ json: { ok: true } });
    }
    if (path.endsWith("/chat") && process.env.ASK !== undefined) {
      return route.continue();
    }
    if (path.endsWith("/decision")) {
      const body = { error: "dave has role viewer and cannot decide on this work item: " +
        "an approver or admin must decide" };
      return route.fulfill({ status: 403, json: body });
    }
    problems.push(`blocked ${request.method()} ${path}`);
    return route.abort();
  });
}

async function api(page, path) {
  const response = await page.request.get(`${BASE}/api/v1${path}`);
  return response.ok() ? response.json() : null;
}

async function shot(page, theme, name) {
  await page.waitForLoadState("networkidle").catch(() => undefined);
  await page.waitForTimeout(700);
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({ path: `${OUT}${name}-${theme}.png`, fullPage: true });
  console.log(`saved ${name}-${theme}.png`);
}

async function visit(page, theme, name, path) {
  await page.goto(`${BASE}${path}`);
  await shot(page, theme, name);
}

async function workItemShots(page, theme, items) {
  const waiting = items.find((item) => item.state === "AwaitingApproval");
  const withPull = items.find((item) => item.pr_id !== 0 && item.state === "Done");
  const incident = items.find((item) => item.kind === "incident");
  const rolledBack = items.find((item) => item.state === "RolledBack");
  const target = waiting ?? withPull ?? items[0];
  if (target === undefined) {
    return;
  }
  await visit(page, theme, "work-item", `/work/${target.id}`);
  await page.getByRole("button", { name: "Request changes" }).click();
  await shot(page, theme, "work-item-comment-required");
  await page.getByRole("button", { name: "Approve", exact: true }).click();
  await page.getByRole("dialog").waitFor();
  await shot(page, theme, "work-item-confirm");
  await page.getByRole("dialog").getByRole("button", { name: "Approve" }).click();
  await shot(page, theme, "work-item-refused");
  await page.getByRole("tab", { name: /Jobs and logs/ }).click();
  const job = page.locator(".cds--accordion__heading", { hasText: /^.*Job \d+/ }).first();
  if (await job.count() > 0) {
    await job.click();
  }
  await shot(page, theme, "work-item-jobs");
  await page.getByRole("tab", { name: /Events/ }).click();
  await shot(page, theme, "work-item-events");
  if (withPull !== undefined && withPull.id !== target.id) {
    await visit(page, theme, "work-item-done", `/work/${withPull.id}`);
  }
  if (incident !== undefined) {
    await visit(page, theme, "work-item-incident", `/work/${incident.id}`);
  }
  if (rolledBack !== undefined) {
    await visit(page, theme, "work-item-rolled-back", `/work/${rolledBack.id}`);
  }
}

async function timelineShots(page, theme) {
  await visit(page, theme, "timeline", "/timeline");
  await page.getByRole("button", { name: /Expand the layers/ }).first().click();
  await shot(page, theme, "timeline-expanded");
  await page.getByRole("button", { name: "7 days" }).click();
  await shot(page, theme, "timeline-7-days");
  await page.getByRole("button", { name: "60 days" }).click();
  const scrubber = page.getByRole("slider", { name: /scrubber/ });
  await scrubber.focus();
  for (let step = 0; step < 3; step += 1) {
    await page.keyboard.press("Shift+ArrowLeft");
  }
  await page.getByRole("button", { name: "View the dashboard as of the scrubber" }).click();
  await shot(page, theme, "timeline-as-of");
  await page.getByRole("link", { name: "Topology" }).click();
  await shot(page, theme, "topology-as-of");
  await page.locator("#topology-compare").click({ force: true });
  await shot(page, theme, "topology-compare");
  await page.getByRole("link", { name: "Overview" }).click();
  await shot(page, theme, "overview-as-of");
  await page.getByRole("link", { name: "Activity" }).click();
  await shot(page, theme, "activity-as-of");
}

function check(condition, message) {
  console.log(`${condition ? "ok  " : "FAIL"} ${message}`);
  if (!condition) {
    problems.push(`check failed: ${message}`);
  }
}

const axisText = (page) => page.locator(".tl-axis .tick text").allTextContents()
  .then((texts) => texts.join("|"));

async function dragAcross(page, box, fromRatio, toRatio) {
  const y = box.y + 78;
  await page.mouse.move(box.x + box.width * fromRatio, y);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * toRatio, y, { steps: 8 });
  await page.mouse.up();
  await page.waitForTimeout(400);
}

/** Exercises the pointer paths of the timeline: wheel zoom, drag pan, brush and markers. */
async function timelineChecks(page, theme) {
  await page.goto(`${BASE}/timeline`);
  await page.locator(".tl-marker").first().waitFor();
  const box = await page.locator(".tl-svg").boundingBox();
  let before = await axisText(page);
  await page.mouse.move(box.x + box.width * 0.6, box.y + 78);
  await page.mouse.wheel(0, -900);
  await page.waitForTimeout(400);
  check(await axisText(page) !== before, "the wheel zooms the timeline");
  before = await axisText(page);
  await dragAcross(page, box, 0.7, 0.4);
  check(await axisText(page) !== before, "a drag pans the timeline");
  await page.getByRole("button", { name: "60 days" }).click();
  await page.locator("#tl-select").click({ force: true });
  await dragAcross(page, box, 0.45, 0.75);
  const selected = await page.getByText(/events$/).first().textContent().catch(() => null);
  check(selected !== null, `a brush selects a range (${selected})`);
  await shot(page, theme, "timeline-range");
  await page.getByRole("button", { name: "Clear" }).click();
  await page.locator("#tl-select").click({ force: true });
  const single = page.locator(".tl-svg .tl-marker:has(.tl-outcome-Done)");
  let zoomed = false;
  const onWorkItem = () => /\/work\/WI-/.test(page.url());
  // A cluster zooms in until its markers come apart. A cluster of one work item opens it.
  for (let step = 0; step < 12 && !onWorkItem() && await single.count() === 0; step += 1) {
    before = await axisText(page);
    await shot(page, theme, "timeline-zoomed");
    await page.locator(".tl-svg .tl-marker:has(.tl-cluster)").last().click();
    await page.waitForTimeout(300);
    zoomed ||= !onWorkItem() && await axisText(page) !== before;
  }
  check(zoomed, "a click on a cluster zooms in");
  if (!onWorkItem()) {
    await shot(page, theme, "timeline-zoomed");
    // On a short history every finished change can stay inside a cluster.
    if (await single.count() > 0) {
      await single.first().click();
      await page.waitForURL(/\/work\/WI-/, { timeout: 5000 }).catch(() => undefined);
    }
  }
  check(onWorkItem(), "a click on a marker or a one-item cluster opens its work item");
}

/** Checks that an event on the stream makes a view load again, and the evidence controls. */
async function liveAndEvidenceChecks(context, page, theme, items) {
  const live = await context.newPage();
  const event = { seq: 1, ts: new Date().toISOString(), type: "workitem.state_changed",
    actor: "harness", summary: "synthetic", data: {} };
  await live.route("**/api/v1/events/stream", (route) => route.fulfill({
    contentType: "text/event-stream",
    body: `: connected\n\nid: 1\nevent: audit\ndata: ${JSON.stringify(event)}\n\n`,
  }));
  let loads = 0;
  live.on("request", (request) => {
    if (new URL(request.url()).pathname === "/api/v1/work-items") {
      loads += 1;
    }
  });
  await live.goto(`${BASE}/activity`);
  await live.waitForTimeout(4000);
  check(loads >= 2, `a stream event reloads the work items (${loads} loads)`);
  await live.close();

  const withPull = items.find((item) => item.pr_id !== 0);
  if (withPull !== undefined) {
    await page.goto(`${BASE}/work/${withPull.id}`);
    await page.getByRole("button", { name: "Pull request diff" }).click();
    await page.getByRole("button", { name: /Raw plan JSON/ }).first().click();
    await page.getByLabel(/Raw plan JSON of/).first().waitFor();
    check(await page.getByLabel(/^Diff of /).count() > 0, "the pull request diff opens");
    await shot(page, theme, "work-item-evidence");
  }
  await page.goto(`${BASE}/audit`);
  const download = page.waitForEvent("download", { timeout: 20_000 }).catch(() => null);
  await page.getByRole("button", { name: "Download evidence pack" }).click();
  const file = await download;
  check(file !== null && /clanker-evidence-.*\.json/.test(file.suggestedFilename()),
    "the evidence pack downloads as JSON");
}

async function run(theme, colorScheme) {
  const browser = await chromium.launch({ channel: "chromium" });
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 },
    colorScheme });
  await guard(context);
  const page = await context.newPage();
  page.on("console", (message) => {
    // The mocked refusal of a decision is an expected 403.
    if (message.type() === "error" && !message.text().includes("status of 403")) {
      problems.push(`[${theme}] console: ${message.text().slice(0, 300)}`);
    }
  });
  page.on("pageerror", (error) => problems.push(`[${theme}] page error: ${error.message}`));
  const items = (await api(page, "/work-items")) ?? [];
  const pulls = (await api(page, "/forge/pulls")) ?? [];

  await visit(page, theme, "overview", "/");
  await timelineShots(page, theme);
  await visit(page, theme, "topology", "/topology");
  await visit(page, theme, "upcoming", "/upcoming");
  await visit(page, theme, "approvals", "/approvals");
  await workItemShots(page, theme, items);
  await visit(page, theme, "activity", "/activity");
  await page.getByRole("button", { name: /expand current row/i }).first().click();
  await shot(page, theme, "activity-expanded");
  await visit(page, theme, "code", "/code");
  await visit(page, theme, "code-repo",
    "/code/runbooks?ref=main&path=notifications/routes.json&file=1");
  if (pulls[0] !== undefined) {
    await visit(page, theme, "code-pull", `/code/pulls/${pulls[0].id}`);
  }
  await visit(page, theme, "access", "/access");
  await visit(page, theme, "audit", "/audit");
  await page.getByRole("searchbox").fill("grant");
  await shot(page, theme, "audit-search");
  await visit(page, theme, "notifications", "/notifications");
  await visit(page, theme, "ask", "/ask");
  if (process.env.ASK === theme) {
    // One real question. The answer is written by the server's model, so this is opt-in.
    await page.getByRole("button", { name: /have access to right now/ }).click();
    await page.locator(".app-chat-answer").first().waitFor({ timeout: 120_000 });
    await shot(page, theme, "ask");
  }
  const aiLabel = page.locator(".app-chat-answer .cds--ai-label button").first();
  if (await aiLabel.count() > 0) {
    await aiLabel.click();
    await page.waitForTimeout(500);
    await page.screenshot({ path: `${OUT}ask-ai-label-${theme}.png` });
  }
  await visit(page, theme, "demo", "/demo");
  await page.getByRole("button", { name: "Switch user" }).click();
  await page.screenshot({ path: `${OUT}user-switcher-${theme}.png` });
  if (theme === "light") {
    await timelineChecks(page, theme);
    await liveAndEvidenceChecks(context, page, theme, items);
  }
  await browser.close();
}

await mkdir(OUT, { recursive: true });
for (const [theme, colorScheme] of THEMES) {
  if (process.env.THEME === undefined || process.env.THEME === theme) {
    await run(theme, colorScheme);
  }
}
if (problems.length > 0) {
  console.log(`\n${problems.length} problems:`);
  for (const problem of new Set(problems)) {
    console.log(`  ${problem}`);
  }
  process.exitCode = 1;
}
