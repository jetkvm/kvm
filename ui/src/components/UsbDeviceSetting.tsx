import { useCallback } from "react";

import { useEffect, useState } from "react";
import { useJsonRpc } from "../hooks/useJsonRpc";
import notifications from "../notifications";
import { SettingsItem } from "../routes/devices.$id.settings";
import Checkbox from "./Checkbox";
import { Button } from "./Button";
import { SelectMenuBasic } from "./SelectMenuBasic";
import { SettingsSectionHeader } from "./SettingsSectionHeader";
import Fieldset from "./Fieldset";
export interface USBConfig {
  vendor_id: string;
  product_id: string;
  serial_number: string;
  manufacturer: string;
  product: string;
}

export interface UsbDeviceConfig {
  keyboard: boolean;
  absolute_mouse: boolean;
  relative_mouse: boolean;
  mass_storage: boolean;
}

const defaultUsbDeviceConfig: UsbDeviceConfig = {
  keyboard: true,
  absolute_mouse: true,
  relative_mouse: true,
  mass_storage: true,
};

const usbPresets = [
  {
    label: "Keyboard, Mouse and Mass Storage",
    value: "default",
    config: {
      keyboard: true,
      absolute_mouse: true,
      relative_mouse: true,
      mass_storage: true,
    },
  },
  {
    label: "Keyboard Only",
    value: "keyboard_only",
    config: {
      keyboard: true,
      absolute_mouse: false,
      relative_mouse: false,
      mass_storage: false,
    },
  },
  {
    label: "Custom",
    value: "custom",
  },
];

export function UsbDeviceSetting() {
  const [send] = useJsonRpc();
  const [loading, setLoading] = useState(false);

  const [usbDeviceConfig, setUsbDeviceConfig] =
    useState<UsbDeviceConfig>(defaultUsbDeviceConfig);
  const [selectedPreset, setSelectedPreset] = useState<string>("default");

  const syncUsbDeviceConfig = useCallback(() => {
    send("getUsbDevices", {}, resp => {
      if ("error" in resp) {
        console.error("Failed to load USB devices:", resp.error);
        notifications.error(
          `Failed to load USB devices: ${resp.error.data || "Unknown error"}`,
        );
      } else {
        const usbConfigState = resp.result as UsbDeviceConfig;
        setUsbDeviceConfig(usbConfigState);

        // Set the appropriate preset based on current config
        const matchingPreset = usbPresets.find(
          preset =>
            preset.value !== "custom" &&
            preset.config &&
            Object.keys(preset.config).length === Object.keys(usbConfigState).length &&
            Object.keys(preset.config).every(key => {
              const configKey = key as keyof typeof preset.config;
              return preset.config[configKey] === usbConfigState[configKey];
            }),
        );

        setSelectedPreset(matchingPreset ? matchingPreset.value : "custom");
      }
    });
  }, [send]);

  const handleUsbConfigChange = useCallback(
    (devices: UsbDeviceConfig) => {
      setLoading(true);
      send("setUsbDevices", { devices }, async resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set usb devices: ${resp.error.data || "Unknown error"}`,
          );
          setLoading(false);
          return;
        }

        // We need some time to ensure the USB devices are updated
        await new Promise(resolve => setTimeout(resolve, 2000));
        setLoading(false);
        syncUsbDeviceConfig();
        notifications.success(`USB Devices updated`);
      });
    },
    [send, syncUsbDeviceConfig],
  );

  const onUsbConfigItemChange = useCallback(
    (key: keyof UsbDeviceConfig) => (e: React.ChangeEvent<HTMLInputElement>) => {
      setUsbDeviceConfig(prev => ({
        ...prev,
        [key]: e.target.checked,
      }));
    },
    [],
  );

  const handlePresetChange = useCallback(
    async (e: React.ChangeEvent<HTMLSelectElement>) => {
      const newPreset = e.target.value;
      setSelectedPreset(newPreset);

      if (newPreset !== "custom") {
        const presetConfig = usbPresets.find(
          preset => preset.value === newPreset,
        )?.config;

        if (presetConfig) {
          handleUsbConfigChange(presetConfig);
        }
      }
    },
    [handleUsbConfigChange],
  );

  useEffect(() => {
    syncUsbDeviceConfig();
  }, [syncUsbDeviceConfig]);

  return (
    <Fieldset disabled={loading} className="space-y-4">
      <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />

      <SettingsSectionHeader
        title="USB Device"
        description="USB devices to emulate on the target computer"
      />

      <SettingsItem
        loading={loading}
        title="Classes"
        description="USB device classes in the composite device"
      >
        <SelectMenuBasic
          size="SM"
          label=""
          className="max-w-[292px]"
          value={selectedPreset}
          fullWidth
          onChange={handlePresetChange}
          options={usbPresets}
        />
      </SettingsItem>

      {selectedPreset === "custom" && (
        <div className="ml-2 border-l border-slate-800/10 pl-4 dark:border-slate-300/20 ">
          <div className="space-y-4">
            <div className="space-y-4">
              <SettingsItem title="Enable Keyboard" description="Enable Keyboard">
                <Checkbox
                  checked={usbDeviceConfig.keyboard}
                  onChange={onUsbConfigItemChange("keyboard")}
                />
              </SettingsItem>
            </div>
            <div className="space-y-4">
              <SettingsItem
                title="Enable Absolute Mouse (Pointer)"
                description="Enable Absolute Mouse (Pointer)"
              >
                <Checkbox
                  checked={usbDeviceConfig.absolute_mouse}
                  onChange={onUsbConfigItemChange("absolute_mouse")}
                />
              </SettingsItem>
            </div>
            <div className="space-y-4">
              <SettingsItem
                title="Enable Relative Mouse"
                description="Enable Relative Mouse"
              >
                <Checkbox
                  checked={usbDeviceConfig.relative_mouse}
                  onChange={onUsbConfigItemChange("relative_mouse")}
                />
              </SettingsItem>
            </div>
            <div className="space-y-4">
              <SettingsItem
                title="Enable USB Mass Storage"
                description="Sometimes it might need to be disabled to prevent issues with certain devices"
              >
                <Checkbox
                  checked={usbDeviceConfig.mass_storage}
                  onChange={onUsbConfigItemChange("mass_storage")}
                />
              </SettingsItem>
            </div>
          </div>
          <div className="mt-6 flex gap-x-2">
            <Button
              size="SM"
              loading={loading}
              theme="primary"
              text="Update USB Classes"
              onClick={() => handleUsbConfigChange(usbDeviceConfig)}
            />
            <Button
              size="SM"
              theme="light"
              text="Restore to Default"
              onClick={() => handleUsbConfigChange(defaultUsbDeviceConfig)}
            />
          </div>
        </div>
      )}
    </Fieldset>
  );
}
