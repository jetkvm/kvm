import { useEffect, useState } from "react";
import { XMarkIcon, UserIcon, GlobeAltIcon, ComputerDesktopIcon } from "@heroicons/react/20/solid";

import { Button } from "./Button";

type RequestType = "session_approval" | "primary_control";

interface UnifiedSessionRequest {
  id: string; // sessionId or requestId
  type: RequestType;
  source: "local" | "cloud" | string; // Allow string for IP addresses
  identity?: string;
  nickname?: string;
}

interface UnifiedSessionRequestDialogProps {
  request: UnifiedSessionRequest | null;
  onApprove: (id: string) => void | Promise<void>;
  onDeny: (id: string) => void | Promise<void>;
  onClose: () => void;
}

export default function UnifiedSessionRequestDialog({
  request,
  onApprove,
  onDeny,
  onClose
}: UnifiedSessionRequestDialogProps) {
  const [timeRemaining, setTimeRemaining] = useState(0);
  const [isProcessing, setIsProcessing] = useState(false);
  const [hasTimedOut, setHasTimedOut] = useState(false);

  useEffect(() => {
    if (!request) return;

    const isSessionApproval = request.type === "session_approval";
    const initialTime = isSessionApproval ? 60 : 0; // 60s for session approval, no timeout for primary control

    setTimeRemaining(initialTime);
    setIsProcessing(false);
    setHasTimedOut(false);

    // Only start timer for session approval requests
    if (isSessionApproval) {
      const timer = setInterval(() => {
        setTimeRemaining(prev => {
          const newTime = prev - 1;
          if (newTime <= 0) {
            clearInterval(timer);
            setHasTimedOut(true);
            return 0;
          }
          return newTime;
        });
      }, 1000);

      return () => clearInterval(timer);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [request?.id, request?.type]); // Only depend on stable properties to avoid unnecessary re-renders

  // Handle auto-deny when timeout occurs
  useEffect(() => {
    if (hasTimedOut && !isProcessing && request) {
      setIsProcessing(true);
      Promise.resolve(onDeny(request.id))
        .catch(error => {
          console.error("Failed to auto-deny request:", error);
        })
        .finally(() => {
          onClose();
        });
    }
  }, [hasTimedOut, isProcessing, request, onDeny, onClose]);

  if (!request) return null;

  const isSessionApproval = request.type === "session_approval";
  const isPrimaryControl = request.type === "primary_control";

  // Determine if source is cloud, local, or IP address
  const getSourceInfo = () => {
    if (request.source === "cloud") {
      return {
        type: "cloud",
        label: "Cloud Session",
        icon: GlobeAltIcon,
        iconColor: "text-blue-500"
      };
    } else if (request.source === "local") {
      return {
        type: "local",
        label: "Local Session",
        icon: ComputerDesktopIcon,
        iconColor: "text-green-500"
      };
    } else {
      // Assume it's an IP address or hostname
      return {
        type: "ip",
        label: request.source,
        icon: ComputerDesktopIcon,
        iconColor: "text-green-500"
      };
    }
  };

  const sourceInfo = getSourceInfo();

  const getTitle = () => {
    if (isSessionApproval) return "New Session Request";
    if (isPrimaryControl) return "Primary Control Request";
    return "Session Request";
  };

  const getDescription = () => {
    if (isSessionApproval) return "A new session is attempting to connect to this device:";
    if (isPrimaryControl) return "A user is requesting primary control of this session:";
    return "A user is making a request:";
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-white dark:bg-slate-800 rounded-lg shadow-xl max-w-md w-full mx-4">
        <div className="flex items-center justify-between p-4 border-b dark:border-slate-700">
          <h3 className="text-lg font-semibold text-slate-900 dark:text-white">
            {getTitle()}
          </h3>
          <button
            onClick={onClose}
            className="text-slate-400 hover:text-slate-600 dark:hover:text-slate-300"
          >
            <XMarkIcon className="h-5 w-5" />
          </button>
        </div>

        <div className="p-4 space-y-4">
          <p className="text-slate-700 dark:text-slate-300">
            {getDescription()}
          </p>

          <div className="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-3 space-y-2">
            {/* Session type - always show with icon for both session approval and primary control */}
            <div className="flex items-center gap-2">
              <sourceInfo.icon className={`h-5 w-5 ${sourceInfo.iconColor}`} />
              <span className="text-sm font-medium text-slate-900 dark:text-white">
                {sourceInfo.type === "cloud" ? "Cloud Session" :
                 sourceInfo.type === "local" ? "Local Session" :
                 `Local Session`}
              </span>
              {sourceInfo.type === "ip" && (
                <span className="text-sm text-slate-600 dark:text-slate-400">
                  ({sourceInfo.label})
                </span>
              )}
            </div>

            {/* Nickname - always show with icon for consistency */}
            {request.nickname && (
              <div className="flex items-center gap-2">
                <UserIcon className="h-5 w-5 text-slate-400" />
                <span className="text-sm text-slate-700 dark:text-slate-300">
                  <span className="font-medium text-slate-600 dark:text-slate-400">Nickname:</span>{" "}
                  <span className="font-medium text-slate-900 dark:text-white">{request.nickname}</span>
                </span>
              </div>
            )}

            {/* Identity/User */}
            {request.identity && (
              <div className={`text-sm ${isSessionApproval ? 'text-slate-600 dark:text-slate-400' : ''}`}>
                {isSessionApproval ? (
                  <p>Identity: {request.identity}</p>
                ) : (
                  <p>
                    <span className="font-medium text-slate-600 dark:text-slate-400">User:</span>{" "}
                    <span className="text-slate-900 dark:text-white">{request.identity}</span>
                  </p>
                )}
              </div>
            )}
          </div>

          {/* Security Note - only for session approval */}
          {isSessionApproval && (
            <div className="bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg p-3">
              <p className="text-sm text-amber-800 dark:text-amber-300">
                <strong>Security Note:</strong> Only approve sessions you recognize.
                Approved sessions will have observer access and can request primary control.
              </p>
            </div>
          )}

          {/* Auto-deny timer - only for session approval */}
          {isSessionApproval && (
            <div className="text-center">
              <p className="text-sm text-slate-500 dark:text-slate-400">
                Auto-deny in <span className="font-mono font-bold">{timeRemaining}</span> seconds
              </p>
            </div>
          )}

          <div className="flex gap-3">
            <Button
              onClick={async () => {
                if (isProcessing) return;
                setIsProcessing(true);
                try {
                  await onApprove(request.id);
                  onClose();
                } catch (error) {
                  console.error("Failed to approve request:", error);
                  setIsProcessing(false);
                }
              }}
              theme="primary"
              size="MD"
              text="Approve"
              fullWidth
              disabled={isProcessing}
            />
            <Button
              onClick={async () => {
                if (isProcessing) return;
                setIsProcessing(true);
                try {
                  await onDeny(request.id);
                  onClose();
                } catch (error) {
                  console.error("Failed to deny request:", error);
                  setIsProcessing(false);
                }
              }}
              theme="light"
              size="MD"
              text="Deny"
              fullWidth
              disabled={isProcessing}
            />
          </div>
        </div>
      </div>
    </div>
  );
}