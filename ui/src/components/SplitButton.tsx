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
}

const baseStyles = cx(
  "h-[28px] font-display text-xs leading-tight font-medium select-none",
  "outline-hidden transition-all duration-200",
);

const lightTheme = cx(
  "border border-slate-800/30 bg-white text-black shadow-xs",
  "dark:border-slate-300/20 dark:bg-slate-800 dark:text-white",
);

const lightHover = cx(
  "hover:bg-blue-50/80 dark:hover:bg-slate-700",
  "active:bg-blue-100/60 dark:active:bg-slate-600",
);

const primaryClass = cx(
  baseStyles,
  lightTheme,
  lightHover,
  "inline-flex cursor-pointer items-center gap-x-1.5 rounded-l-sm border-r-0 px-2",
  "disabled:pointer-events-none disabled:opacity-50",
);

const iconClass = "h-3.5 shrink-0 text-black dark:text-white";

// eslint-disable-next-line react-refresh/only-export-components
export const SplitButtonPrimary = React.forwardRef<
  HTMLButtonElement,
  {
    icon?: React.FC<{ className: string | undefined }>;
    label: string;
    className?: string;
  } & React.ButtonHTMLAttributes<HTMLButtonElement>
>(({ icon: Icon, label, className, ...props }, ref) => (
  <button ref={ref} type="button" {...props} className={cx(primaryClass, className)}>
    {Icon && <Icon className={iconClass} />}
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

export function SplitButtonCaret({ menuItems }: { menuItems: SplitButtonMenuItem[] }) {
  return (
    <Menu as="div" className="relative">
      <MenuButton
        className={cx(
          baseStyles,
          lightTheme,
          lightHover,
          "inline-flex cursor-pointer items-center rounded-r-sm px-1",
          "border-l-slate-800/15 dark:border-l-slate-300/10",
          "disabled:pointer-events-none disabled:opacity-50",
        )}
      >
        <ChevronDownIcon className="size-3.5 text-black dark:text-white" />
      </MenuButton>

      <MenuItems
        anchor="bottom end"
        transition
        className={cx(
          "z-20 mt-1 min-w-[160px] origin-top-right rounded-md",
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
                <span>{item.label}</span>
              </button>
            </MenuItem>
          ))}
        </div>
      </MenuItems>
    </Menu>
  );
}
