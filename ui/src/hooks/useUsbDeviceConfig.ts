import { useCallback, useEffect, useState } from "react";

import { JsonRpcResponse, useJsonRpc } from "./useJsonRpc";

export interface UsbDeviceConfig {
  keyboard: boolean;
  absolute_mouse: boolean;
  relative_mouse: boolean;
  mass_storage: boolean;
  audio: boolean;
}

export function useUsbDeviceConfig() {
  const { send } = useJsonRpc();
  const [usbDeviceConfig, setUsbDeviceConfig] = useState<UsbDeviceConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchUsbDeviceConfig = useCallback(() => {
    setLoading(true);
    setError(null);
    
    send("getUsbDevices", {}, (resp: JsonRpcResponse) => {
      setLoading(false);
      
      if ("error" in resp) {
        console.error("Failed to load USB devices:", resp.error);
        setError(resp.error.data || "Unknown error");
        setUsbDeviceConfig(null);
      } else {
        const config = resp.result as UsbDeviceConfig;
        setUsbDeviceConfig(config);
        setError(null);
      }
    });
  }, [send]);

  useEffect(() => {
    fetchUsbDeviceConfig();
  }, [fetchUsbDeviceConfig]);

  return {
    usbDeviceConfig,
    loading,
    error,
    refetch: fetchUsbDeviceConfig,
  };
}