// Small helpers for the prerender: escaping and the formats used on every page.

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

export function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

// A tagged template that escapes every interpolated value. Wrap trusted markup in raw().
export function html(strings, ...values) {
  let out = strings[0];
  values.forEach((value, index) => {
    out += render(value) + strings[index + 1];
  });
  return raw(out);
}

export function raw(markup) {
  return { __html: String(markup) };
}

function render(value) {
  if (value === null || value === undefined || value === false) return "";
  if (Array.isArray(value)) return value.map(render).join("");
  if (typeof value === "object" && "__html" in value) return value.__html;
  return escapeHtml(value);
}

export function day(ts) {
  const date = new Date(ts);
  return `${date.getUTCDate()} ${MONTHS[date.getUTCMonth()]}`;
}

export function dayYear(ts) {
  return `${day(ts)} ${new Date(ts).getUTCFullYear()}`;
}

export function clock(ts) {
  return new Date(ts).toISOString().slice(11, 19);
}

export function minute(ts) {
  return new Date(ts).toISOString().slice(11, 16);
}

export function dayRange(from, to) {
  return day(from) === day(to) ? dayYear(from) : `${day(from)} to ${dayYear(to)}`;
}

export function duration(totalSeconds) {
  if (totalSeconds < 10) return `${totalSeconds.toFixed(1)} s`;
  if (totalSeconds < 90) return `${Math.round(totalSeconds)} s`;
  const minutes = Math.floor(totalSeconds / 60);
  return `${minutes} min ${Math.round(totalSeconds - minutes * 60)} s`;
}

export function shortHash(hash) {
  return hash ? hash.slice(0, 7) : "";
}

export function list(words) {
  if (words.length <= 1) return words.join("");
  return `${words.slice(0, -1).join(", ")} and ${words[words.length - 1]}`;
}

export function plural(count, one, many = `${one}s`) {
  return `${count} ${count === 1 ? one : many}`;
}

const NUMBER_WORDS = ["zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"];

export function numberWord(count) {
  return NUMBER_WORDS[count] ?? String(count);
}
