import type { AuditEvent } from "@/api/types";

/** The main path of the state machine in section 5.1 of the plan. */
export const MAIN_PATH = [
  "Detected",
  "Drafting",
  "Planned",
  "AwaitingApproval",
  "Approved",
  "Scheduled",
  "Applying",
  "Verifying",
  "Done",
];

export const TERMINAL_STATES = ["Done", "Failed", "RolledBack", "Rejected"];
const BRANCH_STATES = ["ChangesRequested", "Failed", "RolledBack", "Rejected"];

export interface StateStep {
  state: string;
  /** Total time in this state over every visit. */
  ms: number;
  visits: number;
  status: "complete" | "current" | "skipped" | "upcoming";
}

interface Visit {
  state: string;
  from: number;
}

function visitsOf(createdAt: string, events: AuditEvent[]): Visit[] {
  const visits: Visit[] = [{ state: "Detected", from: new Date(createdAt).getTime() }];
  const changes = events
    .filter((event) => event.type === "workitem.state_changed")
    .toSorted((a, b) => a.seq - b.seq);
  for (const change of changes) {
    const to = change.data?.["to"];
    if (typeof to === "string") {
      visits.push({ state: to, from: new Date(change.ts).getTime() });
    }
  }
  return visits;
}

/**
 * Computes the time spent in each state from the state-change events of one work item. A state
 * the machine entered more than once has its visits summed. The current state counts up to
 * `now`, unless it is terminal.
 */
export function stateSteps(createdAt: string, events: AuditEvent[], now: Date): StateStep[] {
  const visits = visitsOf(createdAt, events);
  const totals = new Map<string, { ms: number; visits: number }>();
  visits.forEach((visit, index) => {
    const next = visits[index + 1];
    const isLast = next === undefined;
    const end = isLast ? now.getTime() : next.from;
    const ms = isLast && TERMINAL_STATES.includes(visit.state) ? 0 : Math.max(end - visit.from, 0);
    const total = totals.get(visit.state) ?? { ms: 0, visits: 0 };
    totals.set(visit.state, { ms: total.ms + ms, visits: total.visits + 1 });
  });
  const current = visits.at(-1)?.state ?? "Detected";
  const currentIndex = MAIN_PATH.indexOf(current);
  const lastMainIndex = Math.max(...visits.map((visit) => MAIN_PATH.indexOf(visit.state)));
  const steps: StateStep[] = [];
  MAIN_PATH.forEach((state, index) => {
    const total = totals.get(state);
    let status: StateStep["status"] = "upcoming";
    if (state === current) {
      status = "current";
    } else if (total !== undefined) {
      status = "complete";
    } else if (index < (currentIndex === -1 ? lastMainIndex : currentIndex)) {
      status = "skipped";
    }
    if (currentIndex === -1 && TERMINAL_STATES.includes(current) && status === "upcoming") {
      return;
    }
    steps.push({ state, ms: total?.ms ?? 0, visits: total?.visits ?? 0, status });
  });
  for (const state of BRANCH_STATES) {
    const total = totals.get(state);
    if (total !== undefined) {
      const status = state === current ? "current" : "complete";
      steps.push({ state, ms: total.ms, visits: total.visits, status });
    }
  }
  return steps;
}
