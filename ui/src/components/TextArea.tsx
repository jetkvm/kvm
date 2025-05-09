import React, { JSX } from "react";
import clsx from "clsx";

import FieldLabel from "@/components/FieldLabel";
import { FieldError } from "@/components/InputField";
import Card from "@/components/Card";
import { cx } from "@/cva.config";

type TextAreaProps = JSX.IntrinsicElements["textarea"] & {
  error?: string | null;
};

const TextArea = React.forwardRef<HTMLTextAreaElement, TextAreaProps>(
  function TextArea(props, ref) {
    return (
      <Card
        className={cx(
          "relative w-full",
          "invalid-within::ring-2 invalid-within::ring-red-600 invalid-within::ring-offset-2",
          "focus-within:border-slate-300 focus-within:outline-none focus-within:ring-1 focus-within:ring-blue-700 dark:focus-within:border-slate-600",
        )}
      >
        <textarea
          ref={ref}
          {...props}
          id="asd"
          className={clsx(
            "block w-full rounded border-transparent bg-transparent text-black placeholder:text-slate-300 focus:ring-0 disabled:pointer-events-none disabled:select-none disabled:bg-slate-50 disabled:text-slate-300 dark:text-white dark:placeholder:text-slate-500 dark:disabled:bg-slate-800 sm:text-sm",
            props.className,
          )}
        />
      </Card>
    );
  },
);

type TextAreaWithLabelProps = {
  label: string | React.ReactNode;
  id?: string;
  description?: string;
  error?: string | null;
} & React.ComponentProps<typeof TextArea>;

export const TextAreaWithLabel = React.forwardRef<
  HTMLTextAreaElement,
  TextAreaWithLabelProps
>(function TextAreaWithLabel({ label, error, id, description, ...props }, ref) {
  return (
    <div className="space-y-1">
      <FieldLabel label={label} id={id} description={description} />
      <TextArea ref={ref} {...props} />
      {error && <FieldError error={error} />}
    </div>
  );
});

export default TextArea;
