import { useCallback, useEffect, useState } from "react";

import { devError } from '../utils/debug';

import { JsonRpcResponse, useJsonRpc } from "./useJsonRpc";
import { useAudioEvents } from "./useAudioEvents";

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
        devError("Failed to load USB devices:", resp.error);
        setError(resp.error.data || "Unknown error");
        setUsbDeviceConfig(null);
      } else {
        const config = resp.result as UsbDeviceConfig;
        setUsbDeviceConfig(config);
        setError(null);
      }
    });
  }, [send]);

  // Listen for audio device changes to update USB config in real-time
  const handleAudioDeviceChanged = useCallback(() => {
    // Audio device changed, refetching USB config
    fetchUsbDeviceConfig();
  }, [fetchUsbDeviceConfig]);

  // Subscribe to audio events for real-time updates
  useAudioEvents(handleAudioDeviceChanged);

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