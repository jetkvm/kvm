import React, { JSX } from "react";
import { FetcherWithComponents, Link, LinkProps, useNavigation } from "react-router-dom";

import ExtLink from "@/components/ExtLink";
import LoadingSpinner from "@/components/LoadingSpinner";
import { cva, cx } from "@/cva.config";

const sizes = {
  XS: "h-[28px] px-2 text-xs",
  SM: "h-[36px] px-3 text-[13px]",
  MD: "h-[40px] px-3.5 text-sm",
  LG: "h-[48px] px-4 text-base",
  XL: "h-[56px] px-5 text-base",
};

const themes = {
  primary: cx(
    // Base styles
    "border border-blue-900/60 bg-blue-700 text-white shadow-sm dark:border-blue-600",
    // Hover states
    "group-hover:bg-blue-800",
    // Active states
    "group-active:bg-blue-900",
  ),
  danger: cx(
    // Base styles
    "border-red-700 bg-red-600 text-white shadow-xs shadow-red-200/80 dark:border-red-600 dark:shadow-red-900/20",
    // Hover states
    "group-hover:border-red-800 group-hover:bg-red-700 dark:group-hover:border-red-600 dark:group-hover:bg-red-700",
    // Active states
    "group-active:bg-red-800 dark:group-active:bg-red-800",
    // Focus states
    "group-focus:ring-red-700 dark:group-focus:ring-red-600",
  ),
  light: cx(
    // Base styles
    "border-slate-800/30 bg-white text-black shadow-xs dark:border-slate-300/20 dark:bg-slate-800 dark:text-white",
    // Hover states
    "group-hover:bg-blue-50/80 dark:group-hover:bg-slate-700",
    // Active states
    "group-active:bg-blue-100/60 dark:group-active:bg-slate-600",
    // Disabled states
    "group-disabled:group-hover:bg-white dark:group-disabled:group-hover:bg-slate-800",
  ),
  lightDanger: cx(
    // Base styles
    "border-red-400/60 bg-white text-black shadow-xs",
    // Hover states
    "group-hover:bg-red-50/80",
    // Active states
    "group-active:bg-red-100/60",
    // Focus states
    "group-focus:ring-red-700",
  ),
  blank: cx(
    // Base styles
    "border-transparent bg-white/0 text-black dark:text-white",
    // Hover states
    "group-hover:border-slate-800/30 group-hover:bg-white group-hover:shadow-sm dark:group-hover:border-slate-600 dark:group-hover:bg-slate-700",
    // Active states
    "group-active:bg-slate-100/80",
  ),
};

const btnVariants = cva({
  base: cx(
    // Base styles
    "rounded-sm border select-none",
    // Size classes
    "shrink-0 items-center justify-center",
    // Transition classes
    "outline-hidden transition-all duration-200",
    // Text classes
    "text-center font-display leading-tight font-medium",
    // States
    "group-focus:ring-2 group-focus:ring-blue-700 group-focus:ring-offset-2 group-focus:outline-hidden",
    "group-disabled:pointer-events-none group-disabled:opacity-50",
  ),

  variants: {
    size: sizes,
    theme: themes,
  },
});

const iconVariants = cva({
  variants: {
    size: {
      XS: "h-3.5",
      SM: "h-3.5",
      MD: "h-5",
      LG: "h-6",
      XL: "h-6",
    },
    theme: {
      primary: "text-white",
      danger: "text-white ",
      light: "text-black dark:text-white",
      lightDanger: "text-black dark:text-white",
      blank: "text-black dark:text-white",
    },
  },
});

interface ButtonContentPropsType {
  text?: string | React.ReactNode;
  LeadingIcon?: React.FC<{ className: string | undefined }> | null;
  TrailingIcon?: React.FC<{ className: string | undefined }> | null;
  fullWidth?: boolean;
  className?: string;
  textAlign?: "left" | "center" | "right";
  size: keyof typeof sizes;
  theme: keyof typeof themes;
  loading?: boolean;
}

function ButtonContent(props: ButtonContentPropsType) {
  const { text, LeadingIcon, TrailingIcon, fullWidth, className, textAlign, loading } =
    props;

  // Based on the size prop, we'll use the corresponding variant classnames
  const iconClassName = iconVariants(props);
  const btnClassName = btnVariants(props);
  return (
    <div className={cx(className, fullWidth ? "flex" : "inline-flex", btnClassName)}>
      <div
        className={cx(
          "flex w-full min-w-0 items-center gap-x-1.5 text-center",
          textAlign === "left" ? "text-left!" : "",
          textAlign === "center" ? "text-center!" : "",
          textAlign === "right" ? "text-right!" : "",
        )}
      >
        {loading ? (
          <div>
            <LoadingSpinner className={cx(iconClassName, "animate-spin")} />
          </div>
        ) : (
          LeadingIcon && (
            <LeadingIcon className={cx(iconClassName, "shrink-0 justify-start")} />
          )
        )}

        {text && typeof text === "string" ? (
          <span className="relative w-full truncate">{text}</span>
        ) : (
          text
        )}

        {TrailingIcon && (
          <TrailingIcon className={cx(iconClassName, "shrink-0 justify-end")} />
        )}
      </div>
    </div>
  );
}

type ButtonPropsType = Pick<
  JSX.IntrinsicElements["button"],
  | "type"
  | "disabled"
  | "onClick"
  | "name"
  | "value"
  | "formNoValidate"
  | "onMouseLeave"
  | "onMouseDown"
  | "onMouseUp"
  | "onMouseLeave"
> &
  React.ComponentProps<typeof ButtonContent> & {
    fetcher?: FetcherWithComponents<unknown>;
  };

export const Button = React.forwardRef<HTMLButtonElement, ButtonPropsType>(
  ({ type, disabled, onClick, formNoValidate, loading, fetcher, ...props }, ref) => {
    const classes = cx(
      "group outline-hidden",
      props.fullWidth ? "w-full" : "",
      loading ? "pointer-events-none" : "",
    );
    const navigation = useNavigation();
    const loader = fetcher ? fetcher : navigation;
    return (
      <button
        ref={ref}
        formNoValidate={formNoValidate}
        className={classes}
        type={type}
        disabled={disabled}
        onClick={onClick}
        onMouseDown={props?.onMouseDown}
        onMouseUp={props?.onMouseUp}
        onMouseLeave={props?.onMouseLeave}
        name={props.name}
        value={props.value}
      >
        <ButtonContent
          {...props}
          loading={
            loading ??
            (type === "submit" &&
              (loader.state === "submitting" || loader.state === "loading") &&
              loader.formMethod?.toLowerCase() === "post")
          }
        />
      </button>
    );
  },
);

Button.displayName = "Button";

type LinkPropsType = Pick<LinkProps, "to"> &
  React.ComponentProps<typeof ButtonContent> & { disabled?: boolean };
export const LinkButton = ({ to, ...props }: LinkPropsType) => {
  const classes = cx(
    "group outline-hidden",
    props.disabled ? "pointer-events-none opacity-70!" : "",
    props.fullWidth ? "w-full" : "",
    props.loading ? "pointer-events-none" : "",
    props.className,
  );

  if (to.toString().startsWith("http")) {
    return (
      <ExtLink href={to.toString()} className={classes}>
        <ButtonContent {...props} />
      </ExtLink>
    );
  } else {
    return (
      <Link to={to} className={classes}>
        <ButtonContent {...props} />
      </Link>
    );
  }
};

type LabelPropsType = Pick<HTMLLabelElement, "htmlFor"> &
  React.ComponentProps<typeof ButtonContent> & { disabled?: boolean };
export const LabelButton = ({ htmlFor, ...props }: LabelPropsType) => {
  const classes = cx(
    "group block cursor-pointer outline-hidden",
    props.disabled ? "pointer-events-none opacity-70!" : "",
    props.fullWidth ? "w-full" : "",
    props.loading ? "pointer-events-none" : "",
    props.className,
  );

  return (
    <div>
      <label htmlFor={htmlFor} className={classes}>
        <ButtonContent {...props} />
      </label>
    </div>
  );
};
