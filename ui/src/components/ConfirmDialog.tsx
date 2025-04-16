import {
  ExclamationTriangleIcon,
  CheckCircleIcon,
  InformationCircleIcon,
} from "@heroicons/react/24/outline";

import { cx } from "@/cva.config";
import { Button } from "@/components/Button";
import Modal from "@/components/Modal";

type Variant = "danger" | "success" | "warning" | "info";

interface ConfirmDialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  description: string;
  variant?: Variant;
  confirmText?: string;
  cancelText?: string | null;
  onConfirm: () => void;
  isConfirming?: boolean;
}

const variantConfig = {
  danger: {
    icon: ExclamationTriangleIcon,
    iconClass: "text-red-600",
    iconBgClass: "bg-red-100",
    buttonTheme: "danger",
  },
  success: {
    icon: CheckCircleIcon,
    iconClass: "text-green-600",
    iconBgClass: "bg-green-100",
    buttonTheme: "primary",
  },
  warning: {
    icon: ExclamationTriangleIcon,
    iconClass: "text-yellow-600",
    iconBgClass: "bg-yellow-100",
    buttonTheme: "lightDanger",
  },
  info: {
    icon: InformationCircleIcon,
    iconClass: "text-blue-600",
    iconBgClass: "bg-blue-100",
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
              <div className="mt-3 text-center sm:ml-4 sm:mt-0 sm:text-left">
                <h2 className="text-lg font-bold leading-tight text-black dark:text-white">
                  {title}
                </h2>
                <div className="mt-2 text-sm leading-snug text-slate-600 dark:text-slate-400">
                  {description}
                </div>
              </div>
            </div>

            <div className="flex justify-end gap-x-2">
              {cancelText && (
                <Button size="SM" theme="blank" text={cancelText} onClick={onClose} />
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
