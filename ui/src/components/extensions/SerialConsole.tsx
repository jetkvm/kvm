import { LuTerminal } from "react-icons/lu";
import { useEffect, useState } from "react";

import { Button } from "@components/Button";
import Card from "@components/Card";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { useJsonRpc } from "@/hooks/useJsonRpc";
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
  const [send] = useJsonRpc();
  const [settings, setSettings] = useState<SerialSettings>({
    baudRate: "9600",
    dataBits: "8",
    stopBits: "1",
    parity: "none",
  });

  useEffect(() => {
    send("getSerialSettings", {}, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to get serial settings: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setSettings(resp.result as SerialSettings);
    });
  }, [send]);

  const handleSettingChange = (setting: keyof SerialSettings, value: string) => {
    const newSettings = { ...settings, [setting]: value };
    send("setSerialSettings", { settings: newSettings }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to update serial settings: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setSettings(newSettings);
    });
  };
  const setTerminalType = useUiStore(state => state.setTerminalType);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Serial Console"
        description="Configure your serial console settings"
      />

      <Card className="animate-fadeIn">
        <div className="space-y-4 p-3">
          {/* Open Console Button */}
          <div className="flex items-center">
            <Button
              size="SM"
              theme="primary"
              LeadingIcon={LuTerminal}
              text="Open Console"
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
              label="Baud Rate"
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
              label="Data Bits"
              options={[
                { label: "8", value: "8" },
                { label: "7", value: "7" },
              ]}
              value={settings.dataBits}
              onChange={e => handleSettingChange("dataBits", e.target.value)}
            />

            <SelectMenuBasic
              label="Stop Bits"
              options={[
                { label: "1", value: "1" },
                { label: "1.5", value: "1.5" },
                { label: "2", value: "2" },
              ]}
              value={settings.stopBits}
              onChange={e => handleSettingChange("stopBits", e.target.value)}
            />

            <SelectMenuBasic
              label="Parity"
              options={[
                { label: "None", value: "none" },
                { label: "Even", value: "even" },
                { label: "Odd", value: "odd" },
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
