import { useMemo } from "react";

import { useCallback } from "react";

import { useEffect, useState } from "react";
import { UsbConfigState } from "../hooks/stores";
import { useJsonRpc } from "../hooks/useJsonRpc";
import notifications from "../notifications";
import { SettingsItem } from "../routes/devices.$id.settings";
import { SelectMenuBasic } from "./SelectMenuBasic";
import USBConfigDialog from "./USBConfigDialog";

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

export function UsbConfigSetting() {
  const [send] = useJsonRpc();

  const [usbConfigProduct, setUsbConfigProduct] = useState("");
  const [deviceId, setDeviceId] = useState("");
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
    send("getDeviceID", {}, async resp => {
      if ("error" in resp) {
        return notifications.error(
          `Failed to get device ID: ${resp.error.data || "Unknown error"}`,
        );
      }
      setDeviceId(resp.result as string);
    });

    syncUsbConfigProduct();
  }, [send, syncUsbConfigProduct]);

  return (
    <>
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
    </>
  );
}
