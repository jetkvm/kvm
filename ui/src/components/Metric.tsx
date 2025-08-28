/* eslint-disable react-refresh/only-export-components */
import { ComponentProps } from "react";
import { cva, cx } from "cva";

import { someIterable } from "../utils";

import { GridCard } from "./Card";
import MetricsChart from "./MetricsChart";

interface ChartPoint {
  date: number;
  metric: number | null;
}

interface MetricProps<T, K extends keyof T> {
  title: string;
  description: string;
  stream?: Map<number, T>;
  metric?: K;
  data?: ChartPoint[];
  gate?: Map<number, unknown>;
  supported?: boolean;
  map?: (p: { date: number; metric: number | null }) => ChartPoint;
  domain?: [number, number];
  unit: string;
  heightClassName?: string;
  referenceValue?: number;
  badge?: ComponentProps<typeof MetricHeader>["badge"];
  badgeTheme?: ComponentProps<typeof MetricHeader>["badgeTheme"];
}

/**
 * Creates a chart array from a metrics map and a metric name.
 *
 * @param metrics - Expected to be ordered from oldest to newest.
 * @param metricName - Name of the metric to create a chart array for.
 */
export function createChartArray<T, K extends keyof T>(
  metrics: Map<number, T>,
  metricName: K,
) {
  const result: { date: number; metric: number | null }[] = [];
  const iter = metrics.entries();
  let next = iter.next() as IteratorResult<[number, T]>;
  const nowSeconds = Math.floor(Date.now() / 1000);

  // We want 120 data points, in the chart.
  const firstDate = Math.min(next.value?.[0] ?? nowSeconds, nowSeconds - 120);

  for (let t = firstDate; t < nowSeconds; t++) {
    while (!next.done && next.value[0] < t) next = iter.next();
    const has = !next.done && next.value[0] === t;

    let metric = null;
    if (has) metric = next.value[1][metricName] as number;
    result.push({ date: t, metric });

    if (has) next = iter.next();
  }

  return result;
}

function computeReferenceValue(points: ChartPoint[]): number | undefined {
  const values = points
    .filter(p => p.metric != null && Number.isFinite(p.metric))
    .map(p => Number(p.metric));

  if (values.length === 0) return undefined;

  const sum = values.reduce((acc, v) => acc + v, 0);
  const mean = sum / values.length;
  return Math.round(mean);
}

const theme = {
  light:
    "bg-white text-black border border-slate-800/20 dark:border dark:border-slate-700 dark:bg-slate-800 dark:text-slate-300",
  danger: "bg-red-500 dark:border-red-700 dark:bg-red-800 dark:text-red-50",
  primary: "bg-blue-500 dark:border-blue-700 dark:bg-blue-800 dark:text-blue-50",
};

interface SettingsItemProps {
  readonly title: string;
  readonly description: string | React.ReactNode;
  readonly badge?: string;
  readonly className?: string;
  readonly children?: React.ReactNode;
  readonly badgeTheme?: keyof typeof theme;
}

export function MetricHeader(props: SettingsItemProps) {
  const { title, description, badge } = props;
  const badgeVariants = cva({ variants: { theme: theme } });

  return (
    <div className="space-y-0.5">
      <div className="flex items-center gap-x-2">
        <div className="flex w-full items-center justify-between text-base font-semibold text-black dark:text-white">
          {title}
          {badge && (
            <span
              className={cx(
                "ml-2 rounded-sm px-2 py-1 font-mono text-[10px] leading-none font-medium",
                badgeVariants({ theme: props.badgeTheme ?? "light" }),
              )}
            >
              {badge}
            </span>
          )}
        </div>
      </div>
      <div className="text-sm text-slate-700 dark:text-slate-300">{description}</div>
    </div>
  );
}

export function Metric<T, K extends keyof T>({
  title,
  description,
  stream,
  metric,
  data,
  gate,
  supported,
  map,
  domain = [0, 600],
  unit = "",
  heightClassName = "h-[127px]",
  badge,
  badgeTheme,
}: MetricProps<T, K>) {
  const ready = gate ? gate.size > 0 : stream ? stream.size > 0 : true;
  const supportedFinal =
    supported ??
    (stream && metric ? someIterable(stream, ([, s]) => s[metric] !== undefined) : true);

  // Either we let the consumer provide their own chartArray, or we create one from the stream and metric.
  const raw = data ?? ((stream && metric && createChartArray(stream, metric)) || []);

  // If the consumer provides a map function, we apply it to the raw data.
  const dataFinal: ChartPoint[] = map ? raw.map(map) : raw;

  // Compute the average value of the metric.
  const referenceValue = computeReferenceValue(dataFinal);

  return (
    <div className="space-y-2">
      <MetricHeader
        title={title}
        description={description}
        badge={badge}
        badgeTheme={badgeTheme}
      />

      <GridCard>
        <div
          className={`flex ${heightClassName} w-full items-center justify-center text-sm text-slate-500`}
        >
          {!ready ? (
            <div className="flex flex-col items-center space-y-1">
              <p className="text-slate-700">Waiting for data...</p>
            </div>
          ) : supportedFinal ? (
            <MetricsChart
              data={dataFinal}
              domain={domain}
              unit={unit}
              referenceValue={referenceValue}
            />
          ) : (
            <div className="flex flex-col items-center space-y-1">
              <p className="text-black">Metric not supported</p>
            </div>
          )}
        </div>
      </GridCard>
    </div>
  );
}
