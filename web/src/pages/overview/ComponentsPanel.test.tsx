// @vitest-environment happy-dom
import { act } from "react";
import type { ReactNode } from "react";
import { createRoot } from "react-dom/client";
import type { Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ComponentState, Observation, ObservedComponent, ObservedInstance }
  from "@/api/types";
import { TopologyDiagram } from "@/components/topology/TopologyDiagram";
import { commonVersion, ComponentsPanel, ComponentsView }
  from "@/pages/overview/ComponentsPanel";

const session = vi.hoisted(() => ({ asOf: null as Date | null }));
vi.mock("@/state/session", () => ({
  useSession: () => ({ userName: "alice", asOf: session.asOf }),
}));
vi.mock("@/state/live", () => ({
  useLive: () => ({ connected: true, subscribe: () => () => undefined }),
}));

let host: HTMLElement;
let root: Root;

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  session.asOf = null;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

function instance(id: string, state: ComponentState, version?: string, restarts = 0):
ObservedInstance {
  return version === undefined ? { id, state, restarts } : { id, state, version, restarts };
}

function component(name: string, state: ComponentState, instances: ObservedInstance[]):
ObservedComponent {
  const ready = instances.filter((one) => one.state === "ok").length;
  return { name, runtime: "kubernetes", state, wanted: instances.length, ready, instances };
}

function observation(components: ObservedComponent[], age = 7, stale = false): Observation {
  return { schema: 1, env: "local", at: "2026-11-20T20:35:38Z", application: "paperboat",
    version: { wanted: "1.0.0", source: "image" }, components, checks: [], volumes: [],
    age_seconds: age, stale };
}

function show(node: ReactNode): void {
  act(() => root.render(node));
}

/** The text of the cells of each component row, without the cell of the expand button. */
function rows(): string[][] {
  return [...host.querySelectorAll("tbody > tr")].map((row) =>
    [...row.querySelectorAll("td")].slice(1).map((cell) => cell.textContent ?? ""));
}

function stubFetch(status: number, body: string, type: string): void {
  vi.stubGlobal("fetch", () => Promise.resolve(
    new Response(body, { status, headers: { "Content-Type": type } })));
}

async function showPanel(envs: string[]): Promise<void> {
  // The request settles after the render, so the test waits for it inside `act`.
  await act(async () => {
    root.render(<ComponentsPanel envs={envs} />);
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

describe("the rows of the components", () => {
  it("names each of the four states as text", () => {
    show(<ComponentsView observation={observation([
      component("api", "ok", [instance("api-1", "ok", "1.0.0")]),
      component("web", "degraded", [instance("web-1", "ok", "1.0.0"),
        instance("web-2", "down", "1.0.0")]),
      component("worker", "down", []),
      component("loadgen", "unknown", [instance("loadgen-1", "unknown")]),
    ])} />);
    expect(rows()).toEqual([
      ["api", "Healthy", "1 of 1 ready", "1.0.0", "–"],
      ["web", "Degraded", "1 of 2 ready", "1.0.0", "–"],
      ["worker", "Down", "0 of 0 ready", "–", "–"],
      ["loadgen", "Unknown", "0 of 1 ready", "–", "–"],
    ]);
    expect(host.querySelector("table")?.getAttribute("aria-label"))
      .toBe("Components of paperboat in local");
    expect([...host.querySelectorAll("thead th")].slice(1).map((head) => head.textContent))
      .toEqual(["Component", "State", "Ready", "Version", "Restarts"]);
  });

  it("says mixed when the instances run different versions", () => {
    show(<ComponentsView observation={observation([
      component("api", "ok", [instance("api-1", "ok", "1.0.0"), instance("api-2", "ok", "1.1.0")]),
    ])} />);
    expect(rows()[0]?.[3]).toBe("mixed");
    expect(commonVersion([instance("a", "ok", "1.0.0"), instance("b", "ok")])).toBe("1.0.0");
    expect(commonVersion([])).toBe("–");
  });

  it("adds the restarts of the instances, and shows them only above zero", () => {
    show(<ComponentsView observation={observation([
      component("api", "ok", [instance("api-1", "ok", "1.0.0", 2),
        instance("api-2", "ok", "1.0.0", 1)]),
      component("web", "ok", [instance("web-1", "ok", "1.0.0", 1)]),
    ])} />);
    expect(rows().map((row) => row[4])).toEqual(["3 restarts", "1 restart"]);
  });

  it("shows the instances of a component when its row expands", () => {
    show(<ComponentsView observation={observation([
      component("api", "degraded", [instance("api-6d9f7c5b8-x2k4q", "ok", "1.0.0"),
        instance("api-6d9f7c5b8-p9w2z", "down", undefined, 4)]),
    ])} />);
    const button = host.querySelector("tbody button");
    expect(button?.getAttribute("aria-label")).toBe("Show the instances of api");
    act(() => {
      button?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    const inner = host.querySelector("table[aria-label='Instances of api']");
    const cells = [...(inner?.querySelectorAll("tbody tr") ?? [])].map((row) =>
      [...row.querySelectorAll("td")].map((cell) => cell.textContent));
    expect(cells).toEqual([
      ["api-6d9f7c5b8-x2k4q", "Healthy", "1.0.0", "–"],
      ["api-6d9f7c5b8-p9w2z", "Down", "–", "4 restarts"],
    ]);
  });
});

describe("the age of the report", () => {
  it("says how old a fresh report is", () => {
    show(<ComponentsView observation={observation([], 7)} />);
    expect(host.textContent).toContain("Last report 7 seconds ago");
    expect(host.querySelector(".cds--inline-notification")).toBeNull();
    show(<ComponentsView observation={observation([], 1)} />);
    expect(host.textContent).toContain("Last report 1 second ago");
  });

  it("warns when the report is stale", () => {
    show(<ComponentsView observation={observation(
      [component("api", "unknown", [instance("api-1", "unknown")])], 75, true)} />);
    expect(host.querySelector(".cds--inline-notification--warning")?.textContent)
      .toContain("No report for more than 60 seconds; the state is unknown");
    expect(host.textContent).not.toContain("Last report");
    expect(rows()[0]?.[1]).toBe("Unknown");
  });

  it("says that no report arrived yet", () => {
    show(<ComponentsView observation={observation([], -1, true)} />);
    expect(host.querySelector(".cds--inline-notification--warning")?.textContent)
      .toContain("No report yet");
    expect(host.textContent).not.toContain("60 seconds");
  });
});

describe("the panel", () => {
  it("loads the report of the environment", async () => {
    const calls: string[] = [];
    vi.stubGlobal("fetch", (url: string) => {
      calls.push(url);
      return Promise.resolve(Response.json(observation([component("api", "ok", [])])));
    });
    await showPanel(["local"]);
    expect(calls).toEqual(["/api/v1/observation?env=local"]);
    expect(host.querySelector("h2")?.textContent).toBe("Components");
    expect(rows()[0]?.[0]).toBe("api");
  });

  it("shows nothing when the control plane answers 404", async () => {
    stubFetch(404, JSON.stringify({ error: "not found" }), "application/json");
    await showPanel(["local"]);
    expect(host.innerHTML).toBe("");
  });

  it("shows nothing when an older control plane answers with the dashboard page", async () => {
    stubFetch(200, "<!doctype html><html></html>", "text/html; charset=utf-8");
    await showPanel(["local"]);
    expect(host.innerHTML).toBe("");
  });

  it("shows another error as the other panels do", async () => {
    stubFetch(500, JSON.stringify({ error: "the store is closed" }), "application/json");
    await showPanel(["local"]);
    expect(host.textContent).toContain("Could not load the components of local.");
    expect(host.textContent).toContain("the store is closed");
  });

  it("shows nothing for a past time and with no deployed environment", async () => {
    const fetched = vi.fn();
    vi.stubGlobal("fetch", fetched);
    await showPanel([]);
    expect(host.innerHTML).toBe("");
    session.asOf = new Date("2026-11-19T00:00:00Z");
    await showPanel(["local"]);
    expect(host.innerHTML).toBe("");
    expect(fetched).not.toHaveBeenCalled();
  });
});

describe("a node of the topology", () => {
  it("names each of the four statuses as text, each with its own colour", () => {
    const states: ComponentState[] = ["ok", "degraded", "down", "unknown"];
    const nodes = states.map((status) => ({ id: status, layer: "50-app", kind: "service",
      label: status, detail: "", version: "", status, badges: [] }));
    show(<TopologyDiagram topology={{ env: "local", as_of: "", snapshot_ts: "",
      snapshot_reason: "", layers: ["50-app"], nodes, edges: [], health: null }} />);
    const tags = [...host.querySelectorAll(".topo-node .cds--tag")];
    expect(tags.map((tag) => tag.textContent)).toEqual(["Healthy", "Degraded", "Down",
      "Unknown"]);
    const colours = tags.map((tag) =>
      [...tag.classList].find((name) => /--(green|magenta|red|gray)$/.test(name)));
    expect(new Set(colours).size).toBe(4);
  });
});
