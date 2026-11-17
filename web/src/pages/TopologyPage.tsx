import { useState } from "react";
import { Dropdown, InlineNotification, ListItem, Tag, Toggle, UnorderedList }
  from "@carbon/react";
import type { Timeline, Topology } from "@/api/types";
import { query } from "@/api/urls";
import { Loaded, PageHeader, Section } from "@/components/common";
import { StatusTag } from "@/components/status";
import { TopologyDiagram } from "@/components/topology/TopologyDiagram";
import { anyEvent, useApi } from "@/hooks/useApi";
import { diffTopology } from "@/lib/topologyLayout";
import { formatTime } from "@/lib/time";
import { useSession } from "@/state/session";

const ALL = "All environments";

function Caption({ topology }: { topology: Topology }) {
  const health = topology.health;
  const rate = health === null || health.total === 0 ? null : (health.ok / health.total) * 100;
  return (
    <div className="app-row">
      <Tag type="outline">Snapshot {formatTime(topology.snapshot_ts)}</Tag>
      <Tag type="outline">Reason: {topology.snapshot_reason}</Tag>
      {rate !== null && (
        <StatusTag status={rate >= 90 ? "ok" : "degraded"}
          label={`${rate.toFixed(1)}% of requests succeed`} />
      )}
      {topology.nodes.length === 0 && <StatusTag status="not deployed" />}
    </div>
  );
}

function ThenAndNow({ env }: { env: string }) {
  const { asOf } = useSession();
  const path = `/topology${query({ env })}`;
  const then = useApi<Topology>(path);
  const now = useApi<Topology>(path, { live: anyEvent, ignoreAsOf: true });
  const changes = then.data !== null && now.data !== null
    ? diffTopology(then.data, now.data) : new Map<string, string[]>();
  return (
    <div className="app-stack">
      <InlineNotification kind="info" lowContrast hideCloseButton className="app-note"
        title={changes.size === 0 ? "Nothing differs between then and now."
          : `${changes.size} parts differ between then and now.`}
        subtitle="A part that differs has a dashed border and a Differs tag in both diagrams." />
      {changes.size > 0 && (
        <UnorderedList>
          {[...changes].map(([id, reasons]) => (
            <ListItem key={id}><strong>{id}</strong>: {reasons.join(". ")}</ListItem>
          ))}
        </UnorderedList>
      )}
      <div className="app-split">
        <div className="app-stack-tight">
          <h3 className="app-heading">As of {formatTime(asOf)}</h3>
          <Loaded state={then} what={`the past topology of ${env}`}>
            {(data) => (
              <>
                <Caption topology={data} />
                <TopologyDiagram topology={data} changes={changes} compact />
              </>
            )}
          </Loaded>
        </div>
        <div className="app-stack-tight">
          <h3 className="app-heading">Now</h3>
          <Loaded state={now} what={`the current topology of ${env}`}>
            {(data) => (
              <>
                <Caption topology={data} />
                <TopologyDiagram topology={data} changes={changes} compact />
              </>
            )}
          </Loaded>
        </div>
      </div>
    </div>
  );
}

function Single({ env }: { env: string }) {
  const topology = useApi<Topology>(`/topology${query({ env })}`, { live: anyEvent });
  return (
    <Loaded state={topology} what={`the topology of ${env}`}>
      {(data) => (
        <div className="app-stack-tight">
          <Caption topology={data} />
          <TopologyDiagram topology={data} />
        </div>
      )}
    </Loaded>
  );
}

function Environments({ envs }: { envs: string[] }) {
  const { asOf } = useSession();
  const [choice, setChoice] = useState(envs[0] ?? ALL);
  const [compare, setCompare] = useState(false);
  const shown = choice === ALL ? envs : [choice];
  const comparing = compare && asOf !== null;
  return (
    <div className="app-stack">
      <div className="app-row app-row-end">
        <Dropdown id="topology-env" titleText="Environment" label="Environment"
          items={[...envs, ALL]} selectedItem={choice}
          onChange={({ selectedItem }) => setChoice(selectedItem ?? ALL)} />
        <Toggle id="topology-compare" labelText="Compare with now" labelA="Off" labelB="On"
          toggled={comparing} disabled={asOf === null} onToggle={setCompare} />
        {asOf === null && (
          <p className="app-label">
            To compare, first set an as-of time in the header or with the timeline scrubber.
          </p>
        )}
      </div>
      {shown.map((env) => (
        <Section key={env} title={env}>
          {comparing ? <ThenAndNow env={env} /> : <Single env={env} />}
        </Section>
      ))}
    </div>
  );
}

/** What exists in each environment, how it is connected, and the state of each part. */
export function TopologyPage() {
  const timeline = useApi<Timeline>("/timeline?buckets=1");
  return (
    <>
      <PageHeader title="Topology">
        One diagram for each environment, by layer, with the application on top. Each part
        shows its version, its status and badges for a pending change, drift or an incident.
      </PageHeader>
      <Loaded state={timeline} what="the environments">
        {(data) => <Environments envs={data.lanes.map((lane) => lane.env)} />}
      </Loaded>
    </>
  );
}
