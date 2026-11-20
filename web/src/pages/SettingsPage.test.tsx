// @vitest-environment happy-dom
import { act } from "react";
import { createRoot } from "react-dom/client";
import type { Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LocalInference, ModelSettings, Role, User } from "@/api/types";
import type { ApiState } from "@/hooks/useApi";
import { localControl, ModelSection, SettingsPage } from "@/pages/SettingsPage";

const post = vi.hoisted(() => vi.fn());
const api = vi.hoisted(() => ({ state: null as unknown }));
const session = vi.hoisted(() => ({ user: null as unknown }));

vi.mock("@/api/client", () => ({
  apiPost: post,
  errorMessage: (error: unknown) => (error instanceof Error ? error.message : String(error)),
}));
vi.mock("@/hooks/useApi", () => ({ useApi: () => api.state }));
vi.mock("@/state/session", () => ({ useSession: () => ({ user: session.user }) }));

function person(role: Role): User {
  return { name: role, display: `The ${role}`, role, email: "", last_seen: "",
    self_declared: false };
}

const RUNNING: LocalInference = {
  available: true, url: "http://127.0.0.1:11434", running: true, version: "0.40.16",
  model: "src-local", model_present: true, model_loaded: false, started_by_src: false,
};

function settings(change: Partial<ModelSettings> = {}): ModelSettings {
  return {
    mode: "local", provider: "Ollama on this machine",
    models: { reviewer: "src-local", author: "src-local" },
    base_url: "http://127.0.0.1:11434/v1", key_set: false, local: RUNNING, ...change,
  };
}

let host: HTMLElement;
let root: Root;

function show(data: ModelSettings, role: Role, reload: () => void = () => undefined): void {
  act(() => root.render(<ModelSection data={data} user={person(role)} reload={reload} />));
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
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("the section Language model", () => {
  it("shows the provider, the mode, the endpoint and the model of each role", () => {
    show(settings(), "admin");
    const text = host.textContent ?? "";
    expect(text).toContain("Ollama on this machine");
    expect(text).toContain("Mode: local");
    expect(text).toContain("http://127.0.0.1:11434/v1");
    expect(text).toContain("No key is set.");
    expect(text).toContain("on an endpoint that the operator runs");
    const cells = [...host.querySelectorAll("tbody td")].map((cell) => cell.textContent);
    expect(cells).toEqual(["author", "src-local", "reviewer", "src-local"]);
  });

  it("says what each mode means", () => {
    show(settings({ mode: "replay", provider: "Recorded answers", models: {} }), "admin");
    expect(host.textContent).toContain("Answers are recorded, and no model is called.");
    expect(host.textContent).toContain("No model is called in this mode.");
    show(settings({ mode: "openrouter", provider: "OpenRouter", key_set: true }), "admin");
    expect(host.textContent).toContain("through OpenRouter");
    expect(host.textContent).toContain("A key is set. The dashboard never shows it.");
  });

  it("sends a person to the wizard to change the provider", () => {
    show(settings(), "admin");
    expect(host.textContent).toContain("This page does not change the provider.");
    expect(host.textContent).toContain("clanker setup");
    expect(host.querySelector("select, input[type='radio']")).toBeNull();
  });

  it("shows the state of the server and of the model", () => {
    show(settings(), "admin");
    expect(host.textContent).toContain("Ollama 0.40.16 runs");
    expect(host.textContent)
      .toContain("The server holds the model src-local. It is not in memory.");
    expect(host.textContent).toContain("S.R.C. did not start the server");
    show(settings({ local: { ...RUNNING, model_loaded: true, started_by_src: true } }), "admin");
    expect(host.textContent).toContain("The model src-local is in memory.");
    expect(host.textContent).toContain("S.R.C. started the server, and Stop stops it.");
    show(settings({ local: { ...RUNNING, running: false, model_present: false } }), "admin");
    expect(host.textContent).toContain("Not running");
    expect(host.textContent).toContain("The server does not answer at http://127.0.0.1:11434.");
    show(settings({ local: { ...RUNNING, model_present: false } }), "admin");
    expect(host.textContent).toContain("The server does not hold the model src-local.");
  });
});

describe("the buttons Start and Stop", () => {
  it("are enabled for an admin when the local inference is available", () => {
    show(settings(), "admin");
    expect(button("Start").disabled).toBe(false);
    expect(button("Stop").disabled).toBe(false);
  });

  it("are disabled for a viewer and for an approver, with the reason", () => {
    for (const role of ["viewer", "approver"] as const) {
      show(settings(), role);
      expect(button("Start").disabled).toBe(true);
      expect(button("Stop").disabled).toBe(true);
      expect(host.textContent).toContain(
        `Only an admin starts or stops the local inference. You are The ${role} (${role}).`);
    }
  });

  it("are disabled for an admin when this target has no local control", () => {
    const local: LocalInference = { available: false, reason: "No program starts here.",
      running: false, model_present: false, model_loaded: false, started_by_src: false };
    show(settings({ mode: "openrouter", provider: "OpenRouter", local }), "admin");
    expect(button("Start").disabled).toBe(true);
    expect(button("Stop").disabled).toBe(true);
    expect(host.textContent).toContain("No program starts here.");
    expect(host.textContent).not.toContain("Not running");
  });

  it("follow the same rule as the server", () => {
    const off: LocalInference = { ...RUNNING, available: false };
    expect(localControl(person("admin"), RUNNING).allowed).toBe(true);
    expect(localControl(person("approver"), RUNNING).allowed).toBe(false);
    expect(localControl(person("viewer"), RUNNING).allowed).toBe(false);
    expect(localControl(null, RUNNING).allowed).toBe(false);
    expect(localControl(person("admin"), off).allowed).toBe(false);
  });

  it("start the local inference, show what was done and load the state again", async () => {
    const reload = vi.fn();
    post.mockResolvedValue(settings({ done: "the server already ran; the model is loaded" }));
    show(settings(), "admin", reload);
    await click(button("Start"));
    expect(post).toHaveBeenCalledExactlyOnceWith("/settings/model/local/start", {});
    expect(host.textContent).toContain("the server already ran; the model is loaded");
    expect(reload).toHaveBeenCalledOnce();
    expect(button("Start").disabled).toBe(false);
  });

  it("stop the local inference", async () => {
    post.mockResolvedValue(settings({ done: "the model src-local is unloaded" }));
    show(settings(), "admin");
    await click(button("Stop"));
    expect(post).toHaveBeenCalledExactlyOnceWith("/settings/model/local/stop", {});
    expect(host.textContent).toContain("the model src-local is unloaded");
  });

  it("show the error of the server when it refuses", async () => {
    post.mockRejectedValue(new Error("carol has role viewer; this action needs one of: admin"));
    show(settings(), "admin");
    await click(button("Start"));
    expect(host.textContent).toContain("The local inference did not change.");
    expect(host.textContent).toContain("this action needs one of: admin");
  });

  it("send nothing when a disabled button is clicked", async () => {
    show(settings(), "viewer");
    await click(button("Start"));
    await click(button("Stop"));
    expect(post).not.toHaveBeenCalled();
  });
});

function page(state: Partial<ApiState<ModelSettings>>, role: Role): string {
  api.state = { data: null, error: null, loading: false, reload: () => undefined, ...state };
  session.user = person(role);
  act(() => root.render(<SettingsPage />));
  return host.textContent ?? "";
}

describe("the page Settings", () => {
  it("shows that it loads", () => {
    const text = page({ loading: true }, "admin");
    expect(text).toContain("Settings");
    expect(text).toContain("Loading the settings");
    expect(host.querySelector("button")).toBeNull();
  });

  it("shows the error of the server", () => {
    const text = page({ error: "HTTP 500: the database is gone" }, "admin");
    expect(text).toContain("Could not load the settings.");
    expect(text).toContain("the database is gone");
  });

  it("shows the section for the user of the session", () => {
    expect(page({ data: settings() }, "admin")).toContain("Language model");
    expect(button("Start").disabled).toBe(false);
    page({ data: settings() }, "viewer");
    expect(button("Start").disabled).toBe(true);
  });
});
