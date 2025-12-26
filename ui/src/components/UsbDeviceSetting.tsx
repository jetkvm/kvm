import { useCallback, useEffect, useState } from "react";

import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { m } from "@localizations/messages.js";
import { SettingsItem } from "@components/SettingsItem";
import Checkbox from "@components/Checkbox";
import { Button } from "@components/Button";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsSectionHeader } from "@components/SettingsSectionHeader";
import Fieldset from "@components/Fieldset";
import notifications from "@/notifications";
import { sleep } from "@/utils";

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
    label: m.usb_device_keyboard_mouse_and_mass_storage(),
    value: "default",
    config: {
      keyboard: true,
      absolute_mouse: true,
      relative_mouse: true,
      mass_storage: true,
    },
  },
  {
    label: m.usb_device_keyboard_only(),
    value: "keyboard_only",
    config: {
      keyboard: true,
      absolute_mouse: false,
      relative_mouse: false,
      mass_storage: false,
    },
  },
  {
    label: m.usb_device_custom(),
    value: "custom",
  },
];

export function UsbDeviceSetting() {
  const { send } = useJsonRpc();
  const [loading, setLoading] = useState(false);

  const [usbDeviceConfig, setUsbDeviceConfig] = useState<UsbDeviceConfig>(defaultUsbDeviceConfig);
  const [selectedPreset, setSelectedPreset] = useState<string>("default");

  const syncUsbDeviceConfig = useCallback(() => {
    send("getUsbDevices", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to load USB devices:", resp.error);
        notifications.error(
          m.usb_device_failed_load({ error: String(resp.error.data || m.unknown_error()) }),
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
      send("setUsbDevices", { devices }, async (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            m.usb_device_failed_set({ error: String(resp.error.data || m.unknown_error()) }),
          );
          setLoading(false);
          return;
        }

        // We need some time to ensure the USB devices are updated
        await sleep(2000);
        setLoading(false);
        syncUsbDeviceConfig();
        notifications.success(m.usb_device_updated());
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
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const newPreset = e.target.value;
      setSelectedPreset(newPreset);

      if (newPreset !== "custom") {
        const presetConfig = usbPresets.find(preset => preset.value === newPreset)?.config;

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
      <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />

      <SettingsSectionHeader
        title={m.usb_device_title()}
        description={m.usb_device_description()}
      />

      <SettingsItem
        loading={loading}
        title={m.usb_device_classes_title()}
        description={m.usb_device_classes_description()}
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
        <div className="ml-2 border-l border-slate-800/10 pl-4 dark:border-slate-300/20">
          <div className="space-y-4">
            <div className="space-y-4">
              <SettingsItem
                title={m.usb_device_enable_keyboard_title()}
                description={m.usb_device_enable_keyboard_description()}
              >
                <Checkbox
                  checked={usbDeviceConfig.keyboard}
                  onChange={onUsbConfigItemChange("keyboard")}
                />
              </SettingsItem>
            </div>
            <div className="space-y-4">
              <SettingsItem
                title={m.usb_device_enable_absolute_mouse_title()}
                description={m.usb_device_enable_absolute_mouse_description()}
              >
                <Checkbox
                  checked={usbDeviceConfig.absolute_mouse}
                  onChange={onUsbConfigItemChange("absolute_mouse")}
                />
              </SettingsItem>
            </div>
            <div className="space-y-4">
              <SettingsItem
                title={m.usb_device_enable_relative_mouse_title()}
                description={m.usb_device_enable_relative_mouse_description()}
              >
                <Checkbox
                  checked={usbDeviceConfig.relative_mouse}
                  onChange={onUsbConfigItemChange("relative_mouse")}
                />
              </SettingsItem>
            </div>
            <div className="space-y-4">
              <SettingsItem
                title={m.usb_device_enable_mass_storage_title()}
                description={m.usb_device_enable_mass_storage_description()}
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
              text={m.usb_device_update_classes()}
              onClick={() => handleUsbConfigChange(usbDeviceConfig)}
            />
            <Button
              size="SM"
              theme="light"
              text={m.usb_device_restore_default()}
              onClick={() => handleUsbConfigChange(defaultUsbDeviceConfig)}
            />
          </div>
        </div>
      )}
    </Fieldset>
  );
}
