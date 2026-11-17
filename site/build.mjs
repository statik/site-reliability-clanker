#!/usr/bin/env node
// Prerender the data-driven parts of the pages.
//
//   node build.mjs          write the pages
//   node build.mjs --check  exit 1 if a page is not up to date with the data
//
// Each page is a plain HTML file. The build replaces only the text between
// `<!-- build:NAME -->` and `<!-- /build:NAME -->` markers, with markup made from
// data/month.json and screens/manifest.json. The text is therefore in the HTML itself,
// and the pages work without JavaScript. The build has no dependencies.

import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { dayYear, html } from "./lib/html.mjs";
import { steps, totals } from "./lib/month.mjs";
import {
  renderFlowSteps, renderGrantCard, renderGrantLanes, renderRefusal, renderStatePaths,
  renderStateSvg, renderSystemSvg,
} from "./lib/render-architecture.mjs";
import { renderApproval, renderPrinciples, renderStats } from "./lib/render-landing.mjs";
import { renderGallery } from "./lib/render-screens.mjs";
import { renderRail, renderSteps } from "./lib/render-walkthrough.mjs";

const SITE_DIR = dirname(fileURLToPath(import.meta.url));
const DAY_MS = 86400000;

function readJson(path, fallback) {
  if (!existsSync(path)) {
    if (fallback !== undefined) return fallback;
    throw new Error(`${path} is missing. Run: node export-data.mjs`);
  }
  return JSON.parse(readFileSync(path, "utf8"));
}

function axisOf(month) {
  const start = Math.floor(new Date(month.first_event).getTime() / DAY_MS) * DAY_MS;
  const end = Math.ceil(new Date(month.last_event).getTime() / DAY_MS) * DAY_MS;
  return { start: new Date(start).toISOString(), end: new Date(end).toISOString() };
}

function fragments() {
  const month = readJson(join(SITE_DIR, "data", "month.json"));
  const manifest = readJson(join(SITE_DIR, "screens", "manifest.json"), { shots: [] });
  const axis = axisOf(month);
  const span = new Date(axis.end).getTime() - new Date(axis.start).getTime();
  const position = (ts) => {
    const share = (new Date(ts).getTime() - new Date(axis.start).getTime()) / span;
    return Math.min(100, Math.max(0, share * 100));
  };
  const monthTotals = totals(month);
  const context = { month, users: month.users, totals: monthTotals, axis, position };
  const stepList = steps(month);
  const period = html`${dayYear(month.first_event)} to ${dayYear(month.last_event)}`;
  return {
    "index.html": {
      stats: renderStats(monthTotals),
      principles: renderPrinciples(month, monthTotals),
      approval: renderApproval(month),
      period,
    },
    "walkthrough.html": {
      rail: renderRail(stepList, context),
      steps: renderSteps(stepList, context),
      period,
    },
    "architecture.html": {
      system: renderSystemSvg(),
      flow: renderFlowSteps(month),
      states: renderStateSvg(month),
      paths: renderStatePaths(month),
      grant: renderGrantCard(month),
      lanes: renderGrantLanes(month, monthTotals, position, axis),
      refusal: renderRefusal(month),
    },
    "screens.html": { gallery: renderGallery(manifest) },
  };
}

function fill(page, source, parts) {
  let output = source;
  for (const [name, fragment] of Object.entries(parts)) {
    const pattern = new RegExp(`(<!-- build:${name} -->)[\\s\\S]*?(<!-- /build:${name} -->)`, "g");
    if (!pattern.test(output)) throw new Error(`${page} has no marker for build:${name}`);
    const markup = fragment.__html.replace(/\s*\n\s*/g, "\n").trim();
    output = output.replace(pattern, (_match, open, close) => `${open}\n${markup}\n${close}`);
  }
  return output;
}

function main() {
  const check = process.argv.includes("--check");
  const stale = [];
  for (const [page, parts] of Object.entries(fragments())) {
    const path = join(SITE_DIR, page);
    const source = readFileSync(path, "utf8");
    const output = fill(page, source, parts);
    if (output === source) continue;
    if (check) stale.push(page);
    else writeFileSync(path, output);
  }
  if (check && stale.length > 0) {
    console.error(`build: not up to date with the data: ${stale.join(", ")}. Run: node build.mjs`);
    process.exitCode = 1;
    return;
  }
  console.log(check ? "build: all pages are up to date" : "build: pages written");
}

main();
