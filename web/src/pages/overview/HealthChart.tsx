import { useMemo } from "react";
import { LineChart, ScaleTypes } from "@carbon/charts-react";
import type { LineChartOptions } from "@carbon/charts-react";
import { ChartLine } from "@carbon/icons-react";
import type { Timeline } from "@/api/types";
import { query, serverTime } from "@/api/urls";
import { Empty, Loaded } from "@/components/common";
import { anyEvent, useApi } from "@/hooks/useApi";

const WINDOW_MS = 14 * 86_400_000;
const HOUR_MS = 3_600_000;
const MARKED_OUTCOMES = ["Done", "Failed", "RolledBack"];

interface Point {
  group: string;
  date: Date;
  value: number;
}

function Chart({ timeline }: { timeline: Timeline }) {
  const points = useMemo<Point[]>(() => Object.entries(timeline.health).flatMap(
    ([env, buckets]) => buckets.map((bucket) => ({
      group: env,
      date: new Date(bucket.ts),
      value: Math.round(bucket.attainment * 10_000) / 100,
    }))), [timeline.health]);
  const changes = timeline.markers.filter((marker) =>
    (marker.category === "change" && MARKED_OUTCOMES.includes(marker.outcome) &&
      marker.tier !== "T0" && marker.tier !== "T1") || marker.type === "incident.opened");
  if (points.length === 0) {
    return (
      <Empty icon={<ChartLine size={32} />}>
        No health sample exists in this period. Samples start when the application runs.
      </Empty>
    );
  }
  const options: LineChartOptions = {
    title: "Share of requests that succeed, by environment, with changes and incidents",
    axes: {
      bottom: {
        mapsTo: "date",
        scaleType: ScaleTypes.TIME,
        thresholds: changes.map((marker) => ({
          value: new Date(marker.ts),
          label: marker.summary,
        })),
      },
      left: { mapsTo: "value", title: "Success (%)", scaleType: ScaleTypes.LINEAR,
        includeZero: false },
    },
    height: "320px",
    theme: "g100",
    points: { radius: 2 },
    toolbar: { enabled: false },
  };
  return <LineChart data={points} options={options} />;
}

/** Success rate over time from the health buckets, with change markers as vertical lines. */
export function HealthChart({ now }: { now: Date }) {
  const hour = Math.floor(now.getTime() / HOUR_MS) * HOUR_MS;
  const path = `/timeline${query({
    from: serverTime(new Date(hour - WINDOW_MS)),
    to: serverTime(new Date(hour + HOUR_MS)),
    buckets: 84,
  })}`;
  const timeline = useApi<Timeline>(path, { live: anyEvent });
  return (
    <Loaded state={timeline} what="the health history">
      {(data) => <Chart timeline={data} />}
    </Loaded>
  );
}
