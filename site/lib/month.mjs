// Turn the exported month (data/month.json) into the steps of the walkthrough.
//
// The simulator marks the start of each scene with a `sim.scene_started` event, so
// the steps follow the event stream itself. Two long stretches are split at a real
// event, so that each step tells one story:
//   - the first run, at the first successful apply (bootstrap, then the deployment);
//   - the incident, at the first pull request that a person opened afterwards.
// A final step has no scene: it reports the audit chain and the totals of the month.

const HUMAN = "human";
const MODEL = "model";
const CODE = "code";
const SIM = "simulator";

const MODEL_ACTORS = new Set(["author", "explainer", "investigator", "agent", "clanker-author"]);
const SIM_ACTORS = new Set(["simulator"]);

const NOISE_TYPES = new Set(["notify.sent", "sim.scene_started", "sim.clock_advanced"]);

export function actorKind(actor, users) {
  if (users.some((user) => user.name === actor)) return HUMAN;
  if (MODEL_ACTORS.has(actor)) return MODEL;
  if (SIM_ACTORS.has(actor)) return SIM;
  return CODE;
}

export function displayName(actor, users) {
  if (actor?.startsWith("standing:")) {
    return `the standing approval of ${displayName(actor.slice("standing:".length), users)}`;
  }
  return users.find((user) => user.name === actor)?.display ?? actor;
}

export function seconds(from, to) {
  return (new Date(to).getTime() - new Date(from).getTime()) / 1000;
}

function splitByScene(events) {
  const segments = [{ scene: "first-run", events: [] }];
  for (const event of events) {
    if (event.type === "sim.scene_started") {
      segments.push({ scene: event.data.scene, events: [] });
    }
    segments[segments.length - 1].events.push(event);
  }
  return segments;
}

function splitAfter(segment, names, isBoundary, inclusive) {
  const index = segment.events.findIndex(isBoundary);
  if (index === -1) return [{ ...segment, scene: names[0] }];
  const cut = inclusive ? index + 1 : index;
  return [
    { scene: names[0], events: segment.events.slice(0, cut) },
    { scene: names[1], events: segment.events.slice(cut) },
  ];
}

function segmentsOf(month) {
  const result = [];
  for (const segment of splitByScene(month.events)) {
    if (segment.scene === "first-run") {
      const isFirstApply = (event) => event.type === "apply.succeeded";
      result.push(...splitAfter(segment, ["blank-slate", "deploy"], isFirstApply, true));
    } else if (segment.scene === "incident") {
      const isHumanPull = (event) =>
        event.type === "workitem.created" && actorKind(event.actor, month.users) === HUMAN &&
        month.work_items[event.work_item]?.pull;
      result.push(...splitAfter(segment, ["incident", "stale-plan"], isHumanPull, false));
    } else {
      result.push(segment);
    }
  }
  return result.filter((segment) => segment.events.length > 0);
}

function grantView(grant) {
  return {
    id: grant.id,
    work_item: grant.work_item,
    identity: grant.identity,
    layer: grant.layer,
    kind: grant.kind,
    tier: grant.tier,
    approvers: grant.approvers ?? [],
    status: grant.status,
    issued_at: grant.issued_at,
    ended_at: grant.ended_at,
    window_minutes: Math.round(seconds(grant.not_before, grant.expires) / 60),
    held_seconds: grant.ended_at ? seconds(grant.issued_at, grant.ended_at) : null,
    plan_sha256: grant.plan_sha256 ?? "",
    commit: grant.commit ?? "",
  };
}

function statePath(events, workItem) {
  const path = [];
  for (const event of events) {
    if (event.type !== "workitem.state_changed" || event.work_item !== workItem) continue;
    if (path.length === 0) path.push(event.data.from);
    path.push(event.data.to);
  }
  return path;
}

function stepOf(segment, month) {
  const events = segment.events;
  const shown = events.filter((event) => !NOISE_TYPES.has(event.type));
  const timed = shown.length > 0 ? shown : events;
  const ids = [...new Set(events.map((event) => event.work_item).filter(Boolean))];
  const seqs = new Set(events.map((event) => event.seq));
  const grantIds = new Set(
    events.filter((event) => event.type.startsWith("grant.") && event.data?.grant)
      .map((event) => event.data.grant),
  );
  const notifications = {};
  for (const event of events) {
    if (event.type !== "notify.sent") continue;
    notifications[event.data.channel] = (notifications[event.data.channel] ?? 0) + 1;
  }
  const scene = month.sim.scenes.find((candidate) => candidate.name === segment.scene);
  return {
    id: segment.scene,
    scene,
    from: timed[0].ts,
    to: timed[timed.length - 1].ts,
    first_seq: events[0].seq,
    last_seq: events[events.length - 1].seq,
    events: shown,
    event_count: events.length,
    notifications,
    work_items: ids.map((id) => ({
      ...month.work_items[id].work_item,
      approvers_required: month.work_items[id].approvers_required,
      path: statePath(month.events, id),
      path_in_step: statePath(events, id),
      approvals: month.work_items[id].approvals.filter(
        (approval) => approval.at >= events[0].ts && approval.at <= events[events.length - 1].ts,
      ),
      plans: month.work_items[id].plans,
      pull: month.work_items[id].pull,
      diff: month.work_items[id].diff,
    })),
    grants: month.grants.filter((grant) => grantIds.has(grant.id)).map(grantView)
      .sort((a, b) => a.issued_at.localeCompare(b.issued_at)),
    has: (type) => events.some((event) => event.type === type && seqs.has(event.seq)),
    find: (type) => events.find((event) => event.type === type),
    all: (type) => events.filter((event) => event.type === type),
  };
}

export function totals(month) {
  const grants = month.grants.map(grantView);
  const ended = grants.filter((grant) => grant.held_seconds !== null);
  const write = ended.filter((grant) => grant.kind !== "debug");
  const count = (type) => month.events.filter((event) => event.type === type).length;
  const kinds = { human: 0, model: 0, code: 0, simulator: 0 };
  for (const event of month.events) kinds[actorKind(event.actor, month.users)] += 1;
  const span = seconds(month.first_event, month.last_event);
  const heldTotal = ended.reduce((sum, grant) => sum + grant.held_seconds, 0);
  return {
    events: month.events.length,
    days: Math.round(span / 86400),
    span_seconds: span,
    work_items: Object.keys(month.work_items).length,
    approvals: count("approval.recorded"),
    applies: count("apply.succeeded"),
    runbooks: count("runbook.succeeded"),
    rollbacks: Object.values(month.work_items)
      .filter((detail) => detail.work_item.state === "RolledBack").length,
    refused: count("apply.refused"),
    incidents: count("incident.opened"),
    notifications: count("notify.sent"),
    grants: grants.length,
    grants_open: grants.length - ended.length,
    write_grants: write.length,
    debug_grants: ended.length - write.length,
    held_seconds: heldTotal,
    longest: ended.reduce((best, grant) => (grant.held_seconds > best.held_seconds ? grant : best)),
    held_share: heldTotal / span,
    actor_kinds: kinds,
    chain: month.audit,
  };
}

export function grantViews(month) {
  return month.grants.map(grantView).sort((a, b) => a.issued_at.localeCompare(b.issued_at));
}

export function steps(month) {
  const list = segmentsOf(month).map((segment) => stepOf(segment, month));
  list.push({
    id: "audit",
    scene: undefined,
    from: month.last_event,
    to: month.sim.now,
    first_seq: 1,
    last_seq: month.events[month.events.length - 1].seq,
    events: [],
    event_count: 0,
    notifications: {},
    work_items: [],
    grants: [],
    has: () => false,
    find: () => undefined,
    all: () => [],
  });
  return list;
}

/**
 * Returns the event where code refused what the Investigator proposed for an incident.
 * A failure of another model role, for example of the Reviewer, is not a refusal.
 */
export function incidentRefusal(month, events = month.events) {
  return events.find(
    (event) =>
      event.type === "agent.failed" &&
      month.work_items[event.work_item]?.work_item.kind === "incident",
  );
}
