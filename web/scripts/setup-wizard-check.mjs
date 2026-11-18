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
await page.getByRole("heading", { level: 1 }).first().waitFor({ timeout: 15000 });
const first = await page.getByRole("heading", { level: 1 }).first().textContent();
if (first === "Where do you want S.R.C. to run?") {
  await shoot("target");
  await page.getByText("This machine", { exact: true }).click();
  await press("Continue");

  await heading(/This machine is ready|needs a few things/);
  await shoot("checks");
  await press("Continue");

  // Each choice of the language model, one after the other. The last one stays.
  const model = "Choose the language model of S.R.C.";
  const review = "This is what the wizard will create.";
  const choose = (name) => page.getByRole("radio", { name }).check({ force: true });
  const reviewShows = async (name) => {
    await heading(review);
    const shown = await page.locator(".facts dd").first().textContent();
    if (shown !== name) problems.push(`the review shows the model "${shown}", want "${name}"`);
  };
  await heading(model);
  await shoot("model");
  await choose(/^A hosted key/);
  await page.getByLabel("OpenRouter API key").fill("sk-or-v1-0000000000000000000000-walk");
  await shoot("model-hosted");
  await press("Save the key and continue");
  await reviewShows("A hosted key");
  await press("Back");

  await heading(model);
  await choose(/^A model on this machine/);
  await page.getByRole("heading", { level: 2, name: "What the model on this machine needs" }).waitFor();
  await shoot("model-local");
  // The walk does not download 18 GB. It uses the model only when Ollama already holds it.
  if (await page.getByRole("button", { name: "Use this model and continue", exact: true }).count() === 1) {
    await press("Use this model and continue");
    await reviewShows("A model on this machine");
    await press("Back");
    await heading(model);
  } else {
    console.log("setup wizard: the model is not on this machine; the walk skips this choice");
  }

  await choose(/^Recorded answers/);
  await shoot("model-recorded");
  await press("Use recorded answers and continue");
  await reviewShows("Recorded answers");

  await heading("This is what the wizard will create.");
  await shoot("review");
  await press("Start the control plane");
} else {
  // This machine was set up before, so the wizard opens on its last page.
  await heading("The control plane does not answer here.");
  await shoot("set-up-before");
  await press("Start the control plane");
}

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
