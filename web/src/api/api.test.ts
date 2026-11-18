import { describe, expect, it } from "vitest";
import { buildDecision, validApprovers, waitNote } from "@/api/decision";
import type { Approval, WorkItem } from "@/api/types";
import { apiUrl, honoursAsOf, query, serverTime } from "@/api/urls";

const AS_OF = new Date("2026-11-20T02:30:00.000Z");

describe("apiUrl", () => {
  it("leaves the URL alone when no as-of time is set", () => {
    expect(apiUrl("/overview")).toBe("/api/v1/overview");
    expect(apiUrl("/topology?env=local", null)).toBe("/api/v1/topology?env=local");
  });

  it("adds as_of in the server's time format to the endpoints that honour it", () => {
    expect(apiUrl("/overview", AS_OF))
      .toBe("/api/v1/overview?as_of=2026-11-20T02%3A30%3A00.000Z");
    expect(apiUrl("/topology?env=local", AS_OF))
      .toBe("/api/v1/topology?env=local&as_of=2026-11-20T02%3A30%3A00.000Z");
    expect(apiUrl("/work-items", AS_OF)).toContain("as_of=");
    expect(apiUrl("/posture", AS_OF)).toContain("as_of=");
  });

  it("does not add as_of to other endpoints, or to one work item", () => {
    for (const path of ["/access", "/timeline", "/events?q=x", "/work-items/WI-0001", "/sim"]) {
      expect(honoursAsOf(path)).toBe(false);
      expect(apiUrl(path, AS_OF)).not.toContain("as_of");
    }
  });

  it("formats times as the server does, so that string comparison on the server works", () => {
    expect(serverTime(new Date("2026-11-20T02:30:00Z"))).toBe("2026-11-20T02:30:00.000Z");
    expect(serverTime(new Date("2026-11-20T03:30:00+01:00"))).toBe("2026-11-20T02:30:00.000Z");
  });
});

describe("query", () => {
  it("drops empty and undefined parameters", () => {
    expect(query({ q: "", from: undefined })).toBe("");
    expect(query({ q: "apply failed", limit: 5, to: "" })).toBe("?q=apply+failed&limit=5");
  });
});

describe("buildDecision", () => {
  it("builds exactly the three fields that the server accepts", () => {
    const request = buildDecision("WI-0007", "approve", "  looks right ", "");
    expect(request.path).toBe("/work-items/WI-0007/decision");
    expect(request.body).toEqual({ decision: "approve", comment: "looks right",
      scheduled_for: "" });
    expect(Object.keys(request.body).toSorted()).toEqual(["comment", "decision",
      "scheduled_for"]);
  });

  it("sends approve-and-schedule as approve with a UTC time", () => {
    const request = buildDecision("WI-0007", "schedule", "", "2026-11-21T02:00:00Z");
    expect(request.body.decision).toBe("approve");
    expect(request.body.scheduled_for).toBe("2026-11-21T02:00:00.000Z");
  });

  it("refuses to schedule without a valid time", () => {
    expect(() => buildDecision("WI-0007", "schedule", "", "")).toThrow(/date and time/);
    expect(() => buildDecision("WI-0007", "schedule", "", "not a date")).toThrow(/date and time/);
  });

  it("requires a comment to reject or to request changes", () => {
    expect(() => buildDecision("WI-0007", "reject", "   ", "")).toThrow(/comment/);
    expect(() => buildDecision("WI-0007", "request_changes", "", "")).toThrow(/comment/);
    expect(buildDecision("WI-0007", "request_changes", "pin the version", "").body.decision)
      .toBe("request_changes");
  });

  it("escapes the work item ID in the path", () => {
    expect(buildDecision("WI/1", "approve", "", "").path).toBe("/work-items/WI%2F1/decision");
  });
});

function approval(approver: string, decision: string, commit: string): Approval {
  return { approver, decision, commit } as Approval;
}

describe("validApprovers", () => {
  const item = { commit: "abc" } as WorkItem;
  it("counts each approver once, and only approvals of the current commit", () => {
    const approvals = [
      approval("alice", "approve", "abc"),
      approval("alice", "approve", "abc"),
      approval("bob", "approve", "old"),
      approval("carol", "request_changes", "abc"),
    ];
    expect(validApprovers(item, approvals)).toEqual(["alice"]);
  });

  it("returns nobody when there is no approval", () => {
    expect(validApprovers(item, [])).toEqual([]);
  });
});

describe("waitNote", () => {
  const start = "2026-11-21T02:00:00.000Z";

  it("says nothing when no time is set", () => {
    expect(waitNote({ state: "AwaitingApproval", scheduled_for: "", maintenance_window: "" }))
      .toBe("");
  });

  it("names the time that an approver set", () => {
    expect(waitNote({ state: "Scheduled", scheduled_for: start, maintenance_window: "" }))
      .toBe("scheduled for 21 Nov 2026, 02:00 UTC");
  });

  it("names the start of the maintenance window", () => {
    expect(waitNote({ state: "Scheduled", scheduled_for: start, maintenance_window: start }))
      .toBe("waits for the maintenance window that starts at 21 Nov 2026, 02:00 UTC");
  });

  it("names the time of the apply when it is after the start of the window", () => {
    const note = waitNote({ state: "Scheduled", scheduled_for: "2026-11-21T03:00:00.000Z",
      maintenance_window: start });
    expect(note).toBe("waits for the maintenance window that starts at 21 Nov 2026, 02:00 UTC" +
      "; the apply starts at 21 Nov 2026, 03:00 UTC");
  });

  it("does not name a window that the work item no longer waits for", () => {
    expect(waitNote({ state: "Rejected", scheduled_for: start, maintenance_window: start }))
      .toBe("scheduled for 21 Nov 2026, 02:00 UTC");
  });
});
