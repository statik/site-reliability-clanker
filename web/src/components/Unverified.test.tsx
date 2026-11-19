// @vitest-environment happy-dom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import { UserName } from "@/components/Unverified";

vi.mock("@/state/session", () => ({
  useSession: () => ({
    users: [
      { name: "sam", display: "Sam Lee", role: "approver", email: "", last_seen: "",
        self_declared: true },
      { name: "bob", display: "Bob Okafor", role: "approver", email: "", last_seen: "",
        self_declared: false },
    ],
  }),
}));

function textOf(name: string): string {
  const host = document.createElement("div");
  act(() => createRoot(host).render(<UserName name={name} />));
  return host.textContent ?? "";
}

it("marks only a name that the person gave", () => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  expect(textOf("sam")).toBe("sam Unverified");
  expect(textOf("bob")).toBe("bob");
  expect(textOf("nobody")).toBe("nobody");
});
