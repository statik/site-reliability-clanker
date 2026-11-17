import {
  symbol,
  symbolCircle,
  symbolCross,
  symbolDiamond,
  symbolSquare,
  symbolStar,
  symbolTriangle,
  symbolWye,
} from "d3";
import type { SymbolType } from "d3";

interface Shape {
  type: SymbolType;
  rotate: number;
  label: string;
}

export const CATEGORY_SHAPE: Record<string, Shape> = {
  change: { type: symbolCircle, rotate: 0, label: "Change finished" },
  apply: { type: symbolSquare, rotate: 0, label: "Apply of a layer" },
  approval: { type: symbolDiamond, rotate: 0, label: "Approval" },
  incident: { type: symbolTriangle, rotate: 0, label: "Incident" },
  finding: { type: symbolCross, rotate: 45, label: "Detector finding" },
  grant: { type: symbolWye, rotate: 0, label: "Grant issued" },
  runbook: { type: symbolStar, rotate: 0, label: "Runbook, backup or drill" },
  verify: { type: symbolCross, rotate: 0, label: "Verification" },
  external: { type: symbolTriangle, rotate: 180, label: "Outside input" },
};

export const OUTCOME_LABEL: Record<string, string> = {
  Done: "Done: filled, green",
  Failed: "Failed: red with a ring",
  RolledBack: "Rolled back: hollow, orange",
  Rejected: "Rejected: hollow, dashed",
};

export const FUTURE_LABEL: Record<string, string> = {
  scheduled: "Scheduled work",
  forecast: "Forecast",
  window: "Maintenance window",
};

const FUTURE_SHAPE: Record<string, Shape> = {
  scheduled: { type: symbolCircle, rotate: 0, label: "Scheduled work" },
  forecast: { type: symbolDiamond, rotate: 0, label: "Forecast" },
  window: { type: symbolSquare, rotate: 0, label: "Maintenance window" },
};

const FALLBACK: Shape = { type: symbolCircle, rotate: 0, label: "Event" };

function shapePath(shape: Shape, area: number): string {
  return symbol(shape.type, area)() ?? "";
}

/** A past marker. The shape shows the category and the fill style shows the outcome. */
export function MarkerGlyph({ category, outcome, area = 110 }: {
  category: string;
  outcome: string;
  area?: number;
}) {
  const shape = CATEGORY_SHAPE[category] ?? FALLBACK;
  const outcomeClass = outcome === "" ? "tl-outcome-none" : `tl-outcome-${outcome}`;
  return (
    <g className={`tl-glyph ${outcomeClass}`}>
      {outcome === "Failed" && <circle className="tl-ring" r={Math.sqrt(area) * 0.95} />}
      <path d={shapePath(shape, area)} transform={`rotate(${shape.rotate})`} />
    </g>
  );
}

/** A future item: hollow and dashed, so that it cannot be read as something that happened. */
export function FutureGlyph({ kind, area = 130 }: { kind: string; area?: number }) {
  const shape = FUTURE_SHAPE[kind] ?? FALLBACK;
  return (
    <g className={`tl-glyph tl-future tl-future-${kind}`}>
      <path d={shapePath(shape, area)} transform={`rotate(${shape.rotate})`} />
    </g>
  );
}

/** A cluster of markers that are too close to draw apart. */
export function ClusterGlyph({ count, bad }: { count: number; bad: boolean }) {
  return (
    <g className={`tl-cluster${bad ? " tl-cluster-bad" : ""}`}>
      <rect x={-11} y={-10} width={22} height={20} rx={10} />
      <text dy="0.35em" textAnchor="middle">{count > 99 ? "99+" : count}</text>
    </g>
  );
}
