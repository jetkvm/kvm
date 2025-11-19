import { cx } from "@/cva.config";

interface NestedSettingsGroupProps {
  readonly children: React.ReactNode;
  readonly className?: string;
}

export function NestedSettingsGroup(props: NestedSettingsGroupProps) {
  const { children, className } = props;

  return (
    <div
      className={cx(
        "space-y-4 border-l-2 border-slate-200 ml-2 pl-4 dark:border-slate-700",
        className,
      )}
    >
      {children}
    </div>
  );
}

