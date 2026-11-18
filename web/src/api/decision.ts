import { serverTime } from "@/api/urls";
import { formatTime } from "@/lib/time";
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

/**
 * Says what a work item with a scheduled time waits for. A tier T3 change can wait for a
 * maintenance window of the runbooks repository. The apply can start after the start of
 * that window, when an approver asked for a later time inside it.
 *
 * @returns Text without a capital letter or a full stop, or "" when no time is set.
 */
export function waitNote(
  item: Pick<WorkItem, "state" | "scheduled_for" | "maintenance_window">,
): string {
  if (item.scheduled_for === "") {
    return "";
  }
  if (item.state !== "Scheduled" || item.maintenance_window === "") {
    return `scheduled for ${formatTime(item.scheduled_for)}`;
  }
  const note =
    `waits for the maintenance window that starts at ${formatTime(item.maintenance_window)}`;
  if (item.scheduled_for === item.maintenance_window) {
    return note;
  }
  return `${note}; the apply starts at ${formatTime(item.scheduled_for)}`;
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
