import { cx } from "@/cva.config";
import LoadingSpinner from "@components/LoadingSpinner";

type SettingsItemSize = "SM" | "MD";

interface SettingsItemProps {
  readonly title: string;
  readonly description: string | React.ReactNode;
  readonly badge?: string;
  readonly badgeTheme?: keyof typeof badgeTheme;
  readonly className?: string;
  readonly loading?: boolean;
  readonly children?: React.ReactNode;
  readonly size?: SettingsItemSize;
}

const badgeTheme = {
  info: "bg-blue-500 text-white",
  success: "bg-green-500 text-white",
  warning: "bg-yellow-500 text-white",
  danger: "bg-red-500 text-white",
};

export function SettingsItem(props: SettingsItemProps) {
  const {
    title,
    description,
    badge,
    badgeTheme: badgeThemeProp = "danger",
    children,
    className,
    loading,
    size = "MD",
  } = props;
  const badgeThemeClass = badgeTheme[badgeThemeProp];

  const isSM = size === "SM";

  return (
    <label
      className={cx("flex items-center justify-between gap-x-8 rounded select-none", className)}
    >
      <div className="space-y-0.5">
        <div className="flex items-center gap-x-2">
          <div
            className={cx(
              "flex items-center font-semibold text-black dark:text-white",
              isSM ? "text-sm" : "text-base",
            )}
          >
            {title}
            {badge && (
              <span
                className={cx(
                  "ml-2 rounded-full px-2 py-1 text-[10px] leading-none font-medium text-white",
                  badgeThemeClass,
                )}
              >
                {badge}
              </span>
            )}
          </div>
          {loading && <LoadingSpinner className="h-4 w-4 text-blue-500" />}
        </div>
        <div className={cx("text-slate-700 dark:text-slate-300", isSM ? "text-xs" : "text-sm")}>
          {description}
        </div>
      </div>
      {children ? <div>{children}</div> : null}
    </label>
  );
}
