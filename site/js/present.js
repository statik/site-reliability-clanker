// Presentation mode: the sections of the four pages work as the slides of one deck.
// P turns the mode on and off, and Escape turns it off. `?presentation` in the address
// starts the page in the mode. Right arrow, Down arrow, Page Down, Space or N shows the
// next slide. Left arrow, Up arrow, Page Up or Shift+Space shows the previous one. Home
// and End show the first and the last slide of the page. A slide that is taller than the
// window scrolls by a part of the window first. After the last slide the next page of the
// site opens, and before the first slide the previous page opens at its end.

import { reducedMotion } from "./site.js";

const FLAG = "presentation";
const NEXT_KEYS = new Set(["ArrowRight", "ArrowDown", "PageDown", "n", "N", " "]);
const PREVIOUS_KEYS = new Set(["ArrowLeft", "ArrowUp", "PageUp"]);
const IDLE_MS = 3000;
// The part of the window that one key press scrolls inside a tall slide.
const STEP = 0.85;
// A part of a slide smaller than this share of the window is not worth one more press.
const REST = 0.12;

const root = document.documentElement;
let active = false;
// The scroll position that a smooth scroll moves to, or null when the page is at rest.
let goal = null;
let goalTimer = 0;
let idleTimer = 0;
let mark = null;

function pageName(url) {
  return new URL(url, window.location.href).pathname.split("/").pop() || "index.html";
}

// The pages of the deck, in the order of the site navigation.
function deckPages() {
  return Array.from(document.querySelectorAll(".shell nav a[href]"))
    .filter((link) => link.origin === window.location.origin)
    .map((link) => link.href);
}

function stickyHeight() {
  let height = 0;
  for (const part of document.querySelectorAll(".shell, .rail")) {
    if (window.getComputedStyle(part).position !== "sticky") continue;
    height += part.getBoundingClientRect().height;
  }
  return Math.round(height);
}

// The slides are the sections of the page that are not inside another section. The text
// above the first section is one more slide.
function layout() {
  const offset = stickyHeight();
  const tops = Array.from(document.querySelectorAll("main section"))
    .filter((section) => section.parentElement.closest("section") === null)
    .map((section) => section.getBoundingClientRect().top + window.scrollY);
  if (tops.length === 0 || tops[0] - offset > 80) tops.unshift(offset);
  const footer = document.querySelector("body > footer") ?? document.querySelector("main");
  const end = footer.getBoundingClientRect().bottom + window.scrollY;
  const targets = tops.map((top) => Math.max(0, Math.round(top - offset)));
  const ends = targets.slice(1).concat(Math.round(end - offset));
  return { targets, ends, view: window.innerHeight - offset };
}

function position() {
  return goal ?? window.scrollY;
}

function slideAt(targets, at) {
  let found = 0;
  targets.forEach((target, index) => {
    if (target <= at + 4) found = index;
  });
  return found;
}

function scrollTo(top, jump = false) {
  goal = top;
  window.clearTimeout(goalTimer);
  goalTimer = window.setTimeout(() => { goal = null; }, 1200);
  const behavior = jump || reducedMotion.matches ? "auto" : "smooth";
  window.scrollTo({ top, behavior });
  renderMark();
}

function leave(direction) {
  const pages = deckPages();
  const here = pages.findIndex((page) => pageName(page) === pageName(window.location.href));
  const target = pages[here + direction];
  if (here < 0 || !target) return;
  const url = new URL(target);
  url.search = direction < 0 ? `?${FLAG}=end` : `?${FLAG}`;
  window.location.assign(url);
}

function next() {
  const { targets, ends, view } = layout();
  const at = position();
  const index = slideAt(targets, at);
  // The distance to the position where the end of the slide is at the bottom of the
  // window. A tall slide takes this distance in equal parts.
  const rest = ends[index] - view - at;
  if (rest > view * REST) scrollTo(at + rest / Math.ceil(rest / (view * STEP)));
  else if (index + 1 < targets.length) scrollTo(targets[index + 1]);
  else leave(1);
}

function previous() {
  const { targets, view } = layout();
  const at = position();
  const index = slideAt(targets, at);
  if (at > targets[index] + 8) scrollTo(Math.max(at - view * STEP, targets[index]));
  else if (index > 0) scrollTo(targets[index - 1]);
  else leave(-1);
}

function renderMark() {
  if (!mark) return;
  const { targets } = layout();
  mark.textContent = `${slideAt(targets, position()) + 1} / ${targets.length}`;
}

function wake() {
  root.classList.remove("present-idle");
  window.clearTimeout(idleTimer);
  if (active) idleTimer = window.setTimeout(() => root.classList.add("present-idle"), IDLE_MS);
}

function writeFlag(on) {
  const url = new URL(window.location.href);
  url.search = on ? `?${FLAG}` : "";
  if (url.hash === "#present") url.hash = "";
  window.history.replaceState(null, "", url);
}

function setActive(on) {
  active = on;
  root.classList.toggle("present", on);
  writeFlag(on);
  if (on && !mark) {
    mark = document.createElement("p");
    mark.className = "present-mark";
    mark.setAttribute("aria-hidden", "true");
    document.body.append(mark);
  }
  if (!on && mark) {
    mark.remove();
    mark = null;
  }
  renderMark();
  wake();
}

// True when the part of the page with the focus needs the key for itself.
function ownsKey(event) {
  const target = event.target;
  if (!(target instanceof Element)) return false;
  if (target.closest("input, textarea, select, [contenteditable='true'], video, audio")) {
    return true;
  }
  return event.key === " " && target.closest("button, a, summary") !== null;
}

function actionFor(event) {
  const key = event.key;
  if (key === "p" || key === "P") return () => setActive(!active);
  if (!active) return null;
  if (key === "Escape") return () => setActive(false);
  if (key === " " && event.shiftKey) return previous;
  if (NEXT_KEYS.has(key)) return next;
  if (PREVIOUS_KEYS.has(key)) return previous;
  if (key === "Home") return () => scrollTo(layout().targets[0]);
  if (key === "End") return () => scrollTo(layout().targets.at(-1));
  return null;
}

// The listener works in the capture phase and stops the event, so the keys of the
// walkthrough do not also move the page while the mode is on.
function onKey(event) {
  if (event.altKey || event.ctrlKey || event.metaKey || ownsKey(event)) return;
  const action = actionFor(event);
  if (!action) return;
  event.preventDefault();
  event.stopImmediatePropagation();
  action();
}

// A link to a page of the deck keeps the mode.
function onClick(event) {
  if (!active || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey) return;
  const link = event.target instanceof Element ? event.target.closest("a[href]") : null;
  if (!link || link.target === "_blank") return;
  if (!deckPages().some((page) => pageName(page) === pageName(link.href))) return;
  if (link.origin !== window.location.origin || link.hash !== "") return;
  event.preventDefault();
  const url = new URL(link.href);
  url.search = `?${FLAG}`;
  window.location.assign(url);
}

function addHint() {
  const footer = document.querySelector("body > footer .wrap");
  if (!footer) return;
  const hint = document.createElement("p");
  hint.className = "present-hint";
  hint.innerHTML = "Press <kbd>P</kbd> for presentation mode.";
  footer.append(hint);
}

function start() {
  const flag = new URLSearchParams(window.location.search).get(FLAG);
  const atEnd = flag === "end";
  if (flag !== null || window.location.hash === "#present") setActive(true);
  // A page that opens at its end shows its last slide. The pictures above it can change
  // the height of the page while they load, so the page goes there again after the load.
  const toEnd = () => scrollTo(layout().targets.at(-1), true);
  if (atEnd) {
    toEnd();
    window.addEventListener("load", toEnd, { once: true });
  }
  let scheduled = false;
  window.addEventListener("scroll", () => {
    if (scheduled || !active) return;
    scheduled = true;
    window.requestAnimationFrame(() => {
      scheduled = false;
      renderMark();
    });
  }, { passive: true });
  window.addEventListener("scrollend", () => { goal = null; });
  window.addEventListener("resize", renderMark);
  for (const name of ["mousemove", "mousedown"]) {
    window.addEventListener(name, wake, { passive: true });
  }
  document.addEventListener("keydown", onKey, { capture: true });
  document.addEventListener("click", onClick);
  addHint();
}

start();
