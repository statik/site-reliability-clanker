// The hero backdrop: the six layers of a platform as rows of resources with the links
// between them. A few changes move up through the layers, one layer at a time, and wait at
// each one. Drawn on a 2D canvas with the colours of the current theme. It animates only
// while it is on screen, the tab is visible and the reader has not asked for reduced
// motion; otherwise it shows one still frame. The button on the hero pauses it.

import { reducedMotion } from "./site.js";

const LAYERS = 6;
const TOP = 0.80;
const BOTTOM = 0.97;
const COLUMN_WIDTH = 84;
const NODE = 6;
const CHANGES = 5;
const SECONDS_PER_LAYER = 3.2;

// The custom properties hold light-dark() and color-mix(), which a canvas does not take.
// An element that uses the property gives the colour that the browser worked out.
function token(name) {
  const probe = document.createElement("span");
  probe.style.color = `var(${name})`;
  document.body.append(probe);
  const color = getComputedStyle(probe).color;
  probe.remove();
  return color;
}

function palette() {
  return { line: token("--border-strong"), node: token("--sea-mid"), change: token("--interactive") };
}

// A fixed spread, so the still frame is the same on every load.
function spread(column, row) {
  return ((column * 0.618034 + row * 0.381966) % 1) - 0.5;
}

function nodeAt(column, row, width, height) {
  const columns = Math.ceil(width / COLUMN_WIDTH) + 1;
  const band = height * (BOTTOM - TOP);
  return {
    x: (column + 0.5) * (width / (columns - 1)) + spread(column, row) * 28 - COLUMN_WIDTH / 2,
    y: height * BOTTOM - (row / (LAYERS - 1)) * band,
  };
}

function hasLinkDown(column, row) {
  return row > 0 && (column * 7 + row * 3) % 4 !== 0;
}

function drawLattice(context, width, height, colors) {
  const columns = Math.ceil(width / COLUMN_WIDTH) + 1;
  context.lineWidth = 1;
  context.strokeStyle = colors.line;
  context.globalAlpha = 0.45;
  context.beginPath();
  for (let row = 0; row < LAYERS; row += 1) {
    for (let column = 0; column < columns; column += 1) {
      const here = nodeAt(column, row, width, height);
      if (column > 0) {
        const left = nodeAt(column - 1, row, width, height);
        context.moveTo(left.x, left.y);
        context.lineTo(here.x, here.y);
      }
      if (hasLinkDown(column, row)) {
        const below = nodeAt(column, row - 1, width, height);
        context.moveTo(here.x, here.y);
        context.lineTo(below.x, below.y);
      }
    }
  }
  context.stroke();
  context.globalAlpha = 0.8;
  context.fillStyle = colors.node;
  for (let row = 0; row < LAYERS; row += 1) {
    for (let column = 0; column < columns; column += 1) {
      const here = nodeAt(column, row, width, height);
      context.fillRect(here.x - NODE / 2, here.y - NODE / 2, NODE, NODE);
    }
  }
}

// A change goes up one layer, then waits there for as long as the move took.
function drawChange(context, index, time, width, height, colors) {
  const columns = Math.ceil(width / COLUMN_WIDTH) + 1;
  const column = Math.floor(((index + 0.5) / CHANGES) * (columns - 1));
  const cycle = SECONDS_PER_LAYER * LAYERS;
  const position = ((time + index * cycle * 0.37) % cycle) / SECONDS_PER_LAYER;
  const row = Math.floor(position);
  const moved = Math.min(1, (position - row) * 2);
  const from = nodeAt(column, row, width, height);
  const to = nodeAt(column, Math.min(LAYERS - 1, row + 1), width, height);
  const x = from.x + (to.x - from.x) * moved;
  const y = from.y + (to.y - from.y) * moved;
  context.globalAlpha = 1;
  context.fillStyle = colors.change;
  context.fillRect(x - NODE, y - NODE, NODE * 2, NODE * 2);
}

function start(canvas) {
  const context = canvas.getContext("2d");
  if (!context) return;
  const toggle = document.querySelector(".motion-toggle");
  let colors = palette();
  let width = 0;
  let height = 0;
  let time = 12;
  let last = 0;
  let visible = true;
  let paused = reducedMotion.matches;
  let frameId = 0;

  function draw() {
    context.clearRect(0, 0, width, height);
    drawLattice(context, width, height, colors);
    for (let index = 0; index < CHANGES; index += 1) {
      drawChange(context, index, time, width, height, colors);
    }
    context.globalAlpha = 1;
  }

  function frame(now) {
    time += Math.min(0.05, (now - last) / 1000);
    last = now;
    draw();
    frameId = window.requestAnimationFrame(frame);
  }

  function updateLoop() {
    window.cancelAnimationFrame(frameId);
    const running = visible && !paused && !document.hidden;
    if (running) {
      last = performance.now();
      frameId = window.requestAnimationFrame(frame);
    } else {
      draw();
    }
    if (toggle) {
      toggle.textContent = paused ? "Play motion" : "Pause motion";
      toggle.setAttribute("aria-pressed", String(paused));
    }
  }

  function resize() {
    const ratio = Math.min(window.devicePixelRatio || 1, 2);
    width = canvas.clientWidth;
    height = canvas.clientHeight;
    if (!width || !height) return;
    canvas.width = Math.round(width * ratio);
    canvas.height = Math.round(height * ratio);
    context.setTransform(ratio, 0, 0, ratio, 0, 0);
    draw();
  }

  new ResizeObserver(resize).observe(canvas);
  new IntersectionObserver((entries) => {
    visible = entries.some((entry) => entry.isIntersecting);
    updateLoop();
  }).observe(canvas);
  document.addEventListener("visibilitychange", updateLoop);
  document.addEventListener("themechange", () => {
    colors = palette();
    draw();
  });
  reducedMotion.addEventListener("change", () => {
    paused = reducedMotion.matches;
    updateLoop();
  });
  toggle?.addEventListener("click", () => {
    paused = !paused;
    updateLoop();
  });
  resize();
  updateLoop();
}

const canvas = document.getElementById("hero-backdrop");
if (canvas) start(canvas);
