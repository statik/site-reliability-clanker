// Prerender the architecture page: the system diagram with a real change travelling
// through it, the work item state machine with real paths, and the grant figures.

import { clock, day, dayYear, duration, html, list, raw, shortHash } from "./html.mjs";
import { displayName, grantViews } from "./month.mjs";
import { BOAT_SVG, tierTag } from "./render-walkthrough.mjs";

// ---------------------------------------------------------------- system diagram

const NODE_W = 150;
const NODE_H = 44;

const SYSTEM_NODES = {
  approver: { x: 80, y: 110, label: "Approver", w: 120 },
  dashboard: { x: 270, y: 110, label: "Dashboard" },
  orchestrator: { x: 450, y: 110, label: "Orchestrator" },
  agent: { x: 630, y: 110, label: "Agent runtime", kind: "model" },
  policy: { x: 270, y: 230, label: "Policy engine" },
  forge: { x: 630, y: 230, label: "Git forge" },
  detectors: { x: 270, y: 350, label: "Detectors" },
  broker: { x: 450, y: 350, label: "Access broker" },
  runner: { x: 630, y: 350, label: "Runner" },
  audit: { x: 450, y: 462, label: "Audit log and evidence", w: 510 },
  openrouter: { x: 935, y: 60, label: "OpenRouter", kind: "model" },
  environment: { x: 935, y: 350, label: "Environment", w: 0 },
  kubernetes: { x: 935, y: 235, label: "Kubernetes" },
  postgres: { x: 935, y: 297, label: "Managed Postgres" },
  files: { x: 935, y: 359, label: "Shared file system" },
  balancer: { x: 935, y: 421, label: "Load balancer" },
  metrics: { x: 935, y: 483, label: "Metrics and logs" },
};

const SYSTEM_EDGES = [
  ["approver", "dashboard"], ["dashboard", "orchestrator"], ["orchestrator", "agent"],
  ["agent", "openrouter"], ["agent", "forge"], ["forge", "orchestrator"],
  ["orchestrator", "policy"], ["detectors", "orchestrator"], ["orchestrator", "runner"],
  ["runner", "broker"], ["broker", "audit"], ["runner", "audit"],
];

const STOP_NODES = {
  "finding.opened": "detectors",
  "workitem.created": "orchestrator",
  "agent.drafted": "agent",
  "pr.opened": "forge",
  "plan.created": "runner",
  "policy.evaluated": "policy",
  "approval.requested": "dashboard",
  "approval.recorded": "approver",
  "grant.issued": "broker",
  "apply.started": "environment",
  "grant.revoked": "broker",
  "apply.succeeded": "runner",
  "verify.passed": "environment",
  "pr.merged": "forge",
};

function boxEdgePoint(node, toward) {
  const halfW = (node.w ?? NODE_W) / 2;
  const halfH = NODE_H / 2;
  const dx = toward.x - node.x;
  const dy = toward.y - node.y;
  if (dx === 0 && dy === 0) return { x: node.x, y: node.y };
  const scale = Math.min(
    dx === 0 ? Infinity : halfW / Math.abs(dx),
    dy === 0 ? Infinity : halfH / Math.abs(dy),
  );
  return { x: node.x + dx * scale, y: node.y + dy * scale };
}

function straightEdge(from, to, className) {
  const start = boxEdgePoint(from, to);
  const end = boxEdgePoint(to, from);
  return `<path class="${className}" d="M${start.x.toFixed(1)} ${start.y.toFixed(1)} L${end.x.toFixed(1)} ${end.y.toFixed(1)}" marker-end="url(#arrow)"/>`;
}

function systemNode(id, node) {
  if (id === "environment") return "";
  const width = node.w ?? NODE_W;
  const x = node.x - width / 2;
  const y = node.y - NODE_H / 2;
  const kind = node.kind ? ` node-${node.kind}` : "";
  return `<g class="node${kind}" data-node="${id}">
    <rect x="${x}" y="${y}" width="${width}" height="${NODE_H}"/>
    <text x="${node.x}" y="${node.y + 5}" text-anchor="middle">${node.label}</text>
  </g>`;
}

export function renderSystemSvg() {
  const edges = SYSTEM_EDGES.map(([from, to]) =>
    straightEdge(SYSTEM_NODES[from], SYSTEM_NODES[to], "edge")).join("");
  const nodes = Object.entries(SYSTEM_NODES).map(([id, node]) => systemNode(id, node)).join("");
  return raw(`<svg class="diagram system-diagram" viewBox="0 0 1100 520" role="img"
    aria-labelledby="system-title system-desc">
    <title id="system-title">The parts of the system</title>
    <desc id="system-desc">An approver uses the dashboard. Inside the control plane, the
      orchestrator works with the agent runtime, the policy engine, detectors, the git forge,
      a runner and an access broker, and every part writes to the audit log. The agent calls a
      language model through OpenRouter. Only the runner changes the environment, and only
      with short-lived credentials from the broker.</desc>
    <defs><marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="9"
      markerHeight="9" markerUnits="userSpaceOnUse" orient="auto-start-reverse"><path class="arrow-head" d="M0 0 L10 5 L0 10 z"/></marker></defs>
    <rect class="group" x="170" y="40" width="560" height="460"/>
    <text class="group-label" x="186" y="64">Clanker control plane</text>
    <rect class="group" x="830" y="170" width="210" height="350" data-node="environment"/>
    <text class="group-label" x="846" y="194">One environment per cloud</text>
    ${edges}
    <path class="edge" d="M705 350 L828 350" marker-end="url(#arrow)"/>
    <text class="edge-label" x="766" y="340" text-anchor="middle">plan, apply</text>
    <path class="edge edge-readonly" d="M705 96 L850 180" marker-end="url(#arrow)"/>
    <text class="edge-label" x="812" y="124" text-anchor="middle">read-only tools</text>
    <text class="edge-label" x="640" y="174" text-anchor="start">opens PRs</text>
    ${nodes}
    <g class="token" id="system-token" transform="translate(270 350)" aria-hidden="true">
      <circle r="20"/><g transform="translate(-13 -10) scale(0.82)">${BOAT_SVG.replace(/<\/?svg[^>]*>/g, "")}</g>
    </g>
  </svg>`);
}

export function pickFlowItem(month) {
  const details = Object.values(month.work_items);
  return details.find((detail) => detail.pull && detail.work_item.state === "Done" &&
    detail.work_item.kind === "app-upgrade") ?? details.find((detail) => detail.pull);
}

export function renderFlowSteps(month) {
  const detail = pickFlowItem(month);
  const id = detail.work_item.id;
  const created = month.events.find(
    (event) => event.type === "workitem.created" && event.work_item === id,
  );
  const finding = month.events.filter(
    (event) => event.type === "finding.opened" && event.seq < created.seq,
  ).at(-1);
  const own = month.events.filter((event) => event.work_item === id && STOP_NODES[event.type]);
  const seen = new Set();
  const stops = [finding, ...own].filter((event) => {
    if (!event || seen.has(event.type)) return false;
    seen.add(event.type);
    return true;
  });
  const total = month.events.filter((event) => event.work_item === id).length;
  const items = stops.map((event) => {
    const node = SYSTEM_NODES[STOP_NODES[event.type]];
    return html`<li data-node="${STOP_NODES[event.type]}" data-x="${node.x}" data-y="${node.y}">
      <span class="flow-where">${node.label}</span>
      <span class="flow-what">${event.summary}</span>
      <time class="mono" datetime="${event.ts}">${clock(event.ts)}</time></li>`;
  });
  items.push(html`<li data-node="audit" data-x="${SYSTEM_NODES.audit.x}" data-y="${SYSTEM_NODES.audit.y}">
    <span class="flow-where">Audit log</span>
    <span class="flow-what">${total} records for ${id}, each with the hash of the record before it</span>
    <time class="mono" datetime="${stops.at(-1).ts}">${clock(stops.at(-1).ts)}</time></li>`);
  return html`<p class="figure-source">The steps below are the real events of work item
      <span class="mono">${id}</span> (${detail.work_item.title}), ${dayYear(created.ts)}, times in UTC.</p>
    <ol class="flow-steps" id="flow-steps">${items}</ol>`;
}

// ---------------------------------------------------------------- state machine

const STATE_W = 132;
const STATE_H = 36;

const STATES = {
  Detected: { x: 90, y: 170 },
  Drafting: { x: 245, y: 170 },
  Planned: { x: 400, y: 170 },
  AwaitingApproval: { x: 570, y: 170, label: "Awaiting approval" },
  Approved: { x: 740, y: 170 },
  Scheduled: { x: 890, y: 170 },
  ChangesRequested: { x: 400, y: 40, label: "Changes requested" },
  Rejected: { x: 740, y: 40 },
  Applying: { x: 890, y: 320 },
  Verifying: { x: 710, y: 320 },
  Done: { x: 530, y: 320 },
  Failed: { x: 530, y: 420 },
  RolledBack: { x: 710, y: 420, label: "Rolled back" },
};

// The transitions of the plan (docs/PLAN.md section 5.1), with an optional bend so that a
// curve does not pass through another state.
const PLANNED_TRANSITIONS = [
  ["Detected", "Drafting", "the agent picks it up"],
  ["Drafting", "Planned", "pull request opened, plan saved"],
  ["Planned", "AwaitingApproval", "policy assigns the tier"],
  ["Planned", "Approved", "covered by a standing approval", -70],
  ["AwaitingApproval", "Approved", "approved"],
  ["AwaitingApproval", "ChangesRequested", "changes requested"],
  ["ChangesRequested", "Drafting", "back to the author"],
  ["AwaitingApproval", "Rejected", "rejected"],
  ["Approved", "Scheduled", "waits for the maintenance window"],
  ["Scheduled", "Applying", "grant issued"],
  ["Applying", "Verifying", "applied; the grant is revoked"],
  ["Verifying", "Done", "checks passed"],
  ["Applying", "Failed", "the apply failed"],
  ["Verifying", "RolledBack", "the canary failed; rolled back"],
  ["Applying", "Planned", "the plan is stale; plan again", 30],
];

// Transitions that the real month shows and the plan's diagram does not draw.
const OBSERVED_LABELS = {
  "Detected>Planned": ["plans saved; no drafting was needed", -70],
  "Detected>Approved": ["a runbook under a standing approval", 50],
  "Approved>Applying": ["no window to wait for; grant issued", 0],
  "Applying>Done": ["the runbook finished", 60],
  "Drafting>Failed": ["no routine runbook fits; a human must decide", 0],
};

function stateEdgePoint(state, toward) {
  const dx = toward.x - state.x;
  const dy = toward.y - state.y;
  const scale = Math.min(
    dx === 0 ? Infinity : STATE_W / 2 / Math.abs(dx),
    dy === 0 ? Infinity : STATE_H / 2 / Math.abs(dy),
  );
  return { x: state.x + dx * scale, y: state.y + dy * scale };
}

function transitionPath(from, to, bend) {
  const a = STATES[from];
  const b = STATES[to];
  const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
  const length = Math.hypot(b.x - a.x, b.y - a.y);
  const normal = { x: -(b.y - a.y) / length, y: (b.x - a.x) / length };
  // Bend twice as far at the control point, so the curve itself bends by `bend`.
  const control = { x: mid.x + normal.x * bend * 2, y: mid.y + normal.y * bend * 2 };
  const start = stateEdgePoint(a, bend === 0 ? b : control);
  const end = stateEdgePoint(b, bend === 0 ? a : control);
  const fixed = (point) => `${point.x.toFixed(1)} ${point.y.toFixed(1)}`;
  return bend === 0
    ? `M${fixed(start)} L${fixed(end)}`
    : `M${fixed(start)} Q${fixed(control)} ${fixed(end)}`;
}

function observedTransitions(month) {
  const seen = new Set();
  for (const event of month.events) {
    if (event.type === "workitem.state_changed") seen.add(`${event.data.from}>${event.data.to}`);
  }
  return seen;
}

export function renderStateSvg(month) {
  const observed = observedTransitions(month);
  const transitions = PLANNED_TRANSITIONS.map(([from, to, label, bend]) => ({
    from, to, label, bend: bend ?? 0, planned: true,
  }));
  for (const key of observed) {
    const [from, to] = key.split(">");
    if (!STATES[from] || !STATES[to]) continue;
    if (transitions.some((edge) => edge.from === from && edge.to === to)) continue;
    const [label, bend] = OBSERVED_LABELS[key] ?? ["seen in the month", 0];
    transitions.push({ from, to, label, bend, planned: false });
  }
  const edges = transitions.map((edge) => {
    const key = `${edge.from}>${edge.to}`;
    const classes = ["edge", edge.planned ? "" : "edge-extra", observed.has(key) ? "is-observed" : ""];
    return `<path class="${classes.join(" ").trim()}" data-edge="${key}" data-label="${edge.label}"
      d="${transitionPath(edge.from, edge.to, edge.bend)}" marker-end="url(#state-arrow)"/>`;
  }).join("");
  const nodes = Object.entries(STATES).map(([name, state]) => `<g class="node state" data-state="${name}">
    <rect x="${state.x - STATE_W / 2}" y="${state.y - STATE_H / 2}" width="${STATE_W}" height="${STATE_H}"/>
    <text x="${state.x}" y="${state.y + 5}" text-anchor="middle">${state.label ?? name}</text></g>`).join("");
  return raw(`<svg class="diagram state-diagram" viewBox="0 0 980 450" role="img"
    aria-labelledby="state-title state-desc">
    <title id="state-title">The life of a work item</title>
    <desc id="state-desc">A work item is detected, drafted, planned, and then waits for approval
      unless a standing approval covers it. After approval it is scheduled, applied under a grant,
      verified and done. It can also be sent back for changes, rejected, fail, roll back, or
      return to planned when its plan is stale.</desc>
    <defs><marker id="state-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="9"
      markerHeight="9" markerUnits="userSpaceOnUse" orient="auto-start-reverse"><path class="arrow-head" d="M0 0 L10 5 L0 10 z"/></marker></defs>
    ${edges}${nodes}
    <circle class="token-dot" id="state-token" r="9" cx="${STATES.Detected.x}" cy="${STATES.Detected.y - 30}" aria-hidden="true"/>
  </svg>`);
}

function fullPath(month, id) {
  const path = [];
  for (const event of month.events) {
    if (event.type !== "workitem.state_changed" || event.work_item !== id) continue;
    if (path.length === 0) path.push(event.data.from);
    path.push(event.data.to);
  }
  return path;
}

function pathChoices(month) {
  const details = Object.values(month.work_items);
  const refused = month.events.find((event) => event.type === "apply.refused")?.work_item;
  const picks = [
    ["A release", pickFlowItem(month)],
    ["A bad release", details.find((detail) => detail.work_item.state === "RolledBack")],
    ["A stale plan", refused ? month.work_items[refused] : undefined],
    ["A routine runbook", details.find((detail) => detail.work_item.kind.startsWith("runbook"))],
    ["An incident", details.find((detail) => detail.work_item.kind === "incident")],
  ];
  return picks.filter(([, detail]) => detail && fullPath(month, detail.work_item.id).length > 1);
}

export function renderStatePaths(month) {
  const choices = pathChoices(month).map(([label, detail], index) => {
    const item = detail.work_item;
    const path = fullPath(month, item.id);
    return html`<li><button type="button" class="chip" data-path="${path.join(">")}"
        aria-pressed="${index === 0 ? "true" : "false"}">${label}
        <span class="mono">${item.id}</span></button>
      <span class="path-text">${tierTag(item.tier)} ${item.title}: ${path.join(" → ")}</span></li>`;
  });
  return html`<ul class="path-choices" id="path-choices">${choices}</ul>
    <p class="figure-caption" id="state-caption" aria-live="polite"></p>`;
}

// ---------------------------------------------------------------- grants

export function renderGrantCard(month) {
  const detail = pickFlowItem(month);
  const grant = grantViews(month).find(
    (candidate) => candidate.work_item === detail.work_item.id && candidate.kind === "apply",
  );
  if (!grant) return html`<p>No apply grant is in the export.</p>`;
  const full = month.grants.find((candidate) => candidate.id === grant.id);
  const rows = [
    ["work_item", grant.work_item], ["environment", full.env], ["layer", grant.layer],
    ["identity", grant.identity], ["commit", shortHash(grant.commit)],
    ["plan_sha256", `${grant.plan_sha256.slice(0, 4)}…${grant.plan_sha256.slice(-4)}`],
    ["tier", grant.tier], ["approvers", grant.approvers.join(", ")],
    ["not_before", full.not_before.slice(0, 19) + "Z"], ["expires", full.expires.slice(0, 19) + "Z"],
  ];
  const share = Math.max(1, (grant.held_seconds / (grant.window_minutes * 60)) * 100);
  return html`<div class="grant-figure" id="grant-figure">
    <div class="grant-card">
      <p class="grant-card-title"><span class="mono">${grant.id}</span>
        <span class="tag tag-green grant-live">Credential live</span>
        <span class="tag tag-red grant-dead">Revoked</span></p>
      <dl class="grant-fields mono">${rows.map(([key, value], index) => html`<div style="--i:${index}">
        <dt>${key}</dt><dd>${value}</dd></div>`)}</dl>
    </div>
    <div class="grant-life">
      <p class="grant-life-title">The ${grant.window_minutes}-minute window of this grant</p>
      <div class="window-bar" role="img" aria-label="The credential was live for
        ${duration(grant.held_seconds)} of the ${grant.window_minutes}-minute window">
        <span class="window-fill" style="--share:${share.toFixed(2)}%"></span>
      </div>
      <p class="grant-life-note">Issued ${clock(grant.issued_at)}. The runner applied the
        approved plan. Revoked ${clock(grant.ended_at)}: live for
        <strong>${duration(grant.held_seconds)}</strong>. The other
        ${duration(grant.window_minutes * 60 - grant.held_seconds)} of the window were never
        used, and the credential was already gone.</p>
      <ol class="grant-checks">
        <li style="--i:0">The signature of the grant is valid</li>
        <li style="--i:1">Approvals: ${list(grant.approvers.map((actor) => displayName(actor, month.users)))}
          (tier ${grant.tier})</li>
        <li style="--i:2">The time is inside the window</li>
        <li style="--i:3">The plan hash is the hash that was approved</li>
      </ol>
    </div>
  </div>`;
}

export function renderGrantLanes(month, totals, position, axis) {
  const grants = grantViews(month);
  const identities = [...new Set(grants.map((grant) => grant.identity))].sort();
  const lanes = identities.map((identity) => {
    const own = grants.filter((grant) => grant.identity === identity);
    const total = own.reduce((sum, grant) => sum + (grant.held_seconds ?? 0), 0);
    return html`<div class="lane"><span class="lane-name mono">${identity}</span>
      <div class="lane-track">${own.map((grant) => html`<span class="blip blip-${grant.kind}"
        style="left:${position(grant.issued_at).toFixed(2)}%"
        title="${grant.id}, ${day(grant.issued_at)} ${clock(grant.issued_at)}, ${duration(grant.held_seconds ?? 0)}"></span>`)}</div>
      <span class="lane-total">${own.length} ×, ${duration(total)}</span></div>`;
  });
  const standing = html`<div class="lane lane-standing"><span class="lane-name">Standing write access</span>
    <div class="lane-track"></div><span class="lane-total">none</span></div>`;
  return html`<div class="lanes" role="img" aria-label="Access over ${totals.days} days. There is no
      standing write access. ${totals.grants} grants appear as thin marks, ${duration(totals.held_seconds)} in total.">
      ${standing}${lanes}
      <div class="lane lane-axis"><span class="lane-name"></span><div class="lane-track">
        <span class="axis-tick" style="left:0%">${day(axis.start)}</span>
        <span class="axis-tick axis-tick-end" style="left:100%">${day(axis.end)}</span></div>
        <span class="lane-total"></span></div>
    </div>
    <p class="figure-caption">Each mark is one grant, drawn wider than its true length so that you
      can see it. At true scale, all ${totals.grants} grants together
      (${duration(totals.held_seconds)}) are ${(totals.held_share * 100).toFixed(3)}% of the
      width of this chart.</p>`;
}

export function renderRefusal(month) {
  const refusal = month.events.find((event) => event.type === "agent.failed");
  const diagnosis = month.events.find((event) => event.type === "incident.diagnosed");
  if (!refusal) return html``;
  return html`<figure class="quote">
    <blockquote><p>“${refusal.data.error}”</p></blockquote>
    <figcaption>The orchestrator, ${dayYear(refusal.ts)} at ${clock(refusal.ts)} UTC, after the
      Investigator role (${diagnosis?.data?.model ?? "a language model"}) wrote its diagnosis of
      the incident. The model could read, under a debug grant. It had no tool to act.</figcaption>
  </figure>`;
}
