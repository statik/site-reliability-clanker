// Walk through the setup wizard for AWS up to the choice of the network, in a real browser.
// Usage: node scripts/setup-aws-survey-check.mjs <wizard address with token> <AWS profile> [screenshot directory]
// Start the wizard first: clanker setup --preview-aws --no-browser --data <an empty directory>
// The wizard only reads the account. The script chooses the proposed network and stops.
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { chromium } from "playwright";

const [address, profile, shots] = process.argv.slice(2);
if (address === undefined || !address.includes("token=") || profile === undefined) {
  console.error("usage: node scripts/setup-aws-survey-check.mjs <wizard address with token> <AWS profile> [screenshot directory]");
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
const heading = (text, timeout = 15000) => page.getByRole("heading", { level: 1, name: text }).waitFor({ timeout });
const press = (name) => page.getByRole("button", { name, exact: true }).click();

await page.goto(address);
await heading("Where do you want S.R.C. to run?");
await page.getByRole("radio", { name: /Your AWS account/ }).check();
await press("Continue");

await heading(/This machine/);
await shoot("checks");
if (await page.getByRole("button", { name: "Continue", exact: true }).isDisabled()) {
  console.error("FAILED: this machine lacks a tool that the wizard needs for AWS. See the page.");
  await browser.close();
  process.exit(1);
}
await press("Continue");

await heading("Give S.R.C. a language model.");
await press("Continue without a key");

await heading("Sign in to your AWS account.");
await page.getByLabel("Profile").selectOption(profile);
await press("Sign in");
await page.getByText("Signed in as").waitFor({ timeout: 30000 });
await shoot("signed-in");
await press("Continue");

await heading("Choose the network of the control plane.", 120000);
await shoot("network");
const networks = await page.getByRole("radio").count();
const proposed = await page.locator(".choice", { has: page.locator(".tag", { hasText: "proposed" }) }).count();
const chosen = await page.getByRole("radio", { checked: true }).count();
if (networks < 1) problems.push("the page lists no network and no new network");
if (proposed !== 1) problems.push(`${proposed} choices are marked as proposed, want 1`);
if (chosen !== 1) problems.push(`${chosen} choices are selected, want the proposed one`);
const regions = await page.getByLabel("Region").locator("option").count();
if (regions < 1) problems.push("the page lists no region");

await page.getByRole("button", { name: /^Use (this|a new) network$/ }).click();
await page.getByText("The choice is saved.").waitFor({ timeout: 15000 });
await shoot("saved");

await browser.close();
if (problems.length > 0) {
  console.error(`FAILED:\n${problems.join("\n")}`);
  process.exit(1);
}
console.log(`RESULT: the wizard surveyed the account, listed ${regions} regions, proposed one of ${networks} choices, and saved the choice`);
