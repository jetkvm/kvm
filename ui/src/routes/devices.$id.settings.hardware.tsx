import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SettingsItem } from "@routes/devices.$id.settings";
import { BacklightSettings, UsbConfigState, useSettingsStore } from "@/hooks/stores";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useJsonRpc } from "@/hooks/useJsonRpc";

import notifications from "../notifications";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import USBConfigDialog from "@components/USBConfigDialog";

const generatedSerialNumber = [generateNumber(1, 9), generateHex(7, 7), 0, 1].join("&");

function generateNumber(min: number, max: number) {
  return Math.floor(Math.random() * (max - min + 1) + min);
}

function generateHex(min: number, max: number) {
  const len = generateNumber(min, max);
  const n = (Math.random() * 0xfffff * 1000000).toString(16);
  return n.slice(0, len);
}

export interface USBConfig {
  vendor_id: string;
  product_id: string;
  serial_number: string;
  manufacturer: string;
  product: string;
}

const usbConfigs = [
  {
    label: "JetKVM Default",
    value: "USB Emulation Device",
  },
  {
    label: "Logitech Universal Adapter",
    value: "Logitech USB Input Device",
  },
  {
    label: "Microsoft Wireless MultiMedia Keyboard",
    value: "Wireless MultiMedia Keyboard",
  },
  {
    label: "Dell Multimedia Pro Keyboard",
    value: "Multimedia Pro Keyboard",
  },
];

type UsbConfigMap = Record<string, USBConfig>;

export default function SettingsHardwareRoute() {
  const [send] = useJsonRpc();
  const settings = useSettingsStore();

  const [usbConfigProduct, setUsbConfigProduct] = useState("");
  const [deviceId, setDeviceId] = useState("");

  const setBacklightSettings = useSettingsStore(state => state.setBacklightSettings);

  const usbConfigData: UsbConfigMap = useMemo(
    () => ({
      "USB Emulation Device": {
        vendor_id: "0x1d6b",
        product_id: "0x0104",
        serial_number: deviceId,
        manufacturer: "JetKVM",
        product: "USB Emulation Device",
      },
      "Logitech USB Input Device": {
        vendor_id: "0x046d",
        product_id: "0xc52b",
        serial_number: generatedSerialNumber,
        manufacturer: "Logitech (x64)",
        product: "Logitech USB Input Device",
      },
      "Wireless MultiMedia Keyboard": {
        vendor_id: "0x045e",
        product_id: "0x005f",
        serial_number: generatedSerialNumber,
        manufacturer: "Microsoft",
        product: "Wireless MultiMedia Keyboard",
      },
      "Multimedia Pro Keyboard": {
        vendor_id: "0x413c",
        product_id: "0x2011",
        serial_number: generatedSerialNumber,
        manufacturer: "Dell Inc.",
        product: "Multimedia Pro Keyboard",
      },
    }),
    [deviceId],
  );

  const handleBacklightSettingsChange = (settings: BacklightSettings) => {
    // If the user has set the display to dim after it turns off, set the dim_after
    // value to never.
    if (settings.dim_after > settings.off_after && settings.off_after != 0) {
      settings.dim_after = 0;
    }

    setBacklightSettings(settings);
    handleBacklightSettingsSave();
  };

  const handleBacklightSettingsSave = () => {
    send("setBacklightSettings", { params: settings.backlightSettings }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set backlight settings: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("Backlight settings updated successfully");
    });
  };
  const syncUsbConfigProduct = useCallback(() => {
    send("getUsbConfig", {}, resp => {
      if ("error" in resp) {
        console.error("Failed to load USB Config:", resp.error);
        notifications.error(
          `Failed to load USB Config: ${resp.error.data || "Unknown error"}`,
        );
      } else {
        console.log("syncUsbConfigProduct#getUsbConfig result:", resp.result);
        const usbConfigState = resp.result as UsbConfigState;
        const product = usbConfigs.map(u => u.value).includes(usbConfigState.product)
          ? usbConfigState.product
          : "custom";
        setUsbConfigProduct(product);
      }
    });
  }, [send]);

  const handleUsbConfigChange = useCallback(
    (usbConfig: USBConfig) => {
      send("setUsbConfig", { usbConfig }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set usb config: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        // setUsbConfigProduct(usbConfig.product);
        notifications.success(
          `USB Config set to ${usbConfig.manufacturer} ${usbConfig.product}`,
        );
        syncUsbConfigProduct();
      });
    },
    [send, syncUsbConfigProduct],
  );

  useEffect(() => {
    send("getBacklightSettings", {}, resp => {
      if ("error" in resp) {
        return notifications.error(
          `Failed to get backlight settings: ${resp.error.data || "Unknown error"}`,
        );
      }
      const result = resp.result as BacklightSettings;
      setBacklightSettings(result);
    });

    send("getDeviceID", {}, async resp => {
      if ("error" in resp) {
        return notifications.error(
          `Failed to get device ID: ${resp.error.data || "Unknown error"}`,
        );
      }
      setDeviceId(resp.result as string);
    });

    syncUsbConfigProduct();
  }, [send, setBacklightSettings, syncUsbConfigProduct]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Hardware"
        description="Configure display settings and hardware options for your JetKVM device"
      />
      <div className="space-y-4">
        <SettingsItem
          title="Display Brightness"
          description="Set the brightness of the display"
        >
          <SelectMenuBasic
            size="SM"
            label=""
            value={settings.backlightSettings.max_brightness.toString()}
            options={[
              { value: "0", label: "Off" },
              { value: "10", label: "Low" },
              { value: "35", label: "Medium" },
              { value: "64", label: "High" },
            ]}
            onChange={e => {
              settings.backlightSettings.max_brightness = parseInt(e.target.value);
              handleBacklightSettingsChange(settings.backlightSettings);
            }}
          />
        </SettingsItem>
        {settings.backlightSettings.max_brightness != 0 && (
          <>
            <SettingsItem
              title="Dim Display After"
              description="Set how long to wait before dimming the display"
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={settings.backlightSettings.dim_after.toString()}
                options={[
                  { value: "0", label: "Never" },
                  { value: "60", label: "1 Minute" },
                  { value: "300", label: "5 Minutes" },
                  { value: "600", label: "10 Minutes" },
                  { value: "1800", label: "30 Minutes" },
                  { value: "3600", label: "1 Hour" },
                ]}
                onChange={e => {
                  settings.backlightSettings.dim_after = parseInt(e.target.value);
                  handleBacklightSettingsChange(settings.backlightSettings);
                }}
              />
            </SettingsItem>
            <SettingsItem
              title="Turn off Display After"
              description="Period of inactivity before display automatically turns off"
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={settings.backlightSettings.off_after.toString()}
                options={[
                  { value: "0", label: "Never" },
                  { value: "300", label: "5 Minutes" },
                  { value: "600", label: "10 Minutes" },
                  { value: "1800", label: "30 Minutes" },
                  { value: "3600", label: "1 Hour" },
                ]}
                onChange={e => {
                  settings.backlightSettings.off_after = parseInt(e.target.value);
                  handleBacklightSettingsChange(settings.backlightSettings);
                }}
              />
            </SettingsItem>
          </>
        )}
        <p className="text-xs text-slate-600 dark:text-slate-400">
          The display will wake up when the connection state changes, or when touched.
        </p>
      </div>

      <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />

      <SettingsItem
        title="USB Device Emulation"
        description="Set a Preconfigured USB Device"
      >
        <SelectMenuBasic
          size="SM"
          label=""
          className="max-w-[192px]"
          value={usbConfigProduct}
          onChange={e => {
            if (e.target.value === "custom") {
              setUsbConfigProduct(e.target.value);
            } else {
              const usbConfig = usbConfigData[e.target.value];
              handleUsbConfigChange(usbConfig);
            }
          }}
          options={[...usbConfigs, { value: "custom", label: "Custom" }]}
        />
      </SettingsItem>
      {usbConfigProduct === "custom" && (
        <USBConfigDialog
          onSetUsbConfig={usbConfig => handleUsbConfigChange(usbConfig)}
          onRestoreToDefault={() =>
            handleUsbConfigChange(usbConfigData[usbConfigs[0].value])
          }
        />
      )}
    </div>
  );
}
