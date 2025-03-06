import { useCallback, useState } from "react";
import { Button } from "@components/Button";
import { InputFieldWithLabel } from "@components/InputField";
import { SettingsPageHeader } from "../components/SettingsPageheader";
import { SelectMenuBasic } from "../components/SelectMenuBasic";
import { SettingsItem } from "./devices.$id.settings";
import { NameConfig } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";

export default function SettingsAppearanceRoute() {
  const [currentTheme, setCurrentTheme] = useState(() => {
    return localStorage.theme || "system";
  });
  const [nameConfig, setNameConfig] = useState<NameConfig>({
    name: '',
    dns:  '',
  });
  const [send] = useJsonRpc();

  send("getNameConfig", {}, resp => {
    if ("error" in resp) return;
    const results = resp.result as NameConfig;
    setNameConfig(results);
    document.title = results.name;
  });

  const handleThemeChange = useCallback((value: string) => {
    const root = document.documentElement;

    if (value === "system") {
      localStorage.removeItem("theme");
      // Check system preference
      const systemTheme = window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light";
      root.classList.remove("light", "dark");
      root.classList.add(systemTheme);
    } else {
      localStorage.theme = value;
      root.classList.remove("light", "dark");
      root.classList.add(value);
    }
  }, []);

    const handleDeviceNameChange = (deviceName: string) => {
      setNameConfig({... nameConfig, name: deviceName})
    };

    const handleUpdateNameConfig = useCallback(() => {
      send("setNameConfig", { deviceName: nameConfig.name }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set name config: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        const rNameConfig = resp.result as NameConfig;
        setNameConfig(rNameConfig);
        document.title = rNameConfig.name;
        notifications.success(`Device name set to "${rNameConfig.name}" successfully.\nDNS Name set to "${rNameConfig.dns}"`);
      });
    }, [send, nameConfig]);

    return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Appearance"
        description="Customize the look and feel of your JetKVM interface"
      />
      <SettingsItem title="Theme" description="Choose your preferred color theme">
        <SelectMenuBasic
          size="SM"
          label=""
          value={currentTheme}
          options={[
            { value: "system", label: "System" },
            { value: "light", label: "Light" },
            { value: "dark", label: "Dark" },
          ]}
          onChange={e => {
            setCurrentTheme(e.target.value);
            handleThemeChange(e.target.value);
          }}
        />
      </SettingsItem>
      <SettingsItem title="Device Name" description="Set your device name">
        <InputFieldWithLabel
          required
          label=""
          placeholder="Enter Device Name"
          description={`DNS Name: ${nameConfig.dns}`}
          defaultValue={nameConfig.name}
          onChange={e => handleDeviceNameChange(e.target.value)}
        />
      </SettingsItem>
      <div className="flex items-center gap-x-2">
        <Button
          size="SM"
          theme="primary"
          text="Update Device Name"
          onClick={handleUpdateNameConfig}
        />
      </div>
    </div>
  );
}
