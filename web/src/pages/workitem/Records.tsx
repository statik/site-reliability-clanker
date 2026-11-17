import { useEffect, useRef, useState } from "react";
import { Accordion, AccordionItem, CodeSnippet, Tab, TabList, TabPanel, TabPanels, Tabs }
  from "@carbon/react";
import type { Approval, AuditEvent, Grant, Job, JobLog, WorkItemDetail } from "@/api/types";
import { Loaded } from "@/components/common";
import { SimpleTable } from "@/components/SimpleTable";
import type { Column } from "@/components/SimpleTable";
import { StatusTag } from "@/components/status";
import { useApi } from "@/hooks/useApi";
import { formatDuration, formatTime } from "@/lib/time";

const LOG_POLL_MS = 3000;

function JobLogView({ job }: { job: Job }) {
  const running = job.ended_at === "";
  const log = useApi<JobLog>(`/jobs/${job.id}/log`, running ? { pollMs: LOG_POLL_MS } : {});
  const { reload } = log;
  // Polling stops when the job ends, so one more load fetches the end of the log.
  const wasRunning = useRef(running);
  useEffect(() => {
    if (wasRunning.current && !running) {
      reload();
    }
    wasRunning.current = running;
  }, [running, reload]);
  return (
    <Loaded state={log} what={`the log of job ${job.id}`}>
      {(data) => (
        <CodeSnippet type="multi" feedback="Copied" className="app-full"
          aria-label={`Log of job ${job.id}`} maxCollapsedNumberOfRows={24}>
          {data.log === "" ? "The job has written no output yet." : data.log}
        </CodeSnippet>
      )}
    </Loaded>
  );
}

function jobTitle(job: Job): string {
  const took = job.ended_at === "" ? "running"
    : formatDuration(new Date(job.ended_at).getTime() - new Date(job.started_at).getTime());
  const layer = job.layer === "" ? "" : ` of ${job.layer}`;
  return `Job ${job.id}: ${job.kind}${layer} · started ${formatTime(job.started_at)} · ${took}`;
}

function JobItem({ job }: { job: Job }) {
  const running = job.ended_at === "";
  const [opened, setOpened] = useState(running);
  return (
    <AccordionItem open={running} onHeadingClick={({ isOpen }) => setOpened(isOpen)} title={
      <span className="app-row">
        <StatusTag status={job.status} size="sm" />
        <span>{jobTitle(job)}</span>
      </span>
    }>
      {job.grant_id !== "" && <p className="app-label">Ran under grant {job.grant_id}</p>}
      {opened && <JobLogView job={job} />}
    </AccordionItem>
  );
}

function Jobs({ jobs }: { jobs: Job[] }) {
  if (jobs.length === 0) {
    return <p className="app-secondary">No job has run for this work item.</p>;
  }
  return (
    <Accordion align="start">
      {jobs.map((job) => <JobItem key={job.id} job={job} />)}
    </Accordion>
  );
}

const APPROVAL_COLUMNS: Column<Approval>[] = [
  { key: "at", header: "Time", value: (row) => row.at, render: (row) => formatTime(row.at) },
  { key: "approver", header: "Who", value: (row) => row.approver },
  { key: "decision", header: "Decision", value: (row) => row.decision.replace("_", " ") },
  { key: "comment", header: "Comment", value: (row) => row.comment },
  { key: "commit", header: "Commit", value: (row) => row.commit.slice(0, 10) },
];

export const GRANT_COLUMNS: Column<Grant>[] = [
  { key: "id", header: "Grant", value: (row) => row.id },
  { key: "identity", header: "Identity", value: (row) => row.identity },
  { key: "scope", header: "Scope", value: (row) => (row.layer === "" || row.layer === row.kind
    ? `${row.kind} in ${row.env}` : `${row.kind} ${row.layer} in ${row.env}`) },
  { key: "status", header: "Status", value: (row) => row.status,
    render: (row) => <StatusTag status={row.status} size="sm" /> },
  { key: "issued", header: "Issued", value: (row) => row.issued_at,
    render: (row) => formatTime(row.issued_at) },
  { key: "ended", header: "Ended", value: (row) => row.ended_at,
    render: (row) => formatTime(row.ended_at) },
  { key: "expires", header: "Expires", value: (row) => row.expires,
    render: (row) => formatTime(row.expires) },
  { key: "approvers", header: "Approvers", value: (row) => (row.approvers ?? []).join(", ") },
];

export const EVENT_COLUMNS: Column<AuditEvent>[] = [
  { key: "seq", header: "Seq", value: (row) => row.seq },
  { key: "ts", header: "Time", value: (row) => row.ts, render: (row) => formatTime(row.ts) },
  { key: "type", header: "Type", value: (row) => row.type },
  { key: "actor", header: "Actor", value: (row) => row.actor },
  { key: "summary", header: "Summary", value: (row) => row.summary },
];

export function EventDetail({ event }: { event: AuditEvent }) {
  return (
    <CodeSnippet type="multi" feedback="Copied" className="app-full"
      aria-label={`Event ${event.seq}`}>
      {JSON.stringify(event, null, 2)}
    </CodeSnippet>
  );
}

/** The record of a work item: jobs with logs, approvals, grants and every event. */
export function Records({ detail }: { detail: WorkItemDetail }) {
  const jobs = detail.jobs ?? [];
  const approvals = detail.approvals ?? [];
  const grants = detail.grants ?? [];
  const events = detail.events ?? [];
  return (
    <Tabs>
      <TabList aria-label="Record of this work item" contained>
        <Tab>Jobs and logs ({jobs.length})</Tab>
        <Tab>Approvals ({approvals.length})</Tab>
        <Tab>Grants ({grants.length})</Tab>
        <Tab>Events ({events.length})</Tab>
      </TabList>
      <TabPanels>
        <TabPanel><Jobs jobs={jobs} /></TabPanel>
        <TabPanel>
          <SimpleTable title="Approvals" rows={approvals} rowId={(row) => String(row.id)}
            columns={APPROVAL_COLUMNS} empty="No decision is recorded." size="sm" />
        </TabPanel>
        <TabPanel>
          <SimpleTable title="Grants" rows={grants} rowId={(row) => row.id}
            columns={GRANT_COLUMNS} empty="No grant was issued for this work item." size="sm" />
        </TabPanel>
        <TabPanel>
          <SimpleTable title="Events" rows={events} rowId={(row) => String(row.seq)}
            columns={EVENT_COLUMNS} empty="No event is recorded." size="sm" pageSize={50}
            expand={(row) => <EventDetail event={row} />} />
        </TabPanel>
      </TabPanels>
    </Tabs>
  );
}
