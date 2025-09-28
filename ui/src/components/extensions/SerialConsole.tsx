import { LuTerminal } from "react-icons/lu";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@components/Button";
import Card from "@components/Card";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { useUiStore } from "@/hooks/stores";
import { SelectMenuBasic } from "@components/SelectMenuBasic";

interface SerialSettings {
  baudRate: string;
  dataBits: string;
  stopBits: string;
  parity: string;
}

export function SerialConsole() {
  const { send } = useJsonRpc();
  const { t } = useTranslation();
  const [settings, setSettings] = useState<SerialSettings>({
    baudRate: "9600",
    dataBits: "8",
    stopBits: "1",
    parity: "none",
  });

  useEffect(() => {
    send("getSerialSettings", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          t('Failed_to_get_serial_settings_msg',{msg:resp.error.data || t('Unknown_error')})
        );
        return;
      }
      setSettings(resp.result as SerialSettings);
    });
  }, [send]);

  const handleSettingChange = (setting: keyof SerialSettings, value: string) => {
    const newSettings = { ...settings, [setting]: value };
    send("setSerialSettings", { settings: newSettings }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          t('Failed_to_update_serial_settings_msg',{msg:resp.error.data || t('Unknown_error')})
        );
        return;
      }
      setSettings(newSettings);
    });
  };
  const { setTerminalType } = useUiStore();

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={t('Serial_Console')}
        description={t('Configure_your_serial_console_settings')}
      />

      <Card className="animate-fadeIn opacity-0">
        <div className="space-y-4 p-3">
          {/* Open Console Button */}
          <div className="flex items-center">
            <Button
              size="SM"
              theme="primary"
              LeadingIcon={LuTerminal}
              text={t('Open_Console')}
              onClick={() => {
                setTerminalType("serial");
                console.log("Opening serial console with settings: ", settings);
              }}
            />
          </div>
          <hr className="border-slate-700/30 dark:border-slate-600/30" />
          {/* Settings */}
          <div className="grid grid-cols-2 gap-4">
            <SelectMenuBasic
              label={t('Baud_Rate')}
              options={[
                { label: "1200", value: "1200" },
                { label: "2400", value: "2400" },
                { label: "4800", value: "4800" },
                { label: "9600", value: "9600" },
                { label: "19200", value: "19200" },
                { label: "38400", value: "38400" },
                { label: "57600", value: "57600" },
                { label: "115200", value: "115200" },
              ]}
              value={settings.baudRate}
              onChange={e => handleSettingChange("baudRate", e.target.value)}
            />

            <SelectMenuBasic
              label={t('Data_Bits')}
              options={[
                { label: "8", value: "8" },
                { label: "7", value: "7" },
              ]}
              value={settings.dataBits}
              onChange={e => handleSettingChange("dataBits", e.target.value)}
            />

            <SelectMenuBasic
              label={t('Stop_Bits')}
              options={[
                { label: "1", value: "1" },
                { label: "1.5", value: "1.5" },
                { label: "2", value: "2" },
              ]}
              value={settings.stopBits}
              onChange={e => handleSettingChange("stopBits", e.target.value)}
            />

            <SelectMenuBasic
              label={t('Parity')}
              options={[
                { label: t("None"), value: "none" },
                { label: t("Even"), value: "even" },
                { label: t("Odd"), value: "odd" },
              ]}
              value={settings.parity}
              onChange={e => handleSettingChange("parity", e.target.value)}
            />
          </div>
        </div>
      </Card>
    </div>
  );
}
