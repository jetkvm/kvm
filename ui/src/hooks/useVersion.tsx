import { useCallback } from "react";

import { useDeviceStore } from "@/hooks/stores";
import { type JsonRpcResponse, RpcMethodNotFound, useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";

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
  const {
    appVersion,
    systemVersion,
    setAppVersion,
    setSystemVersion,
  } = useDeviceStore();
  const { send } = useJsonRpc();
  const getVersionInfo = useCallback(() => {
    return new Promise<SystemVersionInfo>((resolve, reject) => {
      send("getUpdateStatus", {}, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(`Failed to check for updates: ${resp.error}`);
          reject(new Error("Failed to check for updates"));
        } else {
          const result = resp.result as SystemVersionInfo;
          setAppVersion(result.local.appVersion);
          setSystemVersion(result.local.systemVersion);

          if (result.error) {
            notifications.error(`Failed to check for updates: ${result.error}`);
            reject(new Error("Failed to check for updates"));
          } else {
            resolve(result);
          }
        }
      });
    });
  }, [send, setAppVersion, setSystemVersion]);

  const getLocalVersion = useCallback(() => {
    return new Promise<VersionInfo>((resolve, reject) => {
      send("getLocalVersion", {}, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          console.log(resp.error)
          if (resp.error.code === RpcMethodNotFound) {
            console.warn("Failed to get device version, using legacy version");
            return getVersionInfo().then(result => resolve(result.local)).catch(reject);
          }
          console.error("Failed to get device version N", resp.error);
          notifications.error(`Failed to get device version: ${resp.error}`);
          reject(new Error("Failed to get device version"));
        } else {
          const result = resp.result as VersionInfo;

          setAppVersion(result.appVersion);
          setSystemVersion(result.systemVersion);
          resolve(result);
        }
      });
    });
  }, [send, setAppVersion, setSystemVersion, getVersionInfo]);

  return {
    getVersionInfo,
    getLocalVersion,
    appVersion,
    systemVersion,
  };
}