import { useRef } from "react";
import clsx from "clsx";
import { Combobox as HeadlessCombobox, ComboboxInput, ComboboxOption, ComboboxOptions } from "@headlessui/react";
import { cva } from "@/cva.config";
import Card from "./Card";

export interface ComboboxOption {
  value: string;
  label: string;
}

const sizes = {
  XS: "h-[24.5px] pl-3 pr-8 text-xs",
  SM: "h-[32px] pl-3 pr-8 text-[13px]",
  MD: "h-[40px] pl-4 pr-10 text-sm",
  LG: "h-[48px] pl-4 pr-10 px-5 text-base",
} as const;

const comboboxVariants = cva({
  variants: { size: sizes },
});

type BaseProps = React.ComponentProps<typeof HeadlessCombobox>;

interface ComboboxProps extends Omit<BaseProps, 'displayValue'> {
  displayValue: (option: ComboboxOption) => string;
  onInputChange: (option: string) => void;
  options: () => ComboboxOption[];
  placeholder?: string;
  emptyMessage?: string;
  size?: keyof typeof sizes;
  disabledMessage?: string;
}

export function Combobox({
  onInputChange,
  displayValue,
  options,
  disabled = false,
  placeholder = "Search...",
  emptyMessage = "No results found",
  size = "MD",
  onChange,
  disabledMessage = "Input disabled",
  ...otherProps
}: ComboboxProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const classes = comboboxVariants({ size });

  return (
    <HeadlessCombobox 
      onChange={onChange}
      {...otherProps}
    >
      {() => (
        <>
          <Card className="w-auto !border border-solid !border-slate-800/30 shadow outline-0 dark:!border-slate-300/30">
            <ComboboxInput
            ref={inputRef}
            className={clsx(
              classes,
              
              // General styling
              "block w-full cursor-pointer rounded border-none py-0 font-medium shadow-none outline-0 transition duration-300",
              
              // Hover
              "hover:bg-blue-50/80 active:bg-blue-100/60",
              
              // Dark mode
              "dark:bg-slate-800 dark:text-white dark:hover:bg-slate-700 dark:active:bg-slate-800/60",
              
              // Focus
              "focus:outline-blue-600 focus:ring-2 focus:ring-blue-700 focus:ring-offset-2 dark:focus:outline-blue-500 dark:focus:ring-blue-500",
              
              // Disabled
              disabled && "pointer-events-none select-none bg-slate-50 text-slate-500/80 dark:bg-slate-800 dark:text-slate-400/80 disabled:hover:bg-white dark:disabled:hover:bg-slate-800"
            )}
            placeholder={disabled ? disabledMessage : placeholder}
            displayValue={displayValue}
            onChange={(event) => onInputChange(event.target.value)}
            disabled={disabled}
            />
          </Card>
          
          {options().length > 0 && (
            <ComboboxOptions className="absolute left-0 z-[100] mt-1 w-full max-h-60 overflow-auto rounded-md bg-white py-1 text-sm shadow-lg ring-1 ring-black/5 dark:bg-slate-800 dark:ring-slate-700 hide-scrollbar">
              {options().map((option) => (
                <ComboboxOption 
                key={option.value} 
                value={option}
                className={clsx(
                  // General styling
                  "cursor-default select-none py-2 px-4",
                  
                  // Hover and active states
                  "hover:bg-blue-50/80 ui-active:bg-blue-50/80 ui-active:text-blue-900",
                  
                  // Dark mode
                  "dark:text-slate-300 dark:hover:bg-slate-700 dark:ui-active:bg-slate-700 dark:ui-active:text-blue-200"
                )}
                >
                  {option.label}
                </ComboboxOption>
              ))}
            </ComboboxOptions>
          )}
          
          {options().length === 0 && inputRef.current?.value && (
            <div className="absolute left-0 z-[100] mt-1 w-full rounded-md bg-white dark:bg-slate-800 py-2 px-4 text-sm shadow-lg ring-1 ring-black/5 dark:ring-slate-700">
              <div className="text-slate-500 dark:text-slate-400">
                {emptyMessage}
              </div>
            </div>
          )}
        </>
      )}
    </HeadlessCombobox>
  );
}