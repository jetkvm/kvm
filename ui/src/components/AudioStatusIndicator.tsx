import { cx } from "@/cva.config";

interface AudioMetrics {
  frames_dropped: number;
  // Add other metrics properties as needed
}

interface AudioStatusIndicatorProps {
  metrics?: AudioMetrics;
  label: string;
  className?: string;
}

export function AudioStatusIndicator({ metrics, label, className }: AudioStatusIndicatorProps) {
  const hasIssues = metrics && metrics.frames_dropped > 0;
  
  return (
    <div className={cx(
      "text-center p-2 bg-slate-50 dark:bg-slate-800 rounded",
      className
    )}>
      <div className={cx(
        "font-medium",
        hasIssues
          ? "text-red-600 dark:text-red-400" 
          : "text-green-600 dark:text-green-400"
      )}>
        {hasIssues ? "Issues" : "Good"}
      </div>
      <div className="text-slate-500 dark:text-slate-400">{label}</div>
    </div>
  );
}