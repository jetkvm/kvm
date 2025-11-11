import { ReactNode } from "react";

export function SettingsPageHeader({
  title,
  description,
  action,
}: {
  title: string | ReactNode;
  description: string | ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-x-2 select-none">
      <div className="flex flex-col gap-y-1">
        <h2 className="text-xl font-extrabold text-black dark:text-white">{title}</h2>
        <div className="text-sm text-black dark:text-slate-300">{description}</div>
      </div>
      {action && <div className="">{action}</div>}
    </div>
  );
}
