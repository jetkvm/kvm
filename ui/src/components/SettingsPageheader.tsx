import { ReactNode } from "react";

export function SettingsPageHeader({
  title,
  description,
}: {
  title: string | ReactNode;
  description: string | ReactNode;
}) {
  return (
    <div className="select-none">
      <h2 className=" text-xl font-extrabold text-black dark:text-white">{title}</h2>
      <div className="text-sm text-black dark:text-slate-300">{description}</div>
    </div>
  );
}
