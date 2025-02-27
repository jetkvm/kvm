import { SettingsPageHeader } from "../components/SettingsPageheader";

import { SettingsItem } from "./devices.$id.settings";
import { useCallback, useState } from "react";
import { useEffect } from "react";
import { SystemVersionInfo } from "./devices.$id.settings.general.update";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { Button } from "../components/Button";
import notifications from "../notifications";
import Checkbox from "../components/Checkbox";
import { useDeviceUiNavigation } from "../hooks/useAppNavigation";

export default function SettingsGeneralRoute() {
  const [send] = useJsonRpc();
  const { navigateTo } = useDeviceUiNavigation();

  const [autoUpdate, setAutoUpdate] = useState(true);
  const [currentVersions, setCurrentVersions] = useState<{
    appVersion: string;
    systemVersion: string;
  } | null>(null);

  const getCurrentVersions = useCallback(() => {
    send("getUpdateStatus", {}, resp => {
      if ("error" in resp) return;
      const result = resp.result as SystemVersionInfo;
      setCurrentVersions({
        appVersion: result.local.appVersion,
        systemVersion: result.local.systemVersion,
      });
    });
  }, [send]);

  useEffect(() => {
    getCurrentVersions();
    send("getAutoUpdateState", {}, resp => {
      if ("error" in resp) return;
      setAutoUpdate(resp.result as boolean);
    });
  }, [getCurrentVersions, send]);

  const handleAutoUpdateChange = (enabled: boolean) => {
    send("setAutoUpdateState", { enabled }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set auto-update: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setAutoUpdate(enabled);
    });
  };

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="General"
        description="Configure device settings and update preferences"
      />

      <div className="space-y-4">
        <div className="space-y-4 pb-2">
          <div className="mt-2 flex items-center justify-between gap-x-2">
            <SettingsItem
              title="Check for Updates"
              description={
                currentVersions ? (
                  <>
                    App: {currentVersions.appVersion}
                    <br />
                    System: {currentVersions.systemVersion}
                  </>
                ) : (
                  <>
                    App: Loading...
                    <br />
                    System: Loading...
                  </>
                )
              }
            />
            <div>
              <Button
                size="SM"
                theme="light"
                text="Check for Updates"
                onClick={() => navigateTo("./update")}
              />
            </div>
          </div>
          <div className="space-y-4">
            <SettingsItem
              title="Auto Update"
              description="Automatically update the device to the latest version"
            >
              <Checkbox
                checked={autoUpdate}
                onChange={e => {
                  handleAutoUpdateChange(e.target.checked);
                }}
              />
            </SettingsItem>
          </div>
        </div>
      </div>
    </div>
  );
}
