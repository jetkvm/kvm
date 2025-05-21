import React from "react";

import { cx } from "@/cva.config";

interface Props {
  label: string | React.ReactNode;
  id?: string;
  as?: "label" | "span";
  description?: string | React.ReactNode | null;
  disabled?: boolean;
}
export default function FieldLabel({
  label,
  id,
  as = "label",
  description,
  disabled,
}: Props) {
  if (as === "label") {
    return (
      <label
        htmlFor={id}
        className={cx(
          "flex flex-col text-left font-display text-[13px] leading-snug font-semibold text-black select-none dark:text-white",
          disabled && "opacity-50",
        )}
      >
        {label}
        {description && (
          <span className="my-0.5 text-[13px] font-normal text-slate-600 dark:text-slate-400">
            {description}
          </span>
        )}
      </label>
    );
  } else if (as === "span") {
    return (
      <div className="flex flex-col select-none">
        <span className="font-display text-[13px] leading-snug font-medium text-black dark:text-white">
          {label}
        </span>
        {description && (
          <span className="my-0.5 text-[13px] font-normal text-slate-600 dark:text-slate-400">
            {description}
          </span>
        )}
      </div>
    );
  } else {
    return <></>;
  }
}
