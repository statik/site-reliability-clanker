import { Calendar, ChartLineData, EventSchedule } from "@carbon/icons-react";
import type { Finding, FutureItem, Timeline, WorkItem } from "@/api/types";
import { ErrorNote, Loaded, PageHeader, Section, WorkItemLink } from "@/components/common";
import { SimpleTable } from "@/components/SimpleTable";
import type { Column } from "@/components/SimpleTable";
import { IconTag, StateTag, StatusTag, TierTag } from "@/components/status";
import { anyEvent, useApi } from "@/hooks/useApi";
import { TERMINAL_STATES } from "@/lib/stateDurations";
import { formatRelative, formatTime } from "@/lib/time";

const KIND = {
  scheduled: { label: "Scheduled work", icon: Calendar, type: "cyan",
    why: "Approved. It waits for its maintenance window." },
  forecast: { label: "Forecast", icon: ChartLineData, type: "purple",
    why: "A date from an advisory or from a measured trend: the end of support of a "
      + "version, or the day a volume is full." },
  window: { label: "Maintenance window", icon: EventSchedule, type: "teal",
    why: "A routine job that runs on a schedule under a standing approval." },
} as const;

function futureColumns(now: Date): Column<FutureItem>[] {
  return [
    { key: "ts", header: "When", value: (row) => row.ts,
      render: (row) => `${formatTime(row.ts)} (${formatRelative(row.ts, now)})` },
    { key: "kind", header: "Kind", value: (row) => KIND[row.kind].label,
      render: (row) => (
        <IconTag type={KIND[row.kind].type} size="sm" icon={KIND[row.kind].icon}>
          {KIND[row.kind].label}
        </IconTag>
      ) },
    { key: "env", header: "Environment", value: (row) => row.env },
    { key: "title", header: "What", value: (row) => row.title },
    { key: "why", header: "Why", value: (row) => KIND[row.kind].why },
    { key: "tier", header: "Tier", value: (row) => row.tier ?? "",
      render: (row) => (row.tier ? <TierTag tier={row.tier} size="sm" /> : "–") },
    { key: "source", header: "Source",
      value: (row) => row.work_item ?? row.advisory ?? row.source ?? "",
      render: (row) => (row.work_item
        ? <WorkItemLink id={row.work_item} /> : row.advisory ?? row.source ?? "Schedule") },
  ];
}

interface OpenFinding {
  finding: Finding;
  /** The state of the work item that handles the finding, or "" when there is none yet. */
  workState: string;
}

/**
 * Keeps the findings that still need work. The server never closes a finding, so a finding
 * counts as open until its work item reaches a final state.
 */
function openFindings(findings: Finding[], items: WorkItem[]): OpenFinding[] {
  const states = new Map(items.map((item) => [item.id, item.state]));
  return findings
    .map((finding) => ({ finding, workState: states.get(finding.work_item) ?? "" }))
    .filter((entry) => !TERMINAL_STATES.includes(entry.workState));
}

const FINDING_COLUMNS: Column<OpenFinding>[] = [
  { key: "at", header: "Found", value: (row) => row.finding.at,
    render: (row) => formatTime(row.finding.at) },
  { key: "detector", header: "Detector", value: (row) => row.finding.detector },
  { key: "env", header: "Environment", value: (row) => row.finding.env },
  { key: "title", header: "Finding", value: (row) => row.finding.title },
  { key: "state", header: "Handling", value: (row) => row.workState,
    render: (row) => (row.workState === ""
      ? <StatusTag status="open" label="No work item yet" size="sm" />
      : <StateTag state={row.workState} size="sm" />) },
  { key: "work_item", header: "Work item", value: (row) => row.finding.work_item,
    render: (row) => <WorkItemLink id={row.finding.work_item} /> },
];

/** What work is scheduled or forecast, when, and why. */
export function UpcomingPage() {
  const timeline = useApi<Timeline>("/timeline?buckets=1", { live: anyEvent });
  const findings = useApi<Finding[]>("/findings", { live: anyEvent });
  const items = useApi<WorkItem[]>("/work-items", { live: anyEvent, ignoreAsOf: true });
  return (
    <>
      <PageHeader title="Upcoming">
        Scheduled work, forecasts and maintenance windows, in time order, and the findings
        that are still open.
      </PageHeader>
      <Section title="Scheduled and forecast">
        <Loaded state={timeline} what="the upcoming work">
          {(data) => (
            <SimpleTable title="Upcoming"
              rows={data.future.toSorted((a, b) => a.ts.localeCompare(b.ts))}
              rowId={(row) => `${row.kind}/${row.env}/${row.ts}/${row.title}`}
              columns={futureColumns(new Date(data.now))}
              empty="Nothing is scheduled or forecast." />
          )}
        </Loaded>
      </Section>
      <Section title="Open findings">
        <Loaded state={findings} what="the findings">
          {(data) => (
            <SimpleTable title="Findings"
              description="Findings of the detectors whose work is not finished."
              rows={openFindings(data, items.data ?? [])}
              rowId={(row) => String(row.finding.id)} columns={FINDING_COLUMNS}
              empty="No finding is open. The detectors found nothing that needs work." />
          )}
        </Loaded>
        {items.error !== null && (
          <ErrorNote title="Could not load the work items of the findings."
            message={items.error} />
        )}
      </Section>
    </>
  );
}
