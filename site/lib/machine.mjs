// The architecture figure: S.R.C. as a transfer bench. One change, a block on a carriage,
// rides a rail across the bench (the core of the control plane) and meets each checkpoint
// as a real part: a feeler, a press, a gauge, a tier pointer, a pawl that only lifts, a
// barrier with key switches, a clockwork timer, and a lift that opens one drawer of six.
// Built once here with the Anatomy kit (lib/anatomy/iso-kit.mjs); js/machine.js moves the
// parts between the poses that this module works out for each stop.

import * as k from "./anatomy/iso-kit.mjs";

export const WIDTH = 960;
export const HEIGHT = 540;

// The ten stops of one change, in order. Each is a checkpoint or a step of the pipeline.
export const STOPS = [
  "detect", "author", "plan", "policy", "review", "approval", "grant", "apply", "verify", "audit",
];
const TIERS = ["T0", "T1", "T2", "T3"];
const LAYERS = ["00-bootstrap", "10-network", "20-cluster", "30-data", "40-platform", "50-app"];

const FLOOR = -17;
const BENCH = { x: -190, y: -60, w: 380, d: 120, r: 8 };
const RAIL_Y = [26, 40];
const RAIL = [-182, 124];
const SLUG_AT = [-165, -125, -88, -50, -12, 9, 72, 108, 108, 108];
const SLUG_TOP = 15;

const BOARD = { x: -58, y: 8, w: 16, d: 3, r: 0.6 };
const BOARD_H = 46;
const SLOT_Z = (tier) => 8 + tier * 9.5;
const GATE_X = 22;
const TIMER = [72, 8];
const TIMER_R = 10.5;
const TIMER_TOP = 14;
const CABINET = { x: 124, y: -50, w: 58, d: 58, r: 3 };
const DRAWER_H = 14;
const CABINET_H = 2 + LAYERS.length * DRAWER_H + 2;
const DRAWER_OUT = 14;
const MAST = [116, 15];
const CHECK = [153, -21];
const CHECK_R = 11;
const DATABASE = { x: -176, y: -160, w: 70, d: 42, r: 4 };
const INFERENCE = { x: -92, y: -154, w: 62, d: 36, r: 4 };
const NEEDLE_FRAMES = 24;

function projection() {
  return k.fitProjection(
    [
      ...k.boxCorners(BENCH, FLOOR, 0), ...k.boxCorners(CABINET, 0, CABINET_H + 8),
      ...k.boxCorners(DATABASE, FLOOR, 34), ...k.boxCorners(INFERENCE, FLOOR, 28), [MAST[0], MAST[1], 96],
    ],
    WIDTH, HEIGHT, { pad: 26, azimuth: 45 },
  );
}

// Everything below draws with one projection, kept in the closure of build().
function build({ tier, raisedTo, layer }) {
  const P = projection();
  const at = (point) => k.iso(point, P);
  const S = (paths, style) => k.solidSvg(paths, style);
  const L = (d, style) => k.lineSvg(d, style);
  const D = (points, style) => k.dotsSvg(points.map(at), style);
  const depth = (x, y) => k.depthOf([x, y, 0], P);
  const offset = ([dx, dy, dz]) => {
    const [x0, y0] = at([0, 0, 0]);
    const [x1, y1] = at([dx, dy, dz]);
    return [Number((x1 - x0).toFixed(2)), Number((y1 - y0).toFixed(2))];
  };
  // A part that moves: its markup, and where it is at each of the ten stops.
  const moving = (name, markup, moves) =>
    `<g data-part="${name}" data-poses='${JSON.stringify(moves.map(offset))}' transform="translate(${offset(moves[0]).join(" ")})">${markup}</g>`;
  const perStop = (make) => STOPS.map((_, stop) => make(stop));
  // A closed rectangle on a face that looks toward +y (the near left faces of the figure).
  const faceRect = (x0, x1, z0, z1, y) =>
    [[[x0, y, z0], [x1, y, z0]], [[x1, y, z0], [x1, y, z1]], [[x1, y, z1], [x0, y, z1]], [[x0, y, z1], [x0, y, z0]]]
      .map(([from, to]) => k.segment(from, to, P)).join("");
  const foot = (plan, tone = "mid") => S(k.slabOf(plan, 0, 2, P, 3, 0.4), { tone });
  const screws = (plan, z, inset = 3) => D(k.corners(plan, inset).map(([x, y]) => [x, y, z]), { size: 0.42 });

  // Parts behind the rail are drawn before the change, parts over or in front of it after.
  const back = [];
  const over = [];
  const put = (list, x, y, markup) => list.push({ key: depth(x, y), markup });

  function bench() {
    const out = [`<path class="iso-halo" d="${k.haloOf(BENCH, FLOOR, 12, P)}"/>`];
    const feet = k.corners(BENCH, 24).sort((a, b) => depth(...a) - depth(...b));
    for (const [x, y] of feet) {
      out.push(S(k.cylinder(x, y, 8, FLOOR, 1.6, P, 28), { tone: "lo" }));
      out.push(S(k.cylinder(x, y, 6, FLOOR + 1.6, 3.4, P, 28), { tone: "mid" }));
    }
    out.push(S(k.slabOf(BENCH, -12, 12, P, 8, 1.6), {
      tone: "mid", inner: L(k.sideSeam(BENCH, -4, P, 8), { tone: "faint" }),
    }));
    out.push(L(k.planOutline(k.insetPlan(BENCH, 5), 0, P), { tone: "lo" }));
    const corners = k.corners(BENCH, 11);
    out.push(L(corners.map(([x, y]) => k.ring(x, y, 2.4, 0, P, 16)).join(""), { tone: "lo" }));
    out.push(D(corners.map(([x, y]) => [x, y, 0]), { size: 0.5 }));
    // The maker's plate: three rule lines stand for its print.
    const plate = { x: 132, y: 30, w: 44, d: 18, r: 1.5 };
    out.push(S(k.slabOf(plate, 0, 0.8, P, 4), { tone: "lo", crease: "none" }));
    out.push(L(k.planOutline(k.insetPlan(plate, 1.8), 0.8, P), { tone: "faint" }));
    out.push(L([[5, 30], [9, 22], [13, 26]].map(([y, w]) =>
      k.lineOnTop([plate.x + 5, plate.y + y], [plate.x + 5 + w, plate.y + y], 0.8, P)).join(""), { tone: "lo", free: true }));
    out.push(D([[plate.x + 3, plate.y + 3, 0.8], [plate.x + plate.w - 3, plate.y + plate.d - 3, 0.8]], { size: 0.4 }));
    return out.join("");
  }

  function rails() {
    const out = [];
    for (const y of RAIL_Y) {
      out.push(S(k.slabOf({ x: RAIL[0], y, w: RAIL[1] - RAIL[0], d: 4, r: 1 }, 0, 3, P, 3, 0.4), { tone: "mid" }));
      const dots = [];
      for (let x = RAIL[0] + 8; x < RAIL[1]; x += 34) dots.push([x, y + 2, 3]);
      out.push(D(dots, { size: 0.36, tone: "lo" }));
    }
    // The end stops stand in front of parts behind the rail, so they sort with those.
    for (const x of [RAIL[0] - 5, RAIL[1]]) {
      const stop = { x, y: 22, w: 5, d: 26, r: 1.2 };
      put(back, x + 2.5, 35, S(k.slabOf(stop, 0, 9, P, 3, 0.5), { tone: "mid" }) + D([[x + 2.5, 26, 9], [x + 2.5, 44, 9]], { size: 0.42 }));
    }
    return out.join("");
  }

  // The audit log: a strip along the front edge, and a chain that gets links at each stop.
  function chain() {
    const strip = { x: -180, y: 50, w: 304, d: 6, r: 1 };
    const out = [S(k.slabOf(strip, 0, 0.6, P, 3), { tone: "lo", crease: "none" })];
    const ticks = k.topTicks(strip.x + 4, strip.x + strip.w - 4, 4, 5, strip.y + 0.6, 0.6, [1.6, 3], P);
    out.push(L(ticks.minor, { tone: "lo" }), L(ticks.major, { tone: "mid" }));
    out.push(D([[strip.x + 2, 53, 0.6], [strip.x + strip.w - 2, 53, 0.6]], { size: 0.42 }));
    const perLink = 3;
    STOPS.forEach((_, stop) => {
      const rings = [];
      for (let link = 0; link < perLink; link += 1) {
        const x = strip.x + 12 + (stop * perLink + link) * 9.4;
        rings.push(k.ring(x, 54.4, 2.9, 0.7, P, 14), k.lineOnTop([x + 2.9, 54.4], [x + 6.5, 54.4], 0.7, P));
      }
      const shown = perStop((now) => (now >= stop ? 1 : 0));
      out.push(`<g data-part="link" data-show='${JSON.stringify(shown)}' opacity="${shown[0]}">${L(rings.join(""), { tone: "hi" })}</g>`);
    });
    return out.join("");
  }

  // The change itself: the one lit part.
  function slug() {
    const carriage = { x: -10, y: 23, w: 20, d: 24, r: 2 };
    const block = { x: -6, y: 28, w: 12, d: 14, r: 1.5 };
    const out = [S(k.slabOf(carriage, 3, 4, P, 4, 0.5), { tone: "mid" })];
    out.push(D(k.corners(carriage, 2.6).map(([x, y]) => [x, y, 7]), { size: 0.4, tone: "lo" }));
    out.push(S(k.slabOf(block, 7, SLUG_TOP - 7, P, 4, 0.6), { tone: "hi", lit: true }));
    out.push(L(k.planOutline(k.insetPlan(block, 2.4), SLUG_TOP, P, 3), { tone: "lit" }));
    return moving("change", out.join(""), SLUG_AT.map((x) => [x, 0, 0]));
  }

  function detector() {
    const x = SLUG_AT[0];
    put(back, x, 14, foot({ x: x - 6, y: 8, w: 12, d: 12, r: 2 }) + screws({ x: x - 6, y: 8, w: 12, d: 12, r: 2 }, 2)
      + S(k.cylinder(x, 14, 3.4, 2, 2, P, 20), { tone: "mid" })
      + S(k.cylinder(x, 14, 2.2, 4, 20, P, 20), { tone: "mid", crease: "none" })
      + L(k.sideArc(x, 14, 2.2, 14, P, 12), { tone: "faint" }));
    put(over, x, 26, S(k.slabOf({ x: x - 2, y: 11, w: 4, d: 26, r: 1.2 }, 24, 3, P, 3, 0.4), { tone: "mid" })
      + D([[x, 14, 27], [x, 34, 27]], { size: 0.42 }));
    put(over, x, 34.5, moving("feeler",
      S(k.cylinder(x, 34, 1.3, 16, 8, P, 14), { tone: "hi", crease: "none" }) + S(k.cylinder(x, 34, 2.2, 15, 1.2, P, 16), { tone: "hi" }),
      perStop((stop) => [0, 0, stop === 0 ? 0 : -5])));
  }

  function press() {
    const x = SLUG_AT[1];
    const base = { x: x - 9, y: 0, w: 18, d: 18, r: 2.5 };
    put(back, x, 9, S(k.slabOf(base, 0, 3, P, 4, 0.5), { tone: "mid" }) + screws(base, 3)
      + S(k.cylinder(x, 9, 4.6, 3, 3, P, 24), { tone: "mid" })
      + S(k.cylinder(x, 9, 3.2, 6, 34, P, 24), { tone: "mid", crease: "none" })
      + L([18, 30].map((z) => k.sideArc(x, 9, 3.2, z, P, 14)).join(""), { tone: "faint" }));
    put(over, x, 34.5, moving("ram",
      S(k.cylinder(x, 34, 2.4, 26, 14, P, 18), { tone: "hi", crease: "none" }) + S(k.cylinder(x, 34, 3.4, 25, 1.4, P, 20), { tone: "hi" }),
      perStop((stop) => [0, 0, stop === 1 ? -10 : 0])));
    const head = { x: x - 5, y: 3, w: 10, d: 37, r: 2 };
    put(over, x, 22, S(k.slabOf(head, 40, 7, P, 4, 0.6), {
      tone: "mid", inner: L(k.sideSeam(head, 43.5, P, 4), { tone: "faint" }),
    }) + D([[x, 34, 47], [x - 2.6, 6.5, 47], [x + 2.6, 6.5, 47]], { size: 0.42 })
      + S(k.cylinder(x, 20, 3.6, 47, 4, P, 24), { tone: "mid" })
      + L(k.knurl(x, 20, 3.6, 47, 51, 22, P), { tone: "lo" })
      + L(k.lineOnTop([x, 20], [x + 3.6, 20], 51, P), { tone: "hi" }) + D([[x, 20, 51]], { size: 0.5, tone: "hi" }));
  }

  function planGauge() {
    const x = SLUG_AT[2];
    put(back, x, 19, foot({ x: x - 4, y: 15, w: 8, d: 7, r: 1.5 }) + S(k.cylinder(x, 18.5, 1.9, 2, 26, P, 16), { tone: "mid", crease: "none" }));
    put(over, x, 34.5, moving("plunger",
      S(k.cylinder(x, 34, 1.3, 19, 10, P, 14), { tone: "hi", crease: "none" }) + S(k.cylinder(x, 34, 2, 18, 1.2, P, 16), { tone: "hi" }),
      perStop((stop) => [0, 0, stop === 2 ? -3 : 0])));
    const lintel = { x: x - 3, y: 15.5, w: 6, d: 37, r: 1.4 };
    put(over, x, 50, foot({ x: x - 4, y: 46, w: 8, d: 7, r: 1.5 }) + S(k.cylinder(x, 49.5, 1.9, 2, 26, P, 16), { tone: "mid", crease: "none" }));
    put(over, x, 51, S(k.slabOf(lintel, 28, 4, P, 3, 0.5), { tone: "mid" }) + D([[x, 18.5, 32], [x, 49.5, 32]], { size: 0.42 })
      + S(k.cylinder(x, 34, 4.6, 32, 3, P, 28, 0.4), { tone: "hi", crease: "lo" })
      + L(k.ring(x, 34, 3.4, 35, P, 24), { tone: "lo" })
      + L(k.lineOnTop([x, 34], [x + 2.4, 32.3], 35.1, P), { tone: "hi" }));
  }

  // Fixed rules set the tier: four fixed windows on a board, and one pointer. The pawl of
  // the review can lift the pointer and cannot lower it.
  function tierBoard() {
    const face = BOARD.y + BOARD.d;
    const windows = TIERS.map((_, index) => faceRect(BOARD.x + 3, BOARD.x + 13, SLOT_Z(index), SLOT_Z(index) + 6, face)).join("");
    const pins = TIERS.flatMap((_, index) => [[BOARD.x + 1.5, face, SLOT_Z(index) + 3], [BOARD.x + 14.5, face, SLOT_Z(index) + 3]]);
    put(back, BOARD.x + 8, BOARD.y, foot({ x: BOARD.x - 2, y: BOARD.y - 3, w: 20, d: 10, r: 1.5 })
      + S(k.slabOf(BOARD, 2, BOARD_H, P, 2, 0.4), { tone: "mid" })
      + L(windows, { tone: "lo" }) + D(pins, { size: 0.36, tone: "lo" }));
    const rest = 2.5;
    const levels = perStop((stop) => (stop < 3 ? rest : SLOT_Z(stop < 4 ? tier : raisedTo) + 1));
    const pointer = { x: BOARD.x + 2, y: face + 0.4, w: 12, d: 2.4, r: 0.8 };
    put(back, BOARD.x + 8, face + 1.5, moving("tier",
      S(k.slabOf(pointer, 0, 4, P, 3, 0.4), { tone: "hi", lit: true }), levels.map((z) => [0, 0, z])));
    // The rack and the pawl post of the review, beside the board.
    const rack = { x: -40, y: 9, w: 3.2, d: 3, r: 0.5 };
    const teeth = [];
    for (let z = 6; z < BOARD_H; z += 3) teeth.push(k.segment([rack.x, rack.y + rack.d, z], [rack.x + rack.w, rack.y + rack.d, z], P));
    put(back, rack.x, rack.y, S(k.slabOf(rack, 2, BOARD_H, P, 2), { tone: "mid", inner: L(teeth.join(""), { tone: "lo" }) }));
    const post = [-24, 11];
    put(back, post[0], post[1], foot({ x: post[0] - 5, y: post[1] - 5, w: 10, d: 10, r: 2 })
      + S(k.cylinder(post[0], post[1], 3.2, 2, 2, P, 20), { tone: "mid" })
      + S(k.cylinder(post[0], post[1], 2.2, 4, 42, P, 20), { tone: "mid", crease: "none" })
      + S(k.cylinder(post[0], post[1], 2.9, 46, 2, P, 20), { tone: "mid" }));
    const pawl = { x: rack.x + rack.w - 0.4, y: post[1] - 1.5, w: post[0] - rack.x - rack.w - 1.4, d: 3, r: 1 };
    const spring = k.coil(pawl.x + 3, post[0] - 2.4, [post[1], 1.5], 2.3, 5, "x", P);
    const pawlLevels = perStop((stop) => (stop < 4 ? SLOT_Z(tier) - 2.4 : SLOT_Z(raisedTo) - 2.4));
    put(back, post[0] - 6, post[1] + 1, moving("pawl",
      L(spring.back, { tone: "lo" }) + S(k.slabOf(pawl, 0, 3, P, 3, 0.3), { tone: "mid" }) + L(spring.front, { tone: "mid" })
        + L([pawl.x + 3, post[0] - 2.4].map((x) => k.sideRing([x, post[1], 1.5], 2.3, "x", P)).join(""), { tone: "mid" })
        + D([[pawl.x + 1.6, post[1], 3]], { size: 0.4 }),
      pawlLevels.map((z) => [0, 0, z])));
  }

  function gate() {
    const console_ = { x: 30, y: -4, w: 28, d: 18, r: 2.5 };
    const keys = [[38, 5], [50, 5]];
    const needed = tierApprovers(raisedTo);
    const pointers = keys.map(([x, y], index) => {
      const off = k.lineOnTop([x, y], [x, y - 3.2], 14.1, P);
      const on = k.lineOnTop([x, y], [x + 3.2, y], 14.1, P);
      const frames = perStop((stop) => (stop >= 5 && index < needed ? 1 : 0));
      return `<path class="iso-line" data-tone="lit" data-part="key" data-frames='${JSON.stringify([off, on])}' data-at='${JSON.stringify(frames)}' d="${off}"/>`;
    });
    put(back, console_.x + 14, console_.y, S(k.slabOf(console_, 0, 10, P, 5, 0.8), {
      tone: "mid", inner: L(k.sideSeam(console_, 3, P, 5), { tone: "faint" }),
    }) + L(k.planOutline(k.insetPlan(console_, 2.6), 10, P, 4), { tone: "faint" })
      + keys.map(([x, y]) => S(k.cylinder(x, y, 3.6, 10, 4, P, 24), { tone: "mid" }) + L(k.knurl(x, y, 3.6, 10, 14, 22, P), { tone: "lo" })
        + L(k.ring(x, y, 1.1, 14, P, 10), { tone: "mid" })).join("")
      + pointers.join("") + D([[33, -1, 10], [55, -1, 10], [33, 11, 10], [55, 11, 10]], { size: 0.4 }));
    put(back, GATE_X, 19, foot({ x: GATE_X - 4, y: 14.5, w: 8, d: 8, r: 1.5 })
      + S(k.cylinder(GATE_X, 18.5, 2.1, 2, 26, P, 18), { tone: "mid", crease: "none" }) + S(k.cylinder(GATE_X, 18.5, 2.8, 28, 1.6, P, 18), { tone: "mid" }));
    const bar = { x: GATE_X - 1.6, y: 16, w: 3.2, d: 36, r: 1 };
    put(over, GATE_X, 50, foot({ x: GATE_X - 4, y: 46, w: 8, d: 8, r: 1.5 })
      + S(k.cylinder(GATE_X, 50, 2.1, 2, 26, P, 18), { tone: "mid", crease: "none" }) + S(k.cylinder(GATE_X, 50, 2.8, 28, 1.6, P, 18), { tone: "mid" }));
    put(over, GATE_X, 51, moving("barrier",
      `<g class="m-barrier">${S(k.slabOf(bar, 9, 3.4, P, 3, 0.4), { tone: "hi" })}</g>`, perStop((stop) => [0, 0, stop >= 5 ? 14 : 0])));
    // The bells that call the approvers: email, Slack and Teams.
    const bells = { x: 32, y: -38, w: 26, d: 12, r: 2 };
    const domes = [38, 45, 52];
    put(back, bells.x, bells.y, S(k.slabOf(bells, 0, 6, P, 4, 0.5), { tone: "mid" })
      + domes.map((x) => S(k.cylinder(x, -32, 2.9, 6, 1.6, P, 20), { tone: "hi", crease: "lo" }) + S(k.cylinder(x, -32, 1.5, 7.6, 1.5, P, 14), { tone: "hi" })).join("")
      + D([[34, -36, 6], [56, -28, 6]], { size: 0.4 })
      + k.wireSvg(at([44, -26, 6]), at([44, -4, 10]), { via: [at([44, -20, 0]), at([44, -8, 0])], tone: "lo" }));
  }

  function timer() {
    const [x, y] = TIMER;
    const pedestal = { x: x - 13, y: y - 13, w: 26, d: 26, r: 3 };
    const needleAt = (share) => {
      const angle = share * Math.PI * 1.6;
      return k.lineOnTop([x - Math.sin(angle) * 2.4, y + Math.cos(angle) * 2.4],
        [x + Math.sin(angle) * (TIMER_R - 3), y - Math.cos(angle) * (TIMER_R - 3)], TIMER_TOP + 0.2, P);
    };
    const frames = Array.from({ length: NEEDLE_FRAMES + 1 }, (_, index) => needleAt(index / NEEDLE_FRAMES));
    const shares = perStop((stop) => (stop === 6 ? 1 : stop === 7 ? 0.55 : 0));
    const ticks = k.radialTicks(x, y, TIMER_R - 2.4, 30, 5, TIMER_TOP, [1.1, 2.3], P, 0, Math.PI * 1.6);
    const link = { x: x + 13, y: y - 1.5, w: MAST[0] - x - 15, d: 3, r: 1 };
    put(back, x, y, S(k.slabOf(pedestal, 0, 8, P, 5, 0.8), { tone: "mid", inner: L(k.sideSeam(pedestal, 2.5, P, 5), { tone: "faint" }) })
      + screws(pedestal, 8)
      + S(k.cylinder(x, y, TIMER_R, 8, 6, P, 56, 0.6), { tone: "hi", crease: "lo" })
      + L(k.knurl(x, y, TIMER_R, TIMER_TOP - 2.6, TIMER_TOP, 60, P, 0.3), { tone: "lo" })
      + L(k.sideArc(x, y, TIMER_R, TIMER_TOP - 2.6, P, 36), { tone: "faint" })
      + L(k.ring(x, y, TIMER_R - 1.6, TIMER_TOP, P, 48), { tone: "lo" })
      + L(ticks.minor, { tone: "lo" }) + L(ticks.major, { tone: "mid" })
      + L(k.ring(x, y, 1.3, TIMER_TOP, P, 12), { tone: "mid" })
      + `<path class="iso-line" data-tone="lit" data-free="start" data-part="timer" data-frames='${JSON.stringify(frames)}' data-at='${JSON.stringify(shares.map((share) => Math.round(share * NEEDLE_FRAMES)))}' d="${frames[0]}" style="stroke-width:1.2"/>`
      + S(k.slabOf(link, 0, 2, P, 2), { tone: "lo", crease: "none" }) + D([[link.x + 3, y, 2], [link.x + link.w - 3, y, 2]], { size: 0.4 }));
  }

  // The runner: a carriage on a mast. It goes up to one drawer for one job, then comes back.
  function lift() {
    const [x, y] = MAST;
    const level = 2 + layer * DRAWER_H + 4;
    const fork = { x: x - 5, y: y - 4, w: 13, d: 8, r: 1.5 };
    put(back, x, y, foot({ x: x - 5, y: y - 5, w: 10, d: 10, r: 2 }) + S(k.cylinder(x, y, 2, 2, CABINET_H + 4, P, 18), { tone: "mid", crease: "none" })
      + L([30, 60].map((z) => k.sideArc(x, y, 2, z, P, 12)).join(""), { tone: "faint" })
      + S(k.cylinder(x, y, 2.8, CABINET_H + 6, 1.6, P, 18), { tone: "mid" }));
    put(back, x, y + 1, moving("runner",
      S(k.slabOf(fork, 0, 5, P, 3, 0.4), { tone: "hi" }) + D([[x + 5, y, 5]], { size: 0.42, tone: "hi" }),
      perStop((stop) => [0, 0, stop === 7 ? level : 3])));
  }

  // The environment: six drawers, one for each layer. A grant unlocks one of them.
  function cabinet() {
    const face = CABINET.y + CABINET.d;
    const out = [S(k.slabOf(CABINET, 0, CABINET_H, P, 5, 1), {
      tone: "mid", inner: L(k.sideSeam(CABINET, 1.6, P, 5), { tone: "faint" }),
    })];
    out.push(L(k.planOutline(k.insetPlan(CABINET, 3), CABINET_H, P, 4), { tone: "faint" }), screws(CABINET, CABINET_H, 6));
    LAYERS.forEach((_, index) => {
      const z = 2 + index * DRAWER_H;
      const front = { x: CABINET.x + 3, y: face, w: CABINET.w - 6, d: 1.6, r: 0.5 };
      const pull = { x: CABINET.x + 22, y: face + 1.6, w: 14, d: 2.2, r: 0.9 };
      const drawn = S(k.slabOf(front, z + 1, DRAWER_H - 2, P, 2, 0.3), { tone: index === layer ? "hi" : "mid" })
        + L(faceRect(CABINET.x + 7, CABINET.x + 17, z + 4.5, z + 9.5, face + 1.6), { tone: "lo" })
        + S(k.slabOf(pull, z + 5.6, 2.4, P, 2), { tone: "mid" })
        + D([[CABINET.x + CABINET.w - 7, face + 1.6, z + 7]], { size: 0.5, tone: index === layer ? "hi" : "lo" });
      if (index !== layer) {
        out.push(drawn);
        return;
      }
      const box = { x: front.x + 1, y: face - DRAWER_OUT, w: front.w - 2, d: DRAWER_OUT, r: 0.5 };
      const open = perStop((stop) => (stop === 7 ? 1 : 0));
      const body = `<g data-part="drawer-box" data-show='${JSON.stringify(open)}' opacity="0">${S(k.slabOf(box, z + 1.6, DRAWER_H - 3.2, P, 2), { tone: "mid" })}</g>`;
      out.push(moving("drawer", body + drawn, open.map((share) => [0, share * DRAWER_OUT, 0])));
    });
    // The canary check: a gauge on top.
    const [x, y] = CHECK;
    const needleAt = (share) => {
      const angle = -0.9 + share * 1.8;
      return k.lineOnTop([x - Math.sin(angle) * 2.4, y + Math.cos(angle) * 2.4],
        [x + Math.sin(angle) * (CHECK_R - 3), y - Math.cos(angle) * (CHECK_R - 3)], CABINET_H + 6.2, P);
    };
    const frames = Array.from({ length: NEEDLE_FRAMES + 1 }, (_, index) => needleAt(index / NEEDLE_FRAMES));
    const ticks = k.radialTicks(x, y, CHECK_R - 2.4, 20, 5, CABINET_H + 6, [1.1, 2.3], P, -0.9, 1.8);
    out.push(S(k.cylinder(x, y, CHECK_R, CABINET_H, 6, P, 56, 0.6), { tone: "hi", crease: "lo" })
      + L(k.sideArc(x, y, CHECK_R, CABINET_H + 3.4, P, 36), { tone: "faint" })
      + L(k.ring(x, y, CHECK_R - 1.6, CABINET_H + 6, P, 48), { tone: "lo" })
      + L(ticks.minor, { tone: "lo" }) + L(ticks.major, { tone: "mid" }) + L(k.ring(x, y, 1.3, CABINET_H + 6, P, 12), { tone: "mid" })
      + `<path class="iso-line" data-tone="lit" data-free="start" data-part="check" data-frames='${JSON.stringify(frames)}' data-at='${JSON.stringify(perStop((stop) => (stop >= 8 ? NEEDLE_FRAMES : 0)))}' d="${frames[0]}" style="stroke-width:1.2"/>`);
    return out.join("");
  }

  // Outside the bench: the database that holds all state, and the model provider.
  function outside() {
    const out = [];
    const box = (plan, top, extra) => {
      out.push(`<path class="iso-halo" d="${k.haloOf(plan, FLOOR, 8, P)}"/>`);
      for (const [x, y] of k.corners(plan, 8).sort((a, b) => depth(...a) - depth(...b))) {
        out.push(S(k.cylinder(x, y, 4, FLOOR, 4, P, 18), { tone: "lo" }));
      }
      out.push(S(k.slabOf(plan, FLOOR + 4, top - FLOOR - 4, P, 6, 1), {
        tone: "mid", inner: L(k.sideSeam(plan, top - 6, P, 6), { tone: "faint" }),
      }), L(k.planOutline(k.insetPlan(plan, 4), top, P, 5), { tone: "faint" }), screws(plan, top, 7), extra);
    };
    const dbFace = DATABASE.y + DATABASE.d;
    // One hasp and one lock: one writer at a time.
    box(DATABASE, 30, L(faceRect(DATABASE.x + 10, DATABASE.x + 36, 6, 20, dbFace), { tone: "lo" })
      + L([[16, 18], [13, 12], [10, 15]].map(([z, w]) => k.segment([DATABASE.x + 13, dbFace, z], [DATABASE.x + 13 + w, dbFace, z], P)).join(""), { tone: "lo", free: true })
      + S(k.slabOf({ x: DATABASE.x + 48, y: dbFace, w: 9, d: 2, r: 0.6 }, 12, 12, P, 2, 0.3), { tone: "hi" })
      + L(k.sideRing([DATABASE.x + 52.5, dbFace + 2, 18], 1.5, "y", P), { tone: "hi" })
      + S(k.slabOf({ x: DATABASE.x + 28, y: DATABASE.y + 15, w: 14, d: 8, r: 1.5 }, 30, 3, P, 3, 0.3), { tone: "mid" }));
    const vents = [];
    for (let z = -2; z <= 14; z += 4) vents.push(faceRect(INFERENCE.x + 9, INFERENCE.x + 33, z, z + 1.6, INFERENCE.y + INFERENCE.d));
    const dial = [INFERENCE.x + 46, INFERENCE.y + 12];
    box(INFERENCE, 24, L(vents.join(""), { tone: "lo" })
      + S(k.cylinder(dial[0], dial[1], 5, 24, 3.4, P, 28), { tone: "mid" }) + L(k.knurl(dial[0], dial[1], 5, 24, 27.4, 30, P), { tone: "lo" })
      + L(k.lineOnTop(dial, [dial[0] + 5, dial[1]], 27.4, P), { tone: "hi" }) + D([[dial[0], dial[1], 27.4]], { size: 0.5, tone: "hi" })
      + S(k.slabOf({ x: INFERENCE.x + 12, y: INFERENCE.y + 14, w: 16, d: 8, r: 1.5 }, 24, 3, P, 3, 0.3), { tone: "mid" }));
    return out.join("");
  }

  // The cables run on the floor from each box to the back of the bench, so the bench hides
  // their far ends. The database has one. The model provider has two: one for the press of
  // the author and one for the pawl of the review, the two parts that call a model.
  const PORTS = { database: -141, author: SLUG_AT[1], review: -24 };
  function floorCables() {
    const run = (from, x) => k.wireSvg(at([from[0], from[1], FLOOR + 8]), at([x, BENCH.y + 2, FLOOR + 8]), {
      via: [at([from[0], from[1] + 10, FLOOR]), at([x, from[1] + 10, FLOOR]), at([x, BENCH.y - 6, FLOOR])], tone: "mid", size: 1.4,
    });
    const dbFace = DATABASE.y + DATABASE.d;
    const modelFace = INFERENCE.y + INFERENCE.d;
    return run([DATABASE.x + 62, dbFace], PORTS.database) + run([INFERENCE.x + 40, modelFace], PORTS.author)
      + run([INFERENCE.x + 52, modelFace], PORTS.review);
  }

  // Where a cable comes up onto the bench: a terminal block at the back edge, and for the
  // two model cables a lead on the top to the part that it serves.
  function terminals() {
    const out = [];
    for (const [name, x] of Object.entries(PORTS)) {
      const block = { x: x - 6, y: BENCH.y + 5, w: 12, d: 7, r: 1.5 };
      out.push(S(k.slabOf(block, 0, 3, P, 3, 0.3), { tone: "mid" }), D([[x - 3, block.y + 3.5, 3], [x + 3, block.y + 3.5, 3]], { size: 0.4 }));
      if (name === "database") continue;
      out.push(k.wireSvg(at([x, block.y + 7, 0]), at([x, name === "author" ? 0 : 6, 0]), { tone: "mid", size: 1.2 }));
    }
    return out.join("");
  }

  const railMarkup = rails();
  detector();
  press();
  planGauge();
  tierBoard();
  gate();
  timer();
  lift();
  const sorted = (list) => list.sort((a, b) => a.key - b.key).map((item) => item.markup).join("");
  const body = [outside(), floorCables(), bench(), terminals(), railMarkup, chain(), sorted(back), cabinet(), slug(), sorted(over)];
  return k.figureSvg({
    width: WIDTH, height: HEIGHT, className: "machine",
    label: "S.R.C. drawn as a transfer bench. A lit block, one change, rides a rail past a feeler, a press, "
      + "a gauge, a tier pointer with a pawl that only lifts, a barrier with two key switches and a clockwork "
      + "timer, to a cabinet of six drawers. A lift opens one drawer while the timer runs. A chain along the "
      + "front edge gets links at each stop. A database box and a model box stand behind the bench.",
    body,
  });
}

function tierApprovers(tier) {
  return tier >= 3 ? 2 : 1;
}

// The figure for one work item: its tier, the tier after the review, and its layer.
export function renderMachineSvg({ tier, raisedTo = tier, layer }) {
  const tierIndex = TIERS.indexOf(tier);
  const layerIndex = LAYERS.indexOf(layer);
  if (tierIndex < 0) throw new Error(`machine: unknown tier ${tier}`);
  if (layerIndex < 0) throw new Error(`machine: unknown layer ${layer}`);
  return build({ tier: tierIndex, raisedTo: Math.max(tierIndex, TIERS.indexOf(raisedTo)), layer: layerIndex });
}
