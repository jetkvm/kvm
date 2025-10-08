import { useState, useEffect, useRef, useCallback } from "react";

import { useJsonRpc, JsonRpcRequest } from "@/hooks/useJsonRpc";
import { useSessionStore } from "@/stores/sessionStore";
import { useRTCStore } from "@/hooks/stores";

type RpcSendFunction = (method: string, params: Record<string, unknown>, callback: (response: { result?: unknown; error?: { message: string } }) => void) => void;

// Permission types matching backend
export enum Permission {
  // Video/Display permissions
  VIDEO_VIEW = "video.view",

  // Input permissions
  KEYBOARD_INPUT = "keyboard.input",
  MOUSE_INPUT = "mouse.input",
  PASTE = "clipboard.paste",

  // Session management permissions
  SESSION_TRANSFER = "session.transfer",
  SESSION_APPROVE = "session.approve",
  SESSION_KICK = "session.kick",
  SESSION_REQUEST_PRIMARY = "session.request_primary",
  SESSION_RELEASE_PRIMARY = "session.release_primary",
  SESSION_MANAGE = "session.manage",

  // Mount/Media permissions
  MOUNT_MEDIA = "mount.media",
  UNMOUNT_MEDIA = "mount.unmedia",
  MOUNT_LIST = "mount.list",

  // Extension permissions
  EXTENSION_MANAGE = "extension.manage",
  EXTENSION_ATX = "extension.atx",
  EXTENSION_DC = "extension.dc",
  EXTENSION_SERIAL = "extension.serial",
  EXTENSION_WOL = "extension.wol",

  // Settings permissions
  SETTINGS_READ = "settings.read",
  SETTINGS_WRITE = "settings.write",
  SETTINGS_ACCESS = "settings.access",

  // System permissions
  SYSTEM_REBOOT = "system.reboot",
  SYSTEM_UPDATE = "system.update",
  SYSTEM_NETWORK = "system.network",

  // Power/USB control permissions
  POWER_CONTROL = "power.control",
  USB_CONTROL = "usb.control",

  // Terminal/Serial permissions
  TERMINAL_ACCESS = "terminal.access",
  SERIAL_ACCESS = "serial.access",
}

interface PermissionsResponse {
  mode: string;
  permissions: Record<string, boolean>;
}

export function usePermissions() {
  const { currentMode } = useSessionStore();
  const { setRpcHidProtocolVersion, rpcHidChannel } = useRTCStore();
  const [permissions, setPermissions] = useState<Record<string, boolean>>({});
  const [isLoading, setIsLoading] = useState(true);
  const previousCanControl = useRef<boolean>(false);

  // Function to poll permissions
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

  // Handle connectionModeChanged events that require WebRTC reconnection
  const handleRpcRequest = useCallback((request: JsonRpcRequest) => {
    if (request.method === "connectionModeChanged") {
      console.info("Connection mode changed, WebRTC reconnection required", request.params);

      // For session promotion that requires reconnection, refresh the page
      // This ensures WebRTC connection is re-established with proper mode
      const params = request.params as { action?: string; reason?: string };
      if (params.action === "reconnect_required" && params.reason === "session_promotion") {
        console.info("Session promoted, refreshing page to re-establish WebRTC connection");
        // Small delay to ensure all state updates are processed
        setTimeout(() => {
          window.location.reload();
        }, 500);
      }
    }
  }, []);

  const { send } = useJsonRpc(handleRpcRequest);

  useEffect(() => {
    pollPermissions(send);
  }, [send, currentMode, pollPermissions]);

  // Monitor permission changes and re-initialize HID when gaining control
  useEffect(() => {
    const currentCanControl = hasPermission(Permission.KEYBOARD_INPUT) && hasPermission(Permission.MOUSE_INPUT);
    const hadControl = previousCanControl.current;

    // If we just gained control permissions, re-initialize HID
    if (currentCanControl && !hadControl && rpcHidChannel?.readyState === "open") {
      console.info("Gained control permissions, re-initializing HID");

      // Reset protocol version to force re-handshake
      setRpcHidProtocolVersion(null);

      // Import handshake functionality dynamically
      import("./hidRpc").then(({ HID_RPC_VERSION, HandshakeMessage }) => {
        // Send handshake after a small delay
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
  }, [permissions, rpcHidChannel, setRpcHidProtocolVersion]); // hasPermission depends on permissions which is already in deps

  const hasPermission = (permission: Permission): boolean => {
    return permissions[permission] === true;
  };

  const hasAnyPermission = (...perms: Permission[]): boolean => {
    return perms.some(perm => hasPermission(perm));
  };

  const hasAllPermissions = (...perms: Permission[]): boolean => {
    return perms.every(perm => hasPermission(perm));
  };

  // Session mode helpers
  const isPrimary = () => currentMode === "primary";
  const isObserver = () => currentMode === "observer";
  const isPending = () => currentMode === "pending";

  return {
    permissions,
    isLoading,
    hasPermission,
    hasAnyPermission,
    hasAllPermissions,
    isPrimary,
    isObserver,
    isPending,
  };
}