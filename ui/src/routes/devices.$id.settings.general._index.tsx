import { useState, useEffect, useMemo } from "react";

import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { useDeviceStore } from "@hooks/stores";
import { Button } from "@components/Button";
import Checkbox from "@components/Checkbox";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import notifications from "@/notifications";
import { getLocale, setLocale, locales, baseLocale } from "@localizations/runtime.js";
import { m } from "@localizations/messages.js";
import { deleteCookie, map_locale_code_to_name } from "@/utils";

export default function SettingsGeneralRoute() {
  const { send } = useJsonRpc();
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
          m.general_auto_update_error({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      setAutoUpdate(enabled);
    });
  };

  const [currentLocale, setCurrentLocale] = useState(getLocale());

  const localeOptions = useMemo(() => {
    return ["", ...locales].map(code => {
      const [localizedName, nativeName] = map_locale_code_to_name(currentLocale, code);
      // don't repeat the name if it's the same in both locales (or blank)
      const label =
        nativeName && nativeName !== localizedName
          ? `${localizedName} - ${nativeName}`
          : localizedName;
      return { value: code, label: label };
    });
  }, [currentLocale]);

  const handleLocaleChange = (newLocale: string) => {
    if (newLocale === currentLocale) return;

    let validLocale = newLocale as (typeof locales)[number];

    if (newLocale !== "") {
      if (!locales.includes(validLocale)) {
        validLocale = baseLocale;
      }

      setLocale(validLocale); // tell the i18n system to change locale
    } else {
      deleteCookie("JETKVM_LOCALE", "", "/"); // delete the cookie that the i18n system uses to store the locale
    }

    setCurrentLocale(validLocale);
    notifications.success(m.locale_change_success({ locale: validLocale || m.locale_auto() }));
  };

  return (
    <div className="space-y-4">
      <SettingsPageHeader title={m.general_title()} description={m.general_page_description()} />

      <div className="space-y-4">
        <div className="space-y-4 pb-2">
          <div className="space-y-4">
            <SettingsItem
              badge="Beta"
              badgeTheme="info"
              title={m.user_interface_language_title()}
              description={m.user_interface_language_description()}
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={currentLocale}
                options={localeOptions}
                onChange={e => {
                  handleLocaleChange(e.target.value);
                }}
              />
            </SettingsItem>
          </div>
          <div className="mt-2 flex items-center justify-between gap-x-2">
            <SettingsItem
              title={m.general_check_for_updates()}
              description={
                <>
                  {m.general_app_version({
                    version: currentVersions ? currentVersions.appVersion : m.loading(),
                  })}
                  <br />
                  {m.general_system_version({
                    version: currentVersions ? currentVersions.systemVersion : m.loading(),
                  })}
                </>
              }
            />
            <div className="flex items-center justify-start gap-x-2">
              <Button
                size="SM"
                theme="light"
                text={m.general_check_for_updates()}
                onClick={() => navigateTo("./update")}
              />
            </div>
          </div>
          <div className="space-y-4">
            <SettingsItem
              title={m.general_auto_update_title()}
              description={m.general_auto_update_description()}
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
              title={m.general_reboot_device()}
              description={m.general_reboot_device_description()}
            />
            <div>
              <Button
                size="SM"
                theme="light"
                text={m.general_reboot_device()}
                onClick={() => navigateTo("./reboot")}
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
