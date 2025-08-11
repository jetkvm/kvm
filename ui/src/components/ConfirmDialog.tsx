import { CheckCircleIcon } from "@heroicons/react/24/outline";
import { CloseButton } from "@headlessui/react";
import { LuInfo, LuOctagonAlert, LuTriangleAlert } from "react-icons/lu";

import { Button } from "@/components/Button";
import Modal from "@/components/Modal";
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
    icon: LuOctagonAlert,
    iconClass: "text-red-600",
    iconBgClass: "bg-red-100 border border-red-500/90",
    buttonTheme: "danger",
  },
  success: {
    icon: CheckCircleIcon,
    iconClass: "text-green-600",
    iconBgClass: "bg-green-100 border border-green-500/90",
    buttonTheme: "primary",
  },
  warning: {
    icon: LuTriangleAlert,
    iconClass: "text-yellow-600",
    iconBgClass: "bg-yellow-100 border border-yellow-500/90",
    buttonTheme: "primary",
  },
  info: {
    icon: LuInfo,
    iconClass: "text-blue-600",
    iconBgClass: "bg-blue-100 border border-blue-500/90",
    buttonTheme: "primary",
  },
} as Record<
  Variant,
  {
    icon: React.ElementType;
    iconClass: string;
    iconBgClass: string;
    buttonTheme: "danger" | "primary" | "blank" | "light" | "lightDanger";
  }
>;

export function ConfirmDialog({
  open,
  onClose,
  title,
  description,
  variant = "info",
  confirmText = "Confirm",
  cancelText = "Cancel",
  onConfirm,
  isConfirming = false,
}: ConfirmDialogProps) {
  const { icon: Icon, iconClass, iconBgClass, buttonTheme } = variantConfig[variant];

  return (
    <Modal open={open} onClose={onClose}>
      <div className="mx-auto max-w-xl px-4 transition-all duration-300 ease-in-out">
        <div className="pointer-events-auto relative w-full overflow-hidden rounded-lg bg-white p-6 text-left align-middle shadow-xl transition-all dark:bg-slate-800">
          <div className="space-y-4">
            <div className="sm:flex sm:items-start">
              <div
                className={cx(
                  "mx-auto flex size-12 shrink-0 items-center justify-center rounded-full sm:mx-0 sm:size-10",
                  iconBgClass,
                )}
              >
                <Icon aria-hidden="true" className={cx("size-6", iconClass)} />
              </div>
              <div className="mt-3 text-center sm:mt-0 sm:ml-4 sm:text-left">
                <h2 className="text-lg leading-tight font-bold text-black dark:text-white">
                  {title}
                </h2>
                <div className="mt-2 text-sm leading-snug text-slate-600 dark:text-slate-400">
                  {description}
                </div>
              </div>
            </div>

            <div className="flex justify-end gap-x-2" autoFocus>
              {cancelText && (
                <CloseButton as={Button} size="SM" theme="blank" text={cancelText} />
              )}
              <Button
                size="SM"
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
  );
}
