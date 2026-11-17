// The words of the walkthrough. Headlines and sentence frames are written here; every
// name, number, time, tier and identifier is read from the exported month, so the
// story cannot say something that the product did not do.

import { clock, dayYear, duration, html, list, minute, numberWord, plural } from "./html.mjs";
import { displayName, incidentRefusal, seconds } from "./month.mjs";

export const PRINCIPLES = [
  {
    title: "The model proposes. Code decides.",
    text:
      "The language model drafts changes, summaries and answers. Fixed rules set the risk " +
      "tier from the plan itself. A second model can raise that tier and can never lower it. " +
      "The model that wrote the change has no say in its tier, holds no cloud credential, " +
      "and has no tool that applies a change.",
  },
  {
    title: "Every change is a pull request.",
    text:
      "No console clicks and no ad-hoc kubectl. Even “restart that deployment” is a " +
      "recorded runbook job with parameters.",
  },
  {
    title: "What was approved is exactly what runs.",
    text:
      "An approval is bound to a commit and to the hash of a saved plan. If the world " +
      "moved, the apply is refused and the change comes back for approval.",
  },
  {
    title: "No standing write access.",
    text:
      "Read-only by default. Write permission exists for one approved job, for the one " +
      "layer that the job touches, and is revoked when the job ends.",
  },
  {
    title: "Multi-cloud is a contract, not a port.",
    text:
      "One interface, four implementations (AWS, Google Cloud, Azure and a local " +
      "target), and one conformance suite for all of them.",
  },
];

function names(actors, users) {
  return list([...new Set(actors)].map((actor) => displayName(actor, users)));
}

function approvers(step, item, users) {
  const fromApprovals = (item?.approvals ?? []).map((approval) => approval.approver);
  if (fromApprovals.length > 0) return names(fromApprovals, users);
  const grant = step.grants.find((candidate) => candidate.work_item === item?.id);
  return grant ? names(grant.approvers, users) : "nobody";
}

function writeGrant(step, item) {
  return step.grants.find(
    (grant) => grant.kind !== "debug" && (!item || grant.work_item === item.id),
  );
}

function held(grant) {
  return grant?.held_seconds === null || grant === undefined
    ? "a short time"
    : duration(grant.held_seconds);
}

function canary(event) {
  const check = event?.data?.checks?.find((text) => text.startsWith("canary:"));
  return check ? check.replace(/^canary:\s*/, "") : "the canary check ran";
}

function advisory(step) {
  return step.find("sim.advisory_published")?.data ?? {};
}

function finding(step) {
  return step.find("finding.opened")?.data?.detail ?? {};
}

function daysAdvanced(step, month) {
  const jumps = month.events.filter(
    (event) => event.type === "sim.clock_advanced" && event.seq >= step.first_seq &&
      event.seq <= step.last_seq,
  );
  return Math.round(jumps.reduce((sum, event) => sum + event.data.seconds, 0) / 86400);
}

const STORY = {
  "blank-slate": {
    principle: 1,
    title: "A blank slate, and the first question",
    lead(step, { users }) {
      const item = step.work_items[0];
      const signed = step.all("standing_approval.signed");
      const grant = writeGrant(step, item);
      return html`Paperboat rents out live fish. Before one fish is listed,
        ${names(signed.map((event) => event.actor), users)} signs
        ${numberWord(signed.length)} standing approvals for routine runbooks. The
        orchestrator then opens ${item.id}: ${item.title.toLowerCase()}. Policy code reads
        the plan and rates it <strong>${item.tier}</strong> under the rule
        <code>${item.tier_rule}</code>, so it needs
        ${numberWord(item.approvers_required)} approvers.
        ${approvers(step, item, users)} approve. The broker issues the first grant of the
        month to <code>${grant?.identity}</code> and takes it back ${held(grant)} later.`;
    },
  },
  deploy: {
    principle: 4,
    title: "One layer at a time, one grant at a time",
    lead(step) {
      const envelope = step.all("plan.within_envelope");
      const layers = step.grants.map((grant) => grant.layer);
      const verified = step.find("verify.passed");
      const longest = step.grants.reduce(
        (best, grant) => (grant.held_seconds > (best?.held_seconds ?? 0) ? grant : best),
        null,
      );
      return html`The same approval covers the rest of the first deployment, because
        policy checked that each of the next ${numberWord(envelope.length)} plans only
        creates resources. Every layer still gets its own grant, scoped to that layer:
        ${list(layers)}. The longest, for <code>${longest?.layer}</code>, lasts
        ${held(longest)}. Then the verifier checks the result:
        ${list(verified?.data?.checks ?? [])}. The fish are open for business.`;
    },
  },
  "feature-release": {
    principle: 2,
    title: "The fish start to age: release 1.1.0",
    lead(step, { users }) {
      const item = step.work_items[0];
      const detail = finding(step);
      const pull = step.find("pr.opened");
      const approval = step.find("approval.recorded");
      const grant = writeGrant(step, item);
      return html`Paperboat ${detail.value} is published: fish now have a condition
        score and a retirement age, and the release has a schema migration. The release
        detector sees that local still runs ${detail.running}. The Author role, a
        language model, opens pull request #${pull?.data?.pr}, which changes
        ${plural(pull?.data?.paths?.length ?? 1, "file")}. Policy rates it
        <strong>${item.tier}</strong> (<code>${item.tier_rule}</code>): one approver.
        ${approvers(step, item, users)} approves at ${minute(approval?.ts)} UTC. The
        runner applies with a grant for <code>${grant?.identity}</code> that lives for
        ${held(grant)}. Result: ${canary(step.find("verify.passed"))}.`;
    },
  },
  "quiet-week": {
    principle: 4,
    title: "A quiet week",
    lead(step, { users, month }) {
      const item = step.work_items[0];
      const grant = writeGrant(step, item);
      const signer = month.events.find((event) => event.type === "standing_approval.signed");
      return html`${numberWord(daysAdvanced(step, month))} days pass and nobody is
        paged. The scheduler runs the ${item.title.toLowerCase()} as a
        <strong>${item.tier}</strong> runbook, under the standing approval that
        ${names([signer.actor], users)} signed on the first day. Nobody is asked this
        time, and nothing is hidden: the run still needs a grant (${held(grant)}), and
        each use is a record in the audit log.`;
    },
  },
  cve: {
    principle: 2,
    title: "A vulnerability in the base image",
    lead(step, { users }) {
      const item = step.work_items[0];
      const notice = advisory(step);
      const detail = finding(step);
      const grant = writeGrant(step, item);
      return html`Advisory ${notice.advisory} (severity ${notice.severity}): the base
        image of Paperboat ${detail.running} is vulnerable, and release ${detail.value}
        has the fix. The advisory detector opens a finding, the Author opens pull request
        #${step.find("pr.opened")?.data?.pr}, and policy rates it
        <strong>${item.tier}</strong>. ${approvers(step, item, users)} approves. The
        write access lasts ${held(grant)}. There is no functional change, and the fish
        do not notice.`;
    },
  },
  "bad-release": {
    principle: 1,
    title: "A bad release, caught and rolled back",
    lead(step, { users }) {
      const [change, rollback] = step.work_items;
      const failed = step.find("verify.failed");
      const passed = step.find("verify.passed");
      const authorised = step.find("rollback.authorized");
      const elapsed = failed && passed ? duration(seconds(failed.ts, passed.ts)) : "moments";
      return html`Paperboat ${finding(step).value} looks fine on paper.
        ${approvers(step, change, users)} approves it as a normal
        <strong>${change.tier}</strong> change, and after the apply the health check
        still answers 200. The canary disagrees: ${canary(failed)}. Verification fails,
        and the orchestrator opens ${rollback?.id ?? "a rollback"}.
        ${authorised?.summary}. ${elapsed} after the failure, the check reads:
        ${canary(passed)}. No person had to wake up to decide that half of the inventory
        requests failing is bad.`;
    },
  },
  "kubernetes-eol": {
    principle: 5,
    title: "A forecast, not an emergency",
    lead(step, { month }) {
      const notice = advisory(step);
      const forecast = month.timeline.future.find((item) => item.advisory === notice.advisory);
      return html`Advisory ${notice.advisory}: ${forecast?.title ?? step.scene?.title}. The
        date is ${notice.due ? dayYear(notice.due) : "some weeks away"}, so no work
        item opens today. It becomes a forecast on the timeline, past the
        “now” marker, next to the planned backup and restore windows. An honest
        note from the simulator: “${step.scene?.detail}” The scripted month
        therefore shows this upgrade as planned work and does not apply it.`;
    },
  },
  "database-patch": {
    principle: 4,
    title: "A patch for the database",
    lead(step, { users, totals }) {
      const item = step.work_items[0];
      const detail = finding(step);
      const grant = writeGrant(step, item);
      const isLongest = grant && totals.longest.id === grant.id;
      return html`Advisory ${advisory(step).advisory}: this environment runs Postgres
        ${detail.running}, and ${detail.value} is pending. The change is one variable in
        the layer <code>${detail.layer}</code>. Policy rates it
        <strong>${item.tier}</strong>, and ${approvers(step, item, users)} approves. The
        grant for <code>${grant?.identity}</code> is held for ${held(grant)}${
          isLongest ? ", the longest of the month" : ""
        }: a database restart takes the time that it takes, and the access lasts exactly
        that long.`;
    },
  },
  "database-upkeep": {
    principle: 2,
    title: "Vacuum, analyze, no meeting",
    lead(step) {
      const item = step.work_items[0];
      const grant = writeGrant(step, item);
      return html`${item.title} is due. It is a routine, reversible job, so it runs as a
        <strong>${item.tier}</strong> runbook under a standing approval. It is still a
        recorded job with parameters, at a fixed commit of the runbooks repository, and
        not somebody at a database prompt. Grant issued, used for ${held(grant)},
        revoked.`;
    },
  },
  "restore-drill": {
    principle: 2,
    title: "A backup is only as good as its restore",
    lead(step) {
      const titles = step.work_items.map((item) => item.title.toLowerCase());
      const done = step.all("runbook.succeeded").at(-1);
      return html`Two runbooks run in order: the ${list(titles)}. The drill restores
        the newest backup into a scratch database and checks it. The question
        “when was the last successful restore test?” now has an answer with a
        date on it: ${done ? dayYear(done.ts) : "this month"}.`;
    },
  },
  drift: {
    principle: 2,
    title: "Someone used kubectl",
    lead(step, { users }) {
      const item = step.work_items[0];
      const manual = step.find("sim.manual_change");
      const resources = finding(step).resources ?? [];
      const grant = writeGrant(step, item);
      return html`${manual?.summary}. No pull request and no approval. The nightly
        drift check plans every layer and finds that <code>${resources.join(", ")}</code>
        differs from the code. To put it back is a change like any other:
        <strong>${item.tier}</strong>, approved by ${approvers(step, item, users)},
        applied with a grant for <code>${grant?.identity}</code> (${held(grant)}).`;
    },
  },
  incident: {
    principle: 1,
    title: "02:00. Inventory requests start to fail",
    lead(step, { users, month }) {
      const opened = step.all("incident.opened");
      const debug = step.grants.find((grant) => grant.kind === "debug");
      const refusal = incidentRefusal(month, step.events);
      const runbook = step.find("runbook.succeeded");
      const fix = step.work_items.find((item) => item.kind.startsWith("runbook"));
      const fixGrant = writeGrant(step, fix);
      const starter = step.events.find(
        (event) => event.type === "workitem.created" && event.work_item === fix?.id,
      );
      const span = duration(seconds(opened[0].ts, opened.at(-1).ts));
      // What happened next depends on the run, so each sentence is there only if the
      // log has the event that it describes.
      const refused = refusal
        ? html`It writes a diagnosis and proposes no routine runbook, so code stops
            there: “${refusal.data?.error}”.`
        : html`It writes a diagnosis. Which runbook may run is for policy code to decide,
            not for the model.`;
      const repeats = opened.length > 1
        ? html`The detector fires ${numberWord(opened.length)} times in ${span}.`
        : "";
      const fixed = runbook && starter
        ? html`At ${clock(starter.ts)}, ${names([starter.actor], users)} starts the runbook
            <code>${runbook.data?.runbook}</code>${
              runbook.data?.params?.deployment
                ? html` on <code>${runbook.data.params.deployment}</code>` : ""
            } under the standing approval. ${held(fixGrant)} later it is done.`
        : "";
      return html`At ${clock(opened[0].ts)} UTC the health detector reports:
        ${step.find("finding.opened")?.summary}. The Investigator role gets a debug
        grant: read-only, one namespace, a ${debug?.window_minutes}-minute window, closed
        after ${held(debug)}. ${refused} ${repeats} ${fixed}`;
    },
  },
  "stale-plan": {
    principle: 3,
    title: "The plan went stale, so the apply was refused",
    lead(step, { users }) {
      const refused = step.find("apply.refused");
      const stale = step.work_items.find((item) => item.id === refused?.work_item);
      const applied = step.work_items.find((item) => item.id !== stale?.id);
      const pulls = step.all("pr.opened");
      const staleGrant = writeGrant(step, stale);
      return html`After the incident, ${names(pulls.map((event) => event.actor), users)}
        opens ${numberWord(pulls.length)} hand-written pull requests against the same
        layer. People use the same road as the agent. ${approvers(step, applied, users)}
        approves “${applied?.title}” and it is applied.
        ${approvers(step, stale, users)} then approves “${stale?.title}”, but
        the world has moved since its plan was saved: “${refused?.data?.reason}”.
        The grant is revoked after ${held(staleGrant)}, the pull request is updated, a new
        plan is saved, and the approval is requested again. ${stale?.id} is in the state
        ${stale?.state} as this page is built.`;
    },
  },
  audit: {
    principle: 3,
    title: "The auditor asks for the evidence",
    lead(_step, { totals }) {
      return html`Every event of the month is in one append-only log, and each record
        carries the hash of the record before it. The chain
        ${totals.chain.ok ? "verifies" : "does NOT verify"}: ${totals.chain.count} events,
        head <code>${totals.chain.head_hash.slice(0, 12)}</code>. In it:
        ${totals.approvals} approvals, ${totals.applies} applies,
        ${totals.runbooks} runbook runs, ${plural(totals.rollbacks, "rollback")},
        ${plural(totals.refused, "refused apply", "refused applies")} and
        ${totals.grants} grants. In ${totals.days} days, write or debug access existed for a
        total of ${duration(totals.held_seconds)}.`;
    },
  },
};

export function story(step, context) {
  const entry = STORY[step.id];
  if (entry) {
    try {
      return { title: entry.title, principle: entry.principle, lead: entry.lead(step, context) };
    } catch (error) {
      // The data does not have the shape that this story expects (for example, a scene
      // that opened no work item). Say less instead of failing the whole build.
      console.warn(`story: the step "${step.id}" uses the plain text of its scene (${error.message})`);
    }
  }
  // A scene that this file does not know yet still gets a truthful step.
  return {
    title: entry?.title ?? step.scene?.title ?? step.id,
    principle: entry?.principle ?? null,
    lead: html`${step.scene?.detail ?? ""}`,
  };
}
