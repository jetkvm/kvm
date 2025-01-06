import React from "react";
import { cx } from "@/cva.config";

type Props = {
  label: string | React.ReactNode;
  id?: string;
  as?: "label" | "span";
  description?: string | React.ReactNode | null;
  disabled?: boolean;
};
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
          "flex select-none flex-col text-left font-display text-[13px] font-semibold leading-snug text-black dark:text-white",
          disabled && "opacity-50",
        )}
      >
        {label}
        {description && (
          <span className="my-0.5 text-[13px] font-normal text-slate-600">
            {description}
          </span>
        )}
      </label>
    );
  } else if (as === "span") {
    return (
      <div className="flex flex-col select-none">
        <span className="font-display text-[13px] font-medium leading-snug text-black">
          {label}
        </span>
        {description && (
          <span className="my-0.5 text-[13px] font-normal text-slate-600">
            {description}
          </span>
        )}
      </div>
    );
  } else {
    return <></>;
  }
}
