import React from "react";
import { LuInfo } from "react-icons/lu";

import { cx } from "@/cva.config";

interface Props {
  label: string | React.ReactNode;
  id?: string;
  as?: "label" | "span";
  description?: string | React.ReactNode | null;
  disabled?: boolean;
  info?: string;
}
export default function FieldLabel({
  label,
  id,
  as = "label",
  description,
  disabled,
  info,
}: Props) {
  const labelContent = (
    <>
      <div className="flex items-center gap-1">
        {label}
        {info && (
          <div className="group relative cursor-pointer">
            <LuInfo className={cx(
              "h-4 w-4",
              "text-blue-500 hover:text-blue-600 dark:text-blue-400 dark:hover:text-blue-300"
            )} />
            <div className={cx(
              "absolute left-1/2 top-full z-10 mt-1 hidden w-64 -translate-x-1/2",
              "rounded-md bg-slate-800 px-3 py-2 text-xs text-white shadow-lg",
              "group-hover:block dark:bg-slate-700"
            )}>
              <p>{info}</p>
            </div>
          </div>
        )}
      </div>
      {description && (
        <span className="my-0.5 text-[13px] font-normal text-slate-600 dark:text-slate-400">
          {description}
        </span>
      )}
    </>
  );

  if (as === "label") {
    return (
      <label
        htmlFor={id}
        className={cx(
          "flex select-none flex-col text-left font-display text-[13px] font-semibold leading-snug text-black dark:text-white",
          disabled && "opacity-50",
        )}
      >
        {labelContent}
      </label>
    );
  } else if (as === "span") {
    return (
      <div className="flex select-none flex-col">
        <span className="font-display text-[13px] font-medium leading-snug text-black dark:text-white">
          {labelContent}
        </span>
      </div>
    );
  } else {
    return <></>;
  }
}
