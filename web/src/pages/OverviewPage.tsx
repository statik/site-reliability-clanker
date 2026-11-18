import { useEffect, useMemo, useState } from "react";
import {
  ClickableTile,
  Link,
  StructuredListBody,
  StructuredListCell,
  StructuredListHead,
  StructuredListRow,
  StructuredListWrapper,
  Tag,
  Tile,
} from "@carbon/react";
import { CloudOffline, Launch, TaskComplete } from "@carbon/icons-react";
import { useNavigate } from "react-router-dom";
import { apiPost, errorMessage } from "@/api/client";
import type { CatchUp, EnvironmentSummary, Overview, Posture, WorkItem } from "@/api/types";
import { AiMark, AppLink, Empty, ErrorNote,
  Loaded, PageHeader, ScrollRegion, Section } from "@/components/common";
import { IconTag, StatusTag, TierTag } from "@/components/status";
import { anyEvent, useApi } from "@/hooks/useApi";
import { formatTime, shortHash } from "@/lib/time";
import { HealthChart } from "@/pages/overview/HealthChart";
import { useSession } from "@/state/session";

const COMPONENT_LABEL: Record<string, string> = {
  kubernetes: "Kubernetes",
  node_image: "Node image",
  postgres: "Postgres",
  paperboat: "Paperboat",
};

function Count({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <p className="app-big">{value}</p>
      <p className="app-label">{label}</p>
    </div>
  );
}

function CatchUpPanel({ catchUp }: { catchUp: CatchUp }) {
  return (
    <Tile className="app-stack" decorator={
      <AiMark source="template" what="catch-up narrative" />
    }>
      <p className="app-label">Since your last visit, {formatTime(catchUp.since)}</p>
      <p className="app-prose">{catchUp.narrative}</p>
      <div className="app-row app-spread">
        <Count label="Changes applied" value={catchUp.applied} />
        <Count label="Failed" value={catchUp.failed} />
        <Count label="Rolled back" value={catchUp.rolled_back} />
        <Count label="Incidents" value={catchUp.incidents} />
        <Count label="Decisions waiting" value={catchUp.waiting} />
      </div>
      <p className="app-label">Source of the narrative: {catchUp.narrative_source}.</p>
    </Tile>
  );
}

function healthOf(env: EnvironmentSummary) {
  if (!env.deployed) {
    return <StatusTag status="not deployed" />;
  }
  const health = env.health;
  if (health === null || health.total === 0) {
    return <StatusTag status="open" label="No health sample yet" />;
  }
  const rate = (health.ok / health.total) * 100;
  return (
    <StatusTag status={rate >= 99 ? "ok" : rate >= 90 ? "degraded" : "failed"}
      label={`${rate.toFixed(1)}% success, p95 ${Math.round(health.p95_ms)} ms`} />
  );
}

function EnvironmentTile({ env }: { env: EnvironmentSummary }) {
  const layers = Object.values(env.layers);
  return (
    <Tile className="app-stack-tight">
      <div className="app-row app-spread">
        <h3 className="app-heading">{env.name}</h3>
        <Tag type="outline" size="sm">{env.cloud}</Tag>
      </div>
      {healthOf(env)}
      <p className="app-label">
        {layers.filter(Boolean).length} of {layers.length} layers applied · state as of{" "}
        {formatTime(env.snapshot_ts)}
      </p>
      <div className="app-row">
        {Object.entries(env.versions).map(([component, version]) => (
          <Tag key={component} type="gray" size="sm">
            {COMPONENT_LABEL[component] ?? component} {version === "" ? "–" : version}
          </Tag>
        ))}
      </div>
      <div className="app-row app-spread">
        <AppLink to="/activity">{env.open_work_items} open work items</AppLink>
        {env.app_url !== "" && (
          <Link href={env.app_url} target="_blank" rel="noreferrer" renderIcon={Launch}>
            Open the application
          </Link>
        )}
      </div>
    </Tile>
  );
}

function PlannedCloud({ cloud }: { cloud: string }) {
  return (
    <Tile className="app-stack-tight">
      <div className="app-row app-spread">
        <h3 className="app-heading">{cloud}</h3>
        <IconTag type="outline" icon={CloudOffline}>Validated, not applied</IconTag>
      </div>
      <p className="app-secondary">
        The platform code for this cloud passes validation. Nothing is created in a cloud
        account until the owner approves the cost.
      </p>
    </Tile>
  );
}

function NeedsYou({ waiting }: { waiting: WorkItem[] }) {
  const navigate = useNavigate();
  if (waiting.length === 0) {
    return <Empty icon={<TaskComplete size={32} />}>Nothing waits for a decision.</Empty>;
  }
  return (
    <div className="app-stack-tight">
      {waiting.map((item) => (
        <ClickableTile key={item.id} onClick={() => navigate(`/work/${item.id}`)}>
          <div className="app-row">
            <TierTag tier={item.tier} size="sm" />
            <Tag type="outline" size="sm">{item.env}</Tag>
            <strong>{item.id} · {item.title}</strong>
          </div>
          <p>{item.explanation.sentence || item.summary}</p>
        </ClickableTile>
      ))}
    </div>
  );
}

function PostureMatrix({ posture }: { posture: Posture }) {
  return (
    <ScrollRegion label="Version posture">
      <StructuredListWrapper aria-label="Version posture">
        <StructuredListHead>
          <StructuredListRow head>
            <StructuredListCell head>Environment</StructuredListCell>
            {posture.components.map((component) => (
              <StructuredListCell head key={component}>
                {COMPONENT_LABEL[component] ?? component}
              </StructuredListCell>
            ))}
          </StructuredListRow>
        </StructuredListHead>
        <StructuredListBody>
          {posture.rows.map((row) => (
            <StructuredListRow key={row.env}>
              <StructuredListCell noWrap>{row.env}</StructuredListCell>
              {posture.components.map((component) => {
                const cell = row.cells[component];
                if (cell === undefined) {
                  return <StructuredListCell key={component}>–</StructuredListCell>;
                }
                const behind = cell.status === "behind"
                  ? `Behind: ${cell.current}, latest ${cell.latest}` : undefined;
                return (
                  <StructuredListCell key={component}>
                    <StatusTag status={cell.status} size="sm"
                      {...(behind === undefined ? {} : { label: behind })} />
                    {cell.status === "current" && <span> {cell.current}</span>}
                  </StructuredListCell>
                );
              })}
            </StructuredListRow>
          ))}
        </StructuredListBody>
      </StructuredListWrapper>
    </ScrollRegion>
  );
}

const SEEN_DELAY_MS = 5000;

/**
 * Records the visit, so that the catch-up of the next visit starts here. It waits until the
 * panel has been on screen, and it never runs for a past time.
 */
function useMarkSeen(): string | null {
  const { asOf } = useSession();
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (asOf !== null) {
      return;
    }
    const timer = window.setTimeout(() => {
      apiPost("/me/seen", {}).then(() => setError(null),
        (failure: unknown) => setError(errorMessage(failure)));
    }, SEEN_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [asOf]);
  return error;
}

function Body({ overview }: { overview: Overview }) {
  const posture = useApi<Posture>("/posture", { live: anyEvent });
  const now = useMemo(() => new Date(overview.now), [overview.now]);
  const chain = overview.chain;
  // The catch-up of this visit stays on screen after the visit is recorded.
  const [catchUp] = useState(overview.catch_up);
  const seenError = useMarkSeen();
  return (
    <div className="app-stack">
      <Section title="Catch-up"><CatchUpPanel catchUp={catchUp} /></Section>
      {seenError !== null && (
        <ErrorNote title="Could not record this visit." message={seenError} />
      )}
      <div className="app-two-col">
        <Section title="Needs you"><NeedsYou waiting={overview.waiting} /></Section>
        <Section title="Audit chain">
          <Tile className="app-stack-tight">
            <StatusTag status={chain.ok ? "ok" : "failed"}
              label={chain.ok ? "Chain verified" : "Chain broken"} />
            <p>{chain.count} events. Head {shortHash(chain.head_hash, 16)}</p>
            {!chain.ok && <p>Broken at event {chain.broken_seq}: {chain.reason}</p>}
            <AppLink to="/audit">Open the audit log</AppLink>
          </Tile>
        </Section>
      </div>
      <Section title="Environments">
        <div className="app-grid">
          {overview.environments.map((env) => <EnvironmentTile key={env.name} env={env} />)}
          {overview.planned_clouds.map((cloud) => <PlannedCloud key={cloud} cloud={cloud} />)}
        </div>
      </Section>
      <Section title="Version posture">
        <Loaded state={posture} what="the version posture">
          {(data) => <PostureMatrix posture={data} />}
        </Loaded>
      </Section>
      <Section title="Success rate, last 14 days"><HealthChart now={now} /></Section>
      <p className="app-label">
        Server time {formatTime(overview.now)} · agent mode {overview.agent_mode}
      </p>
    </div>
  );
}

/** Is each environment healthy, what needs me now, and what happened since I last looked? */
export function OverviewPage() {
  const overview = useApi<Overview>("/overview", { live: anyEvent });
  return (
    <>
      <PageHeader title="Overview" />
      <Loaded state={overview} what="the overview">
        {(data) => <Body overview={data} />}
      </Loaded>
    </>
  );
}
