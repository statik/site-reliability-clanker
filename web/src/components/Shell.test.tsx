// @vitest-environment happy-dom
import { act } from "react";
import { createRoot } from "react-dom/client";
import type { Root } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Shell } from "@/components/Shell";

vi.mock("@/state/session", () => ({
  useSession: () => ({
    users: [],
    user: null,
    userName: "alice",
    usersError: null,
    setUser: () => undefined,
    signIn: false,
    asOf: null,
    setAsOf: () => undefined,
  }),
}));
vi.mock("@/state/live", () => ({ useLive: () => ({ connected: true, notice: null }) }));
vi.mock("@/state/clock", () => ({
  useServerNow: () => new Date("2026-11-17T12:00:00Z"),
  useClockError: () => null,
}));

// The test environment does no layout, so the width of the window is this stand-in for
// matchMedia. `becomeWide` reports the change that a browser sends at the breakpoint.
type WidthListener = (event: { matches: boolean }) => void;
const wideListeners = new Set<WidthListener>();
function becomeWide(): void {
  act(() => {
    for (const listener of wideListeners) {
      listener({ matches: true });
    }
  });
}

let host: HTMLElement;
let root: Root;

function menuButton(): HTMLButtonElement {
  const button = document.querySelector<HTMLButtonElement>("button[aria-controls='app-navigation']");
  if (button === null) {
    throw new Error("the header has no menu button");
  }
  return button;
}

function navigation(): HTMLElement {
  const nav = document.getElementById("app-navigation");
  if (nav === null) {
    throw new Error("the page has no navigation");
  }
  return nav;
}

function content(): HTMLElement {
  const main = document.getElementById("main-content");
  if (main === null) {
    throw new Error("the page has no content region");
  }
  return main;
}

function click(element: Element): void {
  act(() => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
  });
}

function press(key: string): void {
  act(() => {
    window.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true }));
  });
}

beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener: (_type: string, listener: WidthListener) => wideListeners.add(listener),
    removeEventListener: (_type: string, listener: WidthListener) => wideListeners.delete(listener),
  }));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => {
    root.render(
      <MemoryRouter initialEntries={["/audit"]}>
        <Shell>
          <p>page</p>
        </Shell>
      </MemoryRouter>,
    );
  });
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  wideListeners.clear();
  vi.unstubAllGlobals();
});

describe("the navigation of a narrow window", () => {
  it("starts closed, with the page in use", () => {
    expect(menuButton().getAttribute("aria-expanded")).toBe("false");
    expect(menuButton().getAttribute("aria-label")).toBe("Open the navigation");
    expect(content().hasAttribute("inert")).toBe(false);
  });

  it("opens from the menu button, takes the focus and makes the page inert", () => {
    click(menuButton());
    expect(menuButton().getAttribute("aria-expanded")).toBe("true");
    expect(menuButton().getAttribute("aria-label")).toBe("Close the navigation");
    expect(document.activeElement).toBe(navigation());
    expect(content().hasAttribute("inert")).toBe(true);
  });

  it("closes from the menu button and gives the focus back to it", () => {
    click(menuButton());
    click(menuButton());
    expect(menuButton().getAttribute("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(menuButton());
    expect(content().hasAttribute("inert")).toBe(false);
  });

  it("closes on Escape and gives the focus back to the menu button", () => {
    click(menuButton());
    press("Escape");
    expect(menuButton().getAttribute("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(menuButton());
  });

  it("stays open on another key", () => {
    click(menuButton());
    press("Enter");
    expect(menuButton().getAttribute("aria-expanded")).toBe("true");
  });

  it("closes when a person picks a page", () => {
    click(menuButton());
    const link = navigation().querySelector("a[href='/timeline']");
    if (link === null) {
      throw new Error("the navigation has no link to the timeline");
    }
    click(link);
    expect(menuButton().getAttribute("aria-expanded")).toBe("false");
    expect(content().hasAttribute("inert")).toBe(false);
  });

  it("closes when the window becomes wide", () => {
    click(menuButton());
    becomeWide();
    expect(menuButton().getAttribute("aria-expanded")).toBe("false");
  });

  it("marks the page of the address as the current one", () => {
    const current = navigation().querySelector("a[aria-current='page']");
    expect(current?.getAttribute("href")).toBe("/audit");
  });
});
