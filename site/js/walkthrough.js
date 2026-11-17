// The walkthrough: one step of the month at a time.
// Scroll, or use the keys: right arrow, Page Down or Space for the next step; left arrow
// or Page Up for the previous step; Home and End for the first and the last step.
// The text is already in the page. This script tracks the step on screen, moves the
// marker on the month axis, and scrolls for the keys and buttons.

import { reducedMotion } from "./site.js";

const NEXT_KEYS = new Set(["ArrowRight", "PageDown", " "]);
const PREVIOUS_KEYS = new Set(["ArrowLeft", "PageUp"]);

function typingInField(target) {
  return target.closest("input, textarea, select, [contenteditable='true']") !== null;
}

function start(rail) {
  const steps = Array.from(document.querySelectorAll(".step"));
  const dots = Array.from(rail.querySelectorAll(".dot"));
  const previous = rail.querySelector(".rail-prev");
  const next = rail.querySelector(".rail-next");
  const counter = rail.querySelector(".rail-counter");
  const marker = rail.querySelector(".axis-marker");
  let current = -1;

  function render(index) {
    if (index === current) return;
    current = index;
    steps.forEach((step, position) => step.classList.toggle("is-active", position === index));
    dots.forEach((dot, position) => {
      if (position === index) dot.setAttribute("aria-current", "step");
      else dot.removeAttribute("aria-current");
    });
    previous.disabled = index <= 0;
    next.disabled = index === steps.length - 1;
    counter.textContent = `${Math.max(index, 0) + 1} / ${steps.length}`;
    if (marker && index >= 0) marker.style.setProperty("--at", `${steps[index].dataset.at}%`);
    document.dispatchEvent(new CustomEvent("stepchange", { detail: { index } }));
  }

  function go(index) {
    const target = Math.min(steps.length - 1, Math.max(0, index));
    steps[target].scrollIntoView({
      behavior: reducedMotion.matches ? "auto" : "smooth",
      block: "start",
    });
    window.history.replaceState(null, "", `#${steps[target].id}`);
  }

  // The step on screen is the last one whose top is above the middle of the window.
  function stepOnScreen() {
    const line = window.innerHeight * 0.5;
    let found = -1;
    steps.forEach((step, index) => {
      if (step.getBoundingClientRect().top <= line) found = index;
    });
    return found;
  }

  let scheduled = false;
  function onScroll() {
    if (scheduled) return;
    scheduled = true;
    window.requestAnimationFrame(() => {
      scheduled = false;
      render(stepOnScreen());
    });
  }

  previous.addEventListener("click", () => go(current - 1));
  next.addEventListener("click", () => go(current + 1));
  dots.forEach((dot, index) => {
    dot.addEventListener("click", (event) => {
      event.preventDefault();
      go(index);
    });
  });

  document.addEventListener("keydown", (event) => {
    if (event.altKey || event.ctrlKey || event.metaKey) return;
    if (typingInField(event.target)) return;
    // Space on a button, link or summary activates it, so it must not also change the step.
    if (event.key === " " && event.target.closest("button, a, summary")) return;
    if (NEXT_KEYS.has(event.key)) go(current + 1);
    else if (PREVIOUS_KEYS.has(event.key)) go(current - 1);
    else if (event.key === "Home") go(0);
    else if (event.key === "End") go(steps.length - 1);
    else return;
    event.preventDefault();
  });

  window.addEventListener("scroll", onScroll, { passive: true });
  window.addEventListener("resize", onScroll);
  render(stepOnScreen());
}

const rail = document.querySelector(".rail");
if (rail) start(rail);
