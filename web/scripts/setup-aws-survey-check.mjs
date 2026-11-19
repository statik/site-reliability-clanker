// Walk through the setup wizard for AWS in a real browser, to the review, which is the
// last step for AWS. The walk runs against a stand-in for the AWS command line, never
// against an account.
// Usage: node scripts/setup-aws-survey-check.mjs <wizard address with token> <fixture log> [screenshot directory]
// Start the wizard first, from the root of the repository:
//   PATH="$PWD/web/scripts/fixtures/aws-account:$PATH" SRC_FIXTURE_LOG=<fixture log> \
//     clanker setup --no-browser --data <an empty directory>
import { mkdir, readFile } from "node:fs/promises";
import { join } from "node:path";
import { chromium } from "playwright";

const [address, fixtureLog, shots] = process.argv.slice(2);
if (address === undefined || !address.includes("token=") || fixtureLog === undefined) {
  console.error("usage: node scripts/setup-aws-survey-check.mjs <wizard address with token> <fixture log> [screenshot directory]");
  process.exit(2);
}
if (shots !== undefined) await mkdir(shots, { recursive: true });

// The wizard must use the stand-in. With the real AWS command line, the walk stops here.
const token = new URL(address).searchParams.get("token");
const checks = await (await fetch(new URL("/api/checks?target=aws", address), {
  headers: { "X-Setup-Token": token },
})).json();
const program = checks.checks.find((check) => check.name === "aws")?.detail ?? "";
if (!program.endsWith("fixtures/aws-account/aws")) {
  console.error(`FAILED: the wizard would run "${program}", not the stand-in. Start it with the stand-in first in PATH.`);
  process.exit(1);
}

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
await page.getByRole("radio", { name: /Your AWS account/ }).check({ force: true });
await shoot("target");
await press("Continue");

await heading("This machine is ready.");
await shoot("checks");
const steps = await page.locator("#steps li").allTextContents();
for (const absent of ["Create", "Hand over"]) {
  if (steps.includes(absent)) problems.push(`the steps for AWS include "${absent}"`);
}
if (steps.at(-1) !== "Review") problems.push(`the last step for AWS is "${steps.at(-1)}", want "Review"`);
await press("Continue");

await heading("Choose the language model of S.R.C.");
if (!(await page.getByRole("radio", { name: /^A model on this machine/ }).isDisabled())) {
  problems.push("the model on this machine is offered for AWS");
}
await page.getByRole("radio", { name: /^A hosted key/ }).check({ force: true });
// The AWS path ends at the review and starts no control plane, so the key is never used.
await page.getByLabel("OpenRouter API key").fill("sk-or-v1-0000000000000000000000-walk");
await shoot("model");
await press("Save the key and continue");

await heading("Sign in to your AWS account.");
await page.getByLabel("Profile").selectOption("fixture-operator");
await press("Sign in");
await page.getByText("Signed in as").waitFor({ timeout: 30000 });
await shoot("signed-in");
await press("Continue");

await heading("Choose the network of the control plane.", 120000);
await shoot("network");
const networks = await page.getByRole("radio").count();
const proposed = await page.locator(".choice", { has: page.locator(".tag", { hasText: "proposed" }) }).count();
const chosen = await page.getByRole("radio", { checked: true }).count();
if (networks !== 2) problems.push(`the page lists ${networks} choices, want the network of the fixture and a new network`);
if (proposed !== 1) problems.push(`${proposed} choices are marked as proposed, want 1`);
if (chosen !== 1) problems.push(`${chosen} choices are selected, want the proposed one`);
const regions = await page.getByLabel("Region").locator("option").count();
if (regions !== 2) problems.push(`the page lists ${regions} regions, want the 2 of the fixture`);
await press("Use this network");

await heading("This is what S.R.C. creates in your AWS account.");
await shoot("review");
const review = await page.locator("main").textContent();
for (const want of ["t4g.small", "auto scaling group of one", "db.t4g.micro", "machine image and its snapshot",
  "Roles", "Parameters", "admits only the router of your private network", "clanker_env=clanker",
  "5.51", "US West (Oregon)", "2026-11-18", "The wizard stops here for AWS.", "111122223333"]) {
  if (!review.includes(want)) problems.push(`the review lacks "${want}"`);
}
const buttons = await page.getByRole("button").allTextContents();
if (buttons.join(",") !== "Back") problems.push(`the review has the buttons "${buttons.join(", ")}", want only "Back"`);

await browser.close();

// The stand-in logged each call. Every one must be a read.
const calls = (await readFile(fixtureLog, "utf8")).trim().split("\n");
const reads = /^(configure list-profiles|sts get-caller-identity|(ec2|ecs) (describe|list)-[a-z-]+|iam (get-role|simulate-principal-policy)) /;
for (const call of calls) {
  if (!reads.test(`${call} `)) problems.push(`the wizard made a call that is not a read: aws ${call}`);
}

if (problems.length > 0) {
  console.error(`FAILED:\n${problems.join("\n")}`);
  process.exit(1);
}
console.log(`RESULT: the wizard surveyed the fixture account with ${calls.length} read calls and ended at the review`);
