import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Button, MultiSelect, Tile, Toggle } from "@carbon/react";
import { ChevronLeft, ChevronRight, ZoomIn, ZoomOut } from "@carbon/icons-react";
import type { Timeline, TimelineMarker } from "@/api/types";
import { query, serverTime } from "@/api/urls";
import { AppLink, Loaded, PageHeader, WorkItemLink } from "@/components/common";
import { SimpleTable } from "@/components/SimpleTable";
import type { Column } from "@/components/SimpleTable";
import { TierTag } from "@/components/status";
import { CATEGORY_SHAPE } from "@/components/timeline/glyphs";
import { TimelineLegend } from "@/components/timeline/Legend";
import { MIN_SPAN_MS, TimelineChart } from "@/components/timeline/TimelineChart";
import { anyEvent, useApi } from "@/hooks/useApi";
import { clampDomain, filterMarkers, initialDomain, NO_TIER, panDomain, zoomDomain }
  from "@/lib/timelineLayout";
import { formatTime } from "@/lib/time";
import { useSession } from "@/state/session";

type Domain = [Date, Date];
const DAY_MS = 86_400_000;
const PRESETS: [string, number][] = [
  ["60 days", 60 * DAY_MS],
  ["7 days", 7 * DAY_MS],
  ["24 hours", DAY_MS],
  ["1 hour", DAY_MS / 24],
];
const CATEGORY_ITEMS = Object.entries(CATEGORY_SHAPE).map(([id, shape]) => ({
  id,
  text: shape.label,
}));
const TIER_ITEMS = [
  { id: "T0", text: "T0 Read-only" },
  { id: "T1", text: "T1 Routine" },
  { id: "T2", text: "T2 Normal change" },
  { id: "T3", text: "T3 High risk" },
  { id: NO_TIER, text: "No tier" },
];

const COLUMNS: Column<TimelineMarker>[] = [
  { key: "ts", header: "Time", value: (row) => row.ts, render: (row) => formatTime(row.ts) },
  { key: "env", header: "Environment", value: (row) => row.env || "all" },
  { key: "layer", header: "Layer", value: (row) => row.layer || "–" },
  { key: "category", header: "Category",
    value: (row) => CATEGORY_SHAPE[row.category]?.label ?? row.category },
  { key: "outcome", header: "Outcome", value: (row) => row.outcome || "–" },
  { key: "tier", header: "Tier", value: (row) => row.tier ?? "",
    render: (row) => (row.tier ? <TierTag tier={row.tier} size="sm" /> : "–") },
  { key: "summary", header: "Summary", value: (row) => row.summary },
  { key: "work_item", header: "Work item", value: (row) => row.work_item,
    render: (row) => <WorkItemLink id={row.work_item} /> },
];

type ClusterStep =
  | { kind: "open"; workItem: string }
  | { kind: "zoom"; domain: Domain }
  | { kind: "list"; range: Domain };

/**
 * Decides what a click on a cluster does. Markers of one work item open it. Otherwise the view
 * zooms to the cluster, and at the closest zoom the cluster becomes the selected range, so
 * that the table lists its markers.
 */
function openCluster(items: TimelineMarker[], domain: Domain): ClusterStep {
  const workItems = new Set(items.map((item) => item.work_item));
  const [only] = workItems;
  if (workItems.size === 1 && only !== undefined && only !== "") {
    return { kind: "open", workItem: only };
  }
  const times = items.map((item) => new Date(item.ts).getTime());
  const first = Math.min(...times);
  const last = Math.max(...times);
  if (domain[1].getTime() - domain[0].getTime() <= MIN_SPAN_MS * 1.01) {
    return { kind: "list", range: [new Date(first - 1), new Date(last + 1)] };
  }
  const pad = Math.max((last - first) * 0.2, MIN_SPAN_MS / 2);
  return { kind: "zoom", domain: [new Date(first - pad), new Date(last + pad)] };
}

function useDebounced<T>(value: T, ms: number): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setSettled(value), ms);
    return () => window.clearTimeout(timer);
  }, [value, ms]);
  return settled;
}

/** Loads the health buckets again for the visible window, so a zoom shows finer buckets. */
function useVisibleHealth(domain: Domain | null, now: string | undefined) {
  const settled = useDebounced(domain, 600);
  let path: string | null = null;
  if (settled !== null && now !== undefined && settled[0].getTime() < new Date(now).getTime()) {
    const to = new Date(Math.min(settled[1].getTime(), new Date(now).getTime()));
    path = `/timeline${query({ from: serverTime(settled[0]), to: serverTime(to), buckets: 240 })}`;
  }
  return useApi<Timeline>(path).data?.health ?? null;
}

interface ToolbarProps {
  domain: Domain;
  limits: Domain;
  now: Date;
  setDomain: (domain: Domain) => void;
  selecting: boolean;
  setSelecting: (on: boolean) => void;
  setCategories: (ids: Set<string>) => void;
  setTiers: (ids: Set<string>) => void;
}

function Toolbar(props: ToolbarProps) {
  const { domain, setDomain } = props;
  const around = (span: number): Domain => {
    const centre = Math.min((domain[0].getTime() + domain[1].getTime()) / 2,
      props.limits[1].getTime());
    return [new Date(centre - span / 2), new Date(centre + span / 2)];
  };
  return (
    <div className="app-stack-tight">
      <div className="app-row app-row-end">
        <Button hasIconOnly kind="ghost" size="md" renderIcon={ChevronLeft}
          iconDescription="Earlier" onClick={() => setDomain(panDomain(domain, -0.4))} />
        <Button hasIconOnly kind="ghost" size="md" renderIcon={ZoomOut}
          iconDescription="Zoom out" onClick={() => setDomain(zoomDomain(domain, 0.5))} />
        <Button hasIconOnly kind="ghost" size="md" renderIcon={ZoomIn}
          iconDescription="Zoom in" onClick={() => setDomain(zoomDomain(domain, 2))} />
        <Button hasIconOnly kind="ghost" size="md" renderIcon={ChevronRight}
          iconDescription="Later" onClick={() => setDomain(panDomain(domain, 0.4))} />
        {PRESETS.map(([label, span]) => (
          <Button key={label} kind="tertiary" size="md" onClick={() => setDomain(around(span))}>
            {label}
          </Button>
        ))}
        <Button kind="tertiary" size="md" onClick={() => {
          const span = domain[1].getTime() - domain[0].getTime();
          setDomain([new Date(props.now.getTime() - span * 0.7),
            new Date(props.now.getTime() + span * 0.3)]);
        }}>
          Go to now
        </Button>
      </div>
      <div className="app-row app-row-end">
        <MultiSelect id="tl-category" titleText="Category" label="All categories" size="md"
          items={CATEGORY_ITEMS} itemToString={(item) => item?.text ?? ""}
          onChange={({ selectedItems }) =>
            props.setCategories(new Set((selectedItems ?? []).map((item) => item.id)))} />
        <MultiSelect id="tl-tier" titleText="Risk tier" label="All tiers" size="md"
          items={TIER_ITEMS} itemToString={(item) => item?.text ?? ""}
          onChange={({ selectedItems }) =>
            props.setTiers(new Set((selectedItems ?? []).map((item) => item.id)))} />
        <Toggle id="tl-select" size="sm" labelText="Drag" labelA="Pans" labelB="Selects a range"
          toggled={props.selecting} onToggle={props.setSelecting} />
      </div>
    </div>
  );
}

function Selection({ range, cursor, count, onZoom, onClear }: {
  range: Domain | null;
  cursor: Date | null;
  count: number;
  onZoom: () => void;
  onClear: () => void;
}) {
  const { asOf, setAsOf } = useSession();
  return (
    <Tile className="app-row app-spread">
      <div>
        <p className="app-label">Scrubber</p>
        <p>{cursor === null ? "On now (live)" : formatTime(cursor)}</p>
      </div>
      <Button kind="primary" size="md" disabled={cursor === null}
        onClick={() => setAsOf(cursor)}>
        View the dashboard as of the scrubber
      </Button>
      {asOf !== null && (
        <Button kind="tertiary" size="md" onClick={() => setAsOf(null)}>Back to now</Button>
      )}
      <div>
        <p className="app-label">Selected range</p>
        <p>
          {range === null ? "None. Turn on range selection and drag across the timeline."
            : `${formatTime(range[0])} to ${formatTime(range[1])}: ${count} events`}
        </p>
      </div>
      {range !== null && (
        <div className="app-row">
          <Button kind="tertiary" size="md" onClick={onZoom}>Zoom to selection</Button>
          <Button kind="tertiary" size="md" onClick={() => setAsOf(range[1])}>
            View as of its end
          </Button>
          <AppLink to={`/audit${query({ from: serverTime(range[0]), to: serverTime(range[1]) })}`}>
            Audit events in this range
          </AppLink>
          <Button kind="ghost" size="md" onClick={onClear}>Clear</Button>
        </div>
      )}
    </Tile>
  );
}

function TimelineView({ data }: { data: Timeline }) {
  const navigate = useNavigate();
  const { asOf } = useSession();
  const limits = useMemo<Domain>(() => [new Date(data.from), new Date(data.to)],
    [data.from, data.to]);
  const now = useMemo(() => new Date(data.now), [data.now]);
  const [domain, setDomainRaw] = useState<Domain>(() => initialDomain(now, limits[0], limits[1]));
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set());
  const [categories, setCategories] = useState<ReadonlySet<string>>(new Set());
  const [tiers, setTiers] = useState<ReadonlySet<string>>(new Set());
  const [selecting, setSelecting] = useState(false);
  const [range, setRange] = useState<Domain | null>(null);
  const [cursor, setCursor] = useState<Date | null>(asOf);
  const [hover, setHover] = useState<string | null>(null);
  const [picked, setPicked] = useState<TimelineMarker | null>(null);
  const setDomain = (next: Domain) =>
    setDomainRaw(clampDomain(next, limits[0], limits[1], MIN_SPAN_MS));
  const health = useVisibleHealth(domain, data.now) ?? data.health;
  const shown = useMemo(() => filterMarkers(data.markers, { categories, tiers, range: null }),
    [data.markers, categories, tiers]);
  const listed = useMemo(() => filterMarkers(shown, { categories, tiers, range }).toReversed(),
    [shown, categories, tiers, range]);
  const idle = picked === null
    ? "Hover a marker for its details. Click a marker to open its work item."
    : `${formatTime(picked.ts)} · ${picked.summary} · by ${picked.actor} (no work item)`;

  return (
    <div className="app-stack">
      <Toolbar domain={domain} limits={limits} now={now} setDomain={setDomain}
        selecting={selecting} setSelecting={setSelecting} setCategories={setCategories}
        setTiers={setTiers} />
      <div>
        <p className="tl-hover" aria-live="polite">{hover ?? idle}</p>
        <TimelineChart data={data} markers={shown} health={health} expanded={expanded}
          domain={domain} onDomain={setDomain} selecting={selecting} range={range}
          onRange={setRange} cursor={cursor} onCursor={setCursor} onHover={setHover}
          onToggleLane={(env) => setExpanded((current) => {
            const next = new Set(current);
            if (!next.delete(env)) {
              next.add(env);
            }
            return next;
          })}
          onOpenCluster={(items) => {
            const step = openCluster(items, domain);
            if (step.kind === "open") {
              navigate(`/work/${step.workItem}`);
            } else if (step.kind === "zoom") {
              setDomain(step.domain);
            } else {
              setRange(step.range);
            }
          }}
          onOpenMarker={(marker) => {
            if (marker.work_item === "") {
              setPicked(marker);
            } else {
              navigate(`/work/${marker.work_item}`);
            }
          }}
          onOpenFuture={(item) => navigate(item.work_item ? `/work/${item.work_item}`
            : "/upcoming")} />
        <TimelineLegend />
      </div>
      <Selection range={range} cursor={cursor} count={listed.length}
        onZoom={() => range !== null && setDomain(range)} onClear={() => setRange(null)} />
      <SimpleTable title="Events on the timeline"
        description={range === null ? "Every marker that passes the filters, newest first."
          : "The markers inside the selected range, newest first."}
        rows={listed} rowId={(row) => String(row.seq)} columns={COLUMNS} size="sm"
        empty="No event matches the filters." />
    </div>
  );
}

/** The timeline: what happened, when, across every environment, and what is coming next. */
export function TimelinePage() {
  const timeline = useApi<Timeline>("/timeline", { live: anyEvent });
  return (
    <>
      <PageHeader title="Timeline">
        The past is on the left of the now line and scheduled or forecast work is on the right.
        Scroll to zoom, drag to pan, and expand an environment to see its layers.
      </PageHeader>
      <Loaded state={timeline} what="the timeline">
        {(data) => <TimelineView data={data} />}
      </Loaded>
    </>
  );
}
