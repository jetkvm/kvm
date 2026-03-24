import { useCallback, useState } from "react";

import { useJsonRpc, JsonRpcResponse } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";

export interface ChromecastDevice {
  name: string;
  uuid: string;
  address: string;
  port: number;
}

export interface CastState {
  isCasting: boolean;
  activeDevice: { name: string; address: string; port: number } | null;
  discoveredDevices: ChromecastDevice[];
  isDiscovering: boolean;
  isStarting: boolean;
  error: string | null;
}

export function useCast() {
  const { send } = useJsonRpc();
  const [state, setState] = useState<CastState>({
    isCasting: false,
    activeDevice: null,
    discoveredDevices: [],
    isDiscovering: false,
    isStarting: false,
    error: null,
  });

  const discoverDevices = useCallback(() => {
    setState(s => ({ ...s, isDiscovering: true, error: null }));
    send("discoverChromecasts", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        setState(s => ({
          ...s,
          isDiscovering: false,
          error: resp.error?.message || "Discovery failed",
        }));
        return;
      }
      setState(s => ({
        ...s,
        isDiscovering: false,
        discoveredDevices: (resp.result as ChromecastDevice[]) || [],
      }));
    });
  }, [send]);

  const startCasting = useCallback(
    (device: ChromecastDevice) => {
      setState(s => ({ ...s, isStarting: true, error: null }));
      send(
        "startCasting",
        { address: device.address, port: device.port },
        (resp: JsonRpcResponse) => {
          if ("error" in resp) {
            const msg = resp.error?.message || "Failed to start casting";
            setState(s => ({ ...s, isStarting: false, error: msg }));
            notifications.error(`Cast failed: ${msg}`);
            return;
          }
          setState(s => ({
            ...s,
            isStarting: false,
            isCasting: true,
            activeDevice: {
              name: device.name,
              address: device.address,
              port: device.port,
            },
          }));
          notifications.success(`Casting to ${device.name}`);
        },
      );
    },
    [send],
  );

  const stopCasting = useCallback(() => {
    send("stopCasting", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error("Failed to stop casting");
        return;
      }
      setState(s => ({
        ...s,
        isCasting: false,
        activeDevice: null,
      }));
    });
  }, [send]);

  const refreshStatus = useCallback(() => {
    send("getCastingStatus", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const status = resp.result as { active: boolean; deviceName: string };
      setState(s => ({
        ...s,
        isCasting: status.active,
        activeDevice: status.active
          ? { name: status.deviceName, address: "", port: 0 }
          : null,
      }));
    });
  }, [send]);

  return {
    ...state,
    discoverDevices,
    startCasting,
    stopCasting,
    refreshStatus,
  };
}
