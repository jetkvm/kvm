import React, { JSX } from "react";
import clsx from "clsx";

import FieldLabel from "@/components/FieldLabel";
import { cva } from "@/cva.config";

import Card from "./Card";


type SelectMenuProps = Pick<
  JSX.IntrinsicElements["select"],
  "disabled" | "onChange" | "name" | "value"
> & {
  defaultSelection?: string;
  className?: string;
  options: {
    label: string;
    value: string;
    disabled?: boolean;
  }[];
  size?: keyof typeof sizes;
  direction?: "vertical" | "horizontal";
  error?: string;
  fullWidth?: boolean;
} & Partial<React.ComponentProps<typeof FieldLabel>>;

const sizes = {
  XS: "h-[24.5px] pl-3 pr-8 text-xs",
  SM: "h-[32px] pl-3 pr-8 text-[13px]",
  MD: "h-[40px] pl-4 pr-10 text-sm",
  LG: "h-[48px] pl-4 pr-10 px-5 text-base",
};

const selectMenuVariants = cva({
  variants: { size: sizes },
});

export const SelectMenuBasic = React.forwardRef<HTMLSelectElement, SelectMenuProps>(
  function SelectMenuBasic(
    {
      fullWidth,
      options,
      className,
      direction = "vertical",
      label,
      size = "MD",
      name,
      disabled,
      value,
      id,
      onChange,
    },
    ref,
  ) {
    const classes = selectMenuVariants({ size });
    return (
      <div
        className={clsx(
          direction === "vertical" ? "space-y-1" : "flex items-center gap-x-2",
          fullWidth ? "w-full" : "w-auto",
          className,
          "text-sm",
        )}
      >
        {label && <FieldLabel label={label} id={id} as="span" />}
        <Card className="w-auto border! border-solid border-slate-800/30! shadow-xs outline-0 dark:border-slate-300/30!">
          <select
            ref={ref}
            name={name}
            disabled={disabled}
            className={clsx(
              classes,

              // General styling
              "block w-full cursor-pointer rounded-sm border-none py-0 font-medium shadow-none outline-0 transition duration-300",

              // Hover
              "hover:bg-blue-50/80 active:bg-blue-100/60 disabled:hover:bg-white",

              // Dark mode
              "dark:bg-slate-800 dark:text-white dark:hover:bg-slate-700 dark:active:bg-slate-800/60 dark:disabled:hover:bg-slate-800",

              // Invalid
              "invalid:ring-2 invalid:ring-red-600 invalid:ring-offset-2",

              // Focus
              "focus:outline-blue-600 focus:ring-2 focus:ring-blue-700 focus:ring-offset-2 dark:focus:outline-blue-500 dark:focus:ring-blue-500",

              // Disabled
              "disabled:pointer-events-none disabled:select-none disabled:bg-slate-50 disabled:text-slate-500/80 dark:disabled:bg-slate-800 dark:disabled:text-slate-400/80",
            )}
            value={value}
            id={id}
            onChange={onChange}
          >
            {options.map(x => (
              <option key={x.value} value={x.value} disabled={x.disabled}>
                {x.label}
              </option>
            ))}
          </select>
        </Card>
      </div>
    );
  },
);
