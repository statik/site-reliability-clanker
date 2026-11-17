// The pipeline scene: one real change of the scripted month passes nine checkpoints.
// Every name, number and hash on the screen comes from data/month.json, for the same
// work item that the architecture page follows.

import { clock, duration, plural, shortHash } from "../lib/html.mjs";
import { displayName, seconds } from "../lib/month.mjs";
import { pickFlowItem } from "../lib/render-architecture.mjs";
import { onBeat } from "./scene.js";

// The approval lands at the end of the sentence about the approvers.
const APPROVE_AFTER_MS = 3600;
const VERIFY_AFTER_MS = 2600;
const RECORD_AFTER_MS = 4200;
const STATE_AFTER_MS = 1600;

// The audit records that each checkpoint writes, by event type.
const RECORDS = {
  detect: ["finding.opened", "workitem.created"],
  plan: ["agent.drafted", "pr.opened", "plan.created"],
  policy: ["policy.evaluated"],
  review: ["review.completed"],
  approval: ["agent.explained", "approval.requested", "approval.recorded"],
  grant: ["grant.issued", "grant.revoked"],
  apply: ["apply.started", "apply.succeeded", "pr.merged"],
  verify: ["verify.passed"],
};

function facts(month) {
  const detail = pickFlowItem(month);
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
  const held = duration(seconds(grant.issued_at, grant.ended_at));
  const windowMinutes = Math.round(seconds(grant.not_before, grant.expires) / 60);
  const checks = find("verify.passed").data.checks;
  const text = {
    id: item.id,
    title: item.title,
    tier: item.tier,
    trigger: item.trigger,
    opened: `A finding opens the work item ${item.id}.`,
    pull: `Pull request #${item.pr_id}. ${find("plan.created").summary}.`,
    "plan-hash": `plan_sha256 ${shortHash(grant.plan_sha256)}`,
    rule: `${item.tier} by the rule ${item.tier_rule}. It needs ${plural(detail.approvers_required, "approver")}.`,
    approver: displayName(approval.actor, month.users),
    decision: `Approves this plan at ${clock(approval.ts)} UTC.`,
    "grant-scope": `${grant.identity}, for the layer ${grant.layer} only.`,
    "grant-window": `It expires in ${windowMinutes} minutes, or when the job ends.`,
    "apply-hash": `plan_sha256 ${shortHash(find("apply.started").data.plan_sha256)}`,
    // An export from before the adversarial review has no record of it.
    review: review?.summary ?? "This export of the month is older than the review, so it has no record of one.",
    records: `${plural(own.length, "record")} for ${item.id}, from the finding to the verification.`,
    "last-hash": `head ${shortHash(own.at(-1).hash)}`,
    "result-detect": "found",
    "result-plan": `PR #${item.pr_id}`,
    "result-policy": item.tier,
    "result-review": review ? `tier ${review.data.tier}` : "no record",
    "result-approval": displayName(approval.actor, month.users),
    "result-grant": `${windowMinutes} min`,
    "result-apply": "same hash",
    "result-verify": `${plural(checks.length, "check")}`,
    "result-audit": `${plural(own.length, "record")}`,
  };
  for (const [stage, types] of Object.entries(RECORDS)) {
    const seqs = all.filter((event) => types.includes(event.type)).map((event) => event.seq);
    text[`record-${stage}`] = seqs.length > 1 ? `audit #${seqs[0]} to #${seqs.at(-1)}` : `audit #${seqs[0]}`;
    if (seqs.length === 0) text[`record-${stage}`] = "";
  }
  return { text, tier: item.tier, checks, held: `held ${held}` };
}

function fill({ text, tier, checks }) {
  const details = Array.from(document.querySelectorAll(".detail"));
  if (details.length !== document.querySelectorAll(".stage").length) {
    throw new Error("pipeline: each checkpoint needs one detail panel");
  }
  for (const slot of document.querySelectorAll("[data-fill]")) {
    const value = text[slot.dataset.fill];
    if (value === undefined) throw new Error(`pipeline: no value for ${slot.dataset.fill}`);
    slot.textContent = value;
  }
  document.getElementById("change-tier").classList.add(`tag-${tier.toLowerCase()}`);
  for (const one of document.querySelectorAll("#tiers li")) {
    one.classList.toggle("is-set", one.textContent === tier);
  }
  const list = document.getElementById("checks");
  for (const check of checks) {
    const line = document.createElement("p");
    line.textContent = check;
    list.append(line);
  }
}

function play(held) {
  const stages = Array.from(document.querySelectorAll(".stage"));
  const details = Array.from(document.querySelectorAll(".detail"));
  const state = document.getElementById("change-state");
  const pass = (index) => {
    stages[index].classList.remove("is-active");
    stages[index].classList.add("is-passed");
  };
  const reach = (index) => {
    for (let before = 0; before < index; before += 1) pass(before);
    stages[index].classList.add("is-active");
    details.forEach((detail, at) => detail.classList.toggle("is-active", at === index));
  };
  const show = (name, className = "tag-gray") => {
    state.textContent = name;
    state.className = `tag ${className}`;
  };
  const later = (milliseconds, action) => window.setTimeout(action, milliseconds);
  const beats = {
    2: () => { reach(0); show("Detected"); },
    3: () => { reach(1); show("Drafting"); },
    4: () => {
      reach(2);
      show("Planned");
      later(STATE_AFTER_MS, () => { document.getElementById("change-tier").style.opacity = "1"; });
    },
    5: () => { reach(3); later(STATE_AFTER_MS, () => show("AwaitingApproval", "tag-purple")); },
    6: () => {
      reach(4);
      later(APPROVE_AFTER_MS, () => {
        document.getElementById("detail-approval").classList.add("is-decided");
        pass(4);
        show("Approved", "tag-blue");
      });
    },
    7: () => { reach(5); show("Applying", "tag-blue"); },
    8: () => { reach(6); },
    9: () => {
      reach(7);
      // The grant ended when the apply ended, before the verification.
      document.getElementById("result-grant").textContent = held;
      show("Verifying", "tag-blue");
      later(VERIFY_AFTER_MS, () => { reach(8); show("Done", "tag-green"); });
      later(RECORD_AFTER_MS, () => pass(8));
    },
  };
  onBeat((beat) => beats[beat]?.());
}

const response = await fetch("../data/month.json");
if (!response.ok) throw new Error(`pipeline: data/month.json answered ${response.status}`);
const found = facts(await response.json());
fill(found);
play(found.held);
