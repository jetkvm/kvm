import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { XCircleIcon } from "@heroicons/react/24/outline";
import { Button } from "./Button";
import { DEVICE_API, CLOUD_API } from "@/ui.config";
import { isOnDevice } from "@/main";
import { useUserStore } from "@/hooks/stores";
import { useSessionStore, useSharedSessionStore } from "@/stores/sessionStore";
import api from "@/api";

interface AccessDeniedOverlayProps {
  show: boolean;
  message?: string;
  onRetry?: () => void;
}

export default function AccessDeniedOverlay({
  show,
  message = "Your session access was denied",
  onRetry
}: AccessDeniedOverlayProps) {
  const navigate = useNavigate();
  const setUser = useUserStore(state => state.setUser);
  const { clearSession } = useSessionStore();
  const { clearNickname } = useSharedSessionStore();
  const [countdown, setCountdown] = useState(10);

  const handleLogout = async () => {
    try {
      const logoutUrl = isOnDevice ? `${DEVICE_API}/auth/logout` : `${CLOUD_API}/logout`;
      const res = await api.POST(logoutUrl);
      if (!res.ok) {
        console.warn("Logout API call failed, but continuing with local cleanup");
      }
    } catch (error) {
      console.error("Logout API call failed:", error);
    }

    // Always clear local state and navigate, regardless of API call result
    setUser(null);
    clearSession();
    clearNickname();
    navigate("/");
  };

  useEffect(() => {
    if (!show) return;

    const timer = setInterval(() => {
      setCountdown(prev => {
        if (prev <= 1) {
          clearInterval(timer);
          // Auto-redirect with proper logout
          handleLogout();
          return 0;
        }
        return prev - 1;
      });
    }, 1000);

    return () => clearInterval(timer);
  }, [show]);

  if (!show) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80">
      <div className="max-w-md w-full mx-4 bg-white dark:bg-slate-800 rounded-lg shadow-xl p-6 space-y-4">
        <div className="flex items-center gap-3">
          <XCircleIcon className="h-8 w-8 text-red-500 flex-shrink-0" />
          <div>
            <h3 className="text-lg font-semibold text-slate-900 dark:text-white">
              Access Denied
            </h3>
            <p className="text-sm text-slate-600 dark:text-slate-400">
              {message}
            </p>
          </div>
        </div>

        <div className="space-y-3">
          <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-3">
            <p className="text-sm text-red-800 dark:text-red-300">
              The primary session has denied your access request. This could be for security reasons
              or because the session is restricted.
            </p>
          </div>

          <p className="text-center text-sm text-slate-500 dark:text-slate-400">
            Redirecting in <span className="font-mono font-bold">{countdown}</span> seconds...
          </p>

          <div className="flex gap-3">
            {onRetry && (
              <Button
                onClick={onRetry}
                theme="primary"
                size="MD"
                text="Try Again"
                fullWidth
              />
            )}
            <Button
              onClick={() => {
                handleLogout();
              }}
              theme="light"
              size="MD"
              text="Back to Login"
              fullWidth
            />
          </div>
        </div>
      </div>
    </div>
  );
}