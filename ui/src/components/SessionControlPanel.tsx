import {
  LockClosedIcon,
  LockOpenIcon,
  ClockIcon
} from "@heroicons/react/16/solid";
import clsx from "clsx";

import { useSessionStore } from "@/stores/sessionStore";
import { sessionApi } from "@/api/sessionApi";
import { Button } from "@/components/Button";
import { usePermissions, Permission } from "@/hooks/usePermissions";

type RpcSendFunction = (method: string, params: Record<string, unknown>, callback: (response: { result?: unknown; error?: { message: string } }) => void) => void;

interface SessionControlPanelProps {
  sendFn: RpcSendFunction;
  className?: string;
}

export default function SessionControlPanel({ sendFn, className }: SessionControlPanelProps) {
  const {
    currentSessionId,
    currentMode,
    sessions,
    isRequestingPrimary,
    setRequestingPrimary,
    setSessionError,
    canRequestPrimary
  } = useSessionStore();
  const { hasPermission } = usePermissions();


  const handleRequestPrimary = async () => {
    if (!currentSessionId || isRequestingPrimary) return;

    setRequestingPrimary(true);
    setSessionError(null);

    try {
      const result = await sessionApi.requestPrimary(sendFn, currentSessionId);

      if (result.status === "success") {
        if (result.mode === "primary") {
          // Immediately became primary
          setRequestingPrimary(false);
        } else if (result.mode === "queued") {
          // Request sent, waiting for approval
          // Keep isRequestingPrimary true to show waiting state
        }
      } else if (result.status === "error") {
        setSessionError(result.message || "Failed to request primary control");
        setRequestingPrimary(false);
      }
    } catch (error) {
      setSessionError(error instanceof Error ? error.message : "Unknown error");
      console.error("Failed to request primary control:", error);
      setRequestingPrimary(false);
    }
  };

  const handleReleasePrimary = async () => {
    if (!currentSessionId || currentMode !== "primary") return;

    try {
      await sessionApi.releasePrimary(sendFn, currentSessionId);
    } catch (error) {
      setSessionError(error instanceof Error ? error.message : "Unknown error");
      console.error("Failed to release primary control:", error);
    }
  };

  const canReleasePrimary = () => {
    const otherEligibleSessions = sessions.filter(
      s => s.id !== currentSessionId && (s.mode === "observer" || s.mode === "queued")
    );
    return otherEligibleSessions.length > 0;
  };


  return (
    <div className={clsx("space-y-4", className)}>
      {/* Current session controls */}
      <div className="space-y-3">
        <h3 className="text-sm font-semibold text-slate-900 dark:text-white">
          Session Control
        </h3>

        {hasPermission(Permission.SESSION_RELEASE_PRIMARY) && (
          <div>
            <Button
              size="MD"
              theme="light"
              text="Release Primary Control"
              onClick={handleReleasePrimary}
              disabled={!canReleasePrimary()}
              LeadingIcon={LockOpenIcon}
              fullWidth
            />
            {!canReleasePrimary() && (
              <p className="mt-2 text-xs text-slate-500 dark:text-slate-400">
                Cannot release control - no other sessions available to take primary
              </p>
            )}
          </div>
        )}

        {hasPermission(Permission.SESSION_REQUEST_PRIMARY) && (
          <>
            {isRequestingPrimary ? (
              <div className="flex items-center gap-2 p-3 rounded-lg bg-blue-50 dark:bg-blue-900/20">
                <ClockIcon className="h-5 w-5 text-blue-600 dark:text-blue-400 animate-pulse" />
                <span className="text-sm text-blue-700 dark:text-blue-300">
                  Waiting for approval from primary session...
                </span>
              </div>
            ) : (
              <Button
                size="MD"
                theme="primary"
                text="Request Primary Control"
                onClick={handleRequestPrimary}
                disabled={!canRequestPrimary()}
                LeadingIcon={LockClosedIcon}
                fullWidth
              />
            )}
          </>
        )}

        {currentMode === "queued" && (
          <div className="flex items-center gap-2 p-3 rounded-lg bg-yellow-50 dark:bg-yellow-900/20">
            <ClockIcon className="h-5 w-5 text-yellow-600 dark:text-yellow-400" />
            <span className="text-sm text-yellow-700 dark:text-yellow-300">
              Waiting for primary control...
            </span>
          </div>
        )}
      </div>

    </div>
  );
}