import { useEffect, useRef } from "react";

import { useSessionStore, SessionInfo } from "@/stores/sessionStore";
import { useRTCStore } from "@/hooks/stores";
import { sessionApi } from "@/api/sessionApi";
import { notify } from "@/notifications";

type RpcSendFunction = (method: string, params: Record<string, unknown>, callback: (response: { result?: unknown; error?: { message: string } }) => void) => void;

interface SessionEventData {
  sessions: SessionInfo[];
  yourMode: string;
}

interface ModeChangedData {
  mode: string;
}

interface ConnectionModeChangedData {
  newMode: string;
}

export function useSessionEvents(sendFn: RpcSendFunction | null) {
  const {
    currentMode,
    setSessions,
    updateSessionMode,
    setSessionError
  } = useSessionStore();

  const sendFnRef = useRef(sendFn);
  sendFnRef.current = sendFn;

  const handleSessionEvent = (method: string, params: unknown) => {
    switch (method) {
      case "sessionsUpdated":
        handleSessionsUpdated(params as SessionEventData);
        break;
      case "modeChanged":
        handleModeChanged(params as ModeChangedData);
        break;
      case "connectionModeChanged":
        handleConnectionModeChanged(params as ConnectionModeChangedData);
        break;
      case "hidReadyForPrimary":
        handleHidReadyForPrimary();
        break;
      case "otherSessionConnected":
        handleOtherSessionConnected();
        break;
      default:
        break;
    }
  };

  const handleSessionsUpdated = (data: SessionEventData) => {
    if (data.sessions) {
      setSessions(data.sessions);
    }

    // CRITICAL: Only update mode, never show notifications from sessionsUpdated
    // Notifications are exclusively handled by handleModeChanged to prevent duplicates
    if (data.yourMode && data.yourMode !== currentMode) {
      updateSessionMode(data.yourMode as "primary" | "observer" | "queued" | "pending");
    }
  };

  // Debounce notifications to prevent rapid-fire duplicates
  const lastNotificationRef = useRef<{mode: string, timestamp: number}>({mode: "", timestamp: 0});

  const handleModeChanged = (data: ModeChangedData) => {
    if (data.mode) {
      // Get the most current mode from the store to avoid race conditions
      const { currentMode: currentModeFromStore } = useSessionStore.getState();
      const previousMode = currentModeFromStore;
      updateSessionMode(data.mode as "primary" | "observer" | "queued" | "pending");

      if (previousMode === "queued" && data.mode !== "queued") {
        const { setRequestingPrimary } = useSessionStore.getState();
        setRequestingPrimary(false);
      }

      if (previousMode === "pending" && data.mode === "observer") {
        const { resetRejectionCount } = useSessionStore.getState();
        resetRejectionCount();
      }

      // HID re-initialization is now handled automatically by permission changes in usePermissions

      // CRITICAL: Debounce notifications to prevent duplicates from rapid-fire events
      const now = Date.now();
      const lastNotification = lastNotificationRef.current;

      // Only show notification if:
      // 1. Mode actually changed, AND
      // 2. Haven't shown the same notification in the last 2 seconds
      const shouldNotify = previousMode !== data.mode &&
        (lastNotification.mode !== data.mode || now - lastNotification.timestamp > 2000);

      if (shouldNotify) {
        if (data.mode === "primary") {
          notify.success("Primary control granted");
          lastNotificationRef.current = {mode: "primary", timestamp: now};
        } else if (data.mode === "observer" && previousMode === "primary") {
          notify.info("Primary control released");
          lastNotificationRef.current = {mode: "observer", timestamp: now};
        }
      }
    }
  };

  const handleConnectionModeChanged = (data: ConnectionModeChangedData) => {
    if (data.newMode) {
      handleModeChanged({ mode: data.newMode });
    }
  };

  const handleHidReadyForPrimary = () => {
    const { rpcHidChannel } = useRTCStore.getState();
    if (rpcHidChannel?.readyState === "open") {
      rpcHidChannel.dispatchEvent(new Event("open"));
    }
  };

  const handleOtherSessionConnected = () => {
    notify.warning("Another session is connecting", {
      duration: 5000
    });
  };

  useEffect(() => {
    if (!sendFnRef.current) return;

    const fetchSessions = async () => {
      try {
        const sessions = await sessionApi.getSessions(sendFnRef.current!);
        setSessions(sessions);
      } catch (error) {
        console.error("Failed to fetch sessions:", error);
        setSessionError("Failed to fetch session information");
      }
    };

    fetchSessions();
  }, [setSessions, setSessionError]);

  useEffect(() => {
    if (!sendFnRef.current) return;

    const intervalId = setInterval(async () => {
      if (!sendFnRef.current) return;

      try {
        const sessions = await sessionApi.getSessions(sendFnRef.current);
        setSessions(sessions);
      } catch {
        // Silently fail on refresh errors
      }
    }, 30000); // Refresh every 30 seconds

    return () => clearInterval(intervalId);
  }, [setSessions]);

  return {
    handleSessionEvent
  };
}