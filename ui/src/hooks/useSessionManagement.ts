import { useEffect, useCallback, useState } from "react";
import { useSessionStore } from "@/stores/sessionStore";
import { useSessionEvents } from "@/hooks/useSessionEvents";
import { useSettingsStore } from "@/hooks/stores";
import { usePermissions, Permission } from "@/hooks/usePermissions";

interface SessionResponse {
  sessionId?: string;
  mode?: string;
}

interface PrimaryControlRequest {
  requestId: string;
  identity: string;
  source: string;
  nickname?: string;
}

interface NewSessionRequest {
  sessionId: string;
  source: "local" | "cloud";
  identity?: string;
  nickname?: string;
}

export function useSessionManagement(sendFn: Function | null) {
  const {
    setCurrentSession,
    clearSession
  } = useSessionStore();

  const { hasPermission } = usePermissions();

  const { requireSessionApproval } = useSettingsStore();
  const { handleSessionEvent } = useSessionEvents(sendFn);
  const [primaryControlRequest, setPrimaryControlRequest] = useState<PrimaryControlRequest | null>(null);
  const [newSessionRequest, setNewSessionRequest] = useState<NewSessionRequest | null>(null);

  // Handle session info from WebRTC answer
  const handleSessionResponse = useCallback((response: SessionResponse) => {
    if (response.sessionId && response.mode) {
      setCurrentSession(response.sessionId, response.mode as any);
    }
  }, [setCurrentSession]);

  // Handle approval of primary control request
  const handleApprovePrimaryRequest = useCallback(async (requestId: string) => {
    if (!sendFn) return;

    return new Promise<void>((resolve, reject) => {
      sendFn("approvePrimaryRequest", { requesterID: requestId }, (response: any) => {
        if (response.error) {
          console.error("Failed to approve primary request:", response.error);
          reject(new Error(response.error.message || "Failed to approve"));
        } else {
          setPrimaryControlRequest(null);
          resolve();
        }
      });
    });
  }, [sendFn]);

  // Handle denial of primary control request
  const handleDenyPrimaryRequest = useCallback(async (requestId: string) => {
    if (!sendFn) return;

    return new Promise<void>((resolve, reject) => {
      sendFn("denyPrimaryRequest", { requesterID: requestId }, (response: any) => {
        if (response.error) {
          console.error("Failed to deny primary request:", response.error);
          reject(new Error(response.error.message || "Failed to deny"));
        } else {
          setPrimaryControlRequest(null);
          resolve();
        }
      });
    });
  }, [sendFn]);

  // Handle approval of new session
  const handleApproveNewSession = useCallback(async (sessionId: string) => {
    if (!sendFn) return;

    return new Promise<void>((resolve, reject) => {
      sendFn("approveNewSession", { sessionId }, (response: any) => {
        if (response.error) {
          console.error("Failed to approve new session:", response.error);
          reject(new Error(response.error.message || "Failed to approve"));
        } else {
          setNewSessionRequest(null);
          resolve();
        }
      });
    });
  }, [sendFn]);

  // Handle denial of new session
  const handleDenyNewSession = useCallback(async (sessionId: string) => {
    if (!sendFn) return;

    return new Promise<void>((resolve, reject) => {
      sendFn("denyNewSession", { sessionId }, (response: any) => {
        if (response.error) {
          console.error("Failed to deny new session:", response.error);
          reject(new Error(response.error.message || "Failed to deny"));
        } else {
          setNewSessionRequest(null);
          resolve();
        }
      });
    });
  }, [sendFn]);

  // Handle RPC events
  const handleRpcEvent = useCallback((method: string, params: any) => {
    // Pass session events to the session event handler
    if (method === "sessionsUpdated" ||
        method === "modeChanged" ||
        method === "otherSessionConnected") {
      handleSessionEvent(method, params);
    }

    // Handle new session approval request (only if approval is required and user has permission)
    if (method === "newSessionPending" && requireSessionApproval && hasPermission(Permission.SESSION_APPROVE)) {
      setNewSessionRequest(params);
    }

    // Handle primary control request
    if (method === "primaryControlRequested") {
      setPrimaryControlRequest(params);
    }

    // Handle approval/denial responses
    if (method === "primaryControlApproved") {
      // Clear requesting state in store
      const { setRequestingPrimary } = useSessionStore.getState();
      setRequestingPrimary(false);
    }

    if (method === "primaryControlDenied") {
      // Clear requesting state and show error
      const { setRequestingPrimary, setSessionError } = useSessionStore.getState();
      setRequestingPrimary(false);
      setSessionError("Your primary control request was denied");
    }

    // Handle session access denial (when your new session is denied)
    if (method === "sessionAccessDenied") {
      const { clearSession, setSessionError } = useSessionStore.getState();
      setSessionError(params.message || "Session access was denied by the primary session");
      // Clear session data as we're being disconnected
      setTimeout(() => {
        clearSession();
      }, 3000); // Give user time to see the error
    }
  }, [handleSessionEvent]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      clearSession();
    };
  }, [clearSession]);

  return {
    handleSessionResponse,
    handleRpcEvent,
    primaryControlRequest,
    handleApprovePrimaryRequest,
    handleDenyPrimaryRequest,
    closePrimaryControlRequest: () => setPrimaryControlRequest(null),
    newSessionRequest,
    handleApproveNewSession,
    handleDenyNewSession,
    closeNewSessionRequest: () => setNewSessionRequest(null)
  };
}