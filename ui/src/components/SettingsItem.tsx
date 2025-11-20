import { cx } from "@/cva.config";
import LoadingSpinner from "@components/LoadingSpinner";

interface SettingsItemProps {
  readonly title: string;
  readonly description: string | React.ReactNode;
  readonly badge?: string;
  readonly badgeTheme?: keyof typeof badgeTheme;
  readonly className?: string;
  readonly loading?: boolean;
  readonly children?: React.ReactNode;
}

const badgeTheme = {
  info: "bg-blue-500 text-white",
  success: "bg-green-500 text-white",
  warning: "bg-yellow-500 text-white",
  danger: "bg-red-500 text-white",
};

export function SettingsItem(props: SettingsItemProps) {
  const { title, description, badge, badgeTheme: badgeThemeProp = "danger", children, className, loading } = props;
  const badgeThemeClass = badgeTheme[badgeThemeProp];

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
              <span className={cx("ml-2 rounded-full px-2 py-1 text-[10px] font-medium leading-none text-white", badgeThemeClass)}>
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
