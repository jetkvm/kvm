import React from "react";
import { Menu, MenuButton, MenuItem, MenuItems } from "@headlessui/react";
import { ChevronDownIcon } from "@heroicons/react/16/solid";

import { cx } from "@/cva.config";

export interface SplitButtonMenuItem {
  label: string;
  icon?: React.FC<{ className: string | undefined }>;
  onClick: () => void;
  active?: boolean;
  disabled?: boolean;
  // A second line under the label, such as why the item is disabled.
  description?: string;
}

const baseStyles = cx(
  "font-display leading-tight font-medium select-none",
  "outline-hidden transition-all duration-200",
);

// The heights and text of Button's XS and SM sizes.
const sizes = {
  XS: { primary: "h-[28px] px-2 text-xs", caret: "h-[28px] px-1 text-xs" },
  SM: { primary: "h-[36px] px-3 text-[13px]", caret: "h-[36px] px-1.5 text-[13px]" },
};

type SplitButtonSize = keyof typeof sizes;

// The colors of Button's light and primary themes.
const themes = {
  light: {
    button: cx(
      "border border-slate-800/30 bg-white text-black shadow-xs",
      "dark:border-slate-300/20 dark:bg-slate-800 dark:text-white",
      "hover:bg-blue-50/80 dark:hover:bg-slate-700",
      "active:bg-blue-100/60 dark:active:bg-slate-600",
    ),
    divider: "border-l-slate-800/15 dark:border-l-slate-300/10",
    icon: "text-black dark:text-white",
  },
  primary: {
    button: cx(
      "border border-blue-900/60 bg-blue-700 text-white shadow-sm dark:border-blue-600",
      "hover:bg-blue-800 active:bg-blue-900",
    ),
    divider: "border-l-white/30",
    icon: "text-white",
  },
};

type SplitButtonTheme = keyof typeof themes;

const primaryClass = cx(
  baseStyles,
  "inline-flex cursor-pointer items-center gap-x-1.5 rounded-l-sm border-r-0",
  "disabled:pointer-events-none disabled:opacity-50",
);

// eslint-disable-next-line react-refresh/only-export-components
export const SplitButtonPrimary = React.forwardRef<
  HTMLButtonElement,
  {
    icon?: React.FC<{ className: string | undefined }>;
    label: string;
    className?: string;
    theme?: SplitButtonTheme;
    size?: SplitButtonSize;
  } & React.ButtonHTMLAttributes<HTMLButtonElement>
>(({ icon: Icon, label, className, theme = "light", size = "XS", ...props }, ref) => (
  <button
    ref={ref}
    type="button"
    {...props}
    className={cx(primaryClass, sizes[size].primary, themes[theme].button, className)}
  >
    {Icon && <Icon className={cx("h-3.5 shrink-0", themes[theme].icon)} />}
    <span className="truncate">{label}</span>
  </button>
));

SplitButtonPrimary.displayName = "SplitButtonPrimary";

export function SplitButtonGroup({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return <div className={cx("inline-flex", className)}>{children}</div>;
}

export function SplitButtonCaret({
  menuItems,
  theme = "light",
  size = "XS",
  disabled,
  "data-testid": testId,
}: {
  menuItems: SplitButtonMenuItem[];
  theme?: SplitButtonTheme;
  size?: SplitButtonSize;
  disabled?: boolean;
  "data-testid"?: string;
}) {
  return (
    <Menu as="div" className="relative">
      <MenuButton
        disabled={disabled}
        data-testid={testId}
        className={cx(
          baseStyles,
          sizes[size].caret,
          themes[theme].button,
          themes[theme].divider,
          "inline-flex cursor-pointer items-center rounded-r-sm",
          "disabled:pointer-events-none disabled:opacity-50",
        )}
      >
        <ChevronDownIcon className={cx("size-3.5", themes[theme].icon)} />
      </MenuButton>

      <MenuItems
        anchor="bottom end"
        transition
        className={cx(
          "z-20 mt-1 max-w-xs min-w-[160px] origin-top-right rounded-md",
          "border border-slate-800/20 bg-white shadow-lg dark:border-slate-300/20 dark:bg-slate-800",
          "transition duration-200 ease-out data-closed:scale-95 data-closed:opacity-0",
        )}
      >
        <div className="p-1">
          {menuItems.map(item => (
            <MenuItem key={item.label}>
              <button
                type="button"
                className={cx(
                  "flex w-full items-center gap-x-2 rounded-sm px-2 py-1.5 text-xs font-medium",
                  "text-slate-700 dark:text-slate-200",
                  "data-focus:bg-blue-50 dark:data-focus:bg-slate-700",
                  item.active && "bg-blue-50 text-blue-700 dark:bg-slate-700 dark:text-blue-400",
                  item.disabled && "pointer-events-none opacity-50",
                )}
                disabled={item.disabled}
                onClick={item.onClick}
              >
                {item.icon && (
                  <item.icon
                    className={cx(
                      "h-3.5 shrink-0",
                      item.active
                        ? "text-blue-700 dark:text-blue-400"
                        : "text-slate-500 dark:text-slate-400",
                    )}
                  />
                )}
                {item.description ? (
                  <span className="flex flex-col gap-y-0.5 text-left">
                    <span>{item.label}</span>
                    <span className="font-normal text-slate-500 dark:text-slate-400">
                      {item.description}
                    </span>
                  </span>
                ) : (
                  <span>{item.label}</span>
                )}
              </button>
            </MenuItem>
          ))}
        </div>
      </MenuItems>
    </Menu>
  );
}
