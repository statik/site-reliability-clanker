// Moves the parts of the architecture figure (lib/machine.mjs) from stop to stop. The
// build wrote, on each moving part, where it is at each stop: `data-poses` (a screen
// offset), `data-frames` with `data-at` (one of several drawn positions, for a needle) or
// `data-show` (an opacity). This module eases between them. It needs nothing else.

const FOLLOW_SECONDS = 0.34;
const GATE_OPENS_AT = 5;

function read(element, name) {
  return JSON.parse(element.dataset[name]);
}

export function mountMachine(svg) {
  const slides = Array.from(svg.querySelectorAll("[data-poses]"), (element) => {
    const poses = read(element, "poses");
    return { element, poses, now: [...poses[0]] };
  });
  const needles = Array.from(svg.querySelectorAll("[data-frames]"), (element) => {
    const at = read(element, "at");
    return { element, frames: read(element, "frames"), at, now: at[0], drawn: -1 };
  });
  const fades = Array.from(svg.querySelectorAll("[data-show]"), (element) => {
    const show = read(element, "show");
    return { element, show, now: show[0] };
  });
  let stop = 0;
  let frame = 0;
  let last = 0;

  function draw() {
    for (const part of slides) part.element.setAttribute("transform", `translate(${part.now[0].toFixed(2)} ${part.now[1].toFixed(2)})`);
    for (const part of needles) {
      const index = Math.round(part.now);
      if (index === part.drawn) continue;
      part.drawn = index;
      part.element.setAttribute("d", part.frames[index]);
    }
    for (const part of fades) part.element.setAttribute("opacity", part.now.toFixed(2));
  }

  // Moves every part toward its place at this stop. Returns true when all are there.
  function advance(share) {
    let settled = true;
    const toward = (now, target) => {
      const next = now + (target - now) * share;
      if (Math.abs(target - next) < 0.02) return target;
      settled = false;
      return next;
    };
    for (const part of slides) part.now = part.now.map((value, axis) => toward(value, part.poses[stop][axis]));
    for (const part of needles) part.now = toward(part.now, part.at[stop]);
    for (const part of fades) part.now = toward(part.now, part.show[stop]);
    return settled;
  }

  function tick(time) {
    const seconds = last ? Math.min((time - last) / 1000, 1 / 30) : 1 / 60;
    last = time;
    const settled = advance(1 - Math.exp(-seconds / FOLLOW_SECONDS));
    draw();
    frame = settled ? 0 : window.requestAnimationFrame(tick);
  }

  function go(next, { still = false } = {}) {
    stop = next;
    svg.dataset.stop = String(stop);
    svg.toggleAttribute("data-open", stop >= GATE_OPENS_AT);
    if (still) {
      advance(1);
      draw();
      return;
    }
    if (!frame) {
      last = 0;
      frame = window.requestAnimationFrame(tick);
    }
  }

  go(0, { still: true });
  return { go };
}
