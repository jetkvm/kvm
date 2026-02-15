import { useCallback } from "react";

import { useDeviceStore } from "@/hooks/stores";
import { JsonRpcError, RpcMethodNotFound } from "@/hooks/useJsonRpc";
import { getUpdateStatus, getLocalVersion as getLocalVersionRpc } from "@/utils/jsonrpc";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

export interface VersionInfo {
  appVersion: string;
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
      setAppVersion(result.local.appVersion);
      setSystemVersion(result.local.systemVersion);
      return result;
    } catch (error) {
      const jsonRpcError = error as JsonRpcError;
      notifications.error(m.updates_failed_check({ error: jsonRpcError.message }));
      throw jsonRpcError;
    }
  }, [setAppVersion, setSystemVersion]);

  const getLocalVersion = useCallback(async () => {
    try {
      const result = await getLocalVersionRpc();
      setAppVersion(result.appVersion);
      setSystemVersion(result.systemVersion);
      return result;
    } catch (error: unknown) {
      const jsonRpcError = error as JsonRpcError;

      if (jsonRpcError.code === RpcMethodNotFound) {
        console.error("Failed to get local version, using legacy remote version");
        const result = await getVersionInfo();
        return result.local;
      }

      console.error("Failed to get device version", jsonRpcError);
      notifications.error(m.updates_failed_get_device_version({ error: jsonRpcError.message }));
      throw jsonRpcError;
    }
  }, [setAppVersion, setSystemVersion, getVersionInfo]);

  return {
    getVersionInfo,
    getLocalVersion,
    appVersion,
    systemVersion,
  };
}
