// The architecture figure: the parts of S.R.C. and the path of one change through them.
// The core of the control plane is one chassis with one module for each component. Outside
// it stand the model provider, the application pack, the database, the approver, the runner
// and the environment.
// A lit carriage, the work item, rides the rail of the orchestrator from module to module.
// Built once here with the Anatomy kit (lib/anatomy/iso-kit.mjs); js/machine.js moves and
// fades the parts between the states that this module works out for each stop.

import * as k from "./anatomy/iso-kit.mjs";

export const WIDTH = 960;
export const HEIGHT = 560;

// The ten stops of one change, in order.
export const STOPS = [
  "detect", "author", "plan", "policy", "review", "approval", "grant", "apply", "verify", "audit",
];
const TIERS = ["T0", "T1", "T2", "T3"];
const LAYERS = ["00-bootstrap", "10-network", "20-cluster", "30-data", "40-platform", "50-app"];

const FLOOR = -16;
const CORE = { x: -130, y: -40, w: 232, d: 106, r: 6 };
const MODULES = ["Detector", "Agent runtime", "Git forge", "Policy engine", "Dashboard", "Notifier",
  "Access broker", "Audit log"];
const MODULE_W = 22;
const MODULE_Y = [-34, 22];
const MODULE_H = [17, 21, 17, 19, 17, 15, 19, 17];
const moduleX = (index) => -122 + index * 27;
const centre = (index) => moduleX(index) + MODULE_W / 2;
const RAIL = { x: -126, y: 26, w: 222, d: 17, r: 2 };
const RAIL_END = 86;
// Where the work item is at each stop: in front of the module that acts on it.
const ITEM_AT = [centre(0), centre(1), centre(2), centre(3), centre(1), centre(4), centre(6),
  RAIL_END, RAIL_END, centre(7)];
// Which parts act at each stop. The names are the keys of `glow` below.
const ACTIVE = [
  ["m0", "pack"], ["m1", "model", "m2"], ["m2", "runner", "env"], ["m3"], ["m1", "model"],
  ["m4", "m5", "person", "chat"], ["m6"], ["runner", "env"], ["runner", "env", "pack"], ["m7"],
];
const RECORDS_PER_STOP = 2;

const MODEL = { x: -128, y: -122, w: 62, d: 40, r: 4 };
const MODEL_TOP = 10;
const PACK = { x: -58, y: -122, w: 66, d: 40, r: 4 };
const PACK_TOP = 2;
const DATABASE = [-178, 58];
const DATABASE_R = 25;
const DATABASE_TOP = 12;
const DESK = { x: -46, y: 94, w: 58, d: 40, r: 4 };
const CHAT = { x: 20, y: 100, w: 46, d: 30, r: 4 };
const DOCK = { x: 122, y: 4, w: 40, d: 54, r: 4 };
const RUNNER = { x: 128, y: 10, w: 28, d: 32, r: 3 };
const ENV = { x: 192, y: -52, w: 58, d: 58, r: 3 };
const LAYER_H = 8;
const BASE_TOP = FLOOR + 4;

function projection() {
  return k.fitProjection(
    [
      ...k.boxCorners(CORE, FLOOR, 24), ...k.boxCorners(MODEL, FLOOR, MODEL_TOP), ...k.boxCorners(PACK, FLOOR, PACK_TOP + 6),
      ...k.boxCorners({ x: DATABASE[0] - DATABASE_R, y: DATABASE[1] - DATABASE_R, w: 2 * DATABASE_R, d: 2 * DATABASE_R },
        FLOOR, DATABASE_TOP),
      ...k.boxCorners(DESK, FLOOR, 8), ...k.boxCorners(CHAT, FLOOR, 0), ...k.boxCorners(DOCK, FLOOR, 12),
      ...k.boxCorners(ENV, FLOOR, BASE_TOP + LAYERS.length * LAYER_H + 2),
    ],
    WIDTH, HEIGHT, { pad: 22, azimuth: 45 },
  );
}

function build({ tier, raisedTo, layer, approvers }) {
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
  const perStop = (make) => STOPS.map((_, stop) => make(stop));
  // A part that moves: its markup, and where it is at each of the ten stops.
  const moving = (name, markup, moves) =>
    `<g data-part="${name}" data-poses='${JSON.stringify(moves.map(offset))}' transform="translate(${offset(moves[0]).join(" ")})">${markup}</g>`;
  // A part that is there at some stops only.
  const shown = (name, markup, when) => {
    const show = perStop((stop) => (when(stop) ? 1 : 0));
    return `<g data-part="${name}" data-show='${JSON.stringify(show)}' opacity="${show[0]}">${markup}</g>`;
  };
  // The mark of a part that acts at this stop: its outline, brighter.
  const glow = (name, markup) => shown(`glow-${name}`, markup, (stop) => ACTIVE[stop].includes(name));
  const outline = (plan, z, inset = 2.4) => L(k.planOutline(k.insetPlan(plan, inset), z, P, 4), { tone: "hi" });
  // A closed rectangle on a face that looks toward +y (the near left faces of the figure).
  const faceRect = (x0, x1, z0, z1, y) =>
    [[[x0, y, z0], [x1, y, z0]], [[x1, y, z0], [x1, y, z1]], [[x1, y, z1], [x0, y, z1]], [[x0, y, z1], [x0, y, z0]]]
      .map(([from, to]) => k.segment(from, to, P)).join("");
  const screws = (plan, z, inset = 3) => D(k.corners(plan, inset).map(([x, y]) => [x, y, z]), { size: 0.42 });
  const text = (words, point, z, { along = "x", size = 5.6, tone = "hi", anchor = "start" } = {}) =>
    k.faceTextSvg(k.topMatrix(point, z, P, along), words, { size, tone, anchor });
  const faceText = (words, point, z, { size = 5, tone = "hi", face = "left" } = {}) =>
    k.faceTextSvg(k.sideMatrix(point, z, P, face), words, { size, tone });
  const halo = (plan, spread = 8) => `<path class="iso-halo" d="${k.haloOf(plan, FLOOR, spread, P)}"/>`;
  // A unit that stands on the floor: feet, a body, a seam, an inset line and corner screws.
  const cabinet = (plan, top, tone = "mid") => {
    const out = [];
    for (const [x, y] of k.corners(plan, 7).sort((a, b) => depth(...a) - depth(...b))) {
      out.push(S(k.cylinder(x, y, 3.6, FLOOR, 4, P, 18), { tone: "lo" }));
    }
    out.push(S(k.slabOf(plan, BASE_TOP, top - BASE_TOP, P, 6, 1), {
      tone, inner: L(k.sideSeam(plan, top - 4, P, 6), { tone: "faint" }),
    }), L(k.planOutline(k.insetPlan(plan, 3), top, P, 5), { tone: "faint" }), screws(plan, top, 5.5));
    return out.join("");
  };
  // A cable on the floor between two real ports, and the same cable brighter while it carries
  // something.
  const cable = (name, points) => {
    const [from, ...rest] = points.map(([x, y, z = FLOOR]) => at([x, y, z]));
    const to = rest.pop();
    const wire = (tone, flow) => k.wireSvg(from, to, { via: rest, tone, flow, size: 1.5 });
    return wire("mid", false) + (name ? glow(name, wire("hi", true)) : "");
  };

  function cables() {
    const mid = (RAIL.y + RAIL.y + RAIL.d) / 2;
    return [
      cable(null, [[DATABASE[0] + DATABASE_R, DATABASE[1]], [CORE.x - 1, DATABASE[1]]]),
      cable("person", [[centre(4), CORE.y + CORE.d + 1], [centre(4), DESK.y + 1]]),
      cable("chat", [[centre(5), CORE.y + CORE.d + 1], [centre(5), CHAT.y + 1]]),
      cable("runner", [[CORE.x + CORE.w + 1, mid], [DOCK.x + 1, mid]]),
      cable("env", [[DOCK.x + DOCK.w - 1, mid], [ENV.x + 12, mid], [ENV.x + 12, ENV.y + ENV.d + 1]]),
    ].join("");
  }

  // Behind the core: the model provider and the database.
  function behind() {
    const out = [halo(MODEL), cabinet(MODEL, MODEL_TOP)];
    const face = MODEL.y + MODEL.d;
    const vents = [];
    for (let z = -8; z <= 4; z += 4) vents.push(faceRect(MODEL.x + 8, MODEL.x + 34, z, z + 1.6, face));
    out.push(L(vents.join(""), { tone: "lo" }), D([[MODEL.x + 46, face, 0], [MODEL.x + 52, face, 0]], { size: 0.6, tone: "mid" }));
    out.push(text("Model provider", [MODEL.x + 6, MODEL.y + 24], MODEL_TOP));
    out.push(glow("model", outline(MODEL, MODEL_TOP, 3)));
    // The application pack: a case with the files that describe one application.
    out.push(halo(PACK), cabinet(PACK, PACK_TOP));
    const files = [0, 1, 2, 3].map((index) => ({ x: PACK.x + 8 + index * 13, y: PACK.y + 5, w: 10, d: 13, r: 0.8 }));
    for (const file of files) {
      out.push(S(k.slabOf(file, PACK_TOP, 5 - (file.x % 2), P, 2, 0.3), { tone: "mid" }));
    }
    out.push(text("Application pack", [PACK.x + 4, PACK.y + 33], PACK_TOP, { size: 5.4 }));
    out.push(glow("pack", outline(PACK, PACK_TOP, 3)));
    const [x, y] = DATABASE;
    const plan = { x: x - DATABASE_R, y: y - DATABASE_R, w: 2 * DATABASE_R, d: 2 * DATABASE_R, r: DATABASE_R };
    out.push(halo(plan), S(k.cylinder(x, y, DATABASE_R + 1.5, FLOOR, 3, P, 48), { tone: "lo" }));
    out.push(S(k.cylinder(x, y, DATABASE_R, FLOOR + 3, DATABASE_TOP - FLOOR - 3, P, 56, 0.8), { tone: "mid" }));
    for (const z of [-6, 2]) out.push(L(k.sideArc(x, y, DATABASE_R, z, P, 36), { tone: "lo" }));
    out.push(L(k.ring(x, y, DATABASE_R - 3, DATABASE_TOP, P, 48), { tone: "faint" }));
    out.push(text("PostgreSQL", [x, y + 2], DATABASE_TOP, { anchor: "middle" }));
    return out.join("");
  }

  // The core of the control plane: one chassis, one process.
  function chassis() {
    const out = [halo(CORE, 12)];
    for (const [x, y] of k.corners(CORE, 20).sort((a, b) => depth(...a) - depth(...b))) {
      out.push(S(k.cylinder(x, y, 7, FLOOR, 1.6, P, 28), { tone: "lo" }), S(k.cylinder(x, y, 5, FLOOR + 1.6, 4.4, P, 28), { tone: "mid" }));
    }
    out.push(S(k.slabOf(CORE, -10, 10, P, 8, 1.4), { tone: "mid", inner: L(k.sideSeam(CORE, -2.5, P, 8), { tone: "faint" }) }));
    out.push(L(k.planOutline(k.insetPlan(CORE, 3), 0, P), { tone: "lo" }));
    out.push(D(k.corners(CORE, 6).map(([x, y]) => [x, y, 0]), { size: 0.5 }));
    out.push(faceText("Control plane core", [CORE.x + 12, CORE.y + CORE.d], -3.6, { size: 4.6, tone: "mid" }));
    return out.join("");
  }

  // A cable through the air from a unit behind the core to a socket on one module. The model
  // provider has one, to the agent runtime. The application pack has one, to the git forge.
  function airCable(name, module, face, z) {
    const socket = [moduleX(module) + 18, MODULE_Y[0] + 5, MODULE_H[module] + 1.4];
    const port = [socket[0], face, z];
    const plug = { x: socket[0] - 2.4, y: socket[1] - 2.4, w: 4.8, d: 4.8, r: 1 };
    const wire = (tone, flow) => k.wireSvg(at(port), at(socket), { tone, flow, size: 1.5 });
    return S(k.slabOf(plug, MODULE_H[module], 1.4, P, 3, 0.3), { tone: "mid" }) + wire("mid", false) + glow(name, wire("hi", true));
  }
  const modelCable = () => airCable("model", 1, MODEL.y + MODEL.d, MODEL_TOP - 9)
    + airCable("pack", 2, PACK.y + PACK.d, PACK_TOP - 6);

  function modules() {
    const out = [];
    MODULES.forEach((name, index) => {
      const x = moduleX(index);
      const top = MODULE_H[index];
      const plan = { x, y: MODULE_Y[0], w: MODULE_W, d: MODULE_Y[1] - MODULE_Y[0], r: 2 };
      const face = MODULE_Y[1];
      out.push(S(k.slabOf({ ...plan, x: x - 1, w: MODULE_W + 2, y: plan.y - 1, d: plan.d + 2 }, 0, 1.2, P, 3), { tone: "lo", crease: "none" }));
      out.push(S(k.slabOf(plan, 1.2, top - 1.2, P, 4, 0.8), {
        tone: "mid", inner: L(k.sideSeam(plan, top - 3, P, 4), { tone: "faint" }),
      }));
      out.push(L(k.planOutline(k.insetPlan(plan, 2), top, P, 3), { tone: "faint" }));
      out.push(D([[x + 3.4, plan.y + 3.4, top], [x + MODULE_W - 3.4, plan.y + 3.4, top]], { size: 0.4, tone: "lo" }));
      // Two vent slots and a status lamp on the face toward the rail.
      out.push(L([4.5, 7.5].map((z) => faceRect(x + 4, x + 13, z, z + 1.3, face)).join(""), { tone: "lo" }));
      out.push(D([[x + 17.5, face, 5.6]], { size: 0.7, tone: "mid" }));
      out.push(text(name, [x + 13.4, face - 4.5], top, { along: "y" }));
      out.push(glow(`m${index}`, outline(plan, top, 2) + D([[x + 17.5, face, 5.6]], { size: 0.9, tone: "hi" })));
    });
    return out.join("");
  }

  // The orchestrator: the rail that takes each work item from one module to the next. In
  // front of it, the audit log: one record for each event, each joined to the one before.
  function rail() {
    const out = [S(k.slabOf(RAIL, 0, 1, P, 3), { tone: "lo", crease: "none" })];
    for (const y of [RAIL.y + 1.5, RAIL.y + RAIL.d - 4.5]) {
      out.push(S(k.slabOf({ x: RAIL.x + 2, y, w: RAIL.w - 4, d: 3, r: 0.8 }, 1, 2.2, P, 3, 0.3), { tone: "mid" }));
    }
    for (const x of [RAIL.x - 3, RAIL.x + RAIL.w]) {
      out.push(S(k.slabOf({ x, y: RAIL.y, w: 3, d: RAIL.d, r: 0.8 }, 0, 6, P, 3, 0.4), { tone: "mid" }));
    }
    out.push(text("Orchestrator", [RAIL.x + 2, RAIL.y + RAIL.d + 7.4], 0, { size: 4.8 }));
    const strip = { x: RAIL.x, y: CORE.y + CORE.d - 9, w: RAIL.w, d: 7, r: 1 };
    out.push(S(k.slabOf(strip, 0, 0.6, P, 3), { tone: "lo", crease: "none" }));
    const count = STOPS.length * RECORDS_PER_STOP;
    const pitch = (strip.w - 10) / count;
    for (let record = 0; record < count; record += 1) {
      const x = strip.x + 5 + record * pitch;
      const box = { x, y: strip.y + 1.6, w: pitch - 3.2, d: 3.8, r: 0.5 };
      const link = record === 0 ? "" : k.lineOnTop([x - 3.2, strip.y + 3.5], [x, strip.y + 3.5], 0.6, P);
      out.push(shown("record", S(k.slabOf(box, 0.6, 1, P, 2), { tone: "hi", crease: "none" }) + L(link, { tone: "hi" }),
        (stop) => stop * RECORDS_PER_STOP + (stop === STOPS.length - 1 ? RECORDS_PER_STOP : 1) > record));
    }
    return out.join("");
  }

  // The work item: the one lit part. The policy engine puts the tier on it, and each
  // approver adds one mark.
  function workItem() {
    const carriage = { x: -9, y: RAIL.y + 0.5, w: 18, d: RAIL.d - 1, r: 2 };
    const block = { x: -6.5, y: RAIL.y + 3, w: 13, d: RAIL.d - 6, r: 1.5 };
    const out = [S(k.slabOf(carriage, 3.2, 3, P, 4, 0.5), { tone: "mid" })];
    out.push(S(k.slabOf(block, 6.2, 8, P, 4, 0.6), { tone: "hi", lit: true }));
    out.push(L(k.planOutline(k.insetPlan(block, 2), 14.2, P, 3), { tone: "lit" }));
    const tierText = (name) => text(name, [0, block.y + block.d - 3], 14.2, { size: 5.4, anchor: "middle" });
    out.push(shown("tier", tierText(TIERS[tier]), (stop) => stop >= 3 && (stop < 4 || raisedTo === tier)));
    if (raisedTo !== tier) out.push(shown("tier-raised", tierText(TIERS[raisedTo]), (stop) => stop >= 4));
    const marks = Array.from({ length: approvers }, (_, index) => [carriage.x + 1.3, carriage.y + 4 + index * 4, 6.2]);
    out.push(shown("approvals", D(marks, { size: 0.9, tone: "hi" }), (stop) => stop >= 5));
    return moving("change", out.join(""), ITEM_AT.map((x) => [x, 0, 0]));
  }

  // The grant: a credential for one layer. The broker issues it, the runner holds it for one
  // job, and then it is gone.
  function grant() {
    const key = { x: -4, y: -2.5, w: 8, d: 5, r: 1 };
    const markup = S(k.slabOf(key, 0, 1.6, P, 3, 0.3), { tone: "hi" }) + L(k.ring(1.6, 0, 1.1, 1.6, P, 12), { tone: "hi" });
    const from = [centre(6), MODULE_Y[1] + 3, 1];
    const to = [RUNNER.x + RUNNER.w / 2, RUNNER.y + RUNNER.d + 5.5, BASE_TOP];
    return shown("grant", moving("grant-key", markup, perStop((stop) => (stop >= 7 ? to : from))),
      (stop) => stop === 6 || stop === 7);
  }

  // The runner: a unit that exists for one job, on a dock between the core and the
  // environment.
  function runner() {
    const out = [halo(DOCK), S(k.slabOf(DOCK, FLOOR, 4, P, 5, 0.6), { tone: "lo" })];
    out.push(L(k.planOutline(k.insetPlan(RUNNER, -1.5), BASE_TOP, P, 4), { tone: "mid", dashed: true }));
    out.push(screws(DOCK, BASE_TOP, 3));
    out.push(text("Runner", [DOCK.x + 6, DOCK.y + DOCK.d - 3], BASE_TOP, { size: 5.2 }));
    const face = RUNNER.y + RUNNER.d;
    const vents = [];
    for (let z = BASE_TOP + 4; z <= BASE_TOP + 12; z += 4) vents.push(faceRect(RUNNER.x + 4, RUNNER.x + 18, z, z + 1.6, face));
    const unit = S(k.slabOf(RUNNER, BASE_TOP, 20, P, 5, 0.9), { tone: "hi" })
      + L(k.planOutline(k.insetPlan(RUNNER, 3), BASE_TOP + 20, P, 4), { tone: "mid" })
      + L(vents.join(""), { tone: "mid" }) + D([[RUNNER.x + 23, face, BASE_TOP + 6]], { size: 0.8, tone: "hi" })
      + screws(RUNNER, BASE_TOP + 20, 5);
    out.push(shown("runner-unit", unit, (stop) => ACTIVE[stop].includes("runner")));
    return out.join("");
  }

  // The environment: one shelf for each layer of the platform. A job works on one layer.
  function environment() {
    const out = [halo(ENV), S(k.slabOf(ENV, FLOOR, 4, P, 5, 0.6), { tone: "lo" })];
    const face = ENV.y + ENV.d;
    LAYERS.forEach((name, index) => {
      const z = BASE_TOP + index * LAYER_H;
      const shelf = k.insetPlan(ENV, 1.5);
      out.push(S(k.slabOf(shelf, z + 0.8, LAYER_H - 1.6, P, 4, 0.5), { tone: "mid" }));
      out.push(faceText(name, [ENV.x + 6, face - 1.5], z + LAYER_H - 3, { size: 4.4, tone: index === layer ? "hi" : "mid" }));
      out.push(D([[ENV.x + ENV.w - 7, face - 1.5, z + LAYER_H / 2]], { size: 0.55, tone: "mid" }));
      if (index !== layer) return;
      const edge = faceRect(ENV.x + 3, ENV.x + ENV.w - 3, z + 1.6, z + LAYER_H - 1.6, face - 1.5);
      out.push(glow("env", L(edge, { tone: "hi" })));
      out.push(shown("verified", D([[ENV.x + ENV.w - 7, face - 1.5, z + LAYER_H / 2]], { size: 1, tone: "hi" }), (stop) => stop >= 8));
    });
    const top = BASE_TOP + LAYERS.length * LAYER_H;
    out.push(L(k.planOutline(k.insetPlan(ENV, 4), top - 0.8, P, 4), { tone: "faint" }));
    out.push(text("Environment", [ENV.x + 8, ENV.y + 32], top - 0.8));
    return out.join("");
  }

  // In front of the core: the approver at the dashboard, and the chat and mail channels.
  function people() {
    const out = [halo(DESK), S(k.slabOf(DESK, FLOOR, 2, P, 5, 0.4), { tone: "lo" })];
    const deskTop = FLOOR + 2;
    const base = { x: DESK.x + 14, y: DESK.y + 10, w: 30, d: 18, r: 1.5 };
    const screen = { x: base.x, y: base.y - 1.6, w: base.w, d: 1.6, r: 0.5 };
    out.push(S(k.slabOf(screen, deskTop, 20, P, 2, 0.3), { tone: "mid" }));
    out.push(L(faceRect(base.x + 2.5, base.x + base.w - 2.5, deskTop + 3, deskTop + 17.5, base.y), { tone: "lo" }));
    out.push(L([14, 11, 8].map((z, row) => k.segment([base.x + 5, base.y, deskTop + z], [base.x + 5 + [18, 12, 15][row], base.y, deskTop + z], P)).join(""), { tone: "lo", free: true }));
    out.push(glow("person", L(faceRect(base.x + 2.5, base.x + base.w - 2.5, deskTop + 3, deskTop + 17.5, base.y), { tone: "hi" })));
    out.push(S(k.slabOf(base, deskTop, 1.4, P, 3, 0.3), { tone: "mid" }));
    out.push(L(k.planOutline({ x: base.x + 3, y: base.y + 2.5, w: base.w - 6, d: 8, r: 0.6 }, deskTop + 1.4, P, 2), { tone: "lo" }));
    out.push(L(k.planOutline({ x: base.x + 10, y: base.y + 12, w: 10, d: 4.5, r: 0.6 }, deskTop + 1.4, P, 2), { tone: "lo" }));
    out.push(text("Approver", [DESK.x + 6, DESK.y + DESK.d - 3], deskTop, { size: 5.2 }));
    out.push(halo(CHAT), S(k.slabOf(CHAT, FLOOR, 2, P, 5, 0.4), { tone: "lo" }));
    const phone = { x: CHAT.x + 17, y: CHAT.y + 3, w: 12, d: 17, r: 1.8 };
    out.push(S(k.slabOf(phone, deskTop, 1.3, P, 4, 0.3), { tone: "mid" }));
    out.push(L(k.planOutline(k.insetPlan(phone, 1.6), deskTop + 1.3, P, 3), { tone: "lo" }));
    out.push(glow("chat", L(k.planOutline(k.insetPlan(phone, 1.6), deskTop + 1.3, P, 3), { tone: "hi" })));
    out.push(text("Chat, email", [CHAT.x + 4, CHAT.y + CHAT.d - 3], deskTop, { size: 4.8 }));
    return out.join("");
  }

  const body = [cables(), behind(), chassis(), modules(), modelCable(), rail(), workItem(), runner(), grant(), environment(), people()];
  return k.figureSvg({
    width: WIDTH, height: HEIGHT, className: "machine",
    label: "The parts of S.R.C. The core of the control plane is one chassis with eight modules: detector, "
      + "agent runtime, git forge, policy engine, dashboard, notifier, access broker and audit log. A work item "
      + "moves along the orchestrator from module to module. Cables join the agent runtime to the model "
      + "provider, the git forge to the application pack, the core to PostgreSQL, the dashboard to the approver, the notifier to chat and email, and "
      + "the core to the runner. Only the runner has a cable to the environment, which has six layers.",
    body,
  });
}

// The figure for one work item: its tier, the tier after the review, its layer and how
// many people approved it.
export function renderSystemSvg({ tier, raisedTo = tier, layer, approvers = 1 }) {
  const tierIndex = TIERS.indexOf(tier);
  const layerIndex = LAYERS.indexOf(layer);
  if (tierIndex < 0) throw new Error(`system figure: unknown tier ${tier}`);
  if (layerIndex < 0) throw new Error(`system figure: unknown layer ${layer}`);
  return build({
    tier: tierIndex, raisedTo: Math.max(tierIndex, TIERS.indexOf(raisedTo)), layer: layerIndex, approvers,
  });
}
