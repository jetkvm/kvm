import { CloseButton } from "@headlessui/react";
import { LuCircleAlert, LuInfo, LuTriangleAlert } from "react-icons/lu";

import { m } from "@localizations/messages.js";
import { Button } from "@components/Button";
import Modal from "@components/Modal";
import { cx } from "@/cva.config";

type Variant = "danger" | "success" | "warning" | "info";

interface ConfirmDialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  description: React.ReactNode;
  variant?: Variant;
  confirmText?: string;
  cancelText?: string | null;
  onConfirm: () => void;
  isConfirming?: boolean;
}

const variantConfig = {
  danger: {
    icon: LuCircleAlert,
    iconClass: "text-red-600 dark:text-red-400",
    buttonTheme: "danger",
  },
  success: {
    icon: LuCircleAlert,
    iconClass: "text-emerald-600 dark:text-emerald-400",
    buttonTheme: "primary",
  },
  warning: {
    icon: LuTriangleAlert,
    iconClass: "text-amber-600 dark:text-amber-400",
    buttonTheme: "primary",
  },
  info: {
    icon: LuInfo,
    iconClass: "text-slate-700 dark:text-slate-300",
    buttonTheme: "primary",
  },
} as Record<
  Variant,
  {
    icon: React.ElementType;
    iconClass: string;
    buttonTheme: "danger" | "primary" | "blank" | "light" | "lightDanger";
  }
>;

export function ConfirmDialog({
  open,
  onClose,
  title,
  description,
  variant = "info",
  confirmText = m.confirm(),
  cancelText = m.cancel(),
  onConfirm,
  isConfirming = false,
}: ConfirmDialogProps) {
  const { icon: Icon, iconClass, buttonTheme } = variantConfig[variant];

  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.stopPropagation();
      onClose();
    }
  };

  return (
    <div onKeyDown={handleKeyDown}>
      <Modal open={open} onClose={onClose}>
        <div className="mx-auto max-w-md px-4 transition-all duration-300 ease-in-out">
          <div className="pointer-events-auto relative w-full overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm transition-all dark:border-slate-800 dark:bg-slate-900">
            <div className="p-6">
              <div className="flex items-start gap-3.5">
                <Icon aria-hidden="true" className={cx("size-[18px] shrink-0 mt-[2px]", iconClass)} />
                <div className="flex-1 min-w-0 space-y-2">
                  <h2 className="font-semibold text-slate-950 dark:text-white">
                    {title}
                  </h2>
                  <div className="text-sm text-slate-700 dark:text-slate-300">
                    {description}
                  </div>
                </div>
              </div>

              <div className="mt-6 flex justify-end gap-2">
                {cancelText && (
                  <CloseButton as={Button} size="SM" theme="blank" text={cancelText} />
                )}
                <Button
                  size="SM"
                  type="button"
                  theme={buttonTheme}
                  text={isConfirming ? `${confirmText}...` : confirmText}
                  onClick={onConfirm}
                  disabled={isConfirming}
                />
              </div>
            </div>
          </div>
        </div>
      </Modal>
    </div>
  );
}
