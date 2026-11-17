// The hero backdrop: paper boats on slow water, with fish under them. Drawn on a 2D
// canvas with the colours of the current theme. It animates only while it is on screen,
// the tab is visible and the reader has not asked for reduced motion; otherwise it shows
// one still frame. The button on the hero pauses it.

import { reducedMotion } from "./site.js";

const WATERLINE = 0.86;
const BOATS = [
  { offset: 0.08, speed: 0.011, size: 1.0 },
  { offset: 0.47, speed: 0.008, size: 0.62 },
  { offset: 0.78, speed: 0.014, size: 0.8 },
];
const FISH_COUNT = 9;

function token(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function palette() {
  return {
    line: token("--water-line"),
    paper: token("--paper"),
    shade: token("--paper-shade"),
    ink: token("--text-primary"),
    fish: token("--interactive"),
  };
}

function waveY(x, time, height, layer) {
  const base = height * (WATERLINE + layer * 0.035);
  return (
    base +
    Math.sin(x * 0.006 + time * 0.5 + layer * 1.7) * (7 - layer) +
    Math.sin(x * 0.013 - time * 0.33 + layer) * 3
  );
}

function drawWave(context, width, height, time, layer, colors) {
  context.beginPath();
  for (let x = 0; x <= width; x += 8) {
    const y = waveY(x, time, height, layer);
    if (x === 0) context.moveTo(x, y);
    else context.lineTo(x, y);
  }
  context.strokeStyle = colors.line;
  context.lineWidth = layer === 0 ? 2 : 1;
  context.stroke();
}

function drawBoat(context, x, y, tilt, size, colors) {
  const unit = 34 * size;
  context.save();
  context.translate(x, y);
  context.rotate(tilt);
  context.lineJoin = "round";
  context.lineWidth = 1.5;
  context.strokeStyle = colors.ink;
  // Sail: the folded peak of a paper boat.
  context.beginPath();
  context.moveTo(0, -unit * 1.25);
  context.lineTo(unit * 0.62, -unit * 0.3);
  context.lineTo(-unit * 0.62, -unit * 0.3);
  context.closePath();
  context.fillStyle = colors.paper;
  context.fill();
  context.stroke();
  context.beginPath();
  context.moveTo(0, -unit * 1.25);
  context.lineTo(0, -unit * 0.3);
  context.stroke();
  // Hull.
  context.beginPath();
  context.moveTo(-unit * 1.25, -unit * 0.3);
  context.lineTo(unit * 1.25, -unit * 0.3);
  context.lineTo(unit * 0.62, unit * 0.28);
  context.lineTo(-unit * 0.62, unit * 0.28);
  context.closePath();
  context.fillStyle = colors.paper;
  context.fill();
  context.stroke();
  context.beginPath();
  context.moveTo(-unit * 0.62, unit * 0.28);
  context.lineTo(0, -unit * 0.3);
  context.lineTo(unit * 0.62, unit * 0.28);
  context.strokeStyle = colors.shade;
  context.stroke();
  context.restore();
}

function drawFish(context, x, y, length, direction, wiggle, colors) {
  context.save();
  context.translate(x, y);
  context.scale(direction, 1);
  context.globalAlpha = 0.28;
  context.fillStyle = colors.fish;
  context.beginPath();
  context.ellipse(0, 0, length * 0.5, length * 0.2, 0, 0, Math.PI * 2);
  context.fill();
  context.beginPath();
  context.moveTo(-length * 0.42, 0);
  context.lineTo(-length * 0.82, -length * 0.22 + wiggle);
  context.lineTo(-length * 0.82, length * 0.22 + wiggle);
  context.closePath();
  context.fill();
  context.restore();
}

function makeFish() {
  const fish = [];
  for (let index = 0; index < FISH_COUNT; index += 1) {
    // A fixed spread, so the still frame is the same on every load.
    const seed = (index * 0.618034) % 1;
    fish.push({
      offset: seed,
      depth: 0.3 + ((index * 0.381966) % 1) * 0.6,
      speed: 0.012 + seed * 0.02,
      length: 16 + ((index * 7) % 5) * 5,
      direction: index % 3 === 0 ? -1 : 1,
    });
  }
  return fish;
}

function start(canvas) {
  const context = canvas.getContext("2d");
  if (!context) return;
  const toggle = document.querySelector(".motion-toggle");
  const fish = makeFish();
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
    const water = height * (1 - WATERLINE);
    for (const one of fish) {
      const travel = (one.offset + time * one.speed * one.direction + 10) % 1;
      const x = travel * (width + 160) - 80;
      const y = height * WATERLINE + 26 + one.depth * (water - 40);
      const wiggle = Math.sin(time * 5 + one.offset * 20) * 2.5;
      drawFish(context, x, y, one.length, one.direction, wiggle, colors);
    }
    for (let layer = 3; layer >= 0; layer -= 1) drawWave(context, width, height, time, layer, colors);
    for (const boat of BOATS) {
      const x = ((boat.offset + time * boat.speed) % 1) * (width + 240) - 120;
      const y = waveY(x, time, height, 0);
      const slope = (waveY(x + 14, time, height, 0) - waveY(x - 14, time, height, 0)) / 28;
      const scale = Math.min(1, width / 900 + 0.35);
      drawBoat(context, x, y + 2, Math.atan(slope), boat.size * scale, colors);
    }
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
