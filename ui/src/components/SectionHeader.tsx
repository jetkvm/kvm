import { ReactNode } from "react";

export function SectionHeader({
  title,
  description,
}: {
  title: string | ReactNode;
  description: string | ReactNode;
}) {
  return (
    <div className="select-none">
      <h2 className=" text-xl font-bold text-black dark:text-white">{title}</h2>
      <div className="text-sm text-black dark:text-slate-300">{description}</div>
    </div>
  );
}
