// Sign in to the dashboard in a real browser, through Keycloak. scripts/verify/keycloak-signin-check.sh
// starts the core and Keycloak and runs this script; see that script.
// Usage:
//   node scripts/keycloak-signin-check.mjs signin <dashboard> <admin email> <approver email> <outsider email> <device page>
//   node scripts/keycloak-signin-check.mjs unverified <dashboard> <approver email>
// The passwords of the accounts come from REALM_ADMIN_PASSWORD, REALM_APPROVER_PASSWORD and
// REALM_OUTSIDER_PASSWORD. The core knows the approver from CLANKER_APPROVER_EMAILS; the
// outsider has no role in the core.
import { chromium } from "playwright";

const [mode, dashboard, ...rest] = process.argv.slice(2);
const usage =
  "usage: node scripts/keycloak-signin-check.mjs signin <dashboard> <admin email> <approver email> <outsider email> <device page>\n" +
  "       node scripts/keycloak-signin-check.mjs unverified <dashboard> <approver email>";
if (!(mode === "signin" && rest.length === 4) && !(mode === "unverified" && rest.length === 1)) {
  console.error(usage);
  process.exit(2);
}

const problems = [];
const evidence = (line) => console.log(`  ${line}`);

// signIn opens the dashboard in a new browser context, so that no earlier session at the
// core or at Keycloak counts, and signs in on the login page of Keycloak.
const signIn = async (browser, email, password) => {
  const context = await browser.newContext({ viewport: { width: 1280, height: 860 } });
  const page = await context.newPage();
  page.on("pageerror", (error) => problems.push(`page error: ${error.message}`));
  await page.goto(`${dashboard}/timeline`);
  const form = page.locator("#username");
  if (!(await form.waitFor({ timeout: 15000 }).then(() => true, () => false))) {
    problems.push(`no login form of Keycloak at ${page.url()}: ${(await page.innerText("body")).slice(0, 300)}`);
    return { context, page, landed: false };
  }
  const at = new URL(page.url());
  evidence(`the dashboard sent the browser to ${at.origin}${at.pathname}`);
  await form.fill(email);
  await page.locator("#password").fill(password);
  await page.locator("#kc-login").click();
  const landed = await page
    .waitForURL((address) => address.origin === new URL(dashboard).origin, { timeout: 15000 })
    .then(() => true, () => false);
  if (!landed) {
    problems.push(`Keycloak did not send ${email} back to the dashboard; the page at ${page.url()} says: ` +
      (await page.innerText("body")).slice(0, 300));
  }
  return { context, page, landed };
};

// sessionCookie reports how the browser keeps the session cookie of the core.
const sessionCookie = async (context) => {
  const cookies = await context.cookies();
  const session = cookies.find((cookie) => cookie.name === "clanker_session");
  if (session === undefined) {
    problems.push("the browser did not keep the session cookie clanker_session");
    return;
  }
  evidence(`clanker_session: Secure=${session.secure} HttpOnly=${session.httpOnly} ` +
    `SameSite=${session.sameSite} Path=${session.path}`);
  if (session.secure) problems.push("the session cookie is Secure on a dashboard served over plain HTTP");
  const others = cookies.filter((cookie) => !cookie.name.startsWith("clanker_"));
  evidence(`cookies of Keycloak: ${others.map((c) => `${c.name} (Secure=${c.secure} SameSite=${c.sameSite})`).join(", ")}`);
};

// approveDevice approves the code of clanker login on the page of the core.
const approveDevice = async (page, email, devicePage) => {
  await page.goto(devicePage);
  const code = new URL(devicePage).searchParams.get("user_code");
  await page.getByText(code).waitFor({ timeout: 15000 }).catch(() =>
    problems.push(`the page of the device flow does not show the code ${code}`));
  await page.getByRole("button", { name: "Approve", exact: true }).click();
  const done = `The program is signed in as ${email}.`;
  if (await page.getByText(done).waitFor({ timeout: 15000 }).then(() => true, () => false)) {
    evidence(`device page: "${done}"`);
  } else {
    problems.push("the approval of the code of clanker login did not work in the browser");
  }
};

// checkApprover signs in the approver of CLANKER_APPROVER_EMAILS. The core must know the
// person as an approver, so that the admin and this person are the two approvers of a change
// of tier T3.
const checkApprover = async (browser, adminEmail, approverEmail) => {
  const approver = await signIn(browser, approverEmail, process.env.REALM_APPROVER_PASSWORD ?? "");
  if (!approver.landed) return;
  const me = await approver.page.request.get(`${dashboard}/api/v1/me`);
  const body = (await me.text()).trim();
  evidence(`GET /api/v1/me as ${approverEmail}: ${me.status()} ${body}`);
  if (!me.ok() || !body.includes('"role":"approver"') || !body.includes(`"email":"${approverEmail}"`)) {
    problems.push(`the core does not know ${approverEmail} as an approver`);
  }
  const items = await approver.page.request.get(`${dashboard}/api/v1/work-items`);
  evidence(`GET /api/v1/work-items as ${approverEmail}: ${items.status()}`);
  if (!items.ok()) problems.push(`the work items answered ${items.status()} to the approver`);
  const users = await approver.page.request.get(`${dashboard}/api/v1/users`);
  const deciders = users.ok()
    ? (await users.json()).filter((u) => u.role === "admin" || u.role === "approver").map((u) => `${u.email} (${u.role})`)
    : [];
  evidence(`people who can approve: ${deciders.join(", ")}`);
  const want = [`${adminEmail} (admin)`, `${approverEmail} (approver)`];
  if (deciders.length !== 2 || !want.every((one) => deciders.includes(one))) {
    problems.push(`the core does not have exactly the two approvers ${want.join(" and ")}`);
  }
};

const checkSignIn = async (browser, adminEmail, approverEmail, outsiderEmail, devicePage) => {
  const admin = await signIn(browser, adminEmail, process.env.REALM_ADMIN_PASSWORD ?? "");
  if (admin.landed) {
    await admin.page.waitForURL(`${dashboard}/timeline`, { timeout: 15000 }).catch(() =>
      problems.push(`the sign-in did not end on the page the browser asked for, but on ${admin.page.url()}`));
    await admin.page.getByRole("button", { name: "Account", exact: true }).click();
    const note = `Signed in as ${adminEmail} (admin).`;
    if (await admin.page.getByText(note).waitFor({ timeout: 15000 }).then(() => true, () => false)) {
      evidence(`dashboard: "${note}"`);
    } else {
      problems.push("the account panel does not name the admin");
    }
    await sessionCookie(admin.context);
    const me = await admin.page.request.get(`${dashboard}/api/v1/me`);
    const body = await me.text();
    evidence(`GET /api/v1/me with the browser session: ${me.status()} ${body.trim()}`);
    if (!me.ok() || !body.includes('"role":"admin"')) problems.push("the browser session does not act as the admin");
    await approveDevice(admin.page, adminEmail, devicePage);
  }

  await checkApprover(browser, adminEmail, approverEmail);

  const outsider = await signIn(browser, outsiderEmail, process.env.REALM_OUTSIDER_PASSWORD ?? "");
  if (outsider.landed) {
    const me = await outsider.page.request.get(`${dashboard}/api/v1/me`);
    const body = (await me.text()).trim();
    evidence(`GET /api/v1/me as ${outsiderEmail}, who is not in the users table: ${me.status()} ${body}`);
    if (me.status() !== 403) problems.push(`the core answered ${me.status()} and not 403 to a person who is not in the users table`);
    const items = await outsider.page.request.get(`${dashboard}/api/v1/work-items`);
    if (items.status() !== 403) problems.push(`the work items answered ${items.status()} to a person who is not in the users table`);
  }
};

// checkUnverified signs in a person whose email address Keycloak did not confirm. The core
// must refuse the sign-in.
const checkUnverified = async (browser, email) => {
  const person = await signIn(browser, email, process.env.REALM_APPROVER_PASSWORD ?? "");
  const text = (await person.page.innerText("body")).trim();
  evidence(`the core answered: ${text}`);
  if (!text.startsWith("Sign-in failed") || !text.includes("email_verified")) {
    problems.push("the core did not refuse an email address that Keycloak did not confirm");
  }
};

const browser = await chromium.launch();
if (mode === "signin") {
  await checkSignIn(browser, ...rest);
} else {
  await checkUnverified(browser, rest[0]);
}
await browser.close();

if (problems.length > 0) {
  console.error(`browser (${mode}): ${problems.length} problems\n  - ${problems.join("\n  - ")}`);
  process.exit(1);
}
