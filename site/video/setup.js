// The setup scene: from the download to a product that is looked after, in five steps.
// The numbers of steps 4 and 5 come from data/month.json.

import { duration, list, plural } from "../lib/html.mjs";
import { displayName, seconds, totals } from "../lib/month.mjs";
import { onBeat } from "./scene.js";

// The kinds of work item after the first deployment, in the order of the tiles.
const KINDS = [
  ["app-upgrade", "application upgrade", "application upgrades"],
  ["rollback", "rollback", "rollbacks"],
  ["postgres-patch", "database patch", "database patches"],
  ["drift", "drift repair", "drift repairs"],
  ["incident", "incident", "incidents"],
  ["runbook", "runbook run", "runbook runs"],
];

function element(tag, className, text = "") {
  const made = document.createElement(tag);
  made.className = className;
  made.textContent = text;
  return made;
}

function fillDeployment(month) {
  const detail = Object.values(month.work_items).find((one) => one.work_item.kind === "blank-slate");
  if (!detail) throw new Error("setup: the month has no blank-slate deployment");
  const item = detail.work_item;
  const approvers = detail.approvals.map((approval) => displayName(approval.approver, month.users));
  const tier = document.getElementById("deploy-tier");
  tier.textContent = `${item.id} · tier ${item.tier}`;
  tier.classList.add(`tag-${item.tier.toLowerCase()}`);
  document.querySelector("[data-fill='deploy-title']").textContent = item.title;
  document.querySelector("[data-fill='deploy-approvers']").textContent =
    `${list([...new Set(approvers)])} approve. Only then does the apply start.`;
  const ladder = document.getElementById("ladder");
  // The top row of the picture is the top layer; the bottom layer appears first.
  [...item.layers].reverse().forEach((layer, index) => {
    const grant = month.grants.find((one) => one.work_item === item.id && one.layer === layer);
    if (!grant) throw new Error(`setup: no grant for ${layer}`);
    const rung = element("li", "rung");
    rung.style.setProperty("--row", String(item.layers.length - 1 - index));
    rung.append(
      element("span", "rung-layer", layer),
      element("span", "rung-identity", grant.identity),
      element("span", "rung-what", "grant, apply, revoke"),
      element("span", "rung-held", duration(seconds(grant.issued_at, grant.ended_at))),
    );
    ladder.append(rung);
  });
}

function fillMaintenance(month) {
  const kinds = Object.values(month.work_items).map((detail) => detail.work_item.kind);
  const tiles = document.getElementById("kinds");
  for (const [prefix, one, many] of KINDS) {
    const count = kinds.filter((kind) => kind === prefix || kind.startsWith(`${prefix}:`)).length;
    if (count === 0) continue;
    const tile = element("li", "kind");
    tile.append(element("p", "kind-count", String(count)), element("p", "kind-name", count === 1 ? one : many));
    tiles.append(tile);
  }
  document.querySelector("[data-fill='month']").textContent =
    `In ${plural(totals(month).days, "day")} on the local target:`;
}

function play() {
  const steps = Array.from(document.querySelectorAll(".setup-step"));
  const panels = Array.from(document.querySelectorAll(".panel"));
  onBeat((beat) => {
    steps.forEach((step, index) => {
      step.classList.toggle("is-passed", index < beat - 1);
      step.classList.toggle("is-active", index === beat - 1);
    });
    panels.forEach((panel, index) => panel.classList.toggle("is-active", index === beat - 1));
  });
}

const response = await fetch("../data/month.json");
if (!response.ok) throw new Error(`setup: data/month.json answered ${response.status}`);
const month = await response.json();
fillDeployment(month);
fillMaintenance(month);
play();
