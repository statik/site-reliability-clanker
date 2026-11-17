// Scripted click-throughs of the live applications. capture.mjs records them as they
// are; the video build records them paced to the narration. Each tour takes the page
// and the number of seconds that it must fill, and only navigates and scrolls.

export const DASHBOARD = "http://127.0.0.1:7070";
export const PAPERBOAT = "http://localhost:8088";
export const MAIL = "http://127.0.0.1:8025";
export const SINK = "http://127.0.0.1:7071";

const NAV_TIMEOUT_MS = 8000;

function wait(page, seconds) {
  return page.waitForTimeout(Math.max(0, seconds) * 1000);
}

async function smoothScroll(page, distance, seconds) {
  const steps = Math.max(1, Math.round(seconds * 30));
  for (let step = 0; step < steps; step += 1) {
    await page.mouse.wheel(0, distance / steps);
    await page.waitForTimeout((seconds * 1000) / steps);
  }
}

// The dashboard is "up" when its side navigation is on the page. The server answers 404
// on / until the dashboard is built in, so the status code alone does not tell.
export async function dashboardIsUp(page) {
  try {
    const response = await page.goto(`${DASHBOARD}/`, { waitUntil: "load", timeout: NAV_TIMEOUT_MS });
    if (!response || response.status() >= 400) return false;
    await dashboardNav(page).waitFor({ state: "visible", timeout: NAV_TIMEOUT_MS });
    return true;
  } catch (error) {
    return false;
  }
}

export function dashboardNav(page) {
  return page.getByRole("navigation").filter({ has: page.getByRole("link", { name: "Timeline" }) }).first();
}

// Block every request that could change something. The capture must stay read-only:
// the dashboard has Approve, Demo and runbook buttons. Marking "seen" is answered
// locally so that the page does not show an error.
export async function makeReadOnly(context) {
  await context.route("**/*", async (route) => {
    const request = route.request();
    if (request.method() === "GET" || request.method() === "HEAD") return route.continue();
    if (request.url().endsWith("/me/seen")) return route.fulfill({ status: 200, json: {} });
    console.warn(`  blocked ${request.method()} ${request.url()} (the capture is read-only)`);
    return route.abort("blockedbyclient");
  });
}

export async function openDashboard(page, area) {
  const link = dashboardNav(page).getByRole("link", { name: area, exact: true });
  await link.waitFor({ state: "visible", timeout: NAV_TIMEOUT_MS });
  await link.click();
  await page.getByRole("main").first().waitFor({ state: "visible", timeout: NAV_TIMEOUT_MS });
  await page.waitForTimeout(1200);
}

export async function waitingWorkItem() {
  const response = await fetch(`${DASHBOARD}/api/v1/work-items`, { method: "GET" });
  if (!response.ok) throw new Error(`GET /work-items answered ${response.status}`);
  const items = await response.json();
  const waiting = items.find((item) => item.state === "AwaitingApproval");
  const explained = items.find((item) => item.explanation?.sentence);
  return (waiting ?? explained ?? items[0])?.id;
}

async function workItems() {
  const response = await fetch(`${DASHBOARD}/api/v1/work-items`, { method: "GET" });
  if (!response.ok) return [];
  return response.json();
}

// A step that makes the tour nicer but that the tour can do without.
async function optional(action) {
  try {
    await action();
  } catch (error) {
    console.log(`  tour: skipped an optional step (${error.message.split("\n")[0]})`);
  }
}

export const TOURS = {
  // The managed application: the live inventory board, then the other three pages.
  async paperboat(page, seconds) {
    await page.goto(`${PAPERBOAT}/`, { waitUntil: "load" });
    await wait(page, seconds * 0.3);
    await smoothScroll(page, 500, seconds * 0.15);
    for (const name of ["Rentals", "Deliveries", "Support desk"]) {
      await page.getByRole("link", { name, exact: true }).first().click();
      await page.waitForLoadState("load");
      await wait(page, seconds * 0.15);
    }
    await wait(page, seconds * 0.1);
  },

  // The approval page of the work item that is waiting for a decision.
  async "dashboard-approval"(page, seconds) {
    const id = await waitingWorkItem();
    await page.goto(`${DASHBOARD}/approvals`, { waitUntil: "load" });
    await dashboardNav(page).waitFor({ state: "visible", timeout: NAV_TIMEOUT_MS });
    await wait(page, seconds * 0.15);
    const link = page.getByRole("link", { name: new RegExp(id) }).first();
    if (await link.isVisible().catch(() => false)) await link.click();
    else await page.goto(`${DASHBOARD}/work/${id}`, { waitUntil: "load" });
    await page.getByRole("heading").first().waitFor({ state: "visible", timeout: NAV_TIMEOUT_MS });
    await wait(page, seconds * 0.25);
    await smoothScroll(page, 1400, seconds * 0.45);
    await wait(page, seconds * 0.15);
  },

  // Overview, then the month on the Timeline, then the work items that the narration
  // talks about: the change that was rolled back, an incident, and the change whose
  // approval is asked again. Each is found through the API, not by a fixed number. With
  // captions, each page opens when its sentence starts (the cue of the caption), and a
  // work item that no sentence tells of is not opened.
  async "dashboard-timeline"(page, seconds, captions = []) {
    const began = Date.now();
    const at = (cue, share) => captions.find((caption) => caption.cue === cue)?.offset ?? seconds * share;
    const told = (cue) => captions.length === 0 || captions.some((caption) => caption.cue === cue);
    const until = (offset) => page.waitForTimeout(Math.max(0, began + offset * 1000 - Date.now()));
    const items = await workItems();
    const show = async (offset, item) => {
      if (!item) return;
      await until(offset - 0.3);
      await page.goto(`${DASHBOARD}/work/${item.id}`, { waitUntil: "load" });
      await page.getByRole("heading").first().waitFor({ state: "visible", timeout: NAV_TIMEOUT_MS });
    };
    await page.goto(`${DASHBOARD}/`, { waitUntil: "load" });
    await dashboardNav(page).waitFor({ state: "visible", timeout: NAV_TIMEOUT_MS });
    await until(at("#blank-slate", 0.05) + 1.5);
    await openDashboard(page, "Timeline");
    await optional(() => page.getByRole("button", { name: "60 days", exact: true }).click({ timeout: 2000 }));
    await until(at("#feature-release", 0.2));
    await optional(() => page.getByRole("button", { name: /local/i }).first().click({ timeout: 2000 }));
    await show(at("#bad-release", 0.4), items.find((item) => item.state === "RolledBack"));
    if (told("#incident")) await show(at("#incident", 0.6), items.findLast((item) => item.kind === "incident"));
    if (told("#stale-plan")) {
      await show(at("#stale-plan", 0.8), items.find((item) => item.state === "AwaitingApproval"));
    }
    await until(seconds);
  },

  async "dashboard-access"(page, seconds) {
    await page.goto(`${DASHBOARD}/access`, { waitUntil: "load" });
    await dashboardNav(page).waitFor({ state: "visible", timeout: NAV_TIMEOUT_MS });
    await wait(page, seconds * 0.3);
    await smoothScroll(page, 500, seconds * 0.2);
    await openDashboard(page, "Audit");
    await wait(page, seconds * 0.3);
    await smoothScroll(page, 400, seconds * 0.2);
  },
};
