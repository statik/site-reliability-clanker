import { apiUrl } from "@/api/urls";

const USER_KEY = "clanker.user";
const DEFAULT_USER = "alice";

/** An error response of the API, with the server's own text. */
export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export function storedUser(): string {
  try {
    return localStorage.getItem(USER_KEY) ?? DEFAULT_USER;
  } catch {
    return DEFAULT_USER;
  }
}

export function storeUser(name: string): void {
  localStorage.setItem(USER_KEY, name);
}

async function errorOf(response: Response): Promise<ApiError> {
  const text = await response.text();
  try {
    const body = JSON.parse(text) as { error?: string };
    if (typeof body.error === "string") {
      return new ApiError(response.status, body.error);
    }
  } catch {
    // The body is not JSON. The raw text below is the best message available.
  }
  const detail = text.trim() === "" ? response.statusText : text.trim().slice(0, 300);
  return new ApiError(response.status, `HTTP ${response.status}: ${detail}`);
}

/** Sends the browser to the sign-in, and back to this page after it. */
export function goToSignIn(): void {
  const here = window.location.pathname + window.location.search;
  window.location.assign(`/auth/login?return=${encodeURIComponent(here)}`);
}

/** Ends the session on the server and opens the sign-in. */
export async function signOut(): Promise<void> {
  await fetch("/auth/logout", { method: "POST" });
  window.location.assign("/");
}

async function send<T>(url: string, init: RequestInit): Promise<T> {
  const headers = new Headers(init.headers);
  // The server reads this header only in the demo, where there is no sign-in.
  headers.set("X-Clanker-User", storedUser());
  const response = await fetch(url, { ...init, headers });
  if (response.status === 401) {
    // The session ended. Only a control plane with a sign-in answers 401.
    goToSignIn();
  }
  if (!response.ok) {
    throw await errorOf(response);
  }
  if ((response.headers.get("Content-Type") ?? "").startsWith("text/html")) {
    // A control plane that does not have the route answers with the page of the dashboard.
    throw new ApiError(404, `HTTP 404: the control plane has no route ${url}`);
  }
  return (await response.json()) as T;
}

export function apiGet<T>(path: string, asOf: Date | null = null): Promise<T> {
  return send<T>(apiUrl(path, asOf), { method: "GET" });
}

export function apiPost<T>(path: string, body: unknown): Promise<T> {
  return send<T>(apiUrl(path), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
