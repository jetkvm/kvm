import { ComponentProps } from "react";
import { cva, cx } from "cva";

import { GridCard } from "./Card";
import StatChart from "./StatChart";

interface ChartPoint {
  date: number;
  stat: number | null;
}

interface MetricProps<T, K extends keyof T> {
  title: string;
  description: string;
  stream?: Map<number, T>;
  metric?: K;
  data?: ChartPoint[];
  gate?: Map<number, unknown>;
  supported?: boolean;
  map?: (p: { date: number; stat: T[K] | null }) => ChartPoint;
  domain?: [number, number];
  unit?: string;
  heightClassName?: string;
  referenceValue?: number;
  badge?: ComponentProps<typeof MetricHeader>["badge"];
  badgeTheme?: ComponentProps<typeof MetricHeader>["badgeTheme"];
}

export function createChartArray<T, K extends keyof T>(
  stream: Map<number, T>,
  metric: K,
): { date: number; stat: T[K] | null }[] {
  const stat = Array.from(stream).map(([key, stats]) => {
    return { date: key, stat: stats[metric] };
  });

  // Sort the dates to ensure they are in chronological order
  const sortedStat = stat.map(x => x.date).sort((a, b) => a - b);

  // Determine the earliest statistic date
  const earliestStat = sortedStat[0];

  // Current time in seconds since the Unix epoch
  const now = Math.floor(Date.now() / 1000);

  // Determine the starting point for the chart data
  const firstChartDate = earliestStat ? Math.min(earliestStat, now - 120) : now - 120;

  // Generate the chart array for the range between 'firstChartDate' and 'now'
  return Array.from({ length: now - firstChartDate }, (_, i) => {
    const currentDate = firstChartDate + i;
    return {
      date: currentDate,
      // Find the statistic for 'currentDate', or use the last known statistic if none exists for that date
      stat: stat.find(x => x.date === currentDate)?.stat ?? null,
    };
  });
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
  referenceValue,
  badge,
  badgeTheme,
}: MetricProps<T, K>) {
  const ready = gate ? gate.size > 0 : stream ? stream.size > 0 : true;
  const supportedFinal =
    supported ??
    (stream && metric
      ? Array.from(stream).some(([, s]) => s[metric] !== undefined)
      : true);

  const raw = stream && metric ? createChartArray(stream, metric) : [];
  const dataFinal: ChartPoint[] =
    data ??
    (map
      ? raw.map(map)
      : raw.map(x => ({
          date: x.date,
          stat: typeof x.stat === "number" ? (x.stat as unknown as number) : null,
        })));

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
            <StatChart
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
