import type { TimelineLane, TimelineMarker } from "@/api/types";

export const GLOBAL_ROW = "*";
export const NO_TIER = "none";
export const ROW_HEIGHT = 36;
export const HEALTH_HEIGHT = 22;
export const LANE_GAP = 12;
const DAY_MS = 86_400_000;

export interface Row {
  key: string;
  env: string;
  /** `null` for the row of a whole environment, or of events with no environment. */
  layer: string | null;
  label: string;
  y: number;
}

export interface HealthStrip {
  env: string;
  y: number;
}

export interface Layout {
  rows: Row[];
  strips: HealthStrip[];
  height: number;
}

/**
 * Lays out the swimlanes from top to bottom: one row for events with no environment, then for
 * each environment its row, its layer rows when it is expanded, and its health strip.
 */
export function buildLayout(lanes: TimelineLane[], expanded: ReadonlySet<string>): Layout {
  const rows: Row[] = [{ key: GLOBAL_ROW, env: "", layer: null, label: "All environments", y: 0 }];
  const strips: HealthStrip[] = [];
  let y = ROW_HEIGHT + LANE_GAP;
  for (const lane of lanes) {
    rows.push({ key: lane.env, env: lane.env, layer: null, label: lane.env, y });
    y += ROW_HEIGHT;
    if (expanded.has(lane.env)) {
      for (const layer of lane.layers) {
        rows.push({ key: `${lane.env}/${layer}`, env: lane.env, layer, label: layer, y });
        y += ROW_HEIGHT;
      }
    }
    strips.push({ env: lane.env, y });
    y += HEALTH_HEIGHT + LANE_GAP;
  }
  return { rows, strips, height: y };
}

/** Names the row that shows an event of this environment and layer. */
export function rowKey(env: string, layer: string, expanded: ReadonlySet<string>): string {
  if (env === "") {
    return GLOBAL_ROW;
  }
  return expanded.has(env) && layer !== "" ? `${env}/${layer}` : env;
}

export interface MarkerFilter {
  /** Empty means every category. */
  categories: ReadonlySet<string>;
  /** Empty means every tier. `NO_TIER` selects the markers that have no tier. */
  tiers: ReadonlySet<string>;
  range: [Date, Date] | null;
}

export function filterMarkers(markers: TimelineMarker[], filter: MarkerFilter): TimelineMarker[] {
  return markers.filter((marker) => {
    if (filter.categories.size > 0 && !filter.categories.has(marker.category)) {
      return false;
    }
    const tier = marker.tier === null || marker.tier === "" ? NO_TIER : marker.tier;
    if (filter.tiers.size > 0 && !filter.tiers.has(tier)) {
      return false;
    }
    if (filter.range !== null) {
      const time = new Date(marker.ts).getTime();
      return time >= filter.range[0].getTime() && time <= filter.range[1].getTime();
    }
    return true;
  });
}

export interface Placed<T> {
  item: T;
  row: string;
  x: number;
}

export interface Cluster<T> {
  row: string;
  x: number;
  items: T[];
}

/**
 * Groups the markers of a row that would overlap. A marker joins the open cluster when it is
 * closer than `minGap` pixels to the first marker of that cluster, so a cluster never grows
 * wider than `minGap`.
 */
export function clusterMarkers<T>(placed: Placed<T>[], minGap: number): Cluster<T>[] {
  const clusters: Cluster<T>[] = [];
  const open = new Map<string, { first: number; cluster: Cluster<T> }>();
  for (const marker of placed.toSorted((a, b) => a.x - b.x)) {
    const current = open.get(marker.row);
    if (current !== undefined && marker.x - current.first < minGap) {
      current.cluster.items.push(marker.item);
      current.cluster.x = (current.first + marker.x) / 2;
      continue;
    }
    const cluster: Cluster<T> = { row: marker.row, x: marker.x, items: [marker.item] };
    clusters.push(cluster);
    open.set(marker.row, { first: marker.x, cluster });
  }
  return clusters;
}

/** The largest zoom factor: the whole data range shrinks to a view of `minSpanMs`. */
export function maxZoom(from: Date, to: Date, minSpanMs: number): number {
  return Math.max((to.getTime() - from.getTime()) / minSpanMs, 1);
}

/** Linear time-to-pixel mapping, for the code that has no D3 scale at hand. */
export function timeToX(time: Date, domain: [Date, Date], range: [number, number]): number {
  const span = domain[1].getTime() - domain[0].getTime();
  const ratio = span === 0 ? 0 : (time.getTime() - domain[0].getTime()) / span;
  return range[0] + ratio * (range[1] - range[0]);
}

/** The first view of the timeline: the last 30 days and the next 14, inside the data range. */
export function initialDomain(now: Date, from: Date, to: Date): [Date, Date] {
  const start = Math.max(now.getTime() - 30 * DAY_MS, from.getTime());
  const end = Math.min(now.getTime() + 14 * DAY_MS, to.getTime());
  return [new Date(start), new Date(end)];
}

/** Classifies the attainment of a health bucket against the service level objective. */
export function attainmentLevel(attainment: number): "good" | "warn" | "bad" {
  if (attainment >= 0.995) {
    return "good";
  }
  return attainment >= 0.95 ? "warn" : "bad";
}

/** Keeps a view inside the data range and no narrower than `minSpanMs`. */
export function clampDomain(
  domain: [Date, Date],
  from: Date,
  to: Date,
  minSpanMs: number,
): [Date, Date] {
  const full = to.getTime() - from.getTime();
  const span = Math.min(Math.max(domain[1].getTime() - domain[0].getTime(), minSpanMs), full);
  const start = Math.min(Math.max(domain[0].getTime(), from.getTime()), to.getTime() - span);
  return [new Date(start), new Date(start + span)];
}

/** Zooms a view around its centre. A factor above 1 zooms in. */
export function zoomDomain(domain: [Date, Date], factor: number): [Date, Date] {
  const centre = (domain[0].getTime() + domain[1].getTime()) / 2;
  const half = (domain[1].getTime() - domain[0].getTime()) / factor / 2;
  return [new Date(centre - half), new Date(centre + half)];
}

/** Moves a view by a fraction of its span. A positive fraction moves to later times. */
export function panDomain(domain: [Date, Date], fraction: number): [Date, Date] {
  const shift = (domain[1].getTime() - domain[0].getTime()) * fraction;
  return [new Date(domain[0].getTime() + shift), new Date(domain[1].getTime() + shift)];
}
