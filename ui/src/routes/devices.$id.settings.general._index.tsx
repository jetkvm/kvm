
import { useState , useEffect } from "react";
import { useTranslation } from "react-i18next";

import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";

import { SettingsPageHeader } from "../components/SettingsPageheader";
import { Button } from "../components/Button";
import notifications from "../notifications";
import Checkbox from "../components/Checkbox";
import { useDeviceUiNavigation } from "../hooks/useAppNavigation";
import { useDeviceStore } from "../hooks/stores";

import { SettingsItem } from "./devices.$id.settings";

export default function SettingsGeneralRoute() {
  const { send } = useJsonRpc();
  const { t } = useTranslation();
  const { navigateTo } = useDeviceUiNavigation();
  const [autoUpdate, setAutoUpdate] = useState(true);

  const currentVersions = useDeviceStore(state => {
    const { appVersion, systemVersion } = state;
    if (!appVersion || !systemVersion) return null;
    return { appVersion, systemVersion };
  });

  useEffect(() => {
    send("getAutoUpdateState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setAutoUpdate(resp.result as boolean);
    });
  }, [send]);

  const handleAutoUpdateChange = (enabled: boolean) => {
    send("setAutoUpdateState", { enabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          t('Failed_to_set_auto-update_msg',{msg:resp.error.data || t('Unknown_error')})
        );
        return;
      }
      setAutoUpdate(enabled);
    });
  };

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={t('General')}
        description={t('Configure_device_settings_and_update_preferences')}
      />

      <div className="space-y-4">
        <div className="space-y-4 pb-2">
          <div className="mt-2 flex items-center justify-between gap-x-2">
            <SettingsItem
              title={t('Check_for_Updates')}
              description={
                currentVersions ? (
                  <>
                      {t('App')}: {currentVersions.appVersion}
                    <br />
                      {t('System')}: {currentVersions.systemVersion}
                  </>
                ) : (
                  <>
                      {t('App')}: {t('Loading...')}
                    <br />
                      {t('System')}: {t('Loading...')}
                  </>
                )
              }
            />
            <div>
              <Button
                size="SM"
                theme="light"
                text={t('Check_for_Updates')}
                onClick={() => navigateTo("./update")}
              />
            </div>
          </div>
          <div className="space-y-4">
            <SettingsItem
              title={t('Auto_Update')}
              description={t('Automatically_update_the_device_to_the_latest_version')}
            >
              <Checkbox
                checked={autoUpdate}
                onChange={e => {
                  handleAutoUpdateChange(e.target.checked);
                }}
              />
            </SettingsItem>
          </div>

          <div className="mt-2 flex items-center justify-between gap-x-2">
            <SettingsItem
              title={t('Reboot_Device')}
              description={t('Power_cycle_the_JetKVM')}
            />
            <div>
              <Button
                size="SM"
                theme="light"
                text={t('Reboot_Device')}
                onClick={() => navigateTo("./reboot")}
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
