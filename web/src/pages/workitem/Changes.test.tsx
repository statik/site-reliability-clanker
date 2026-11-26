// @vitest-environment happy-dom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it } from "vitest";
import type { Plan, PlanFacts } from "@/api/types";
import { Changes } from "@/pages/workitem/Changes";

function plan(facts: Partial<PlanFacts>): Plan {
  return {
    id: 1, work_item: "WI-0001", layer: "50-app", commit: "c0ffee", plan_sha256: "abc",
    facts: { add: 0, change: 1, destroy: 0, replace: 0, resources: [], outputs: [], ...facts },
    policy: { tier: "T2", rule: "app-upgrade", reasons: [], create_only: false },
    superseded: false, created_at: "then",
  };
}

function textOf(plans: Plan[]): string {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  const host = document.createElement("div");
  act(() => createRoot(host).render(<Changes plans={plans} deferred={[]} />));
  return host.textContent ?? "";
}

it("shows a warning of the plan before the resources", () => {
  const warning = "This plan lowers harbor_release from 3.6.0 to 3.4.0 across release 3.5.0";
  const text = textOf([plan({ warnings: [warning] })]);
  expect(text).toContain("Read this before you decide.");
  expect(text).toContain(`Layer 50-app: ${warning}`);
});

it("shows no warning callout for a plan without warnings", () => {
  expect(textOf([plan({})])).not.toContain("Read this before you decide.");
});
