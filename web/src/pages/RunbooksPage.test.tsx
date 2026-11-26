// @vitest-environment happy-dom
import { act } from "react";
import { createRoot } from "react-dom/client";
import type { Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Role, Runbook, User } from "@/api/types";
import type { ApiState } from "@/hooks/useApi";
import { RunbookList, RunbooksPage, signature, standingControl } from "@/pages/RunbooksPage";

const post = vi.hoisted(() => vi.fn());
const remove = vi.hoisted(() => vi.fn());
const api = vi.hoisted(() => ({ state: null as unknown }));
const session = vi.hoisted(() => ({ user: null as unknown, users: [] as unknown[] }));

vi.mock("@/api/client", () => ({
  apiPost: post,
  apiDelete: remove,
  errorMessage: (error: unknown) => (error instanceof Error ? error.message : String(error)),
}));
vi.mock("@/hooks/useApi", () => ({ useApi: () => api.state }));
vi.mock("@/state/session", () => ({
  useSession: () => ({ user: session.user, users: session.users }),
}));

function person(role: Role, name: string = role): User {
  return { name, display: `The ${role}`, role, email: `${name}@example.com`, last_seen: "",
    self_declared: false };
}

const ALICE: User = { ...person("approver", "alice"), display: "Alice Ng" };

function runbook(change: Partial<Runbook> = {}): Runbook {
  return { name: "db-backup", title: "Back up the database", description: "Back up the database.",
    tier: "T1", standing_approver: "", standing_signed_at: "", ...change };
}

const SIGNED = runbook({ standing_approver: "alice",
  standing_signed_at: "2026-11-21T02:00:00.000Z" });

let host: HTMLElement;
let root: Root;

function show(runbooks: Runbook[], role: Role, reload: () => void = () => undefined): void {
  act(() => root.render(<RunbookList runbooks={runbooks} user={person(role)} users={[ALICE]}
    reload={reload} />));
}

function button(label: string): HTMLButtonElement {
  const found = [...host.querySelectorAll("button")]
    .find((candidate) => candidate.textContent?.trim() === label);
  if (found === undefined) {
    throw new Error(`the page has no button ${label}`);
  }
  return found;
}

async function click(element: Element): Promise<void> {
  // The answer of the server settles inside act, so the page shows it.
  await act(async () => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    await Promise.resolve();
  });
}

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  post.mockReset();
  remove.mockReset();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("the standing approval of a runbook", () => {
  it("shows who signed it and when, with the name of the person", () => {
    show([SIGNED], "approver");
    expect(host.textContent).toContain("Signed by Alice Ng on 21 Nov 2026, 02:00 UTC.");
    expect(host.textContent).toContain("T1 Routine");
    expect(host.textContent).toContain("db-backup");
  });

  it("says when nobody signed it", () => {
    show([runbook()], "approver");
    expect(host.textContent).toContain("Not signed");
    expect(host.textContent)
      .toContain("Nobody signed a standing approval. The runbook cannot run.");
    expect(button("Sign").disabled).toBe(false);
    expect(button("Revoke").disabled).toBe(true);
  });

  it("names a signer that is not a user, and a signature with no time", () => {
    expect(signature(runbook({ standing_approver: "carol" }), [ALICE])).toBe("Signed by carol.");
  });
});

describe("the buttons Sign and Revoke", () => {
  it("sign as the user of the session and load the list again", async () => {
    const reload = vi.fn();
    post.mockResolvedValue({ runbook: "db-backup", signer: "approver", signed_at: "" });
    show([runbook()], "approver", reload);
    await click(button("Sign"));
    expect(post).toHaveBeenCalledExactlyOnceWith("/runbooks/db-backup/standing-approval", {});
    expect(remove).not.toHaveBeenCalled();
    expect(host.textContent).toContain("You signed the standing approval of db-backup.");
    expect(reload).toHaveBeenCalledOnce();
  });

  it("revoke a signature of another person", async () => {
    remove.mockResolvedValue({ runbook: "db-backup", signer: "", signed_at: "" });
    show([SIGNED], "admin");
    expect(button("Sign again").disabled).toBe(false);
    await click(button("Revoke"));
    expect(remove).toHaveBeenCalledExactlyOnceWith("/runbooks/db-backup/standing-approval");
    expect(host.textContent).toContain("You revoked the standing approval of db-backup.");
  });

  it("show the error of the server when it refuses", async () => {
    post.mockRejectedValue(new Error("invalid request: runbook db-backup needs tier T2"));
    show([runbook()], "admin");
    await click(button("Sign"));
    expect(host.textContent).toContain("The standing approval did not change.");
    expect(host.textContent).toContain("runbook db-backup needs tier T2");
  });

  it("are disabled for a viewer, with the reason, and send nothing", async () => {
    show([SIGNED], "viewer");
    expect(button("Sign again").disabled).toBe(true);
    expect(button("Revoke").disabled).toBe(true);
    expect(host.textContent).toContain(
      "Only an approver or an admin signs or revokes a standing approval. " +
        "You are The viewer (viewer).");
    await click(button("Sign again"));
    await click(button("Revoke"));
    expect(post).not.toHaveBeenCalled();
    expect(remove).not.toHaveBeenCalled();
  });

  it("do not sign a runbook whose tier needs more than a standing approval", () => {
    show([runbook({ name: "drop-database", tier: "T3" })], "admin");
    expect(button("Sign").disabled).toBe(true);
    expect(host.textContent).toContain(
      "This runbook needs tier T3. A standing approval covers only tier T1.");
  });

  it("follow the same rules as the server", () => {
    const high = runbook({ tier: "T2", standing_approver: "alice" });
    expect(standingControl(person("approver"), runbook())).toEqual(
      { sign: true, revoke: false, reason: "" });
    expect(standingControl(person("admin"), SIGNED)).toMatchObject({ sign: true, revoke: true });
    expect(standingControl(person("viewer"), SIGNED)).toMatchObject(
      { sign: false, revoke: false });
    expect(standingControl(null, runbook())).toMatchObject({ sign: false, revoke: false });
    expect(standingControl(person("admin"), high)).toMatchObject({ sign: false, revoke: true });
  });
});

function page(state: Partial<ApiState<Runbook[]>>, role: Role): string {
  api.state = { data: null, error: null, loading: false, reload: () => undefined, ...state };
  session.user = person(role);
  session.users = [ALICE];
  act(() => root.render(<RunbooksPage />));
  return host.textContent ?? "";
}

describe("the page Runbooks", () => {
  it("shows that it loads", () => {
    const text = page({ loading: true }, "admin");
    expect(text).toContain("Runbooks");
    expect(text).toContain("Loading the runbooks");
    expect(host.querySelector("button")).toBeNull();
  });

  it("shows the error of the server", () => {
    const text = page({ error: "HTTP 500: the forge is gone" }, "admin");
    expect(text).toContain("Could not load the runbooks.");
    expect(text).toContain("the forge is gone");
  });

  it("shows the runbooks for the user of the session", () => {
    expect(page({ data: [SIGNED] }, "approver")).toContain("Signed by Alice Ng");
    expect(button("Revoke").disabled).toBe(false);
    page({ data: [SIGNED] }, "viewer");
    expect(button("Revoke").disabled).toBe(true);
  });
});
