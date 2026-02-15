import { Link } from "react-router";

import { cva, cx } from "@/cva.config";
import LoadingSpinner from "@components/LoadingSpinner";

const badgeVariants = cva({
  base: "ml-2 rounded-full px-2 py-1 text-[10px] font-medium leading-none text-white dark:border",
  variants: {
    variant: {
      error: "bg-red-500 dark:border-red-700 dark:bg-red-800 dark:text-red-50",
      info: "bg-blue-500 dark:border-blue-600 dark:bg-blue-700 dark:text-blue-50",
    },
  },
});

interface SettingsItemProps {
  readonly title: string;
  readonly description: string | React.ReactNode;
  readonly badge?: string;
  readonly badgeVariant?: "error" | "info";
  readonly badgeLink?: string;
  readonly className?: string;
  readonly loading?: boolean;
  readonly children?: React.ReactNode;
}

export function SettingsItem(props: SettingsItemProps) {
  const {
    title,
    description,
    badge,
    badgeVariant = "error",
    badgeLink,
    children,
    className,
    loading,
  } = props;

  const badgeClasses = badgeVariants({ variant: badgeVariant });

  const badgeContent =
    badge &&
    (badgeLink ? (
      <Link
        to={badgeLink}
        className={cx(badgeClasses, "cursor-pointer transition-opacity hover:opacity-80")}
      >
        {badge}
      </Link>
    ) : (
      <span className={badgeClasses}>{badge}</span>
    ));

  return (
    <label
      className={cx("flex items-center justify-between gap-x-8 rounded select-none", className)}
    >
      <div className="space-y-0.5">
        <div className="flex items-center gap-x-2">
          <div className="flex items-center text-base font-semibold text-black dark:text-white">
            {title}
            {badgeContent}
          </div>
          {loading && <LoadingSpinner className="h-4 w-4 text-blue-500" />}
        </div>
        <div className="text-sm text-slate-700 dark:text-slate-300">{description}</div>
      </div>
      {children ? <div>{children}</div> : null}
    </label>
  );
}
