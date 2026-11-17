import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Button, InlineLoading, InlineNotification, Tag, Tile } from "@carbon/react";
import { Download } from "@carbon/icons-react";
import { apiGet, errorMessage } from "@/api/client";
import type { AuditEvent, ChainVerification } from "@/api/types";
import { query } from "@/api/urls";
import { AppLink, ErrorNote, Loaded, PageHeader, Section, WorkItemLink }
  from "@/components/common";
import { SimpleTable } from "@/components/SimpleTable";
import type { Column } from "@/components/SimpleTable";
import { StatusTag } from "@/components/status";
import { anyEvent, useApi } from "@/hooks/useApi";
import { formatTime } from "@/lib/time";
import { EVENT_COLUMNS, EventDetail } from "@/pages/workitem/Records";

const LIMIT = 500;

const COLUMNS: Column<AuditEvent>[] = [
  ...EVENT_COLUMNS,
  { key: "env", header: "Environment", value: (row) => row.env ?? "" },
  { key: "work_item", header: "Work item", value: (row) => row.work_item ?? "",
    render: (row) => <WorkItemLink id={row.work_item ?? ""} /> },
];

function saveJson(name: string, value: unknown): void {
  const blob = new Blob([JSON.stringify(value, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = name;
  anchor.click();
  URL.revokeObjectURL(url);
}

function EvidenceButton({ from, to }: { from: string; to: string }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const download = () => {
    setBusy(true);
    setError(null);
    apiGet<{ generated_at: string }>(`/audit/evidence${query({ from, to })}`).then(
      (pack) => saveJson(`clanker-evidence-${pack.generated_at.slice(0, 10)}.json`, pack),
      (failure: unknown) => setError(errorMessage(failure)),
    ).finally(() => setBusy(false));
  };
  return (
    <div className="app-stack-tight">
      <div className="app-row">
        <Button renderIcon={Download} disabled={busy} onClick={download}>
          Download evidence pack
        </Button>
        {busy && <InlineLoading description="Building the evidence pack" />}
      </div>
      <p className="app-label">
        A JSON file with the events{from === "" ? "" : " of the selected range"}, the chain
        verification, and the approvals and grants of every change.
      </p>
      {error !== null && <ErrorNote title="The evidence pack was not built." message={error} />}
    </div>
  );
}

function Chain() {
  const chain = useApi<ChainVerification>("/audit/verify", { live: anyEvent });
  return (
    <Loaded state={chain} what="the chain verification">
      {(data) => (
        <Tile className="app-stack-tight">
          <StatusTag status={data.ok ? "ok" : "failed"}
            label={data.ok ? "Hash chain verified" : "Hash chain broken"} />
          <p>
            {data.count} events. Each event stores the hash of the one before it, so a changed
            or removed event breaks the chain.
          </p>
          {!data.ok && <p>Broken at event {data.broken_seq}: {data.reason}</p>}
          <p className="app-label">Head hash</p>
          <p className="app-mono">{data.head_hash}</p>
        </Tile>
      )}
    </Loaded>
  );
}

function useDebounced(value: string, ms: number): string {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setSettled(value), ms);
    return () => window.clearTimeout(timer);
  }, [value, ms]);
  return settled;
}

/** The searchable event log, the chain verification and the evidence export. */
export function AuditPage() {
  const [search, setSearch] = useSearchParams();
  const [text, setText] = useState("");
  const q = useDebounced(text, 300);
  const from = search.get("from") ?? "";
  const to = search.get("to") ?? "";
  const seq = search.get("seq");
  const path = seq === null
    ? `/events${query({ q, from, to, limit: LIMIT })}`
    : `/events${query({ after: Number(seq) - 1, order: "asc", limit: 1 })}`;
  const events = useApi<AuditEvent[]>(path, { live: anyEvent });
  const scoped = from !== "" || to !== "" || seq !== null;
  return (
    <>
      <PageHeader title="Audit">
        Every step the control plane takes is one event in an append-only log. The search
        runs on the server over type, actor, summary and data.
      </PageHeader>
      <div className="app-two-col">
        <Section title="Chain verification"><Chain /></Section>
        <Section title="Evidence"><EvidenceButton from={from} to={to} /></Section>
      </div>
      {scoped && (
        <InlineNotification kind="info" lowContrast hideCloseButton className="app-note"
          title={seq === null ? `Showing ${formatTime(from)} to ${formatTime(to)}.`
            : `Showing the cited event ${seq}.`}>
          <Button kind="ghost" size="sm" onClick={() => setSearch({})}>Show all events</Button>
        </InlineNotification>
      )}
      <Loaded state={events} what="the events">
        {(data) => (
          <SimpleTable title="Events"
            description={`${data.length} events, newest first${data.length >= LIMIT
              ? ` (the newest ${LIMIT}; search or choose a range to narrow)` : ""}`}
            rows={data} rowId={(row) => String(row.seq)} columns={COLUMNS} size="sm"
            pageSize={50} onSearch={setText} expand={(row) => <EventDetail event={row} />}
            empty="No event matches the search." toolbar={
              <div className="app-row">
                <Tag type="outline" size="sm">Server search</Tag>
                <AppLink to="/timeline">Pick a range on the timeline</AppLink>
              </div>
            } />
        )}
      </Loaded>
    </>
  );
}
