import { useId, useMemo, useRef } from "react";
import type { KeyboardEvent, MouseEvent } from "react";
import { scaleUtc } from "d3";
import type { FutureItem, HealthBucket, Timeline, TimelineMarker } from "@/api/types";
import { useAxis, useBrush, useScrubDrag, useWidth, useZoom }
  from "@/components/timeline/hooks";
import { FutureMarks, HealthBand, LaneLabels, Markers, RowBands }
  from "@/components/timeline/TimelineParts";
import type { XScale } from "@/components/timeline/TimelineParts";
import { buildLayout, maxZoom } from "@/lib/timelineLayout";
import { formatTime } from "@/lib/time";

const GUTTER = 168;
const RIGHT = 16;
const TOP = 64;
export const MIN_SPAN_MS = 60_000;

type Domain = [Date, Date];

export interface TimelineChartProps {
  data: Timeline;
  markers: TimelineMarker[];
  health: Record<string, HealthBucket[]>;
  expanded: ReadonlySet<string>;
  onToggleLane: (env: string) => void;
  domain: Domain;
  onDomain: (domain: Domain) => void;
  /** In select mode a drag brushes a range. Otherwise a drag pans. */
  selecting: boolean;
  range: Domain | null;
  onRange: (range: Domain | null) => void;
  /** The scrubber position. `null` puts the scrubber on "now". */
  cursor: Date | null;
  onCursor: (time: Date) => void;
  onOpenMarker: (marker: TimelineMarker) => void;
  /** A click on a cluster of markers that are too close to draw apart. */
  onOpenCluster: (markers: TimelineMarker[]) => void;
  onOpenFuture: (item: FutureItem) => void;
  onHover: (text: string | null) => void;
}

function Scrubber({ x, time, height, svg, onCursor, limits }: {
  x: XScale;
  time: Date;
  height: number;
  svg: React.RefObject<SVGSVGElement | null>;
  onCursor: (time: Date) => void;
  limits: Domain;
}) {
  const handle = useRef<SVGGElement>(null);
  const clamp = (value: Date) =>
    new Date(Math.min(Math.max(value.getTime(), limits[0].getTime()), limits[1].getTime()));
  useScrubDrag(handle, svg, (px) => onCursor(clamp(x.invert(px))));
  const [start, end] = x.domain() as Domain;
  const step = (end.getTime() - start.getTime()) / 100;
  const onKeyDown = (event: KeyboardEvent) => {
    const direction = event.key === "ArrowLeft" ? -1 : event.key === "ArrowRight" ? 1 : 0;
    if (direction !== 0) {
      event.preventDefault();
      onCursor(clamp(new Date(time.getTime() + direction * step * (event.shiftKey ? 10 : 1))));
    }
  };
  return (
    <g className="tl-scrub" transform={`translate(${x(time)} 0)`}>
      <line y1={-36} y2={height} />
      <g ref={handle} className="tl-scrub-handle" role="slider" tabIndex={0}
        aria-label="As-of scrubber. Arrow keys move it." aria-orientation="horizontal"
        aria-valuemin={limits[0].getTime()} aria-valuemax={limits[1].getTime()}
        aria-valuenow={time.getTime()} aria-valuetext={formatTime(time)} onKeyDown={onKeyDown}>
        <path d="M-8 -60 H8 V-44 L0 -36 L-8 -44 Z" />
      </g>
    </g>
  );
}

function Backdrop({ x, now, height, range, clip }: {
  x: XScale;
  now: Date;
  height: number;
  range: Domain | null;
  clip: string;
}) {
  const [left, right] = x.range() as [number, number];
  const nowX = Math.min(Math.max(x(now), left), right);
  return (
    <g clipPath={`url(#${clip})`}>
      <rect className="tl-future-zone" x={nowX} y={0} width={right - nowX} height={height} />
      {range !== null && (
        <rect className="tl-range" x={x(range[0])} y={0}
          width={Math.max(x(range[1]) - x(range[0]), 1)} height={height} />
      )}
      <g className="tl-now" transform={`translate(${x(now)} 0)`}>
        <line y1={-4} y2={height} />
        <text y={-24} textAnchor="middle">Now</text>
      </g>
      {x(now) < right - 60 && (
        <text className="tl-future-label" x={Math.max(x(now), left) + 8} y={height - 6}>
          Future: scheduled and forecast
        </text>
      )}
    </g>
  );
}

/** The swimlane timeline: custom D3 on Carbon tokens. See PLAN 7.4. */
export function TimelineChart(props: TimelineChartProps) {
  const { data, domain, expanded } = props;
  const wrap = useRef<HTMLDivElement>(null);
  const svg = useRef<SVGSVGElement>(null);
  const axis = useRef<SVGGElement>(null);
  const brush = useRef<SVGGElement>(null);
  const clip = useId();
  const width = useWidth(wrap);
  const layout = useMemo(() => buildLayout(data.lanes, expanded), [data.lanes, expanded]);
  const right = Math.max(width - RIGHT, GUTTER + 10);
  const from = useMemo(() => new Date(data.from), [data.from]);
  const to = useMemo(() => new Date(data.to), [data.to]);
  const now = useMemo(() => new Date(data.now), [data.now]);
  const base = useMemo(() => scaleUtc().domain([from, to]).range([GUTTER, right]),
    [from, to, right]);
  const start = domain[0].getTime();
  const end = domain[1].getTime();
  const x = useMemo(() => scaleUtc().domain([new Date(start), new Date(end)])
    .range([GUTTER, right]), [start, end, right]);
  const height = layout.height;

  useZoom({ svg, base, domain, height, maxScale: maxZoom(from, to, MIN_SPAN_MS),
    dragPans: !props.selecting, onDomain: props.onDomain });
  useBrush({ group: brush, enabled: props.selecting, x, top: 0, height, range: props.range,
    onRange: props.onRange });
  useAxis(axis, x, height);

  const onAxisClick = (event: MouseEvent<SVGRectElement>) => {
    const box = svg.current?.getBoundingClientRect();
    if (box !== undefined) {
      const time = x.invert(event.clientX - box.left);
      props.onCursor(time > now ? now : time);
    }
  };

  return (
    <div ref={wrap} className={`tl-wrap${props.selecting ? " tl-selecting" : ""}`}>
      <svg ref={svg} className="tl-svg" width={width} height={height + TOP + 8} role="group"
        aria-label={`Timeline from ${formatTime(domain[0])} to ${formatTime(domain[1])}. ` +
          "The table below lists the same events."}>
        <defs>
          <clipPath id={clip}>
            <rect x={GUTTER} y={-TOP} width={Math.max(right - GUTTER, 0)}
              height={height + TOP + 8} />
          </clipPath>
        </defs>
        <g transform={`translate(0 ${TOP})`}>
          <RowBands rows={layout.rows} x0={GUTTER} x1={right} />
          <g ref={axis} className="tl-axis" clipPath={`url(#${clip})`} />
          <rect className="tl-axis-band" x={GUTTER} y={-TOP} width={Math.max(right - GUTTER, 0)}
            height={TOP - 4} onClick={onAxisClick}>
            <title>Click to move the as-of scrubber here</title>
          </rect>
          <Backdrop x={x} now={now} height={height} range={props.range} clip={clip} />
          <g clipPath={`url(#${clip})`}>
            {layout.strips.map((strip) => (
              <HealthBand key={strip.env} buckets={props.health[strip.env] ?? []} x={x}
                y={strip.y} onHover={props.onHover} />
            ))}
            <g ref={brush} className="tl-brush" />
            <Markers markers={props.markers} layout={layout} expanded={expanded} x={x}
              onOpen={props.onOpenMarker} onHover={props.onHover}
              onZoomTo={props.onOpenCluster} />
            <FutureMarks items={data.future} layout={layout} x={x} onOpen={props.onOpenFuture}
              onHover={props.onHover} />
            <Scrubber x={x} time={props.cursor ?? now} height={height} svg={svg}
              onCursor={props.onCursor} limits={[from, now]} />
          </g>
          <LaneLabels layout={layout} lanes={data.lanes} expanded={expanded} gutter={GUTTER}
            onToggle={props.onToggleLane} />
        </g>
      </svg>
    </div>
  );
}
