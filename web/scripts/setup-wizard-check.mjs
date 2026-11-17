// Walk through the setup wizard for the local target in a real browser.
// Usage: node scripts/setup-wizard-check.mjs <wizard address with token> [screenshot directory]
// Start the wizard first: clanker setup --no-browser
// The script stops at the hand-over page. It does not approve anything.
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { chromium } from "playwright";

const address = process.argv[2];
const shots = process.argv[3];
if (address === undefined || !address.includes("token=")) {
  console.error("usage: node scripts/setup-wizard-check.mjs <wizard address with token> [screenshot directory]");
  process.exit(2);
}
if (shots !== undefined) await mkdir(shots, { recursive: true });

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1280, height: 860 } });
const problems = [];
page.on("console", (message) => {
  if (message.type() === "error") problems.push(`console error: ${message.text()}`);
});
page.on("pageerror", (error) => problems.push(`page error: ${error.message}`));
page.on("request", (request) => {
  const host = new URL(request.url()).hostname;
  if (host !== "127.0.0.1" && host !== "localhost") problems.push(`request to another host: ${request.url()}`);
});

let count = 0;
const shoot = async (name) => {
  count += 1;
  if (shots !== undefined) await page.screenshot({ path: join(shots, `${count}-${name}.png`), fullPage: true });
};
const heading = (text) => page.getByRole("heading", { level: 1, name: text }).waitFor({ timeout: 15000 });
const press = (name) => page.getByRole("button", { name, exact: true }).click();

await page.goto(address);
if (new URL(page.url()).search !== "") problems.push("the token stayed in the address bar");
await heading("Where do you want Clanker to run?");
await shoot("target");
await page.getByText("This machine", { exact: true }).click();
await press("Continue");

await heading(/This machine is ready|needs a few things/);
await shoot("checks");
await press("Continue");

await heading("Give Clanker a language model.");
await shoot("key");
await press("Continue without a key");

await heading("This is what the wizard will create.");
await shoot("review");
await press("Start the control plane");

await heading("The control plane runs. It is yours now.").catch(async (error) => {
  await shoot("create-failed");
  throw error;
});
await shoot("handover");
const link = await page.getByRole("link", { name: "Open the first approval" }).getAttribute("href");
const answer = await fetch(link.replace("/approvals", "/api/v1/work-items"));
if (!answer.ok) problems.push(`the dashboard behind the hand-over link answered ${answer.status}`);
await browser.close();

if (problems.length > 0) {
  console.error(`setup wizard: ${problems.length} problems\n  - ${problems.join("\n  - ")}`);
  process.exit(1);
}
console.log(`setup wizard: the local path works; the hand-over link is ${link}`);
