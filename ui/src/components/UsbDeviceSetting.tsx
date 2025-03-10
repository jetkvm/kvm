import { useCallback } from "react";

import { useEffect, useState } from "react";
import { useJsonRpc } from "../hooks/useJsonRpc";
import notifications from "../notifications";
import { SettingsItem } from "../routes/devices.$id.settings";
import Checkbox from "./Checkbox";

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
}

export function UsbDeviceSetting() {
  const [send] = useJsonRpc();

  const [usbDeviceConfig, setUsbDeviceConfig] = useState<UsbDeviceConfig>(defaultUsbDeviceConfig);
  const syncUsbDeviceConfig = useCallback(() => {
    send("getUsbDevices", {}, resp => {
      if ("error" in resp) {
        console.error("Failed to load USB devices:", resp.error);
        notifications.error(
          `Failed to load USB devices: ${resp.error.data || "Unknown error"}`,
        );
      } else {
        console.log("syncUsbDeviceConfig#getUsbDevices result:", resp.result);
        const usbConfigState = resp.result as UsbDeviceConfig;
        setUsbDeviceConfig(usbConfigState);
      }
    });
  }, [send]);

  const handleUsbConfigChange = useCallback(
    (devices: UsbDeviceConfig) => {
      send("setUsbDevices", { devices }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set usb devices: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        notifications.success(
          `USB Devices updated`
        );
        syncUsbDeviceConfig();
      });
    },
    [send, syncUsbDeviceConfig],
  );

  const onUsbConfigItemChange = useCallback((key: keyof UsbDeviceConfig) => (e: React.ChangeEvent<HTMLInputElement>) => {
    setUsbDeviceConfig((val) => {
      val[key] = e.target.checked;
      handleUsbConfigChange(val);
      return val;
    });
  }, [handleUsbConfigChange]);

  useEffect(() => {
    syncUsbDeviceConfig();
  }, [syncUsbDeviceConfig]);

  return (
    <>
      <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />
      <div className="space-y-4">
        <SettingsItem
          title="Enable Keyboard"
          description="Enable Keyboard"
        >
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
    </>
  );
}
