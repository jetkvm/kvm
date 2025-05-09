import type { Ref } from "react";
import React, { forwardRef, JSX } from "react";
import clsx from "clsx";

import FieldLabel from "@/components/FieldLabel";
import { cva, cx } from "@/cva.config";

const sizes = {
  SM: "w-4 h-4",
  MD: "w-5 h-5",
};

const checkboxVariants = cva({
  base: cx(
    "block rounded",

    // Colors
    "border-slate-300 dark:border-slate-600 bg-slate-50 dark:bg-slate-800 text-blue-700 dark:text-blue-500 transition-colors",

    // Hover
    "hover:bg-slate-200/50 dark:hover:bg-slate-700/50",

    // Active
    "active:bg-slate-200 dark:active:bg-slate-700",

    // Focus
    "focus:border-slate-300 dark:focus:border-slate-600 focus:outline-none focus:ring-2 focus:ring-blue-700 dark:focus:ring-blue-500 focus:ring-offset-2 dark:focus:ring-offset-slate-900",

    // Disabled
    "disabled:pointer-events-none disabled:opacity-30",
  ),
  variants: { size: sizes },
});

type CheckBoxProps = {
  size?: keyof typeof sizes;
} & Omit<JSX.IntrinsicElements["input"], "size" | "type">;

const Checkbox = forwardRef<HTMLInputElement, CheckBoxProps>(function Checkbox(
  { size = "MD", className, ...props },
  ref,
) {
  const classes = checkboxVariants({ size });
  return <input ref={ref} {...props} type="checkbox" className={clsx(classes, className)} />;
});
Checkbox.displayName = "Checkbox";

type CheckboxWithLabelProps = React.ComponentProps<typeof FieldLabel> &
  CheckBoxProps & {
    fullWidth?: boolean;
    disabled?: boolean;
  };

const CheckboxWithLabel = forwardRef<HTMLInputElement, CheckboxWithLabelProps>(
  function CheckboxWithLabel(
    { label, id, description, fullWidth, readOnly, ...props },
    ref: Ref<HTMLInputElement>,
  ) {
    return (
      <label
        className={clsx(
          "flex shrink-0 items-center justify-between gap-x-2",
          fullWidth ? "flex" : "inline-flex",
          readOnly ? "pointer-events-none opacity-50" : "",
        )}
      >
        <Checkbox ref={ref as never} {...props} />
        <div className="max-w-md">
          <FieldLabel label={label} id={id} description={description} as="span" />
        </div>
      </label>
    );
  },
);
CheckboxWithLabel.displayName = "CheckboxWithLabel";

export default Checkbox;
export { CheckboxWithLabel, Checkbox };
