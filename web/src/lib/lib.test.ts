import { describe, expect, it } from "vitest";
import type { AuditEvent, TimelineLane, TimelineMarker, Topology, TopologyNode }
  from "@/api/types";
import { splitDiff } from "@/lib/diff";
import { stateSteps } from "@/lib/stateDurations";
import { formatDuration } from "@/lib/time";
import {
  attainmentLevel,
  buildLayout,
  clampDomain,
  clusterMarkers,
  filterMarkers,
  GLOBAL_ROW,
  initialDomain,
  maxZoom,
  NO_TIER,
  panDomain,
  rowKey,
  timeToX,
  zoomDomain,
} from "@/lib/timelineLayout";
import { COMPACT, diffTopology, FULL, layoutTopology } from "@/lib/topologyLayout";

const MINUTE = 60_000;
const at = (minutes: number) => new Date(Date.UTC(2026, 10, 17, 0, minutes));

function change(seq: number, minutes: number, to: string): AuditEvent {
  return { seq, ts: at(minutes).toISOString(), type: "workitem.state_changed",
    data: { to } } as unknown as AuditEvent;
}

describe("stateSteps", () => {
  it("measures each state and marks a state that the machine skipped", () => {
    const events = [change(2, 1, "Planned"), change(3, 2, "AwaitingApproval"),
      change(4, 12, "Approved"), change(5, 12, "Applying")];
    const steps = stateSteps(at(0).toISOString(), events, at(20));
    const byState = new Map(steps.map((step) => [step.state, step]));
    expect(byState.get("Detected")).toMatchObject({ ms: MINUTE, status: "complete" });
    expect(byState.get("Drafting")?.status).toBe("skipped");
    expect(byState.get("AwaitingApproval")?.ms).toBe(10 * MINUTE);
    expect(byState.get("Scheduled")?.status).toBe("skipped");
    expect(byState.get("Applying")).toMatchObject({ ms: 8 * MINUTE, status: "current" });
    expect(byState.get("Done")?.status).toBe("upcoming");
  });

  it("sums the visits of a state that the machine entered twice", () => {
    const events = [change(2, 1, "Planned"), change(3, 2, "Applying"),
      change(4, 5, "Planned"), change(5, 7, "Applying"), change(6, 8, "Verifying"),
      change(7, 9, "Done")];
    const steps = stateSteps(at(0).toISOString(), events, at(60));
    const planned = steps.find((step) => step.state === "Planned");
    expect(planned).toMatchObject({ ms: 3 * MINUTE, visits: 2 });
    expect(steps.find((step) => step.state === "Applying")?.ms).toBe(4 * MINUTE);
    expect(steps.at(-1)).toMatchObject({ state: "Done", status: "current", ms: 0 });
  });

  it("ends on a branch state and drops the main states that were never reached", () => {
    const events = [change(2, 1, "Planned"), change(3, 2, "AwaitingApproval"),
      change(4, 3, "Rejected")];
    const steps = stateSteps(at(0).toISOString(), events, at(60));
    expect(steps.map((step) => step.state)).toEqual(["Detected", "Drafting", "Planned",
      "AwaitingApproval", "Rejected"]);
    expect(steps.at(-1)).toMatchObject({ status: "current", ms: 0 });
  });

  it("handles a work item with no state change yet, and events out of order", () => {
    const fresh = stateSteps(at(0).toISOString(), [], at(3));
    expect(fresh[0]).toMatchObject({ state: "Detected", status: "current", ms: 3 * MINUTE });
    const shuffled = stateSteps(at(0).toISOString(),
      [change(3, 4, "AwaitingApproval"), change(2, 1, "Planned")], at(5));
    expect(shuffled.find((step) => step.state === "Planned")?.ms).toBe(3 * MINUTE);
  });
});

describe("timeline layout", () => {
  const lanes: TimelineLane[] = [{ env: "local", cloud: "local", layers: ["10-network",
    "50-app"] }];

  it("gives one row per environment, and layer rows only when expanded", () => {
    const collapsed = buildLayout(lanes, new Set());
    expect(collapsed.rows.map((row) => row.key)).toEqual([GLOBAL_ROW, "local"]);
    const expanded = buildLayout(lanes, new Set(["local"]));
    expect(expanded.rows.map((row) => row.key)).toEqual([GLOBAL_ROW, "local",
      "local/10-network", "local/50-app"]);
    expect(expanded.height).toBeGreaterThan(collapsed.height);
    expect(expanded.strips[0]?.y).toBeGreaterThan(expanded.rows.at(-1)?.y ?? 0);
  });

  it("puts an event in the right row", () => {
    const open = new Set(["local"]);
    expect(rowKey("", "", open)).toBe(GLOBAL_ROW);
    expect(rowKey("local", "50-app", new Set())).toBe("local");
    expect(rowKey("local", "50-app", open)).toBe("local/50-app");
    expect(rowKey("local", "", open)).toBe("local");
  });

  it("maps time to pixels linearly", () => {
    expect(timeToX(at(30), [at(0), at(60)], [100, 300])).toBe(200);
    expect(timeToX(at(0), [at(0), at(0)], [100, 300])).toBe(100);
  });

  it("clusters markers that are closer than the gap, per row", () => {
    const placed = [10, 14, 20, 60, 61].map((x, index) => ({ item: index, row: "a", x }));
    placed.push({ item: 9, row: "b", x: 12 });
    const clusters = clusterMarkers(placed, 16);
    const rowA = clusters.filter((cluster) => cluster.row === "a");
    expect(rowA.map((cluster) => cluster.items)).toEqual([[0, 1, 2], [3, 4]]);
    expect(rowA[0]?.x).toBe(15);
    expect(clusters.filter((cluster) => cluster.row === "b")).toHaveLength(1);
    expect(clusterMarkers([], 16)).toEqual([]);
  });

  it("never grows a cluster wider than the gap", () => {
    const placed = Array.from({ length: 300 }, (_, x) => ({ item: x, row: "a", x: x * 2 }));
    const clusters = clusterMarkers(placed, 16);
    expect(clusters).toHaveLength(38);
    expect(Math.max(...clusters.map((cluster) => cluster.items.length))).toBe(8);
  });

  it("filters by category, tier and range", () => {
    const marker = (seq: number, category: string, tier: string | null, minutes: number) =>
      ({ seq, category, tier, ts: at(minutes).toISOString() }) as TimelineMarker;
    const markers = [marker(1, "change", "T2", 0), marker(2, "grant", null, 5),
      marker(3, "change", "T3", 10)];
    const all = { categories: new Set<string>(), tiers: new Set<string>(), range: null };
    expect(filterMarkers(markers, all)).toHaveLength(3);
    expect(filterMarkers(markers, { ...all, categories: new Set(["change"]) })
      .map((entry) => entry.seq)).toEqual([1, 3]);
    expect(filterMarkers(markers, { ...all, tiers: new Set([NO_TIER]) })
      .map((entry) => entry.seq)).toEqual([2]);
    expect(filterMarkers(markers, { ...all, range: [at(4), at(10)] })
      .map((entry) => entry.seq)).toEqual([2, 3]);
  });

  it("zooms, pans and clamps the visible domain", () => {
    const domain: [Date, Date] = [at(0), at(60)];
    expect(zoomDomain(domain, 2)).toEqual([at(15), at(45)]);
    expect(panDomain(domain, 0.5)).toEqual([at(30), at(90)]);
    expect(clampDomain([at(-30), at(30)], at(0), at(120), MINUTE)).toEqual([at(0), at(60)]);
    expect(clampDomain([at(100), at(160)], at(0), at(120), MINUTE)).toEqual([at(60), at(120)]);
    expect(clampDomain([at(10), at(10)], at(0), at(120), 10 * MINUTE)).toEqual([at(10), at(20)]);
    expect(clampDomain([at(-500), at(500)], at(0), at(120), MINUTE)).toEqual([at(0), at(120)]);
    expect(maxZoom(at(0), at(100), 10 * MINUTE)).toBe(10);
  });

  it("starts on the last 30 days and the next 14, inside the data", () => {
    const day = 24 * 60;
    const [start, end] = initialDomain(at(0), at(-45 * day), at(60 * day));
    expect(start).toEqual(at(-30 * day));
    expect(end).toEqual(at(14 * day));
    expect(initialDomain(at(0), at(-day), at(day))).toEqual([at(-day), at(day)]);
  });

  it("classifies attainment", () => {
    expect(attainmentLevel(1)).toBe("good");
    expect(attainmentLevel(0.97)).toBe("warn");
    expect(attainmentLevel(0.5)).toBe("bad");
  });
});

function node(id: string, layer: string, version = ""): TopologyNode {
  return { id, layer, kind: "", label: id, detail: "", version, status: "ok", badges: [] };
}

function topology(nodes: TopologyNode[]): Topology {
  return { env: "local", nodes, edges: [], layers: [] } as unknown as Topology;
}

describe("topology", () => {
  it("puts the application layer on top and hides the bootstrap layer", () => {
    const layout = layoutTopology(["00-bootstrap", "10-network", "50-app"],
      [node("network", "10-network"), node("web", "50-app"), node("api", "50-app")]);
    expect(layout.rows.map((row) => row.layer)).toEqual(["50-app", "10-network"]);
    const y = (id: string) => layout.nodes.find((placed) => placed.node.id === id)?.y ?? -1;
    expect(y("web")).toBeLessThan(y("network"));
    expect(layout.width).toBeGreaterThan(2 * FULL.nodeWidth);
    const compact = layoutTopology(["50-app"], [node("web", "50-app"), node("api", "50-app")],
      COMPACT);
    expect(compact.width).toBe(2 * COMPACT.nodeWidth + COMPACT.columnGap);
  });

  it("marks a layer with no node as empty", () => {
    const layout = layoutTopology(["10-network", "20-cluster"], [node("network", "10-network")]);
    expect(layout.rows.find((row) => row.layer === "20-cluster")?.empty).toBe(true);
  });

  it("finds version, existence and badge differences", () => {
    const then = topology([node("api", "50-app", "1.0.0"), node("old", "50-app")]);
    const drifted = { ...node("web", "50-app"), badges: ["drift"] };
    const now = topology([node("api", "50-app", "1.1.0"), drifted]);
    const differences = diffTopology(then, now);
    expect(differences.get("api")).toEqual(["Version 1.0.0 then, 1.1.0 now"]);
    expect(differences.get("old")).toEqual(["Does not exist now"]);
    expect(differences.get("web")).toEqual(["Did not exist then"]);
    expect(diffTopology(then, then).size).toBe(0);
  });
});

describe("splitDiff and formatDuration", () => {
  it("splits a diff by file and counts the changed lines", () => {
    const diff = ["diff --git a/x.tf b/x.tf", "--- a/x.tf", "+++ b/x.tf", "@@ -1 +1 @@",
      "-old", "+new", "+more", "diff --git a/y b/y", "+++ b/y", "+only"].join("\n");
    const files = splitDiff(diff);
    expect(files.map((file) => [file.path, file.added, file.removed]))
      .toEqual([["x.tf", 2, 1], ["y", 1, 0]]);
    expect(splitDiff("")).toEqual([]);
  });

  it("formats a duration with its two largest units", () => {
    expect(formatDuration(500)).toBe("under 1 s");
    expect(formatDuration(125 * MINUTE)).toBe("2 h 5 min");
    expect(formatDuration(24 * 60 * MINUTE + 5 * MINUTE)).toBe("1 d");
    expect(formatDuration(61_000)).toBe("1 min 1 s");
  });
});
