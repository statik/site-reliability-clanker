import type { KeyboardEvent } from "react";
import type { ScaleTime } from "d3";
import type { FutureItem, HealthBucket, TimelineLane, TimelineMarker } from "@/api/types";
import { ClusterGlyph, FutureGlyph, MarkerGlyph } from "@/components/timeline/glyphs";
import { attainmentLevel, clusterMarkers, HEALTH_HEIGHT, ROW_HEIGHT, rowKey }
  from "@/lib/timelineLayout";
import type { Cluster, Layout, Row } from "@/lib/timelineLayout";
import { formatTime } from "@/lib/time";

export type XScale = ScaleTime<number, number>;
const CLUSTER_GAP = 16;
const BAD_OUTCOMES = ["Failed", "RolledBack"];

function onActivate(action: () => void) {
  return (event: KeyboardEvent) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      action();
    }
  };
}

/** The row backgrounds and, in the left gutter, the lane labels with their expand buttons. */
export function LaneLabels({ layout, lanes, expanded, gutter, onToggle }: {
  layout: Layout;
  lanes: TimelineLane[];
  expanded: ReadonlySet<string>;
  gutter: number;
  onToggle: (env: string) => void;
}) {
  const envs = new Set(lanes.map((lane) => lane.env));
  return (
    <g>
      <rect className="tl-gutter" x={0} y={-40} width={gutter} height={layout.height + 40} />
      {layout.rows.map((row) => {
        const isEnv = row.layer === null && envs.has(row.env);
        const open = expanded.has(row.env);
        const text = (
          <text className={row.layer === null ? "tl-lane-label" : "tl-layer-label"}
            x={row.layer === null ? 28 : 40} y={row.y + ROW_HEIGHT / 2} dy="0.35em">
            {row.label}
          </text>
        );
        if (!isEnv) {
          return <g key={row.key}>{text}</g>;
        }
        return (
          <g key={row.key} className="tl-lane-toggle" role="button" tabIndex={0}
            aria-expanded={open}
            aria-label={`${open ? "Collapse" : "Expand"} the layers of ${row.env}`}
            onClick={() => onToggle(row.env)} onKeyDown={onActivate(() => onToggle(row.env))}>
            <rect x={4} y={row.y + 4} width={gutter - 8} height={ROW_HEIGHT - 8} />
            <path className="tl-chevron" d={open ? "M10 -3 L16 3 L22 -3" : "M13 -6 L19 0 L13 6"}
              transform={`translate(0 ${row.y + ROW_HEIGHT / 2})`} />
            {text}
          </g>
        );
      })}
      {layout.strips.map((strip) => (
        <text key={strip.env} className="tl-layer-label" x={40} y={strip.y + HEALTH_HEIGHT / 2}
          dy="0.35em">
          SLO attainment
        </text>
      ))}
    </g>
  );
}

export function RowBands({ rows, x0, x1 }: { rows: Row[]; x0: number; x1: number }) {
  return (
    <g>
      {rows.map((row) => (
        <rect key={row.key} className={row.layer === null ? "tl-row" : "tl-row tl-row-layer"}
          x={x0} y={row.y + 2} width={Math.max(x1 - x0, 0)} height={ROW_HEIGHT - 4} />
      ))}
    </g>
  );
}

/** One health strip: a baseline with a spike wherever attainment dropped. */
export function HealthBand({ buckets, x, y, onHover }: {
  buckets: HealthBucket[];
  x: XScale;
  y: number;
  onHover: (text: string | null) => void;
}) {
  return (
    <g className="tl-health" onMouseLeave={() => onHover(null)}>
      {buckets.map((bucket) => {
        const left = x(new Date(bucket.ts));
        const width = Math.max(x(new Date(bucket.end)) - left, 1);
        const level = attainmentLevel(bucket.attainment);
        const spike = Math.min(Math.sqrt(1 - bucket.attainment) * 2, 1) * (HEALTH_HEIGHT - 6);
        const height = 4 + (level === "good" ? 0 : Math.max(spike, 4));
        const percent = (bucket.attainment * 100).toFixed(2);
        return (
          <rect key={bucket.ts} className={`tl-health-${level}`} x={left} width={width}
            y={y + HEALTH_HEIGHT - 2 - height} height={height}
            onMouseEnter={() => onHover(`SLO attainment ${percent}% over ${bucket.requests} ` +
              `requests, ${formatTime(bucket.ts)} to ${formatTime(bucket.end)}`)} />
        );
      })}
    </g>
  );
}

function clusterText(cluster: Cluster<TimelineMarker>): string {
  const first = cluster.items[0];
  const last = cluster.items.at(-1);
  if (first === undefined || last === undefined) {
    return "";
  }
  if (cluster.items.length === 1) {
    return `${formatTime(first.ts)} · ${first.summary} · by ${first.actor}`;
  }
  return `${cluster.items.length} events from ${formatTime(first.ts)} to ` +
    `${formatTime(last.ts)}. Click to zoom in. First: ${first.summary}`;
}

/** The past markers. Markers that would overlap are drawn as one counted cluster. */
export function Markers({ markers, layout, expanded, x, onOpen, onZoomTo, onHover }: {
  markers: TimelineMarker[];
  layout: Layout;
  expanded: ReadonlySet<string>;
  x: XScale;
  onOpen: (marker: TimelineMarker) => void;
  onZoomTo: (items: TimelineMarker[]) => void;
  onHover: (text: string | null) => void;
}) {
  const rowY = new Map(layout.rows.map((row) => [row.key, row.y + ROW_HEIGHT / 2]));
  const [min, max] = x.range() as [number, number];
  const placed = markers
    .map((item) => ({
      item,
      row: rowKey(item.env, item.layer, expanded),
      x: x(new Date(item.ts)),
    }))
    .filter((marker) => marker.x >= min - 20 && marker.x <= max + 20);
  return (
    <g onMouseLeave={() => onHover(null)}>
      {clusterMarkers(placed, CLUSTER_GAP).map((cluster) => {
        const first = cluster.items[0];
        const y = rowY.get(cluster.row);
        if (first === undefined || y === undefined) {
          return null;
        }
        const single = cluster.items.length === 1;
        const text = clusterText(cluster);
        return (
          <g key={`${cluster.row}/${first.seq}`} className="tl-marker"
            transform={`translate(${cluster.x} ${y})`}
            onClick={() => (single ? onOpen(first) : onZoomTo(cluster.items))}
            onMouseEnter={() => onHover(text)}>
            <title>{text}</title>
            <circle className="tl-hit" r={12} />
            {single
              ? <MarkerGlyph category={first.category} outcome={first.outcome} />
              : <ClusterGlyph count={cluster.items.length}
                bad={cluster.items.some((item) => BAD_OUTCOMES.includes(item.outcome))} />}
          </g>
        );
      })}
    </g>
  );
}

/** The future items, to the right of the now line. */
export function FutureMarks({ items, layout, x, onOpen, onHover }: {
  items: FutureItem[];
  layout: Layout;
  x: XScale;
  onOpen: (item: FutureItem) => void;
  onHover: (text: string | null) => void;
}) {
  const rowY = new Map(layout.rows.map((row) => [row.key, row.y + ROW_HEIGHT / 2]));
  return (
    <g onMouseLeave={() => onHover(null)}>
      {items.map((item) => {
        const y = rowY.get(item.env === "" ? "*" : item.env);
        if (y === undefined) {
          return null;
        }
        const text = `${formatTime(item.ts)} · ${item.kind}: ${item.title}`;
        return (
          <g key={`${item.kind}/${item.env}/${item.ts}/${item.title}`} className="tl-marker"
            transform={`translate(${x(new Date(item.ts))} ${y})`}
            onClick={() => onOpen(item)} onMouseEnter={() => onHover(text)}>
            <title>{text}</title>
            <circle className="tl-hit" r={12} />
            <FutureGlyph kind={item.kind} />
          </g>
        );
      })}
    </g>
  );
}
