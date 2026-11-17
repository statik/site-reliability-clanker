// The addresses of the running demo, and what capture.mjs needs to open its dashboard
// without changing anything.

export const DASHBOARD = "http://127.0.0.1:7070";
export const PAPERBOAT = "http://localhost:8088";
export const MAIL = "http://127.0.0.1:8025";
export const SINK = "http://127.0.0.1:7071";

const NAV_TIMEOUT_MS = 8000;

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
