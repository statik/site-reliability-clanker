// Approve one work item through the real dashboard, with no mocked request.
// Usage: node scripts/approve-check.mjs WI-0014 [base URL]
// It clicks Approve, confirms the dialog, and waits until the work item is Done.
import { chromium } from "playwright";

const workItem = process.argv[2];
const base = process.argv[3] ?? "http://127.0.0.1:7070";
if (workItem === undefined) {
  console.error("usage: node scripts/approve-check.mjs <work item ID> [base URL]");
  process.exit(2);
}

const stateOf = async () => {
  const response = await fetch(`${base}/api/v1/work-items/${workItem}`);
  if (!response.ok) throw new Error(`GET work item ${workItem}: HTTP ${response.status}`);
  return (await response.json()).work_item.state;
};

const before = await stateOf();
if (before !== "AwaitingApproval") {
  console.error(`${workItem} is in state ${before}; this check needs AwaitingApproval`);
  process.exit(1);
}

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const decisions = [];
page.on("response", (response) => {
  if (response.url().endsWith("/decision")) decisions.push(response.status());
});
await page.goto(`${base}/work/${workItem}`);
await page.getByRole("button", { name: "Approve", exact: true }).click();
const dialog = page.getByRole("dialog");
await dialog.getByRole("button", { name: "Approve", exact: true }).click();
await page.waitForResponse((response) => response.url().endsWith("/decision"));
await browser.close();

if (decisions.length !== 1 || decisions[0] !== 200) {
  console.error(`the decision request answered ${decisions.join(", ")}; want one 200`);
  process.exit(1);
}
console.log(`decision accepted (HTTP 200) for ${workItem}`);

const deadline = Date.now() + 5 * 60 * 1000;
let state = await stateOf();
while (!["Done", "Failed", "RolledBack"].includes(state) && Date.now() < deadline) {
  await new Promise((resolve) => setTimeout(resolve, 3000));
  state = await stateOf();
}
console.log(`${workItem} ended in state ${state}`);
process.exit(state === "Done" ? 0 : 1);
