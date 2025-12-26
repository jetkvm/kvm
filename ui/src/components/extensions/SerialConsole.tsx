import { useEffect, useState } from "react";
import { LuTerminal } from "react-icons/lu";

import { m } from "@localizations/messages.js";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useUiStore } from "@hooks/stores";
import { Button } from "@components/Button";
import Card from "@components/Card";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import notifications from "@/notifications";

interface SerialSettings {
  baudRate: string;
  dataBits: string;
  stopBits: string;
  parity: string;
}

export function SerialConsole() {
  const { send } = useJsonRpc();
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
          m.serial_console_get_settings_error({ error: resp.error.data || m.unknown_error() }),
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
          m.serial_console_set_settings_error({
            settings: setting,
            error: resp.error.data || m.unknown_error(),
          }),
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
        title={m.extension_serial_console()}
        description={m.serial_console_configure_description()}
      />

      <Card className="animate-fadeIn opacity-0">
        <div className="space-y-4 p-3">
          {/* Open Console Button */}
          <div className="flex items-center">
            <Button
              size="SM"
              theme="primary"
              LeadingIcon={LuTerminal}
              text={m.serial_console_open_console()}
              onClick={() => {
                console.log("Opening serial console with settings: ", settings);
                setTerminalType("serial");
              }}
            />
          </div>
          <hr className="border-slate-700/30 dark:border-slate-600/30" />
          {/* Settings */}
          <div className="grid grid-cols-2 gap-4">
            <SelectMenuBasic
              label={m.serial_console_baud_rate()}
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
              label={m.serial_console_data_bits()}
              options={[
                { label: "8", value: "8" },
                { label: "7", value: "7" },
              ]}
              value={settings.dataBits}
              onChange={e => handleSettingChange("dataBits", e.target.value)}
            />

            <SelectMenuBasic
              label={m.serial_console_stop_bits()}
              options={[
                { label: "1", value: "1" },
                { label: "1.5", value: "1.5" },
                { label: "2", value: "2" },
              ]}
              value={settings.stopBits}
              onChange={e => handleSettingChange("stopBits", e.target.value)}
            />

            <SelectMenuBasic
              label={m.serial_console_parity()}
              options={[
                { label: m.serial_console_parity_none(), value: "none" },
                { label: m.serial_console_parity_even(), value: "even" },
                { label: m.serial_console_parity_odd(), value: "odd" },
                { label: m.serial_console_parity_mark(), value: "mark" },
                { label: m.serial_console_parity_space(), value: "space" },
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
