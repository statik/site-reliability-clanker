import type { ReactNode } from "react";
import { CATEGORY_SHAPE, ClusterGlyph, FUTURE_LABEL, FutureGlyph, MarkerGlyph, OUTCOME_LABEL }
  from "@/components/timeline/glyphs";

function Item({ label, children }: { label: string; children: ReactNode }) {
  return (
    <span className="tl-legend-item">
      <svg width={26} height={22} viewBox="-13 -11 26 22" aria-hidden="true">{children}</svg>
      {label}
    </span>
  );
}

/** Explains the marker shapes (category), fills (outcome) and the future and health styles. */
export function TimelineLegend() {
  return (
    <div className="tl-legend" role="group" aria-label="Timeline legend">
      {Object.entries(CATEGORY_SHAPE).map(([category, shape]) => (
        <Item key={category} label={shape.label}>
          <MarkerGlyph category={category} outcome="" />
        </Item>
      ))}
      {Object.entries(OUTCOME_LABEL).map(([outcome, label]) => (
        <Item key={outcome} label={label}>
          <MarkerGlyph category="change" outcome={outcome} />
        </Item>
      ))}
      {Object.entries(FUTURE_LABEL).map(([kind, label]) => (
        <Item key={kind} label={`${label} (future: hollow, dashed)`}>
          <FutureGlyph kind={kind} />
        </Item>
      ))}
      <Item label="Several events close together"><ClusterGlyph count={5} bad={false} /></Item>
      <Item label="SLO attainment: flat is met, a spike is a drop">
        <rect className="tl-health-good" x={-12} y={4} width={8} height={4} />
        <rect className="tl-health-warn" x={-4} y={-2} width={8} height={10} />
        <rect className="tl-health-bad" x={4} y={-9} width={8} height={17} />
      </Item>
    </div>
  );
}
