import { ReactNode } from "react";

export function SettingsSectionHeader({
  title,
  description,
}: {
  title: string | ReactNode;
  description: string | ReactNode;
}) {
  return (
    <div className="select-none">
      <h2 className="text-lg font-bold text-black dark:text-white">{title}</h2>
      <div className="text-sm text-slate-700 dark:text-slate-300">{description}</div>
    </div>
  );
}
