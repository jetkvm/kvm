import type { Ref } from "react";
import React, { forwardRef, JSX } from "react";
import clsx from "clsx";

import FieldLabel from "@/components/FieldLabel";
import Card from "@/components/Card";
import { cva } from "@/cva.config";

const sizes = {
  XS: "h-[26px] px-3 text-xs",
  SM: "h-[36px] px-3 text-[14px]",
  MD: "h-[40px] px-4 text-sm",
  LG: "h-[48px] py-4 px-5 text-base",
};

const inputVariants = cva({
  variants: { size: sizes },
});

type InputFieldProps = {
  size?: keyof typeof sizes;
  TrailingElm?: React.ReactNode;
  LeadingElm?: React.ReactNode;
  error?: string | null;
} & Omit<JSX.IntrinsicElements["input"], "size">;

type InputFieldWithLabelProps = InputFieldProps & {
  label: React.ReactNode;
  description?: string | null;
};

const InputField = forwardRef<HTMLInputElement, InputFieldProps>(function InputField(
  { LeadingElm, TrailingElm, className, size = "MD", error, ...props },
  ref,
) {
  const sizeClasses = inputVariants({ size });
  return (
    <>
      <Card
        className={clsx(
          // General styling
          "relative flex w-full overflow-hidden",

          "[&:has(:user-invalid)]:ring-2 [&:has(:user-invalid)]:ring-red-600 [&:has(:user-invalid)]:ring-offset-2",

          // Focus Within
          "focus-within:border-slate-300 dark:focus-within:border-slate-600 focus-within:outline-none focus-within:ring-2 focus-within:ring-blue-700 focus-within:ring-offset-2",

          // Disabled Within
          "disabled-within:pointer-events-none disabled-within:select-none disabled-within:bg-slate-50 dark:disabled-within:bg-slate-800 disabled-within:text-slate-500/80",
        )}
      >
        {LeadingElm && (
          <div className={clsx("pointer-events-none border-r border-r-slate-300 dark:border-r-slate-600")}>
            {LeadingElm}
          </div>
        )}
        <input
          ref={ref}
          className={clsx(
            sizeClasses,
            TrailingElm ? "pr-2" : "",
            className,
            "block flex-1 border-0 bg-transparent leading-none placeholder:text-sm placeholder:text-slate-300 dark:placeholder:text-slate-500 focus:ring-0 text-black dark:text-white",
          )}
          {...props}
        />
        {TrailingElm && (
          <div className="flex items-center pr-3 pointer-events-none">{TrailingElm}</div>
        )}
      </Card>
      {error && <FieldError error={error} />}
    </>
  );
});
InputField.displayName = "InputField";

const InputFieldWithLabel = forwardRef<HTMLInputElement, InputFieldWithLabelProps>(
  function InputFieldWithLabel(
    { label, description, id, ...props },
    ref: Ref<HTMLInputElement>,
  ) {
    return (
      <div className="w-full space-y-1">
        {(label || description) && (
          <FieldLabel label={label} id={id} description={description} />
        )}
        <InputField ref={ref as never} id={id} {...props} />
      </div>
    );
  },
);
InputFieldWithLabel.displayName = "InputFieldWithLabel";

export default InputField;
export { InputFieldWithLabel };

export function FieldError({ error }: { error: string | React.ReactNode }) {
  return <div className="mt-[6px] text-[13px] leading-normal text-red-500">{error}</div>;
}
