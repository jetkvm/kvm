import { useEffect, useState } from "react";
import { ClockIcon } from "@heroicons/react/24/outline";

interface PendingApprovalOverlayProps {
  show: boolean;
}

export default function PendingApprovalOverlay({ show }: PendingApprovalOverlayProps) {
  const [dots, setDots] = useState("");

  useEffect(() => {
    if (!show) return;

    const timer = setInterval(() => {
      setDots(prev => (prev.length >= 3 ? "" : prev + "."));
    }, 500);

    return () => clearInterval(timer);
  }, [show]);

  if (!show) return null;

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/60">
      <div className="max-w-md w-full mx-4 bg-white dark:bg-slate-800 rounded-lg shadow-xl p-6 space-y-4">
        <div className="flex flex-col items-center space-y-4">
          <ClockIcon className="h-12 w-12 text-amber-500 animate-pulse" />

          <div className="text-center space-y-2">
            <h3 className="text-lg font-semibold text-slate-900 dark:text-white">
              Awaiting Approval{dots}
            </h3>
            <p className="text-sm text-slate-600 dark:text-slate-400">
              Your session is pending approval from the primary session
            </p>
          </div>

          <div className="bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg p-3 w-full">
            <p className="text-sm text-amber-800 dark:text-amber-300 text-center">
              The primary user will receive a notification to approve or deny your access.
              This typically takes less than 30 seconds.
            </p>
          </div>

          <div className="flex items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
            <div className="h-2 w-2 bg-amber-500 rounded-full animate-pulse" />
            <span>Waiting for response from primary session</span>
          </div>
        </div>
      </div>
    </div>
  );
}