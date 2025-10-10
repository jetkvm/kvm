import { useState, useEffect, useRef, useCallback, ReactNode } from "react";

import { useJsonRpc } from "@/hooks/useJsonRpc";
import { useSessionStore } from "@/stores/sessionStore";
import { useRTCStore } from "@/hooks/stores";
import { Permission } from "@/types/permissions";
import { PermissionsContextValue } from "@/hooks/usePermissions";
import { PermissionsContext } from "@/contexts/PermissionsContext";

type RpcSendFunction = (method: string, params: Record<string, unknown>, callback: (response: { result?: unknown; error?: { message: string } }) => void) => void;

interface PermissionsResponse {
  mode: string;
  permissions: Record<string, boolean>;
}

export function PermissionsProvider({ children }: { children: ReactNode }) {
  const { currentMode } = useSessionStore();
  const { setRpcHidProtocolVersion, rpcHidChannel, rpcDataChannel } = useRTCStore();
  const [permissions, setPermissions] = useState<Record<string, boolean>>({});
  const [isLoading, setIsLoading] = useState(true);
  const previousCanControl = useRef<boolean>(false);

  const pollPermissions = useCallback((send: RpcSendFunction) => {
    if (!send) return;

    setIsLoading(true);
    send("getPermissions", {}, (response: { result?: unknown; error?: { message: string } }) => {
      if (!response.error && response.result) {
        const result = response.result as PermissionsResponse;
        setPermissions(result.permissions);
      }
      setIsLoading(false);
    });
  }, []);

  const { send } = useJsonRpc();

  useEffect(() => {
    if (rpcDataChannel?.readyState !== "open") return;
    pollPermissions(send);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentMode, rpcDataChannel?.readyState]);

  const hasPermission = useCallback((permission: Permission): boolean => {
    return permissions[permission] === true;
  }, [permissions]);

  const hasAnyPermission = useCallback((...perms: Permission[]): boolean => {
    return perms.some(perm => hasPermission(perm));
  }, [hasPermission]);

  const hasAllPermissions = useCallback((...perms: Permission[]): boolean => {
    return perms.every(perm => hasPermission(perm));
  }, [hasPermission]);

  useEffect(() => {
    const currentCanControl = hasPermission(Permission.KEYBOARD_INPUT) && hasPermission(Permission.MOUSE_INPUT);
    const hadControl = previousCanControl.current;

    if (currentCanControl && !hadControl && rpcHidChannel?.readyState === "open") {
      console.info("Gained control permissions, re-initializing HID");

      setRpcHidProtocolVersion(null);

      import("@/hooks/hidRpc").then(({ HID_RPC_VERSION, HandshakeMessage }) => {
        setTimeout(() => {
          if (rpcHidChannel?.readyState === "open") {
            const handshakeMessage = new HandshakeMessage(HID_RPC_VERSION);
            try {
              const data = handshakeMessage.marshal();
              rpcHidChannel.send(data as unknown as ArrayBuffer);
              console.info("Sent HID handshake after permission change");
            } catch (e) {
              console.error("Failed to send HID handshake", e);
            }
          }
        }, 100);
      });
    }

    previousCanControl.current = currentCanControl;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [permissions, rpcHidChannel, setRpcHidProtocolVersion]);

  const isPrimary = useCallback(() => currentMode === "primary", [currentMode]);
  const isObserver = useCallback(() => currentMode === "observer", [currentMode]);
  const isPending = useCallback(() => currentMode === "pending", [currentMode]);

  const value: PermissionsContextValue = {
    permissions,
    isLoading,
    hasPermission,
    hasAnyPermission,
    hasAllPermissions,
    isPrimary,
    isObserver,
    isPending,
  };

  return (
    <PermissionsContext.Provider value={value}>
      {children}
    </PermissionsContext.Provider>
  );
}
