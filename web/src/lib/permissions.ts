import type { WorkItem } from "@/api/types";

export interface PermissionRequest {
  identity: string;
  scope: string;
  duration: string;
}

const BOOTSTRAP_LAYER = "00-bootstrap";

/**
 * Describes, in plain language, the access that a work item asks for. The identities and the
 * durations follow section 6 of the plan and the lifetimes that the orchestrator sets.
 */
export function permissionRequests(workItem: WorkItem): PermissionRequest[] {
  const { kind, env, tier } = workItem;
  if (kind.startsWith("runbook:")) {
    const runbook = kind.slice("runbook:".length);
    return [{
      identity: "runbook",
      scope: `Run the runbook "${runbook}" in ${env}, and nothing else. It is covered by a ` +
        "standing approval that a human signed once; each use is logged.",
      duration: "One job. The token lasts at most 15 minutes and is revoked when the job ends.",
    }];
  }
  if (kind === "debug-session" || kind === "incident") {
    return [{
      identity: "debugger",
      scope: `Read-only diagnostic commands in the paperboat namespace of ${env}. ` +
        "Every command and its output is recorded.",
      duration: "One session of at most 15 minutes.",
    }];
  }
  const layers = workItem.layers ?? [];
  if (tier === "T0" || layers.length === 0) {
    return [];
  }
  const commit = workItem.commit === "" ? "the approved commit" : workItem.commit.slice(0, 10);
  const requests: PermissionRequest[] = [{
    identity: "planner",
    scope: `Read ${env} to make each plan. It cannot change anything.`,
    duration: "A read-only token of 15 minutes for each plan.",
  }];
  for (const layer of layers) {
    const local = layer === BOOTSTRAP_LAYER ? " This layer has no cluster credential." : "";
    requests.push({
      identity: `applier-${layer}`,
      scope: `Apply the approved plan of layer ${layer} in ${env}. The grant is bound to ` +
        `commit ${commit} and to the hash of the plan, so any other plan is refused.${local}`,
      duration: "30 minutes from the start of the apply (one hour on AWS), revoked when the " +
        "job ends.",
    });
  }
  return requests;
}
