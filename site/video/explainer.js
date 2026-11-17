// The pictures of the explainer video. video/record.mjs sets data-cue on the root element
// when a sentence of narration.md starts, and --say to the length of the sentence in
// seconds. This module shows the picture of that cue and fills the pictures with the names
// and numbers of data/month.json.

import { clock, duration, plural } from "../lib/html.mjs";
import { grantViews, totals } from "../lib/month.mjs";

const COUNT_SECONDS = 1.2;
// A picture leaves in this time (scene.css). The number of the step changes after it.
const LEAVE_MS = 250;
const TIERS = ["T0", "T1", "T2", "T3"];

const root = document.documentElement;
const shots = [...document.querySelectorAll(".pic")];
const stepNumber = document.getElementById("step-number");

function put(id, text) {
  document.getElementById(id).textContent = text;
}

function need(value, what) {
  if (value === undefined || value === null || value === "") {
    throw new Error(`explainer: data/month.json has no ${what}`);
  }
  return value;
}

function listItems(id, names, cue) {
  const list = document.getElementById(id);
  names.forEach((name, index) => {
    const item = document.createElement("li");
    item.textContent = name;
    if (cue) {
      item.className = "part";
      item.dataset.at = cue;
      item.style.setProperty("--at", (0.2 + (0.7 * index) / names.length).toFixed(2));
    }
    list.append(item);
  });
}

// A number that counts up to its value when its picture shows.
const counters = new Map();

function counter(id, cue, value, format) {
  put(id, format(0));
  counters.set(cue, [...(counters.get(cue) ?? []), { id, value, format }]);
}

function count(cue) {
  for (const { id, value, format } of counters.get(cue) ?? []) {
    const began = performance.now();
    const tick = (now) => {
      const share = Math.min(1, (now - began) / (COUNT_SECONDS * 1000));
      put(id, format(value * (1 - (1 - share) ** 3)));
      if (share < 1) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }
}

function fill(month) {
  const sums = totals(month);
  const details = Object.values(month.work_items);
  const firstRun = need(details[0], "work item");
  const layers = grantViews(month).filter((grant) => grant.work_item === firstRun.work_item.id)
    .map((grant) => grant.layer);
  if (layers.length === 0) throw new Error("explainer: the first deployment has no grants for its layers");
  listItems("layers", layers, "deploy");
  listItems("demo-layers", layers, "");

  // The change that the model wrote, and that went through every step.
  const written = need(details.find((one) => one.pull?.author === "clanker-author"
    && one.work_item.state === "Done"), "finished change that the model wrote");
  const own = month.events.filter((event) => event.work_item === written.work_item.id);
  const event = (type) => need(own.find((one) => one.type === type), `${type} event`);
  put("pull-path", need(event("pr.opened").data.paths?.[0], "path in the pull request"));
  const plan = need(written.plans[0], "plan").facts;
  put("pull-plan", `Plan: ${plan.add} to add, ${plan.change} to change, ${plan.destroy} to destroy`);
  const tier = need(event("policy.evaluated").data.tier, "tier from the policy rules");
  for (const name of TIERS) {
    const item = document.createElement("li");
    item.textContent = name;
    if (name === tier) item.className = "is-set";
    document.getElementById("tiers").append(item);
  }
  const grant = need(grantViews(month).find((one) => one.work_item === written.work_item.id
    && one.kind === "apply" && one.held_seconds !== null), "ended grant");
  put("access-scope", `Layer ${grant.layer}, and no other`);
  put("access-issued", `${clock(grant.issued_at)} UTC`);
  put("access-revoked", `${clock(grant.ended_at)} UTC`);
  put("access-held", duration(grant.held_seconds));

  // The change that waits for a person: its summary is what the approver reads.
  const waiting = need(details.find((one) => one.work_item.state === "AwaitingApproval"
    && one.work_item.explanation?.sentence), "change that waits for approval");
  put("summary", waiting.work_item.explanation.sentence);
  put("approver", `Needs ${plural(waiting.approvers_required, "approver")}`);

  put("demo-items", String(sums.work_items));
  counter("figure-days", "held", sums.days, (value) => String(Math.round(value)));
  counter("figure-held", "held", sums.held_seconds, (value) => duration(Math.round(value)));
  counter("figure-events", "audit", sums.events, (value) => String(Math.round(value)));
}

function show(cue) {
  const shot = shots.find((one) => one.dataset.cues.split(" ").includes(cue));
  for (const one of shots) one.classList.toggle("is-active", one === shot);
  const step = shot?.dataset.step ?? "";
  if (step !== stepNumber.textContent) {
    stepNumber.classList.remove("is-active");
    window.setTimeout(() => {
      stepNumber.textContent = step;
      stepNumber.classList.toggle("is-active", step !== "");
    }, LEAVE_MS);
  }
  for (const part of document.querySelectorAll(`[data-at="${cue}"]`)) part.classList.add("is-shown");
  count(cue);
}

const response = await fetch("../data/month.json");
if (!response.ok) throw new Error(`explainer: data/month.json answered ${response.status}`);
fill(await response.json());

let shown = "";
const update = () => {
  const cue = root.dataset.cue ?? "";
  if (cue === shown) return;
  shown = cue;
  show(cue);
};
new MutationObserver(update).observe(root, { attributes: true, attributeFilter: ["data-cue"] });
update();
root.dataset.ready = "true";
