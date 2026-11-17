// Prerender the walkthrough: the month rail and one section for each step.

import {
  clock, day, dayRange, duration, html, list, plural, raw, shortHash,
} from "./html.mjs";
import { actorKind, displayName, grantViews, seconds } from "./month.mjs";
import { PRINCIPLES, story } from "./story.mjs";

const HEADLINE_TYPES = new Set([
  "sim.release_published", "sim.advisory_published", "sim.manual_change", "sim.fault_injected",
  "standing_approval.signed", "finding.opened", "pr.opened", "policy.evaluated",
  "approval.recorded", "grant.issued", "grant.debug_issued", "grant.revoked",
  "apply.succeeded", "apply.failed", "apply.refused", "verify.failed", "verify.passed",
  "rollback.authorized", "runbook.succeeded", "incident.diagnosed", "agent.failed",
  "pr.merged", "pr.updated", "approval.requested",
]);
const MAX_HEADLINES = 9;
const MAX_PER_TYPE = 2;
const MAX_TRACKS = 3;
const MAX_GRANT_ROWS = 4;

const KIND_LABEL = { human: "Person", model: "Model", code: "Code", simulator: "Simulator" };
const GOOD_STATES = new Set(["Done"]);
const BAD_STATES = new Set(["Failed", "RolledBack", "Rejected"]);

export function tierTag(tier) {
  if (!tier) return html`<span class="tag tag-gray">No tier</span>`;
  return html`<span class="tag tag-${tier.toLowerCase()}">${tier}</span>`;
}

function stateClass(state) {
  if (GOOD_STATES.has(state)) return "is-good";
  if (BAD_STATES.has(state)) return "is-bad";
  return "";
}

function headlines(step) {
  const perType = {};
  const picked = [];
  for (const event of step.events) {
    if (!HEADLINE_TYPES.has(event.type)) continue;
    // The log can hold the same record twice (a person who approves again). Show it once.
    if (picked.some((other) => other.summary === event.summary && other.actor === event.actor)) continue;
    perType[event.type] = (perType[event.type] ?? 0) + 1;
    if (perType[event.type] <= MAX_PER_TYPE) picked.push(event);
  }
  if (picked.length <= MAX_HEADLINES) return picked;
  // Keep the first and the last events, so the list shows how the step began and ended.
  const head = picked.slice(0, Math.ceil(MAX_HEADLINES / 2));
  const tail = picked.slice(picked.length - Math.floor(MAX_HEADLINES / 2));
  return [...head, ...tail];
}

function eventRow(event, users) {
  const kind = actorKind(event.actor, users);
  const summary = event.summary.length > 190 ? `${event.summary.slice(0, 187)}…` : event.summary;
  return html`<tr>
    <td class="ev-time"><time datetime="${event.ts}">${day(event.ts)} ${clock(event.ts)}</time></td>
    <td class="ev-who"><span class="who who-${kind}"><span class="who-kind">${KIND_LABEL[kind]}</span>
      ${displayName(event.actor, users)}</span></td>
    <td class="ev-what">${summary}${
      event.work_item ? html` <span class="ev-wi">${event.work_item}</span>` : ""
    }</td>
  </tr>`;
}

function eventTable(events, users, caption) {
  return html`<table class="events">
    <caption class="visually-hidden">${caption}</caption>
    <thead><tr><th scope="col">When (UTC)</th><th scope="col">Who</th><th scope="col">What</th></tr></thead>
    <tbody>${events.map((event) => eventRow(event, users))}</tbody>
  </table>`;
}

function track(item) {
  const path = item.path_in_step.length > 0 ? item.path_in_step : [item.state];
  return html`<div class="track-block">
    <p class="track-title">${tierTag(item.tier)} <span class="mono">${item.id}</span>
      ${item.title}</p>
    <ol class="track" aria-label="States of ${item.id} in this step">
      ${path.map((state, index) => html`<li class="${stateClass(state)}" style="--i:${index}">${state}</li>`)}
    </ol>
  </div>`;
}

function grantRow(grant, users) {
  const share = grant.held_seconds === null
    ? 100
    : Math.max(0.6, (grant.held_seconds / (grant.window_minutes * 60)) * 100);
  const who = grant.kind === "debug"
    ? "debug session"
    : list(grant.approvers.map((actor) => displayName(actor, users)));
  return html`<li class="grant">
    <div class="grant-head">
      <span class="mono">${grant.id}</span>
      <span class="grant-identity mono">${grant.identity}</span>
      <span class="tag tag-${grant.status === "used" ? "green" : "red"}">${
        grant.status === "used" ? "Used, then revoked" : "Revoked"
      }</span>
    </div>
    <div class="grant-bar" role="img"
      aria-label="Held for ${duration(grant.held_seconds ?? 0)} of a ${grant.window_minutes}-minute window">
      <span class="grant-fill" style="--share:${share.toFixed(2)}%"></span>
    </div>
    <p class="grant-note">Issued ${clock(grant.issued_at)}, ended ${
      grant.ended_at ? clock(grant.ended_at) : "not yet"
    }. Held <strong>${duration(grant.held_seconds ?? 0)}</strong> of a
      ${grant.window_minutes}-minute window. Approved by: ${who}.</p>
  </li>`;
}

function approvalFact(step, users) {
  const approvals = step.work_items.flatMap((item) => item.approvals);
  if (approvals.length > 0) {
    const people = [...new Set(approvals.map((approval) => approval.approver))];
    return list(people.map((actor) => displayName(actor, users)));
  }
  if (step.has("rollback.authorized")) return "The approval of the original change";
  if (step.work_items.some((item) => item.tier === "T1")) return "Standing approval";
  if (step.work_items.length === 0) return "None needed";
  return "None given";
}

function accessFact(step) {
  if (step.grants.length === 0) return "No grant issued";
  const total = step.grants.reduce((sum, grant) => sum + (grant.held_seconds ?? 0), 0);
  return `${plural(step.grants.length, "grant")}, ${duration(total)} in total`;
}

function outcomeFact(step) {
  if (step.work_items.length === 0) return "A forecast on the timeline";
  const counts = {};
  for (const item of step.work_items) {
    const last = item.path_in_step.at(-1) ?? item.state;
    counts[last] = (counts[last] ?? 0) + 1;
  }
  return Object.entries(counts)
    .map(([state, count]) => (count === 1 ? state : `${state} × ${count}`)).join(", ");
}

function facts(step, users) {
  const tiers = [...new Set(step.work_items.map((item) => item.tier).filter(Boolean))];
  const rules = [...new Set(step.work_items.map((item) => item.tier_rule).filter(Boolean))];
  return html`<dl class="facts">
    <div><dt>Risk tier</dt><dd>${
      tiers.length > 0 ? tiers.map(tierTag) : "No change, no tier"
    }${rules.length > 0 ? html` <span class="fact-note">rule <code>${rules.join(", ")}</code></span>` : ""}</dd></div>
    <div><dt>Approved by</dt><dd>${approvalFact(step, users)}</dd></div>
    <div><dt>Write or debug access</dt><dd>${accessFact(step)}</dd></div>
    <div><dt>Outcome</dt><dd>${outcomeFact(step)}</dd></div>
  </dl>`;
}

function auditEvidence(month, totals) {
  const last = month.events.slice(-4);
  const kinds = totals.actor_kinds;
  return html`<div class="chain" aria-label="The last records of the hash chain">
      ${last.map((event) => html`<div class="chain-link">
        <span class="mono">#${event.seq}</span>
        <span class="chain-type">${event.type}</span>
        <span class="mono chain-hash">${shortHash(event.hash)}</span>
      </div>`)}
      <div class="chain-link chain-head"><span>Head</span>
        <span class="mono chain-hash">${totals.chain.head_hash.slice(0, 16)}…</span>
        <span class="tag tag-${totals.chain.ok ? "green" : "red"}">${
          totals.chain.ok ? "Chain verified" : "Chain broken"
        }</span></div>
    </div>
    <dl class="facts facts-wide">
      <div><dt>Records written by code</dt><dd>${kinds.code}</dd></div>
      <div><dt>By a language model role</dt><dd>${kinds.model}</dd></div>
      <div><dt>By a person</dt><dd>${kinds.human}</dd></div>
      <div><dt>By the simulator</dt><dd>${kinds.simulator}</dd></div>
      <div><dt>Notifications sent</dt><dd>${totals.notifications}</dd></div>
      <div><dt>Longest grant</dt><dd>${duration(totals.longest.held_seconds)}
        <span class="fact-note mono">${totals.longest.identity}</span></dd></div>
    </dl>`;
}

function distinctFirst(items, keyOf) {
  const seen = new Set();
  const first = [];
  const rest = [];
  for (const item of items) {
    const key = keyOf(item);
    if (seen.has(key)) rest.push(item);
    else first.push(item);
    seen.add(key);
  }
  return [...first, ...rest];
}

function evidence(step, context) {
  const { users, month, totals } = context;
  if (step.id === "audit") return auditEvidence(month, totals);
  const shown = headlines(step);
  // Show work items and grants of different kinds first, so that seven incidents with
  // the same path do not hide the runbook that fixed them.
  const tracks = distinctFirst(step.work_items, (item) => `${item.kind} ${item.path_in_step.join(">")}`)
    .slice(0, MAX_TRACKS);
  const moreTracks = step.work_items.length - tracks.length;
  const grants = distinctFirst(step.grants, (grant) => grant.identity).slice(0, MAX_GRANT_ROWS);
  const notified = Object.entries(step.notifications)
    .map(([channel, count]) => `${count} by ${channel}`);
  return html`
    ${tracks.map(track)}
    ${moreTracks > 0 ? html`<p class="more">and ${plural(moreTracks, "more work item")} in this step
      (${list(step.work_items.filter((item) => !tracks.includes(item)).map((item) => item.id))}).</p>` : ""}
    ${grants.length > 0 ? html`<ul class="grants">${grants.map((grant) => grantRow(grant, users))}</ul>` : ""}
    ${step.grants.length > grants.length
      ? html`<p class="more">and ${plural(step.grants.length - grants.length, "more grant")}, each revoked.</p>` : ""}
    ${eventTable(shown, users, `Main events of the step ${step.id}`)}
    <details class="all-events">
      <summary>All ${step.events.length} events of this step${
        notified.length > 0 ? `, and ${list(notified)} notifications` : ""
      }</summary>
      ${eventTable(step.events, users, `All events of the step ${step.id}`)}
    </details>`;
}

export function renderSteps(stepList, context) {
  const sections = stepList.map((step, index) => {
    const words = story(step, context);
    const principle = words.principle ? PRINCIPLES[words.principle - 1] : null;
    const useCase = step.scene?.use_case ? ` · use case ${step.scene.use_case}` : "";
    return html`<section class="step" id="${step.id}" data-step="${index}"
        data-at="${context.position(step.to).toFixed(2)}" aria-labelledby="${step.id}-title">
      <div class="step-inner">
        <div class="step-text">
          <p class="kicker reveal">Step ${index + 1} of ${stepList.length} ·
            ${dayRange(step.from, step.to)}${useCase}</p>
          <h2 class="reveal" id="${step.id}-title">${words.title}</h2>
          <p class="lead reveal">${words.lead}</p>
          ${step.id === "audit" ? "" : html`<div class="reveal">${facts(step, context.users)}</div>`}
          ${principle ? html`<p class="principle-note reveal"><span class="principle-number">Principle
            ${words.principle}</span> ${principle.title}</p>` : ""}
        </div>
        <div class="step-evidence reveal">${evidence(step, context)}</div>
      </div>
    </section>`;
  });
  return html`${sections}`;
}

function healthSegments(month, position) {
  const buckets = Object.values(month.timeline.health).flat();
  return buckets.map((bucket) => {
    const left = position(bucket.ts);
    const width = Math.max(0.5, position(bucket.end) - left);
    const percent = Math.round(bucket.attainment * 100);
    const good = bucket.attainment >= 0.999;
    return html`<span class="health ${good ? "is-good" : "is-bad"}"
      style="left:${left.toFixed(2)}%;width:${width.toFixed(2)}%"
      title="${day(bucket.ts)}: ${percent}% of ${bucket.requests} sampled requests succeeded">${
        good ? "" : html`<span class="health-label">${percent}%</span>`
      }</span>`;
  });
}

export function renderRail(stepList, context) {
  const { month, position } = context;
  const start = new Date(context.axis.start);
  const ticks = [];
  for (let offset = 0; ; offset += 7) {
    const date = new Date(start.getTime() + offset * 86400000);
    if (date.getTime() > new Date(context.axis.end).getTime()) break;
    ticks.push(html`<span class="axis-tick" style="left:${position(date.toISOString()).toFixed(2)}%">${day(date.toISOString())}</span>`);
  }
  const blips = grantViews(month).map((grant) => html`<span class="blip blip-${grant.kind}"
    style="left:${position(grant.issued_at).toFixed(2)}%"></span>`);
  const dots = stepList.map((step, index) => {
    const words = story(step, context);
    return html`<li><a class="dot" href="#${step.id}" data-step="${index}"
      title="${index + 1}. ${words.title}"><span class="visually-hidden">Step ${index + 1}:
      ${words.title}</span></a></li>`;
  });
  const span = Math.round(seconds(context.axis.start, context.axis.end) / 86400);
  return html`<div class="rail-controls">
      <button type="button" class="btn btn-ghost rail-prev js-only" aria-label="Previous step">
        <span aria-hidden="true">←</span> Previous</button>
      <ol class="dots" aria-label="Steps of the month">${dots}</ol>
      <span class="rail-counter js-only" aria-live="polite">1 / ${stepList.length}</span>
      <button type="button" class="btn btn-ghost rail-next js-only" aria-label="Next step">
        Next <span aria-hidden="true">→</span></button>
    </div>
    <div class="axis" role="img" aria-label="${span} days on one axis: the health of the service,
      and a mark for each of the ${month.grants.length} grants. Each mark is a few seconds of access.">
      <div class="axis-row axis-health"><span class="axis-name">Health</span>
        <div class="axis-lane">${healthSegments(month, position)}</div></div>
      <div class="axis-row axis-grants"><span class="axis-name">Grants</span>
        <div class="axis-lane">${blips}</div></div>
      <div class="axis-row axis-dates"><span class="axis-name"></span>
        <div class="axis-lane">${ticks}<span class="axis-marker" aria-hidden="true"></span></div></div>
    </div>`;
}
