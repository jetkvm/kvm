import { cx } from "@/cva.config";
import LoadingSpinner from "@components/LoadingSpinner";

interface SettingsItemProps {
  readonly title: string;
  readonly description: string | React.ReactNode;
  readonly badge?: string;
  readonly className?: string;
  readonly loading?: boolean;
  readonly children?: React.ReactNode;
}

export function SettingsItem(props: SettingsItemProps) {
  const { title, description, badge, children, className, loading } = props;

  return (
    <label
      className={cx(
        "flex select-none items-center justify-between gap-x-8 rounded",
        className,
      )}
    >
      <div className="space-y-0.5">
        <div className="flex items-center gap-x-2">
          <div className="flex items-center text-base font-semibold text-black dark:text-white">
            {title}
            {badge && (
              <span className="ml-2 rounded-full bg-red-500 px-2 py-1 text-[10px] font-medium leading-none text-white dark:border dark:border-red-700 dark:bg-red-800 dark:text-red-50">
                {badge}
              </span>
            )}
          </div>
          {loading && <LoadingSpinner className="h-4 w-4 text-blue-500" />}
        </div>
        <div className="text-sm text-slate-700 dark:text-slate-300">{description}</div>
      </div>
      {children ? <div>{children}</div> : null}
    </label>
  );
}
