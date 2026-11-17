import { serverTime } from "@/api/urls";
import type { Approval, WorkItem } from "@/api/types";

export type DecisionAction = "approve" | "schedule" | "request_changes" | "reject";

export interface DecisionBody {
  decision: "approve" | "request_changes" | "reject";
  comment: string;
  scheduled_for: string;
}

export interface DecisionRequest {
  path: string;
  body: DecisionBody;
}

export const ACTION_LABEL: Record<DecisionAction, string> = {
  approve: "Approve",
  schedule: "Approve and schedule",
  request_changes: "Request changes",
  reject: "Reject",
};

/**
 * Builds the decision request. The server rejects unknown fields, and "approve and schedule" is
 * the decision `approve` with `scheduled_for` set.
 *
 * @param scheduleLocal The value of a datetime-local input, used only by the schedule action.
 * @throws Error when the schedule action has no valid time, or a refusal has no comment.
 */
export function buildDecision(
  workItem: string,
  action: DecisionAction,
  comment: string,
  scheduleLocal: string,
): DecisionRequest {
  const trimmed = comment.trim();
  if ((action === "reject" || action === "request_changes") && trimmed === "") {
    throw new Error("Write a comment that says what must change or why you reject it.");
  }
  let scheduledFor = "";
  if (action === "schedule") {
    const time = new Date(scheduleLocal);
    if (scheduleLocal === "" || Number.isNaN(time.getTime())) {
      throw new Error("Choose the date and time of the maintenance window.");
    }
    scheduledFor = serverTime(time);
  }
  return {
    path: `/work-items/${encodeURIComponent(workItem)}/decision`,
    body: {
      decision: action === "schedule" ? "approve" : action,
      comment: trimmed,
      scheduled_for: scheduledFor,
    },
  };
}

/** Lists the distinct approvers whose approval is for the current commit of the work item. */
export function validApprovers(workItem: WorkItem, approvals: Approval[]): string[] {
  const names = new Set<string>();
  for (const approval of approvals) {
    if (approval.decision === "approve" && approval.commit === workItem.commit) {
      names.add(approval.approver);
    }
  }
  return [...names].toSorted();
}
