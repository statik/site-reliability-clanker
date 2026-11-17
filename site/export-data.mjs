#!/usr/bin/env node
// Export the scripted month from a running control plane into data/month.json.
//
//   node export-data.mjs [--api http://127.0.0.1:7070/api/v1] [--no-build]
//
// The script only sends GET requests. It removes grant signatures, then refuses to
// write the file if the result contains anything that looks like a secret, a local
// path, this machine's host name, or an email address outside example.com (the demo
// users are fictional). After a successful export it runs build.mjs, so the
// prerendered pages cannot drift from the data.

import { execFileSync } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { hostname, userInfo } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const SITE_DIR = dirname(fileURLToPath(import.meta.url));
const OUTPUT = join(SITE_DIR, "data", "month.json");
const DEFAULT_API = "http://127.0.0.1:7070/api/v1";

function argument(name, fallback) {
  const index = process.argv.indexOf(name);
  return index === -1 ? fallback : process.argv[index + 1];
}

async function get(api, path) {
  const url = `${api}${path}`;
  let response;
  try {
    response = await fetch(url, { method: "GET", headers: { Accept: "application/json" } });
  } catch (error) {
    throw new Error(
      `cannot reach the control plane at ${url}: ${error.message}. ` +
        "Start it, or pass --api <base url>.",
    );
  }
  if (!response.ok) throw new Error(`GET ${url} answered ${response.status}`);
  return response.json();
}

function withoutSignature(grant) {
  const { signature: _signature, ...rest } = grant;
  return rest;
}

function trimEvent(event) {
  // prev_hash repeats the hash of the event before it, so the chain is still complete.
  const { prev_hash: _previous, wall_ts: _wall, ...rest } = event;
  return rest;
}

function trimDetail(detail) {
  return {
    work_item: detail.work_item,
    approvers_required: detail.approvers_required,
    deferred_layers: detail.deferred_layers ?? [],
    approvals: detail.approvals ?? [],
    grants: (detail.grants ?? []).map(withoutSignature),
    plans: (detail.plans ?? []).map((plan) => ({
      id: plan.id,
      layer: plan.layer,
      commit: plan.commit,
      plan_sha256: plan.plan_sha256,
      facts: plan.facts,
      tier: plan.tier,
      rule: plan.rule,
      created_at: plan.created_at,
    })),
    jobs: (detail.jobs ?? []).map((job) => ({
      id: job.id,
      layer: job.layer,
      kind: job.kind,
      status: job.status,
      grant_id: job.grant_id,
      started_at: job.started_at,
      ended_at: job.ended_at,
    })),
    pull: detail.pull
      ? {
          id: detail.pull.id,
          repo: detail.pull.repo,
          title: detail.pull.title,
          body: detail.pull.body,
          branch: detail.pull.branch,
          state: detail.pull.state,
          author: detail.pull.author,
        }
      : null,
    diff: detail.diff ?? "",
  };
}

const FORBIDDEN = [
  { name: "grant signature", pattern: /"signature"\s*:/ },
  { name: "private key", pattern: /-----BEGIN [A-Z ]*PRIVATE KEY-----/ },
  { name: "bearer token", pattern: /Bearer\s+[A-Za-z0-9._-]{12,}/ },
  { name: "API key", pattern: /\bsk-[A-Za-z0-9_-]{16,}/ },
  { name: "AWS access key", pattern: /\bAKIA[0-9A-Z]{16}\b/ },
  { name: "GitHub token", pattern: /\bgh[pousr]_[A-Za-z0-9]{20,}/ },
  { name: "JSON web token", pattern: /\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\./ },
  { name: "password assignment", pattern: /pass(word|wd)?["']?\s*[:=]\s*["'][^"']+/i },
  { name: "database URL with credentials", pattern: /postgres(ql)?:\/\/[^\s"@/]+:[^\s"@/]+@/ },
  { name: "home directory path", pattern: /\/(Users|home)\/[A-Za-z0-9._-]+/ },
];

function scan(text) {
  const problems = [];
  for (const { name, pattern } of FORBIDDEN) {
    const match = text.match(pattern);
    if (match) problems.push(`${name}: "${match[0].slice(0, 40)}"`);
  }
  for (const word of [hostname(), hostname().split(".")[0], userInfo().username]) {
    if (word && word.length > 3 && text.toLowerCase().includes(word.toLowerCase())) {
      problems.push(`this machine's host or user name appears in the data ("${word}")`);
    }
  }
  const emails = text.match(/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/g) ?? [];
  const real = [...new Set(emails)].filter((email) => !email.endsWith("@example.com"));
  if (real.length > 0) problems.push(`email addresses outside example.com: ${real.join(", ")}`);
  return [...new Set(problems)];
}

async function main() {
  const api = argument("--api", DEFAULT_API).replace(/\/$/, "");
  const [events, items, access, timeline, sim, audit, users] = await Promise.all([
    get(api, "/events?order=asc&limit=5000"),
    get(api, "/work-items"),
    get(api, "/access"),
    get(api, "/timeline"),
    get(api, "/sim"),
    get(api, "/audit/verify"),
    get(api, "/users"),
  ]);
  if (!Array.isArray(events) || events.length === 0) {
    throw new Error("the control plane returned no events; run the scripted month first");
  }

  const workItems = {};
  const ids = items.map((item) => item.id).sort();
  for (const id of ids) {
    workItems[id] = trimDetail(await get(api, `/work-items/${id}`));
  }

  const month = {
    about:
      "Snapshot of the scripted month from the Clanker control plane. " +
      "Written by site/export-data.mjs. Do not edit by hand.",
    source: "GET /api/v1/{events,work-items,access,timeline,sim,audit/verify,users}",
    first_event: events[0].ts,
    last_event: events[events.length - 1].ts,
    sim: { now: sim.now, scenes: sim.scenes },
    audit,
    users: users.map(({ name, display, role }) => ({ name, display, role })),
    events: events.map(trimEvent),
    work_items: workItems,
    grants: [...(access.history ?? []), ...(access.active ?? [])].map(withoutSignature),
    timeline: { future: timeline.future ?? [], health: timeline.health ?? {} },
  };

  const text = `${JSON.stringify(month, null, 1)}\n`;
  const problems = scan(text);
  if (problems.length > 0) {
    throw new Error(
      `refusing to write ${OUTPUT}; the export failed the secret scan:\n  - ` +
        problems.join("\n  - "),
    );
  }
  await mkdir(dirname(OUTPUT), { recursive: true });
  await writeFile(OUTPUT, text);
  console.log(
    `wrote ${OUTPUT}: ${month.events.length} events, ${ids.length} work items, ` +
      `${month.grants.length} grants, ${(text.length / 1024).toFixed(0)} KB`,
  );

  if (!process.argv.includes("--no-build")) {
    execFileSync(process.execPath, [join(SITE_DIR, "build.mjs")], { stdio: "inherit" });
  }
}

main().catch((error) => {
  console.error(`export-data: ${error.message}`);
  process.exitCode = 1;
});
