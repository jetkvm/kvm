import React from "react";

import { cx } from "@/cva.config";

interface Props {
  title: string;
  status: string;
  icon?: React.FC<{ className: string | undefined }>;
  iconClassName?: string;
  statusIndicatorClassName?: string;
}

export default function StatusCard({
  title,
  status,
  icon: Icon,
  iconClassName,
  statusIndicatorClassName,
}: Props) {
  return (
    <div className="flex items-center gap-x-3 rounded-md border bg-white dark:border-slate-600 dark:bg-slate-800 dark:text-white border-slate-800/20 px-2 py-1.5">
      {Icon ? (
        <span>
          <Icon className={cx(iconClassName, "shrink-0")} />
        </span>
      ) : null}

      <div className="space-y-1">
        <div className="text-xs font-semibold leading-none transition text-ellipsis">
          {title}
        </div>
        <div className="text-xs leading-none">
          <div className="flex items-center gap-x-1">
            <div
              className={cx(
                "h-2 w-2 rounded-full border transition",
                statusIndicatorClassName,
              )}
            />
            <span className={cx("transition")}>{status}</span>
          </div>
        </div>
      </div>
    </div>
  );
}
