// The pipeline scene: one real change of the scripted month passes eight checkpoints.
// Every name, number and hash on the screen comes from data/month.json, for the same
// work item that the architecture page follows.

import { clock, duration, plural, shortHash } from "../lib/html.mjs";
import { displayName, seconds } from "../lib/month.mjs";
import { pickFlowItem } from "../lib/render-architecture.mjs";
import { onBeat } from "./scene.js";

// The approval lands at the end of the sentence about the approvers.
const APPROVE_AFTER_MS = 4800;
const VERIFY_AFTER_MS = 2600;
const RECORD_AFTER_MS = 4200;
const STATE_AFTER_MS = 1600;

// The audit records that each checkpoint writes, by event type.
const RECORDS = {
  detect: ["finding.opened", "workitem.created"],
  plan: ["agent.drafted", "pr.opened", "plan.created"],
  policy: ["policy.evaluated", "agent.explained", "approval.requested"],
  approval: ["approval.recorded"],
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
  const text = {
    id: item.id,
    title: item.title,
    tier: item.tier,
    trigger: item.trigger,
    pull: `Pull request #${item.pr_id}`,
    plan: find("plan.created").summary,
    "plan-hash": `plan_sha256 ${shortHash(grant.plan_sha256)}`,
    rule: `${item.tier} by rule ${item.tier_rule}`,
    "approvers-required": `Needs ${plural(detail.approvers_required, "approver")}.`,
    approver: displayName(approval.actor, month.users),
    decision: "Approves this plan",
    "decided-at": `${clock(approval.ts)} UTC`,
    "grant-id": grant.id,
    "grant-scope": `${grant.identity}, layer ${grant.layer} only`,
    "grant-window": `Expires in ${Math.round(seconds(grant.not_before, grant.expires) / 60)} minutes`,
    "grant-held": `Revoked after ${duration(seconds(grant.issued_at, grant.ended_at))}`,
    "apply-hash": `plan_sha256 ${shortHash(find("apply.started").data.plan_sha256)}`,
    records: `${plural(own.length, "record")} for ${item.id}`,
    "last-hash": `head ${shortHash(own.at(-1).hash)}`,
  };
  for (const [stage, types] of Object.entries(RECORDS)) {
    const seqs = all.filter((event) => types.includes(event.type)).map((event) => event.seq);
    text[`record-${stage}`] = seqs.length > 1 ? `audit #${seqs[0]} to #${seqs.at(-1)}` : `audit #${seqs[0]}`;
  }
  return { text, tier: item.tier, checks: find("verify.passed").data.checks };
}

function fill({ text, tier, checks }) {
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

function play() {
  const stages = Array.from(document.querySelectorAll(".stage"));
  const state = document.getElementById("change-state");
  const pass = (index) => {
    stages[index].classList.remove("is-active");
    stages[index].classList.add("is-passed");
  };
  const reach = (index) => {
    for (let before = 0; before < index; before += 1) pass(before);
    stages[index].classList.add("is-active");
  };
  const show = (name, className = "tag-gray") => {
    state.textContent = name;
    state.className = `tag ${className}`;
  };
  const later = (milliseconds, action) => window.setTimeout(action, milliseconds);
  const beats = {
    2: () => { reach(0); show("Detected"); },
    3: () => { reach(1); show("Drafting"); later(STATE_AFTER_MS, () => show("Planned")); },
    4: () => {
      reach(2);
      later(STATE_AFTER_MS, () => {
        document.getElementById("change-tier").style.opacity = "1";
        show("AwaitingApproval", "tag-purple");
      });
    },
    5: () => { reach(3); later(APPROVE_AFTER_MS, () => { pass(3); show("Approved", "tag-blue"); }); },
    6: () => { reach(4); show("Applying", "tag-blue"); },
    7: () => { reach(5); },
    8: () => {
      reach(6);
      document.getElementById("stage-grant").classList.add("grant-ended");
      show("Verifying", "tag-blue");
      later(VERIFY_AFTER_MS, () => { reach(7); show("Done", "tag-green"); });
      later(RECORD_AFTER_MS, () => pass(7));
    },
  };
  onBeat((beat) => beats[beat]?.());
}

const response = await fetch("../data/month.json");
if (!response.ok) throw new Error(`pipeline: data/month.json answered ${response.status}`);
fill(facts(await response.json()));
play();
