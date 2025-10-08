import toast, { Toast, Toaster, useToasterStore } from "react-hot-toast";
import React, { useEffect } from "react";
import {
  CheckCircleIcon,
  XCircleIcon,
  InformationCircleIcon,
  ExclamationTriangleIcon
} from "@heroicons/react/20/solid";

import Card from "@/components/Card";


interface NotificationOptions {
  duration?: number;
  // Add other options as needed
}

const ToastContent = ({
  icon,
  message,
  t,
}: {
  icon: React.ReactNode;
  message: string;
  t: Toast;
}) => (
  <Card
    className={`${
      t.visible ? "animate-enter" : "animate-leave"
    } pointer-events-auto z-30 w-full max-w-sm shadow-xl!`}
  >
    <div className="flex items-center gap-x-2 p-2.5 px-2">
      {icon}
      <p className="text-[14px] font-medium text-gray-900 dark:text-gray-100">{message}</p>
    </div>
  </Card>
);

const notifications = {
  success: (message: string, options?: NotificationOptions) => {
    return toast.custom(
      t => (
        <ToastContent
          icon={<CheckCircleIcon className="w-5 h-5 text-green-500 dark:text-green-400" />}
          message={message}
          t={t}
        />
      ),
      { duration: 2000, ...options },
    );
  },

  error: (message: string, options?: NotificationOptions) => {
    return toast.custom(
      t => (
        <ToastContent
          icon={<XCircleIcon className="w-5 h-5 text-red-500 dark:text-red-400" />}
          message={message}
          t={t}
        />
      ),
      { duration: 2000, ...options },
    );
  },

  info: (message: string, options?: NotificationOptions) => {
    return toast.custom(
      t => (
        <ToastContent
          icon={<InformationCircleIcon className="w-5 h-5 text-blue-500 dark:text-blue-400" />}
          message={message}
          t={t}
        />
      ),
      { duration: 2000, ...options },
    );
  },

  warning: (message: string, options?: NotificationOptions) => {
    return toast.custom(
      t => (
        <ToastContent
          icon={<ExclamationTriangleIcon className="w-5 h-5 text-yellow-500 dark:text-yellow-400" />}
          message={message}
          t={t}
        />
      ),
      { duration: 3000, ...options },
    );
  },
};

function useMaxToasts(max: number) {
  const { toasts } = useToasterStore();

  useEffect(() => {
    toasts
      .filter(t => t.visible) // Only consider visible toasts
      .filter((_, i) => i >= max) // Is toast index over limit?
      .forEach(t => toast.dismiss(t.id)); // Dismiss – Use toast.remove(t.id) for no exit animation
  }, [toasts, max]);
}

export function Notifications({
  max = 10,
  ...props
}: React.ComponentProps<typeof Toaster> & {
  max?: number;
}) {
  useMaxToasts(max);

  return <Toaster {...props} />;
}

// eslint-disable-next-line react-refresh/only-export-components
export const notify = {
  success: notifications.success,
  error: notifications.error,
  info: notifications.info,
  warning: notifications.warning,
};

// eslint-disable-next-line react-refresh/only-export-components
export default Object.assign(Notifications, notify);
