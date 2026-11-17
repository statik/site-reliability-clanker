import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ClickableTile, Dropdown, Tag } from "@carbon/react";
import { TaskComplete } from "@carbon/icons-react";
import type { WorkItem } from "@/api/types";
import { Empty, Loaded, PageHeader, WorkItemLink } from "@/components/common";
import { SimpleTable } from "@/components/SimpleTable";
import type { Column } from "@/components/SimpleTable";
import { StateTag, stateLabel, TierTag } from "@/components/status";
import { useApi } from "@/hooks/useApi";
import { formatRelative, formatTime } from "@/lib/time";
import { useServerNow } from "@/state/clock";

const ALL = "All";
const workItemEvent = (event: { type: string }) => event.type.startsWith("workitem.");

function ApprovalTile({ item }: { item: WorkItem }) {
  const navigate = useNavigate();
  const now = useServerNow();
  return (
    <ClickableTile onClick={() => navigate(`/work/${item.id}`)}>
      <div className="app-stack-tight">
        <div className="app-row">
          <TierTag tier={item.tier} size="sm" />
          <Tag type="outline" size="sm">{item.env}</Tag>
          <Tag type="outline" size="sm">{item.kind}</Tag>
          <span className="app-label">
            {item.id} · waiting since {formatRelative(item.updated_at, now)}
          </span>
        </div>
        <p className="app-heading">{item.title}</p>
        <p>{item.explanation.sentence || item.summary}</p>
        {item.explanation.if_deferred !== "" && (
          <p className="app-secondary">If you leave it: {item.explanation.if_deferred}</p>
        )}
      </div>
    </ClickableTile>
  );
}

/** What is waiting on a decision. */
export function ApprovalsPage() {
  const items = useApi<WorkItem[]>("/work-items", { live: workItemEvent });
  return (
    <>
      <PageHeader title="Approvals">
        Work items that wait for a decision. Open one to read what changes, then decide.
      </PageHeader>
      <Loaded state={items} what="the work items">
        {(data) => {
          const waiting = data.filter((item) => item.state === "AwaitingApproval");
          if (waiting.length === 0) {
            return (
              <Empty icon={<TaskComplete size={32} />}>
                Nothing waits for a decision. Finished and running work is in Activity.
              </Empty>
            );
          }
          return (
            <div className="app-stack app-narrow">
              {waiting.map((item) => <ApprovalTile key={item.id} item={item} />)}
            </div>
          );
        }}
      </Loaded>
    </>
  );
}

const COLUMNS: Column<WorkItem>[] = [
  { key: "id", header: "ID", value: (row) => row.id,
    render: (row) => <WorkItemLink id={row.id} /> },
  { key: "title", header: "Title", value: (row) => row.title },
  { key: "state", header: "State", value: (row) => stateLabel(row.state),
    render: (row) => <StateTag state={row.state} size="sm" /> },
  { key: "tier", header: "Tier", value: (row) => row.tier,
    render: (row) => <TierTag tier={row.tier} size="sm" /> },
  { key: "kind", header: "Kind", value: (row) => row.kind },
  { key: "env", header: "Environment", value: (row) => row.env },
  { key: "created", header: "Opened", value: (row) => row.created_at,
    render: (row) => formatTime(row.created_at) },
  { key: "updated", header: "Updated", value: (row) => row.updated_at,
    render: (row) => formatTime(row.updated_at) },
];

function Expanded({ item }: { item: WorkItem }) {
  return (
    <div className="app-stack-tight">
      <p>{item.explanation.sentence || item.summary || "No summary is written yet."}</p>
      <p className="app-label">
        Trigger: {item.trigger || "not recorded"} · layers:{" "}
        {(item.layers ?? []).join(", ") || "none"} · rule: {item.tier_rule || "none"}
        {item.scheduled_for !== "" && ` · scheduled for ${formatTime(item.scheduled_for)}`}
      </p>
      <WorkItemLink id={item.id}>Open {item.id}: plan, diff, logs and approvals</WorkItemLink>
    </div>
  );
}

function Filter({ id, label, values, value, onChange }: {
  id: string;
  label: string;
  values: string[];
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <Dropdown id={id} titleText="" label={label} type="inline" size="lg"
      aria-label={`Filter by ${label}`} items={[ALL, ...values]} selectedItem={value}
      itemToString={(item) => (item === ALL ? `${label}: all` : `${label}: ${item ?? ""}`)}
      onChange={({ selectedItem }) => onChange(selectedItem ?? ALL)} />
  );
}

function ActivityTable({ items }: { items: WorkItem[] }) {
  const [state, setState] = useState(ALL);
  const [tier, setTier] = useState(ALL);
  const [kind, setKind] = useState(ALL);
  const distinct = (pick: (item: WorkItem) => string) =>
    [...new Set(items.map(pick))].filter((value) => value !== "").toSorted();
  const rows = useMemo(() => items.filter((item) =>
    (state === ALL || item.state === state) && (tier === ALL || item.tier === tier) &&
    (kind === ALL || item.kind === kind)), [items, state, tier, kind]);
  return (
    <SimpleTable title="Work items" description={`${rows.length} of ${items.length} shown`}
      rows={rows} rowId={(row) => row.id} columns={COLUMNS}
      expand={(row) => <Expanded item={row} />} empty="No work item matches the filters."
      toolbar={
        <>
          <Filter id="activity-state" label="State" values={distinct((item) => item.state)}
            value={state} onChange={setState} />
          <Filter id="activity-tier" label="Tier" values={distinct((item) => item.tier)}
            value={tier} onChange={setTier} />
          <Filter id="activity-kind" label="Kind" values={distinct((item) => item.kind)}
            value={kind} onChange={setKind} />
        </>
      } />
  );
}

/** What ran, who approved it and what happened. */
export function ActivityPage() {
  const items = useApi<WorkItem[]>("/work-items", { live: workItemEvent });
  return (
    <>
      <PageHeader title="Activity">
        Every work item. Sort a column, filter by state, tier or kind, or search. Expand a row
        for the summary, and open the item for its plan, diff, logs and approval record.
      </PageHeader>
      <Loaded state={items} what="the work items">
        {(data) => <ActivityTable items={data} />}
      </Loaded>
    </>
  );
}
