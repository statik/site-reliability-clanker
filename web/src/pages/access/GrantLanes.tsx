import { useRef, useState } from "react";
import { ContentSwitcher, Switch } from "@carbon/react";
import type { Grant } from "@/api/types";
import { useWidth } from "@/components/timeline/hooks";
import { timeToX } from "@/lib/timelineLayout";
import { formatDuration, formatTime } from "@/lib/time";

const DAY_MS = 86_400_000;
const WINDOWS = [
  { name: "45d", text: "45 days", ms: 45 * DAY_MS },
  { name: "7d", text: "7 days", ms: 7 * DAY_MS },
  { name: "24h", text: "24 hours", ms: DAY_MS },
];
const GUTTER = 168;
const ROW = 28;
const TOP = 28;
const TICKS = 6;

function grantEnd(grant: Grant, now: Date): Date {
  if (grant.ended_at !== "") {
    return new Date(grant.ended_at);
  }
  const expires = new Date(grant.expires);
  return expires < now ? expires : now;
}

/**
 * Access over time: one lane for each identity and one bar for each grant. Write access shows
 * as short bars, because a grant lives only while its job runs.
 */
export function GrantLanes({ grants, identities, now }: {
  grants: Grant[];
  identities: string[];
  now: Date;
}) {
  const wrap = useRef<HTMLDivElement>(null);
  const width = useWidth(wrap);
  const [windowIndex, setWindowIndex] = useState(0);
  const span = WINDOWS[windowIndex]?.ms ?? 45 * DAY_MS;
  const domain: [Date, Date] = [new Date(now.getTime() - span), now];
  const range: [number, number] = [GUTTER, Math.max(width - 16, GUTTER + 10)];
  const lanes = [...new Set([...identities, ...grants.map((grant) => grant.identity)])];
  const height = TOP + lanes.length * ROW + 8;
  const ticks = Array.from({ length: TICKS }, (_, index) =>
    new Date(domain[0].getTime() + (span * index) / (TICKS - 1)));
  return (
    <div className="app-stack-tight">
      <ContentSwitcher size="sm" selectedIndex={windowIndex}
        onChange={({ index }) => setWindowIndex(index ?? 0)}>
        {WINDOWS.map((option) => (
          <Switch key={option.name} name={option.name} text={option.text} />
        ))}
      </ContentSwitcher>
      <div ref={wrap} className="tl-wrap">
        <svg width={width} height={height} role="img"
          aria-label={`Grants per identity over the last ${WINDOWS[windowIndex]?.text ?? ""}. ` +
            "The grant history table lists the same grants."}>
          <g className="tl-axis">
            {ticks.map((tick, index) => {
              const x = timeToX(tick, domain, range);
              const anchor = index === 0 ? "start" : index === TICKS - 1 ? "end" : "middle";
              return (
                <g key={tick.getTime()} className="tick">
                  <line x1={x} x2={x} y1={TOP - 6} y2={height} />
                  <text x={x} y={14} textAnchor={anchor}>{formatTime(tick).slice(0, -4)}</text>
                </g>
              );
            })}
          </g>
          {lanes.map((identity, index) => {
            const y = TOP + index * ROW;
            const bars = grants.filter((grant) => grant.identity === identity &&
              grantEnd(grant, now) >= domain[0]);
            return (
              <g key={identity}>
                <rect className="tl-row" x={GUTTER} y={y + 2} width={range[1] - GUTTER}
                  height={ROW - 4} />
                <text className="tl-layer-label" x={8} y={y + ROW / 2} dy="0.35em">
                  {identity} ({bars.length})
                </text>
                {bars.map((grant) => {
                  const start = new Date(grant.issued_at);
                  const end = grantEnd(grant, now);
                  const x = Math.max(timeToX(start, domain, range), GUTTER);
                  const barWidth = Math.max(timeToX(end, domain, range) - x, 3);
                  return (
                    <rect key={grant.id} x={x} y={y + 6} width={barWidth} height={ROW - 12}
                      className={grant.kind === "debug" ? "tl-access-bar-debug" : "tl-access-bar"}>
                      <title>
                        {`${grant.id}: ${grant.kind} ${grant.layer} for ${grant.work_item}, ` +
                          `${formatTime(grant.issued_at)}, held ` +
                          `${formatDuration(end.getTime() - start.getTime())}, ${grant.status}`}
                      </title>
                    </rect>
                  );
                })}
              </g>
            );
          })}
        </svg>
      </div>
      <p className="app-label">
        Each bar is one grant, from issue to revocation. A bar is at least 3 pixels wide so
        that a grant of a few seconds stays visible. Hover a bar for the grant.
      </p>
    </div>
  );
}
