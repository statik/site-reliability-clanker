// Checks the dashboard on a phone (390 px) and a tablet (768 px): no page scrolls sideways,
// the navigation is a drawer that the menu button opens and closes, and a finger can hit the
// controls. Saves a screenshot of every page.
// Usage: start `pnpm preview`, then `node scripts/phone-check.mjs`. BASE_URL overrides the
// address and OUT the directory. It never changes server state: every request that is not a
// GET is blocked.
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { chromium } from "playwright";

const BASE = process.env.BASE_URL ?? "http://127.0.0.1:4173";
const OUT = process.env.OUT ?? new URL("../screenshots/phone/", import.meta.url).pathname;
const WIDTHS = [390, 768];
const TAP = 44;
let failures = 0;

function check(condition, message) {
  console.log(`${condition ? "ok  " : "FAIL"} ${message}`);
  if (!condition) {
    failures += 1;
  }
}

async function pages(page) {
  const items = await api(page, "/work-items");
  const pulls = await api(page, "/forge/pulls");
  const routes = {
    overview: "/", timeline: "/timeline", topology: "/topology", upcoming: "/upcoming",
    approvals: "/approvals", activity: "/activity", code: "/code",
    "code-repo": "/code/runbooks?ref=main&path=notifications/routes.json&file=1",
    access: "/access", audit: "/audit", notifications: "/notifications", ask: "/ask",
    demo: "/demo",
  };
  const withPull = items.find((item) => item.pr_id !== 0) ?? items[0];
  if (withPull !== undefined) {
    routes["work-item"] = `/work/${withPull.id}`;
  }
  if (pulls[0] !== undefined) {
    routes["code-pull"] = `/code/pulls/${pulls[0].id}`;
  }
  return Object.entries(routes);
}

async function api(page, path) {
  const response = await page.request.get(`${BASE}/api/v1${path}`);
  return response.ok() ? response.json() : [];
}

/** Opens what a page keeps folded: a row of a table, the diff, the plan JSON and a job log. */
async function unfold(page, name) {
  const folded = {
    activity: [page.getByRole("button", { name: /expand current row/i }).first()],
    "work-item": [
      page.getByRole("button", { name: "Pull request diff" }),
      page.getByRole("button", { name: /Raw plan JSON/ }).first(),
      page.locator(".cds--accordion__heading", { hasText: /Job \d+/ }).first(),
    ],
  };
  for (const control of folded[name] ?? []) {
    if (await control.count() > 0) {
      await control.click();
      await page.waitForTimeout(500);
    }
  }
}

async function settle(page) {
  await page.waitForLoadState("networkidle").catch(() => undefined);
  await page.waitForTimeout(700);
}

async function widthChecks(page, width) {
  for (const [name, path] of await pages(page)) {
    await page.goto(`${BASE}${path}`);
    await settle(page);
    await unfold(page, name);
    const scrollWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    check(scrollWidth <= width, `${width} px ${name}: scrollWidth ${scrollWidth}`);
    await page.screenshot({ path: join(OUT, `${name}-${width}.png`), fullPage: true });
  }
}

const menuButton = (page) => page.getByRole("button", { name: /the navigation$/ });
const navigation = (page) => page.getByRole("navigation", { name: "Dashboard areas" });
const state = (page) => page.evaluate(() => {
  const button = document.querySelector(".cds--header__menu-toggle");
  const nav = document.querySelector("#app-navigation");
  return {
    label: button.getAttribute("aria-label"),
    expanded: button.getAttribute("aria-expanded"),
    navWidth: nav.getBoundingClientRect().width,
    navInert: nav.hasAttribute("inert"),
    mainInert: document.querySelector("#main-content").hasAttribute("inert"),
    focusInNav: nav.contains(document.activeElement),
    focusOnButton: document.activeElement === button,
  };
});

async function expectClosed(page, how) {
  await page.waitForTimeout(400);
  const now = await state(page);
  check(now.navWidth === 0 && now.navInert && !now.mainInert && now.expanded === "false" &&
    now.label === "Open the navigation", `${how}: the navigation is closed (${JSON.stringify(now)})`);
  return now;
}

async function open(page) {
  await menuButton(page).click();
  await page.waitForTimeout(400);
}

async function drawerChecks(page) {
  await page.goto(`${BASE}/`);
  await settle(page);
  await expectClosed(page, "on load");
  await open(page);
  const opened = await state(page);
  check(opened.navWidth >= 200 && !opened.navInert && opened.expanded === "true" &&
    opened.label === "Close the navigation", `the menu button opens it (${JSON.stringify(opened)})`);
  check(opened.focusInNav, "focus is in the navigation when it opens");
  check(opened.mainInert, "the page behind cannot take focus while it is open");
  await page.screenshot({ path: join(OUT, "navigation-open-390.png") });
  const small = await page.evaluate((least) =>
    [...document.querySelectorAll(".cds--header__action, #app-navigation a")]
      .filter((element) => element.getBoundingClientRect().width > 0)
      .map((element) => ({ name: element.getAttribute("aria-label") ?? element.textContent,
        box: element.getBoundingClientRect() }))
      .filter(({ box }) => box.width < least || box.height < least)
      .map(({ name, box }) => `${name} ${box.width}x${box.height}`), TAP);
  check(small.length === 0, `header buttons and navigation links are ${TAP} px or more (${small})`);

  await page.keyboard.press("Tab");
  const first = await page.evaluate(() => document.activeElement?.textContent);
  check(first === "Overview", `Tab goes to the first link (${first})`);
  await page.keyboard.press("Escape");
  const afterEscape = await expectClosed(page, "Escape");
  check(afterEscape.focusOnButton, "focus is back on the menu button after Escape");

  await open(page);
  await page.mouse.click(370, 500);
  await expectClosed(page, "a tap outside");

  await open(page);
  await navigation(page).getByRole("link", { name: "Timeline" }).click();
  await page.waitForURL(/\/timeline$/);
  await expectClosed(page, "a choice of a page");

  await open(page);
  await menuButton(page).click();
  await expectClosed(page, "the menu button again");

  const header = await page.evaluate(() => {
    const tops = [...document.querySelectorAll(".cds--header__action, .cds--header__name")]
      .map((element) => element.getBoundingClientRect())
      .filter((box) => box.width > 0);
    return { rows: new Set(tops.map((box) => Math.round(box.top))).size,
      right: Math.max(...tops.map((box) => box.right)),
      height: document.querySelector(".cds--header").getBoundingClientRect().height };
  });
  check(header.rows === 1 && header.right <= 390 && header.height === 48,
    `the header is one line inside the window (${JSON.stringify(header)})`);
}

async function wideChecks(browser) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  await guard(context);
  const page = await context.newPage();
  await page.goto(`${BASE}/`);
  await settle(page);
  const wide = await page.evaluate(() => ({
    button: document.querySelector(".cds--header__menu-toggle").getBoundingClientRect().width,
    nav: document.querySelector("#app-navigation").getBoundingClientRect().width,
    navInert: document.querySelector("#app-navigation").hasAttribute("inert"),
    content: document.querySelector("#main-content").getBoundingClientRect().left,
  }));
  check(wide.button === 0 && wide.nav === 256 && !wide.navInert && wide.content === 256,
    `at 1440 px the navigation is open and there is no menu button (${JSON.stringify(wide)})`);
  await context.close();
}

async function guard(context) {
  await context.route("**/*", (route) =>
    (route.request().method() === "GET" ? route.continue() : route.abort()));
}

await mkdir(OUT, { recursive: true });
const browser = await chromium.launch({ channel: "chromium" });
for (const width of WIDTHS) {
  const context = await browser.newContext({ viewport: { width, height: 844 }, hasTouch: true });
  await guard(context);
  const page = await context.newPage();
  await widthChecks(page, width);
  if (width === 390) {
    await drawerChecks(page);
  }
  await context.close();
}
await wideChecks(browser);
await browser.close();
if (failures > 0) {
  console.log(`\n${failures} checks failed`);
  process.exitCode = 1;
}
