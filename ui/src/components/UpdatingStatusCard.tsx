import { CheckCircleIcon } from "@heroicons/react/24/solid"; // adjust import if you use a different icon set

import LoadingSpinner from "@components/LoadingSpinner"; // adjust import path if needed
import { m } from "@localizations/messages.js";

export interface UpdatePart {
  pending: boolean;
  status: string;
  progress: number;
  complete: boolean;
}

export default function UpdatingStatusCard({ label, part }: { label: string; part: UpdatePart }) {
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <p className="text-sm font-semibold text-black dark:text-white">{label}</p>
        {part.progress < 100 ? (
          <LoadingSpinner className="h-4 w-4 text-blue-700 dark:text-blue-500" />
        ) : (
          <CheckCircleIcon className="h-4 w-4 text-blue-700 dark:text-blue-500" />
        )}
      </div>
      <div
        className="h-2.5 w-full overflow-hidden rounded-full bg-slate-300 dark:bg-slate-600"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(part.progress)}
        aria-label={m.general_update_status_progress({ part: label })}
      >
        <div
          className="h-2.5 rounded-full bg-blue-700 transition-all duration-500 ease-linear dark:bg-blue-500"
          style={{ width: `${part.progress}%` }}
        />
      </div>
      <div className="flex justify-between text-sm text-slate-600 dark:text-slate-300">
        <span>{part.status}</span>
        {part.progress < 100 ? <span>{`${Math.round(part.progress)}%`}</span> : null}
      </div>
    </div>
  );
}
