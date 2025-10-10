import { useEffect, useCallback, useState } from "react";

import { useSessionStore } from "@/stores/sessionStore";
import { useSessionEvents } from "@/hooks/useSessionEvents";
import { useSettingsStore } from "@/hooks/stores";
import { usePermissions } from "@/hooks/usePermissions";
import { Permission } from "@/types/permissions";

type RpcSendFunction = (method: string, params: Record<string, unknown>, callback: (response: { result?: unknown; error?: { message: string } }) => void) => void;

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

export function useSessionManagement(sendFn: RpcSendFunction | null) {
  const {
    setCurrentSession,
    clearSession
  } = useSessionStore();

  const { hasPermission, isLoading: isLoadingPermissions } = usePermissions();

  const { requireSessionApproval } = useSettingsStore();
  const { handleSessionEvent } = useSessionEvents(sendFn);
  const [primaryControlRequest, setPrimaryControlRequest] = useState<PrimaryControlRequest | null>(null);
  const [newSessionRequest, setNewSessionRequest] = useState<NewSessionRequest | null>(null);

  const handleSessionResponse = useCallback((response: SessionResponse) => {
    if (response.sessionId && response.mode) {
      setCurrentSession(response.sessionId, response.mode as "primary" | "observer" | "queued" | "pending");
    }
  }, [setCurrentSession]);

  const handleApprovePrimaryRequest = useCallback(async (requestId: string) => {
    if (!sendFn) return;

    return new Promise<void>((resolve, reject) => {
      sendFn("approvePrimaryRequest", { requesterID: requestId }, (response: { result?: unknown; error?: { message: string } }) => {
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

  const handleDenyPrimaryRequest = useCallback(async (requestId: string) => {
    if (!sendFn) return;

    return new Promise<void>((resolve, reject) => {
      sendFn("denyPrimaryRequest", { requesterID: requestId }, (response: { result?: unknown; error?: { message: string } }) => {
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

  const handleApproveNewSession = useCallback(async (sessionId: string) => {
    if (!sendFn) return;

    return new Promise<void>((resolve, reject) => {
      sendFn("approveNewSession", { sessionId }, (response: { result?: unknown; error?: { message: string } }) => {
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

  const handleDenyNewSession = useCallback(async (sessionId: string) => {
    if (!sendFn) return;

    return new Promise<void>((resolve, reject) => {
      sendFn("denyNewSession", { sessionId }, (response: { result?: unknown; error?: { message: string } }) => {
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

  const handleRpcEvent = useCallback((method: string, params: unknown) => {
    if (method === "sessionsUpdated" ||
        method === "modeChanged" ||
        method === "connectionModeChanged" ||
        method === "otherSessionConnected") {
      handleSessionEvent(method, params);
    }

    if (method === "newSessionPending" && requireSessionApproval) {
      if (isLoadingPermissions || hasPermission(Permission.SESSION_APPROVE)) {
        setNewSessionRequest(params as NewSessionRequest);
      }
    }

    if (method === "primaryControlRequested") {
      setPrimaryControlRequest(params as PrimaryControlRequest);
    }

    if (method === "primaryControlApproved") {
      const { setRequestingPrimary } = useSessionStore.getState();
      setRequestingPrimary(false);
    }

    if (method === "primaryControlDenied") {
      const { setRequestingPrimary, setSessionError } = useSessionStore.getState();
      setRequestingPrimary(false);
      setSessionError("Your primary control request was denied");
    }

    if (method === "sessionAccessDenied") {
      const { setSessionError } = useSessionStore.getState();
      const errorParams = params as { message?: string };
      setSessionError(errorParams.message || "Session access was denied by the primary session");
    }
  }, [handleSessionEvent, hasPermission, isLoadingPermissions, requireSessionApproval]);

  useEffect(() => {
    if (!isLoadingPermissions && newSessionRequest && !hasPermission(Permission.SESSION_APPROVE)) {
      setNewSessionRequest(null);
    }
  }, [isLoadingPermissions, hasPermission, newSessionRequest]);

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