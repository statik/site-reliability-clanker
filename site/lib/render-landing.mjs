// Prerender the parts of the landing page that come from the exported month.

import { clock, dayYear, duration, html, plural, shortHash } from "./html.mjs";
import { displayName } from "./month.mjs";
import { pickFlowItem } from "./render-architecture.mjs";
import { tierTag } from "./render-walkthrough.mjs";
import { PRINCIPLES } from "./story.mjs";

export function renderStats(totals) {
  const stats = [
    [totals.days, "days of operation", "one scripted month on the local target"],
    [totals.events, "audit records", totals.chain.ok ? "hash chain verified" : "hash chain broken"],
    [totals.approvals, "human approvals", `for ${totals.work_items} work items`],
    [totals.grants, "grants issued", `${totals.grants - totals.grants_open} already revoked`],
    [duration(totals.held_seconds), "of write or debug access", `in ${totals.days} days, in total`],
    [totals.rollbacks, "automatic rollback", "caught by the canary check"],
  ];
  return html`<dl class="stats">${stats.map(([value, label, note]) => html`<div class="stat reveal">
    <dt>${label}</dt><dd><span class="stat-value">${value}</span>
    <span class="stat-note">${note}</span></dd></div>`)}</dl>`;
}

function evidenceLines(month, totals) {
  const refusal = month.events.find((event) => event.type === "agent.failed");
  const stale = month.events.find((event) => event.type === "apply.refused");
  const pulls = month.events.filter((event) => event.type === "pr.opened").length;
  const manual = month.events.find((event) => event.type === "sim.manual_change");
  const policy = month.events.filter((event) => event.type === "policy.evaluated").length;
  return [
    refusal
      ? html`${dayYear(refusal.ts)}, ${clock(refusal.ts)}: the model proposed a mitigation
          that is not a routine runbook. Code answered: “a human must decide”.
          Policy code set the tier of all ${policy} rated changes.`
      : html`Policy code set the tier of all ${policy} rated changes.`,
    html`${pulls} pull requests and ${totals.runbooks} runbook jobs.${
      manual ? " The one change made by hand was found by the drift check and put back through a pull request." : ""
    }`,
    stale
      ? html`${dayYear(stale.ts)}: ${stale.work_item} was approved, but its saved plan was
          stale. The apply was refused and the approval was requested again.`
      : html`Each of the ${totals.applies} applies ran the plan hash that was approved.`,
    html`${totals.grants} grants in ${totals.days} days. Write or debug access existed for
      ${duration(totals.held_seconds)} in total. The longest grant lasted
      ${duration(totals.longest.held_seconds)}.`,
    html`The local target ran this whole month. The AWS, Google Cloud and Azure stacks
      are validated against the same contract and are not applied yet.`,
  ];
}

export function renderPrinciples(month, totals) {
  const lines = evidenceLines(month, totals);
  return html`<ol class="principles">${PRINCIPLES.map((principle, index) => html`<li class="principle reveal">
    <span class="principle-index" aria-hidden="true">${index + 1}</span>
    <h3>${principle.title}</h3>
    <p>${principle.text}</p>
    <p class="principle-evidence"><span class="evidence-label">In the scripted month</span>
      ${lines[index]}</p>
  </li>`)}</ol>`;
}

function pickApprovalItem(month) {
  const details = Object.values(month.work_items);
  const explained = details.filter((detail) => detail.work_item.explanation?.sentence);
  return (
    explained.find((detail) => detail.work_item.state === "AwaitingApproval") ?? explained.at(-1)
  );
}

function changeList(plan) {
  const resources = plan?.facts?.resources ?? [];
  return html`<ul class="change-list">${resources.map((resource) => html`<li>
    <span class="tag tag-blue">${resource.action}</span>
    <code>${resource.address}</code>
    ${(resource.details ?? []).map((detail) => html`<span class="change-detail mono">${detail}</span>`)}
  </li>`)}</ul>`;
}

export function renderApproval(month) {
  const detail = pickApprovalItem(month);
  if (!detail) return html`<p>No work item with an approval summary is in the export.</p>`;
  const item = detail.work_item;
  const words = item.explanation;
  const plan = detail.plans.at(-1);
  const facts = plan?.facts ?? {};
  const lastGrant = detail.grants.at(-1);
  const waiting = item.state === "AwaitingApproval";
  const requested = month.events.filter(
    (event) => event.type === "approval.requested" && event.work_item === item.id,
  ).at(-1);
  return html`<article class="approval reveal" aria-label="Approval page for ${item.id}">
    <header class="approval-head">
      <p class="approval-id"><span class="mono">${item.id}</span> ${tierTag(item.tier)}
        <span class="tag tag-${waiting ? "blue" : "gray"}">${
          waiting ? "Waiting for approval" : item.state
        }</span></p>
      <h3>${item.title}</h3>
      <p class="approval-sentence">${words.sentence}
        <span class="ai-label" title="Written by ${words.source}; the facts come from the plan">AI</span></p>
    </header>
    <div class="approval-grid">
      <section><h4>Impact</h4>
        <dl class="kv">
          <div><dt>Downtime</dt><dd>${words.downtime}</dd></div>
          <div><dt>Effect on users</dt><dd>${words.user_effect}</dd></div>
          <div><dt>Cost</dt><dd>${words.cost_change}</dd></div>
        </dl></section>
      <section><h4>Risk tier</h4>
        <p>${tierTag(item.tier)} set by the policy rule <code>${item.tier_rule}</code>, not by
          the model. Needs ${plural(detail.approvers_required, "approver")}.</p>
        <h4>What changes</h4>
        <p class="plan-counts mono">${facts.add ?? 0} to add · ${facts.change ?? 0} to change ·
          ${facts.destroy ?? 0} to destroy · ${facts.replace ?? 0} to replace</p>
        ${changeList(plan)}</section>
      <section><h4>Safety</h4>
        <dl class="kv">
          <div><dt>Rollback</dt><dd>${words.rollback}</dd></div>
          <div><dt>Verification</dt><dd>${words.verification}</dd></div>
        </dl></section>
      <section><h4>Permissions requested</h4>
        <p>A grant for <code>${lastGrant?.identity ?? `applier-${item.layers[0]}`}</code>, for
          the layer <code>${item.layers.join(", ")}</code> only, bound to commit
          <code>${shortHash(item.commit)}</code> and plan
          <code>${shortHash(plan?.plan_sha256)}</code>. It is revoked when the job ends.</p>
        <h4>Evidence</h4>
        <p>Trigger: ${item.trigger}. Pull request #${item.pr_id} in
          <code>${detail.pull?.repo}</code>.${
            requested ? html` ${requested.summary} (${dayYear(requested.ts)}, ${clock(requested.ts)} UTC).` : ""
          }</p></section>
    </div>
    <footer class="approval-decide" aria-label="Decisions (illustration, not active)">
      <span class="btn btn-primary is-static">Approve</span>
      <span class="btn btn-tertiary is-static">Approve and schedule</span>
      <span class="btn btn-ghost is-static">Request changes</span>
      <span class="btn btn-ghost is-static">Reject</span>
      <span class="btn btn-ghost is-static">Defer</span>
    </footer>
    <p class="approval-source">The words and numbers are the real work item ${item.id},
      exported from the running control plane; the summary was written by
      <span class="mono">${words.source}</span> around facts read from the plan. The layout
      is an illustration of the approval page. For the dashboard itself, see
      <a href="screens.html">Live screens</a>.</p>
  </article>`;
}

export function renderApproverNames(month) {
  const people = month.users.filter((user) => user.role !== "viewer");
  return html`${people.map((user) => displayName(user.name, month.users)).join(", ")}`;
}

// The notice that the chat channels get for an approval request, with the text that
// internal/notify builds from the event: the summary, the sentence, the facts and the link.
export function renderNotice(month) {
  const id = pickFlowItem(month).work_item.id;
  const request = month.events.find(
    (event) => event.type === "approval.requested" && event.work_item === id,
  );
  const facts = [
    ["Work item", id], ["Environment", request.env], ["Tier", request.data.tier],
    ["Rule", request.data.rule], ["Approvers required", request.data.approvers_required],
  ];
  return html`<figure class="notice reveal">
    <div class="notice-card">
      <p class="notice-title">${request.summary}</p>
      <p>${request.data.sentence} Open the dashboard and sign in to decide. You cannot approve
        from this message.</p>
      <dl class="notice-facts">${facts.map(([label, value]) => html`<div><dt>${label}</dt><dd>${value}</dd></div>`)}</dl>
      <p class="notice-link">Open in Clanker <span aria-hidden="true">&rarr;</span></p>
    </div>
    <figcaption class="figure-caption">A drawing, not a screenshot. The words are the notice
      for ${id}, as the control plane sends it to Slack and to Teams.</figcaption>
  </figure>`;
}
