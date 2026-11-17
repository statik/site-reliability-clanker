// Motion for the architecture page. Three figures, each driven by steps:
// 1. A real change travels through the system diagram, stop by stop.
// 2. A marker follows the real path of a work item through the state machine.
// 3. A grant appears, is checked, is used, and is revoked.
// With reduced motion nothing plays by itself: the buttons still step through the
// figures, and each change is immediate.

import { reducedMotion } from "./site.js";

const FLOW_STOP_MS = 2400;
const STATE_EDGE_MS = 800;

function whenVisible(element, callback) {
  new IntersectionObserver((entries) => {
    callback(entries.some((entry) => entry.isIntersecting));
  }, { threshold: 0.25 }).observe(element);
}

function startFlow(figure) {
  const stops = Array.from(figure.querySelectorAll("#flow-steps li"));
  const token = figure.querySelector("#system-token");
  const playButton = figure.querySelector(".flow-play");
  const previousButton = figure.querySelector(".flow-prev");
  const nextButton = figure.querySelector(".flow-next");
  let current = 0;
  let playing = !reducedMotion.matches;
  let visible = false;
  let timer = null;

  function render() {
    const stop = stops[current];
    token.style.transform = `translate(${stop.dataset.x}px, ${stop.dataset.y}px)`;
    stops.forEach((item, index) => {
      item.classList.toggle("is-active", index === current);
      if (index === current) item.setAttribute("aria-current", "step");
      else item.removeAttribute("aria-current");
    });
    const visited = new Set(stops.slice(0, current).map((item) => item.dataset.node));
    figure.querySelectorAll(".system-diagram [data-node]").forEach((node) => {
      node.classList.toggle("is-active", node.dataset.node === stop.dataset.node);
      node.classList.toggle("is-visited", visited.has(node.dataset.node));
    });
  }

  function go(index) {
    current = (index + stops.length) % stops.length;
    render();
  }

  function updateTimer() {
    window.clearInterval(timer);
    timer = null;
    if (playing && visible && !document.hidden) {
      timer = window.setInterval(() => go(current + 1), FLOW_STOP_MS);
    }
    playButton.textContent = playing ? "Pause" : "Play";
    playButton.setAttribute("aria-pressed", String(playing));
  }

  playButton.addEventListener("click", () => {
    playing = !playing;
    updateTimer();
  });
  previousButton.addEventListener("click", () => {
    playing = false;
    go(current - 1);
    updateTimer();
  });
  nextButton.addEventListener("click", () => {
    playing = false;
    go(current + 1);
    updateTimer();
  });
  stops.forEach((stop, index) => {
    stop.addEventListener("click", () => {
      playing = false;
      go(index);
      updateTimer();
    });
  });
  whenVisible(figure, (isVisible) => {
    visible = isVisible;
    updateTimer();
  });
  document.addEventListener("visibilitychange", updateTimer);
  render();
  updateTimer();
}

function startStates(figure) {
  const svg = figure.querySelector(".state-diagram");
  const token = figure.querySelector("#state-token");
  const caption = figure.querySelector("#state-caption");
  const chips = Array.from(figure.querySelectorAll(".chip"));
  let run = 0;

  const node = (name) => svg.querySelector(`[data-state="${name}"]`);
  const edge = (from, to) => svg.querySelector(`[data-edge="${from}>${to}"]`);

  function centre(name) {
    const box = node(name).querySelector("rect");
    return {
      x: Number(box.getAttribute("x")) + Number(box.getAttribute("width")) / 2,
      y: Number(box.getAttribute("y")) + Number(box.getAttribute("height")) / 2,
    };
  }

  function clear() {
    svg.querySelectorAll(".is-active, .is-visited").forEach((item) => {
      item.classList.remove("is-active", "is-visited");
    });
  }

  function place(point) {
    token.setAttribute("cx", point.x);
    token.setAttribute("cy", point.y - 26);
  }

  function travel(path, duration) {
    return new Promise((resolve) => {
      const length = path.getTotalLength();
      const began = performance.now();
      function frame(now) {
        const share = Math.min(1, (now - began) / duration);
        place(path.getPointAtLength(length * share));
        if (share < 1) window.requestAnimationFrame(frame);
        else resolve();
      }
      window.requestAnimationFrame(frame);
    });
  }

  async function show(states, animate) {
    run += 1;
    const mine = run;
    clear();
    node(states[0]).classList.add("is-active");
    place(centre(states[0]));
    caption.textContent = `Starts in ${states[0]}.`;
    for (let index = 1; index < states.length; index += 1) {
      const from = states[index - 1];
      const to = states[index];
      const path = edge(from, to);
      if (animate && path) {
        path.classList.add("is-active");
        caption.textContent = `${from} → ${to}: ${path.dataset.label}.`;
        await travel(path, STATE_EDGE_MS);
        if (mine !== run) return;
        await new Promise((resolve) => window.setTimeout(resolve, 350));
        if (mine !== run) return;
      } else if (path) {
        path.classList.add("is-active");
      }
      node(from).classList.replace("is-active", "is-visited");
      node(to).classList.add("is-active");
      place(centre(to));
    }
    caption.textContent = `${states.join(" → ")}.`;
  }

  chips.forEach((chip) => {
    chip.addEventListener("click", () => {
      chips.forEach((other) => other.setAttribute("aria-pressed", String(other === chip)));
      show(chip.dataset.path.split(">"), !reducedMotion.matches);
    });
  });

  let started = false;
  whenVisible(figure, (isVisible) => {
    if (!isVisible || started || chips.length === 0) return;
    started = true;
    show(chips[0].dataset.path.split(">"), !reducedMotion.matches);
  });
}

function startGrant(figure) {
  const replay = document.querySelector(".grant-replay");
  function play() {
    figure.classList.remove("is-playing");
    // Reading a layout property makes the browser restart the CSS animations.
    void figure.offsetWidth;
    figure.classList.add("is-playing");
  }
  let started = false;
  whenVisible(figure, (isVisible) => {
    if (!isVisible || started) return;
    started = true;
    play();
  });
  replay?.addEventListener("click", play);
  if (replay && reducedMotion.matches) replay.hidden = true;
}

const flow = document.getElementById("figure-flow");
if (flow) startFlow(flow);
const states = document.getElementById("figure-states");
if (states) startStates(states);
const grant = document.getElementById("grant-figure");
if (grant) startGrant(grant);
