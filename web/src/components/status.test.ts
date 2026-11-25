import { describe, expect, it } from "vitest";
import type { ComponentState } from "@/api/types";
import { componentLook } from "@/components/status";

const STATES: ComponentState[] = ["ok", "degraded", "down", "unknown"];

describe("the look of a live state", () => {
  it("gives each of the four states its own colour and its own label", () => {
    const looks = STATES.map((state) => componentLook(state));
    expect(new Set(looks.map((look) => look.type)).size).toBe(STATES.length);
    expect(new Set(looks.map((look) => look.label)).size).toBe(STATES.length);
  });

  it("draws down as an error and unknown as neutral", () => {
    expect(componentLook("ok")).toEqual({ type: "green", label: "Healthy" });
    expect(componentLook("degraded")).toEqual({ type: "magenta", label: "Degraded" });
    expect(componentLook("down")).toEqual({ type: "red", label: "Down" });
    expect(componentLook("unknown")).toEqual({ type: "gray", label: "Unknown" });
  });
});
