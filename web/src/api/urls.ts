export const API_BASE = "/api/v1";

const AS_OF_PATHS = ["/overview", "/work-items", "/topology", "/posture"];

/** Formats a time exactly as the server's store.FormatTime does (UTC, milliseconds, Z). */
export function serverTime(time: Date): string {
  return time.toISOString();
}

/** Reports whether the endpoint renders a point-in-time view. */
export function honoursAsOf(path: string): boolean {
  const bare = path.split("?")[0] ?? path;
  return AS_OF_PATHS.includes(bare);
}

/**
 * Builds the URL of an API path. It adds `as_of` only for the endpoints that honour it, and
 * never for the detail of one work item.
 */
export function apiUrl(path: string, asOf: Date | null = null): string {
  const url = `${API_BASE}${path}`;
  if (asOf === null || !honoursAsOf(path)) {
    return url;
  }
  const separator = url.includes("?") ? "&" : "?";
  return `${url}${separator}as_of=${encodeURIComponent(serverTime(asOf))}`;
}

/** Builds a query string from the defined, non-empty parameters. */
export function query(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") {
      search.set(key, String(value));
    }
  }
  const text = search.toString();
  return text === "" ? "" : `?${text}`;
}
