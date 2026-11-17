import { useEffect, useRef, useState } from "react";
import type { RefObject } from "react";
import { axisTop, brushX, drag, select, zoom, zoomIdentity, zoomTransform } from "d3";
import type { BrushBehavior, D3BrushEvent, D3ZoomEvent, ScaleTime, ZoomBehavior } from "d3";

type XScale = ScaleTime<number, number>;
type Domain = [Date, Date];

/** Follows the width of an element. */
export function useWidth(ref: RefObject<HTMLElement | null>): number {
  const [width, setWidth] = useState(0);
  useEffect(() => {
    const element = ref.current;
    if (element === null) {
      return;
    }
    const observer = new ResizeObserver(() => setWidth(element.clientWidth));
    observer.observe(element);
    setWidth(element.clientWidth);
    return () => observer.disconnect();
  }, [ref]);
  return width;
}

/** Keeps the latest value in a ref, for D3 handlers that are bound once. */
function useLatest<T>(value: T): RefObject<T> {
  const ref = useRef(value);
  useEffect(() => {
    ref.current = value;
  });
  return ref;
}

interface ZoomOptions {
  svg: RefObject<SVGSVGElement | null>;
  /** The scale of the whole data range. */
  base: XScale;
  domain: Domain;
  height: number;
  maxScale: number;
  /** Dragging pans only when this is true. The wheel always zooms. */
  dragPans: boolean;
  onDomain: (domain: Domain) => void;
}

/**
 * Binds d3-zoom to the chart. The visible domain is React state: a wheel or drag gesture
 * reports a new domain, and a domain set from outside is copied back into the zoom transform.
 */
export function useZoom(options: ZoomOptions): void {
  const { svg, base, domain, height, maxScale } = options;
  const latest = useLatest(options);
  const behavior = useRef<ZoomBehavior<SVGSVGElement, unknown> | null>(null);

  useEffect(() => {
    const element = svg.current;
    if (element === null) {
      return;
    }
    const created = zoom<SVGSVGElement, unknown>()
      .filter((event: Event) => {
        if (event.type === "wheel") {
          return true;
        }
        return latest.current.dragPans && !(event instanceof MouseEvent && event.button !== 0);
      })
      .on("zoom", (event: D3ZoomEvent<SVGSVGElement, unknown>) => {
        if (event.sourceEvent === null) {
          return;
        }
        const [start, end] = event.transform.rescaleX(latest.current.base).domain();
        if (start !== undefined && end !== undefined) {
          latest.current.onDomain([start, end]);
        }
      });
    behavior.current = created;
    select(element).call(created).on("dblclick.zoom", null);
    return () => {
      select(element).on(".zoom", null);
    };
  }, [svg, latest]);

  const [left, right] = base.range() as [number, number];
  const start = domain[0].getTime();
  const end = domain[1].getTime();
  useEffect(() => {
    const element = svg.current;
    const current = behavior.current;
    if (element === null || current === null || right <= left) {
      return;
    }
    current.scaleExtent([1, maxScale]).extent([[left, 0], [right, height]])
      .translateExtent([[left, 0], [right, height]]);
    const scale = (right - left) / Math.max(base(new Date(end)) - base(new Date(start)), 1e-6);
    const target = zoomIdentity.translate(left - scale * base(new Date(start)), 0).scale(scale);
    const now = zoomTransform(element);
    if (Math.abs(now.k - target.k) > target.k * 1e-6 || Math.abs(now.x - target.x) > 0.5) {
      select(element).call(current.transform, target);
    }
  }, [svg, base, left, right, start, end, height, maxScale]);
}

interface BrushOptions {
  group: RefObject<SVGGElement | null>;
  enabled: boolean;
  x: XScale;
  top: number;
  height: number;
  range: Domain | null;
  onRange: (range: Domain | null) => void;
}

/** Binds d3-brush for the "select a range" mode. */
export function useBrush(options: BrushOptions): void {
  const { group, enabled, x, top, height, range } = options;
  const latest = useLatest(options);
  const behavior = useRef<BrushBehavior<unknown> | null>(null);
  const [left, right] = x.range() as [number, number];

  useEffect(() => {
    const element = group.current;
    if (element === null || !enabled || right <= left) {
      return;
    }
    const created = brushX().extent([[left, top], [right, height]])
      .on("end", (event: D3BrushEvent<unknown>) => {
        if (event.sourceEvent === undefined || event.sourceEvent === null) {
          return;
        }
        const selection = event.selection as [number, number] | null;
        const scale = latest.current.x;
        latest.current.onRange(selection === null
          ? null : [scale.invert(selection[0]), scale.invert(selection[1])]);
      });
    behavior.current = created;
    select(element).call(created);
    return () => {
      behavior.current = null;
      select(element).on(".brush", null).selectAll("*").remove();
    };
  }, [group, enabled, left, right, top, height, latest]);

  useEffect(() => {
    const element = group.current;
    if (element === null || behavior.current === null || !enabled) {
      return;
    }
    const selection = range === null ? null : [x(range[0]), x(range[1])] as [number, number];
    select(element).call(behavior.current.move, selection);
  }, [group, enabled, range, x]);
}

/** Draws the time axis with d3-axis. */
export function useAxis(group: RefObject<SVGGElement | null>, x: XScale, height: number): void {
  useEffect(() => {
    const element = group.current;
    if (element === null) {
      return;
    }
    const [left, right] = x.range() as [number, number];
    const ticks = Math.max(Math.floor((right - left) / 110), 2);
    select(element).call(axisTop(x).ticks(ticks).tickSize(-height).tickPadding(8));
  }, [group, x, height]);
}

/** Makes the scrubber handle draggable. It reports the horizontal position in SVG pixels. */
export function useScrubDrag(
  handle: RefObject<SVGGElement | null>,
  svg: RefObject<SVGSVGElement | null>,
  onMove: (x: number) => void,
): void {
  const latest = useLatest(onMove);
  useEffect(() => {
    const element = handle.current;
    const container = svg.current;
    if (element === null || container === null) {
      return;
    }
    const behavior = drag<SVGGElement, unknown>().container(container)
      .on("drag", (event: { x: number }) => latest.current(event.x));
    select(element).call(behavior);
    return () => {
      select(element).on(".drag", null);
    };
  }, [handle, svg, latest]);
}
