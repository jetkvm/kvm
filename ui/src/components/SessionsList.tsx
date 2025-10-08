import { PencilIcon, CheckIcon, XMarkIcon } from "@heroicons/react/20/solid";
import clsx from "clsx";
import { formatters } from "@/utils";
import { usePermissions, Permission } from "@/hooks/usePermissions";

interface Session {
  id: string;
  mode: string;
  nickname?: string;
  identity?: string;
  source?: string;
  createdAt?: string;
}

interface SessionsListProps {
  sessions: Session[];
  currentSessionId?: string;
  onEditNickname?: (sessionId: string) => void;
  onApprove?: (sessionId: string) => void;
  onDeny?: (sessionId: string) => void;
  onTransfer?: (sessionId: string) => void;
  formatDuration?: (createdAt: string) => string;
}

export default function SessionsList({
  sessions,
  currentSessionId,
  onEditNickname,
  onApprove,
  onDeny,
  onTransfer,
  formatDuration = (createdAt: string) => formatters.timeAgo(new Date(createdAt)) || ""
}: SessionsListProps) {
  const { hasPermission } = usePermissions();
  return (
    <div className="space-y-2">
      {sessions.map(session => (
        <div
          key={session.id}
          className={clsx(
            "p-2 rounded-md border text-xs",
            session.id === currentSessionId
              ? "border-blue-500 bg-blue-50 dark:bg-blue-900/10"
              : session.mode === "pending"
              ? "border-orange-300 dark:border-orange-800/50 bg-orange-50/50 dark:bg-orange-900/10"
              : "border-slate-200 dark:border-slate-700"
          )}
        >
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <SessionModeBadge mode={session.mode} />
              {session.id === currentSessionId && (
                <span className="text-blue-600 dark:text-blue-400 font-medium">(You)</span>
              )}
            </div>
            <div className="flex items-center gap-2">
              <span className="text-slate-500 dark:text-slate-400">
                {session.createdAt ? formatDuration(session.createdAt) : ""}
              </span>
              {/* Show approve/deny for pending sessions if user has permission */}
              {session.mode === "pending" && hasPermission(Permission.SESSION_APPROVE) && onApprove && onDeny && (
                <div className="flex items-center gap-1">
                  <button
                    onClick={() => onApprove(session.id)}
                    className="p-1 hover:bg-green-100 dark:hover:bg-green-900/30 rounded transition-colors"
                    title="Approve session"
                  >
                    <CheckIcon className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  </button>
                  <button
                    onClick={() => onDeny(session.id)}
                    className="p-1 hover:bg-red-100 dark:hover:bg-red-900/30 rounded transition-colors"
                    title="Deny session"
                  >
                    <XMarkIcon className="h-3.5 w-3.5 text-red-600 dark:text-red-400" />
                  </button>
                </div>
              )}
              {/* Show Transfer button if user has permission to transfer */}
              {hasPermission(Permission.SESSION_TRANSFER) && session.mode === "observer" && session.id !== currentSessionId && onTransfer && (
                <button
                  onClick={() => onTransfer(session.id)}
                  className="px-2 py-0.5 text-xs font-medium rounded bg-blue-100 hover:bg-blue-200 dark:bg-blue-900/30 dark:hover:bg-blue-900/50 text-blue-700 dark:text-blue-400 transition-colors"
                  title="Transfer primary control"
                >
                  Transfer
                </button>
              )}
              {/* Allow users with session manage permission to edit any nickname, or anyone to edit their own */}
              {onEditNickname && (hasPermission(Permission.SESSION_MANAGE) || session.id === currentSessionId) && (
                <button
                  onClick={() => onEditNickname(session.id)}
                  className="p-1 hover:bg-slate-100 dark:hover:bg-slate-700 rounded transition-colors"
                  title="Edit nickname"
                >
                  <PencilIcon className="h-3 w-3 text-slate-500 dark:text-slate-400" />
                </button>
              )}
            </div>
          </div>

          <div className="mt-1 space-y-1">
            {session.nickname && (
              <p className="text-slate-700 dark:text-slate-200 font-medium">
                {session.nickname}
              </p>
            )}
            {session.identity && (
              <p className="text-slate-600 dark:text-slate-300 text-xs">
                {session.source === "cloud" ? "☁️ " : ""}{session.identity}
              </p>
            )}
            {session.mode === "pending" && (
              <p className="text-orange-600 dark:text-orange-400 text-xs italic">
                Awaiting approval
              </p>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

export function SessionModeBadge({ mode }: { mode: string }) {
  const getBadgeStyle = () => {
    switch (mode) {
      case "primary":
        return "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400";
      case "observer":
        return "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400";
      case "queued":
        return "bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400";
      case "pending":
        return "bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400";
      default:
        return "bg-slate-100 text-slate-700 dark:bg-slate-900/30 dark:text-slate-400";
    }
  };

  return (
    <span className={clsx(
      "inline-flex items-center px-1.5 py-0.5 text-xs font-medium rounded-full",
      getBadgeStyle()
    )}>
      {mode}
    </span>
  );
}