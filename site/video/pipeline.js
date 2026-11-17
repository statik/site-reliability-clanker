// The pipeline scene: one real change of the scripted month goes through the figure of the
// system, stop by stop.
// Every name, number and hash on the screen comes from data/month.json, for the same
// work item that the architecture page follows.

import { clock, plural, shortHash } from "../lib/html.mjs";
import { displayName, seconds } from "../lib/month.mjs";
import { mountMachine } from "../js/machine.js";
import { STOPS, renderMachineSvg } from "../lib/machine.mjs";
import { onBeat } from "./scene.js";

// The approval lands when the sentence names the approvers.
const APPROVE_AFTER_MS = 1800;
const VERIFY_AFTER_MS = 2600;
// The plan follows the pull request inside one sentence.
const PLAN_AFTER_MS = 3200;
const STATE_AFTER_MS = 1600;

// The audit records that each checkpoint writes, by event type.
const RECORDS = {
  detect: ["finding.opened", "workitem.created"],
  author: ["agent.drafted", "pr.opened"],
  plan: ["plan.created"],
  policy: ["policy.evaluated"],
  review: ["review.completed"],
  approval: ["agent.explained", "approval.requested", "approval.recorded"],
  grant: ["grant.issued", "grant.revoked"],
  apply: ["apply.started", "apply.succeeded", "pr.merged"],
  verify: ["verify.passed"],
};

function facts(month) {
  const details = Object.values(month.work_items);
  const detail = details.find((one) => one.pull && one.work_item.state === "Done" && one.work_item.kind === "app-upgrade")
    ?? details.find((one) => one.pull);
  const item = detail.work_item;
  const own = month.events.filter((event) => event.work_item === item.id);
  const find = (type) => own.find((event) => event.type === type);
  const created = find("workitem.created");
  const finding = month.events.filter(
    (event) => event.type === "finding.opened" && event.seq < created.seq,
  ).at(-1);
  const all = finding ? [finding, ...own] : own;
  const grant = month.grants.find((one) => one.work_item === item.id && one.kind === "apply");
  const approval = find("approval.recorded");
  const review = find("review.completed");
  const windowMinutes = Math.round(seconds(grant.not_before, grant.expires) / 60);
  const checks = find("verify.passed").data.checks;
  const text = {
    id: item.id,
    title: item.title,
    tier: item.tier,
    trigger: item.trigger,
    opened: `A finding opens the work item ${item.id}.`,
    pull: `Pull request #${item.pr_id}, by the role Author.`,
    plan: `${find("plan.created").summary}.`,
    "plan-hash": `plan_sha256 ${shortHash(grant.plan_sha256)}`,
    rule: `${item.tier} by the rule ${item.tier_rule}. It needs ${plural(detail.approvers_required, "approver")}.`,
    approver: displayName(approval.actor, month.users),
    decision: `Approves this plan at ${clock(approval.ts)} UTC.`,
    "grant-scope": `${grant.identity}, for the layer ${grant.layer} only.`,
    "grant-window": `It expires in ${windowMinutes} minutes, or when the job ends.`,
    "apply-hash": `plan_sha256 ${shortHash(find("apply.started").data.plan_sha256)}`,
    // An export from before the adversarial review has no record of it.
    review: review ? `${review.summary}.` : "This export of the month is older than the review, so it has no record of one.",
    "review-concern": review?.data?.concerns?.[0] ?? "",
    records: `${plural(own.length, "record")} for ${item.id}, from the finding to the verification.`,
    "last-hash": `head ${shortHash(own.at(-1).hash)}`,
  };
  for (const [stage, types] of Object.entries(RECORDS)) {
    const seqs = all.filter((event) => types.includes(event.type)).map((event) => event.seq);
    text[`record-${stage}`] = seqs.length > 1 ? `audit #${seqs[0]} to #${seqs.at(-1)}` : `audit #${seqs[0]}`;
    if (seqs.length === 0) text[`record-${stage}`] = "";
  }
  return {
    text, checks, tier: find("policy.evaluated").data.tier, raisedTo: review?.data?.tier ?? item.tier,
    layer: item.layers[0], finalTier: item.tier,
  };
}

// The names of the stops, as the count beside the figure says them.
const NAMES = [
  "Detect · code", "Author · model", "Plan · code", "Policy tier · code", "Review · model",
  "Approval · person", "Grant · code", "Apply · code", "Verify · code", "Audit record · code",
];

function fill({ text, finalTier, checks }) {
  const details = Array.from(document.querySelectorAll(".detail"));
  if (details.length !== STOPS.length) throw new Error("pipeline: each stop needs one detail panel");
  for (const slot of document.querySelectorAll("[data-fill]")) {
    const value = text[slot.dataset.fill];
    if (value === undefined) throw new Error(`pipeline: no value for ${slot.dataset.fill}`);
    slot.textContent = value;
  }
  document.getElementById("change-tier").classList.add(`tag-${finalTier.toLowerCase()}`);
  for (const one of document.querySelectorAll("#tiers li")) {
    one.classList.toggle("is-set", one.textContent === finalTier);
  }
  const list = document.getElementById("checks");
  for (const check of checks) {
    const line = document.createElement("p");
    line.textContent = check;
    list.append(line);
  }
}

function play(machine) {
  const details = Array.from(document.querySelectorAll(".detail"));
  const state = document.getElementById("change-state");
  const count = document.getElementById("machine-count");
  // The panel can tell of a stop before the figure gets there: the barrier opens only when
  // the approval lands.
  const tell = (stop) => {
    details.forEach((detail, at) => detail.classList.toggle("is-active", at === stop));
    count.textContent = `${stop + 1} of ${STOPS.length} · ${NAMES[stop]}`;
  };
  const reach = (stop) => {
    machine.go(stop);
    tell(stop);
  };
  const show = (name, className = "tag-gray") => {
    state.textContent = name;
    state.className = `tag ${className}`;
  };
  const later = (milliseconds, action) => window.setTimeout(action, milliseconds);
  const beats = {
    2: () => { reach(0); show("Detected"); },
    3: () => { reach(1); show("Drafting"); later(PLAN_AFTER_MS, () => { reach(2); show("Planned"); }); },
    4: () => { reach(3); later(STATE_AFTER_MS, () => { document.getElementById("change-tier").style.opacity = "1"; }); },
    5: () => { reach(4); later(STATE_AFTER_MS, () => show("AwaitingApproval", "tag-purple")); },
    6: () => {
      tell(5);
      later(APPROVE_AFTER_MS, () => {
        reach(5);
        document.getElementById("detail-approval").classList.add("is-decided");
        show("Approved", "tag-blue");
      });
    },
    7: () => { reach(6); show("Applying", "tag-blue"); },
    8: () => { reach(7); },
    9: () => {
      reach(8);
      show("Verifying", "tag-blue");
      later(VERIFY_AFTER_MS, () => { reach(9); show("Done", "tag-green"); });
    },
  };
  reach(0);
  onBeat((beat) => beats[beat]?.());
}

const response = await fetch("../data/month.json");
if (!response.ok) throw new Error(`pipeline: data/month.json answered ${response.status}`);
const found = facts(await response.json());
fill(found);
const stage = document.getElementById("machine");
stage.innerHTML = renderMachineSvg({ tier: found.tier, raisedTo: found.raisedTo, layer: found.layer });
const figure = stage.querySelector("svg");
// The figure has a wide margin for its card on the site. The scene shows it closer.
figure.setAttribute("viewBox", "176 16 720 500");
play(mountMachine(figure));
