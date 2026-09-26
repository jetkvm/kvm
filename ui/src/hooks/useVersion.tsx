import { useCallback } from "react";

import { useDeviceStore } from "@/hooks/stores";
import { JsonRpcError } from "@/hooks/useJsonRpc";
import { getUpdateStatus } from "@/utils/jsonrpc";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

export interface VersionInfo {
  // Absent on a device whose firmware is a single image with no separate app.
  appVersion?: string;
  systemVersion: string;
}

export interface SystemVersionInfo {
  local: VersionInfo;
  remote?: VersionInfo;
  systemUpdateAvailable: boolean;
  appUpdateAvailable: boolean;
  error?: string;
}

export function useVersion() {
  const { appVersion, systemVersion, setAppVersion, setSystemVersion } = useDeviceStore();

  const getVersionInfo = useCallback(async () => {
    try {
      const result = await getUpdateStatus();
      setAppVersion(result.local.appVersion ?? "");
      setSystemVersion(result.local.systemVersion);
      return result;
    } catch (error) {
      const jsonRpcError = error as JsonRpcError;
      notifications.error(m.updates_failed_check({ error: jsonRpcError.message }));
      throw jsonRpcError;
    }
  }, [setAppVersion, setSystemVersion]);

  return {
    getVersionInfo,
    appVersion,
    systemVersion,
  };
}
