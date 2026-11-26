// Walk through the setup wizard for AWS in a real browser: the survey, the names and tags,
// the review, Create, and Remove. The walk runs against the stand-ins of
// scripts/fixtures/aws-account for aws, tofu, docker, ops, make and pnpm, never against an
// account. It serves /healthz for the stand-in of the load balancer.
// Usage: node scripts/setup-aws-survey-check.mjs <wizard address with token> <fixture log> [screenshot directory]
// Start the wizard first, from the root of the repository, with an empty home and data
// directory and no AWS variables:
//   env -i HOME=<empty directory> PATH="$PWD/web/scripts/fixtures/aws-account:/usr/bin:/bin" \
//     SRC_FIXTURE_LOG=<fixture log> SRC_FIXTURE_STATE=<empty directory> \
//     SRC_FIXTURE_CORE_URL=http://127.0.0.1:7182 \
//     clanker setup --no-browser --data <empty directory> --source "$PWD"
import { mkdir, readFile } from "node:fs/promises";
import { createServer } from "node:http";
import { join } from "node:path";
import { chromium } from "playwright";

const [address, fixtureLog, shots] = process.argv.slice(2);
if (address === undefined || !address.includes("token=") || fixtureLog === undefined) {
  console.error("usage: node scripts/setup-aws-survey-check.mjs <wizard address with token> <fixture log> [screenshot directory]");
  process.exit(2);
}
if (shots !== undefined) await mkdir(shots, { recursive: true });

// The private values of the walk. No page, log line or state may show them.
const tagValue = "owner@example.com";
const modelKey = "sk-or-v1-0000000000000000000000-walk";

// The wizard must use the stand-ins. With a real program, the walk stops here.
const token = new URL(address).searchParams.get("token");
const api = async (path) => (await fetch(new URL(path, address), { headers: { "X-Setup-Token": token } })).json();
const checks = await api("/api/checks?target=aws");
for (const name of ["aws", "tofu", "docker", "ops", "make", "pnpm"]) {
  const program = checks.checks.find((check) => check.name === name)?.detail ?? "";
  if (!program.endsWith(`fixtures/aws-account/${name}`)) {
    console.error(`FAILED: the wizard would run "${program}" for ${name}, not the stand-in. Start it with the ` +
      "stand-ins first in PATH.");
    process.exit(1);
  }
}

// The stand-in of the load balancer: the dashboard of the stand-in core answers /healthz.
const health = createServer((request, response) => {
  response.writeHead(request.url === "/healthz" ? 200 : 404).end();
});
await new Promise((resolve) => health.listen(7182, "127.0.0.1", resolve));

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
const checkPrivate = (where, text) => {
  for (const value of [tagValue, modelKey, "fixture-admin-password", "fixture-registry-token"]) {
    if (text.includes(value)) problems.push(`${where} shows a private value`);
  }
};

await page.goto(address);
await heading("Where do you want S.R.C. to run?");
await page.getByRole("radio", { name: /Your AWS account/ }).check({ force: true });
await shoot("target");
await press("Continue");

await heading("This machine is ready.");
await shoot("checks");
const steps = await page.locator("#steps li").allTextContents();
if (steps.includes("Hand over")) problems.push('the steps for AWS include "Hand over"');
for (const want of ["Names and tags", "Review", "Create", "Done"]) {
  if (!steps.includes(want)) problems.push(`the steps for AWS lack "${want}"`);
}
await press("Continue");

await heading("Choose the language model of S.R.C.");
if (!(await page.getByRole("radio", { name: /^A model on this machine/ }).isDisabled())) {
  problems.push("the model on this machine is offered for AWS");
}
await page.getByRole("radio", { name: /^A hosted key/ }).check({ force: true });
await page.getByLabel("OpenRouter API key").fill(modelKey);
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
if (networks !== 2) problems.push(`the page lists ${networks} choices, want the network of the fixture and a new network`);
if (proposed !== 1) problems.push(`${proposed} choices are marked as proposed, want 1`);
const regions = await page.getByLabel("Region").locator("option").count();
if (regions !== 2) problems.push(`the page lists ${regions} regions, want the 2 of the fixture`);
await press("Use this network");

await heading("Choose the names and the tags.", 60000);
await shoot("names-and-tags");
const zones = page.getByRole("radiogroup", { name: "Zone" }).getByRole("radio");
if ((await zones.count()) !== 2) problems.push(`the page lists ${await zones.count()} zones, want the 2 of the fixture`);
if (!(await page.getByRole("radio", { name: /not delegated/ }).isDisabled())) {
  problems.push("the zone that the parent does not delegate can be chosen");
}
if ((await page.getByRole("radio", { checked: true }).count()) !== 1) problems.push("no zone is chosen");
const details = await page.locator("main").textContent();
if (!details.includes("The rule names the tags example:owner")) problems.push("the page does not name the required tag");
await page.getByLabel("example:owner").fill(tagValue);
await press("Continue");

await heading("This is what S.R.C. creates in your AWS account.");
const state = await api("/api/state");
const weekly = state.aws_plan.weekly_usd.toFixed(2);
const review = await page.locator("main").textContent();
for (const want of ["t4g.small", "auto scaling group of one", "db.t4g.micro", "machine image and its snapshot",
  "Two names in example.com", "src-clanker.example.com", "Layer 00-bootstrap", "Images", "Log groups",
  "example:owner (values not shown)", "admits only the router of your private network", "clanker_env=clanker",
  weekly, "US West (Oregon)", "What Create does", "Build and push the images of the application", "111122223333"]) {
  if (!review.includes(want)) problems.push(`the review lacks "${want}"`);
}
checkPrivate("the review", review);
await shoot("review");

// Until Create, every call reads, or makes a dry run.
const beforeCreate = (await readFile(fixtureLog, "utf8")).trim().split("\n");
const reads = /^(configure list-profiles|sts (get-caller-identity|decode-authorization-message)|(ec2|ecs) (describe|list)-[a-z-]+|iam (get-role|simulate-principal-policy)|route53 (list-hosted-zones|get-hosted-zone)|ssm get-parameter|ec2 run-instances .*--dry-run) /;
for (const call of beforeCreate) {
  if (!reads.test(`${call} `)) problems.push(`the wizard made a call that is not a read before Create: aws ${call}`);
}

await press("Create");
await heading("S.R.C. runs in your AWS account.", 300000);
await shoot("done");
const done = await page.locator("main").textContent();
for (const want of ["http://127.0.0.1:7182", "admin@src-clanker-idp.example.com", "tofu output -raw admin_password",
  `${weekly} USD`]) {
  if (!done.includes(want)) problems.push(`the page after Create lacks "${want}"`);
}
checkPrivate("the page after Create", done);
const installed = await api("/api/state");
if (installed.job.steps.length !== 9 || installed.job.status !== "done") {
  problems.push(`the installation ended ${installed.job.status} with ${installed.job.steps.length} steps`);
}
checkPrivate("the log of Create", installed.job.lines.join("\n"));
checkPrivate("the state of the wizard", JSON.stringify(installed));
if (!installed.job.lines.some((line) => line.includes('"example:owner" = "(hidden)"'))) {
  problems.push("the log of Create does not show the tag hidden");
}

const remove = page.getByRole("button", { name: "Remove clanker" });
if (!(await remove.isDisabled())) problems.push("Remove is possible without the name of the environment");
await page.getByLabel("Name of the environment").fill("clanker");
await remove.click();
await heading("S.R.C. is removed from your AWS account.", 300000);
await shoot("removed");
const removed = await api("/api/state");
if (removed.job.steps.length !== 7 || removed.state.installed) {
  problems.push(`the removal ended with ${removed.job.steps.length} steps and installed=${removed.state.installed}`);
}
if (!removed.job.lines.some((line) => line.startsWith("Nothing is left"))) problems.push("the removal did not check what is left");
checkPrivate("the log of Remove", removed.job.lines.join("\n"));

await browser.close();
health.close();

// No launch: every call of ec2 run-instances is a dry run.
const calls = (await readFile(fixtureLog, "utf8")).trim().split("\n");
for (const call of calls) {
  if (call.startsWith("ec2 run-instances") && !call.includes("--dry-run")) problems.push(`a launch: aws ${call}`);
  checkPrivate("a call of the aws program", call.startsWith("ec2 run-instances") ? "" : call);
}

if (problems.length > 0) {
  console.error(`FAILED:\n${problems.join("\n")}`);
  process.exit(1);
}
console.log(`RESULT: the wizard surveyed the fixture account with ${beforeCreate.length} calls that read, created ` +
  `the control plane with the stand-ins in 9 steps and removed it in 7, and showed no private value`);
