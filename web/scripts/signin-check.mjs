// Sign in to the dashboard in a real browser, through the fake identity provider.
// Usage: node scripts/signin-check.mjs <dashboard address> <provider address> <email> <role>
// Start the control plane with the sign-in settings first; see scripts/verify/README.md.
import { chromium } from "playwright";

const [dashboard, provider, email, role] = process.argv.slice(2);
if (role === undefined) {
  console.error("usage: node scripts/signin-check.mjs <dashboard address> <provider address> <email> <role>");
  process.exit(2);
}

const problems = [];
const person = async (address, verified) => {
  const answer = await fetch(`${provider}/person?email=${encodeURIComponent(address)}&verified=${verified}`, {
    method: "POST",
  });
  if (!answer.ok) problems.push(`the provider answered ${answer.status} to the choice of the account`);
};

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1280, height: 860 } });
page.on("pageerror", (error) => problems.push(`page error: ${error.message}`));

await person(email, true);
await page.goto(`${dashboard}/timeline`);
await page.waitForURL(`${dashboard}/timeline`, { timeout: 15000 });
await page.getByRole("button", { name: "Account", exact: true }).click();
const note = page.getByText(`Signed in as ${email} (${role}).`);
await note.waitFor({ timeout: 15000 }).catch(() => problems.push("the panel does not name the signed-in person"));
if ((await page.getByText("There is no sign-in in this prototype").count()) > 0) {
  problems.push("the panel still offers the user switcher of the demo");
}

// After the sign-out the provider offers an account that it did not confirm, so the next
// sign-in must fail and the dashboard must stay closed.
await person("mallory@example.com", false);
await page.getByText("Sign out", { exact: true }).click();
await page.getByText("Sign-in failed").waitFor({ timeout: 15000 }).catch(() =>
  problems.push("after the sign-out the dashboard did not ask for a new sign-in"));
const closed = await page.request.get(`${dashboard}/api/v1/work-items`);
if (closed.status() !== 401) problems.push(`after the sign-out the API answered ${closed.status()}`);
await browser.close();

if (problems.length > 0) {
  console.error(`sign-in: ${problems.length} problems\n  - ${problems.join("\n  - ")}`);
  process.exit(1);
}
console.log(`sign-in: ${email} signed in as ${role}, signed out, and the dashboard closed`);
