import { useCallback, useState } from "react";
import { Button } from "@components/Button";
import { InputFieldWithLabel } from "@components/InputField";
import { SettingsPageHeader } from "../components/SettingsPageheader";
import { SelectMenuBasic } from "../components/SelectMenuBasic";
import { SettingsItem } from "./devices.$id.settings";
import {NameConfig, useSettingsStore} from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";

export default function SettingsAppearanceRoute() {
  const [currentTheme, setCurrentTheme] = useState(() => {
    return localStorage.theme || "system";
  });
  const [send] = useJsonRpc();
  const [name, setName] = useState("");

  const nameConfigSettings = useSettingsStore(state => state.nameConfig);
  const setNameConfigSettings = useSettingsStore(state => state.setNameConfig);

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

    const handleNameChange = (value: string) => {
      setName(value);
    };

    const handleNameSave = useCallback(() => {
      send("setNameConfig", { deviceName: name }, resp => {
        if ("error" in resp) {
          notifications.error(`Failed to set name config: ${resp.error.data || "Unknown error"}`);
          return;
        }
        const nameConfig = resp.result as NameConfig;
        setNameConfigSettings(nameConfig);
        document.title = nameConfig.name;
        notifications.success(
            `Device name set to "${nameConfig.name}" successfully.\nDNS Name set to "${nameConfig.dns}"`
        );
      });
    }, [send, name, setNameConfigSettings]);

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
          description={`DNS: ${nameConfigSettings.dns}`}
          defaultValue={nameConfigSettings.name}
          onChange={e => handleNameChange(e.target.value)}
        />
      </SettingsItem>
      <div className="flex items-center gap-x-2">
        <Button
          size="SM"
          theme="primary"
          text="Update Device Name"
          onClick={() => {handleNameSave()}}
        />
      </div>
    </div>
  );
}
