const DATE_TIME = new Intl.DateTimeFormat("en-GB", {
  year: "numeric",
  month: "short",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
  timeZone: "UTC",
});

/** Formats a server timestamp as "17 Nov 2026, 03:37 UTC". An empty value gives a dash. */
export function formatTime(value: string | Date | null | undefined): string {
  if (value === null || value === undefined || value === "") {
    return "–";
  }
  const time = typeof value === "string" ? new Date(value) : value;
  if (Number.isNaN(time.getTime())) {
    return String(value);
  }
  return `${DATE_TIME.format(time)} UTC`;
}

const UNITS: [number, string][] = [
  [86_400_000, "d"],
  [3_600_000, "h"],
  [60_000, "min"],
  [1000, "s"],
];

/** Formats a duration with its two largest units, for example "2 h 5 min". */
export function formatDuration(ms: number): string {
  if (ms < 1000) {
    return "under 1 s";
  }
  const parts: string[] = [];
  let rest = ms;
  for (const [size, label] of UNITS) {
    const count = Math.floor(rest / size);
    if (count > 0 && parts.length < 2) {
      parts.push(`${count} ${label}`);
    }
    rest -= count * size;
    if (parts.length > 0 && count === 0) {
      break;
    }
  }
  return parts.join(" ");
}

/** Formats a time relative to "now", for example "3 h ago" or "in 2 d". */
export function formatRelative(value: string, now: Date): string {
  const delta = new Date(value).getTime() - now.getTime();
  const text = formatDuration(Math.abs(delta));
  return delta > 0 ? `in ${text}` : `${text} ago`;
}

/** Converts a time to the value of a datetime-local input, in the browser's zone. */
export function toLocalInput(time: Date): string {
  const shifted = new Date(time.getTime() - time.getTimezoneOffset() * 60_000);
  return shifted.toISOString().slice(0, 16);
}

export function shortHash(hash: string, length = 10): string {
  return hash.length > length ? hash.slice(0, length) : hash;
}
