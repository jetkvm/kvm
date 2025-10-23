import { useMemo, useCallback, useEffect, useState } from "react";

import { UsbConfigState } from "@hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { Button } from "@components/Button";
import Fieldset from "@components/Fieldset";
import { InputFieldWithLabel } from "@components/InputField";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsItem } from "@components/SettingsItem";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

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
    label: m.usb_config_default(),
    value: "USB Emulation Device",
  },
  {
    label: m.usb_config_logitech(),
    value: "Logitech USB Input Device",
  },
  {
    label: m.usb_config_microsoft(),
    value: "Wireless MultiMedia Keyboard",
  },
  {
    label: m.usb_config_dell(),
    value: "Multimedia Pro Keyboard",
  },
];

type UsbConfigMap = Record<string, USBConfig>;

export function UsbInfoSetting() {
  const { send } = useJsonRpc();
  const [loading, setLoading] = useState(false);

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
    send("getUsbConfig", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to load USB Config:", resp.error);
        notifications.error(
          m.usb_config_failed_load({ error: String(resp.error.data || m.unknown_error()) }),
        );
      } else {
        const usbConfigState = resp.result as UsbConfigState;
        console.log("syncUsbConfigProduct#getUsbConfig result:", usbConfigState);
        const product = usbConfigs.map(u => u.value).includes(usbConfigState.product)
          ? usbConfigState.product
          : "custom";
        setUsbConfigProduct(product);
      }
    });
  }, [send]);

  const handleUsbConfigChange = useCallback(
    (usbConfig: USBConfig) => {
      setLoading(true);
      send("setUsbConfig", { usbConfig }, async (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            m.usb_config_failed_set({ error: String(resp.error.data || m.unknown_error()) }),
          );
          setLoading(false);
          return;
        }

        // We need some time to ensure the USB devices are updated
        await new Promise(resolve => setTimeout(resolve, 2000));
        setLoading(false);
        notifications.success(
          m.usb_config_set_success({ manufacturer: usbConfig.manufacturer, product: usbConfig.product }),
        );

        syncUsbConfigProduct();
      });
    },
    [send, syncUsbConfigProduct],
  );

  useEffect(() => {
    send("getDeviceID", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        return notifications.error(
          `Failed to get device ID: ${resp.error.data || m.unknown_error()}`,
        );
      }
      setDeviceId(resp.result as string);
    });

    syncUsbConfigProduct();
  }, [send, syncUsbConfigProduct]);

  return (
    <Fieldset disabled={loading} className="space-y-4">
      <SettingsItem
        loading={loading}
        title={m.usb_config_identifiers_title()}
        description={m.usb_config_identifiers_description()}
      >
        <SelectMenuBasic
          size="SM"
          label=""
          className="max-w-[192px]"
          value={usbConfigProduct}
          fullWidth
          onChange={e => {
            if (e.target.value === "custom") {
              setUsbConfigProduct(e.target.value);
            } else {
              const usbConfig = usbConfigData[e.target.value];
              handleUsbConfigChange(usbConfig);
            }
          }}
          options={[...usbConfigs, { value: "custom", label: m.usb_config_custom() }]}
        />
      </SettingsItem>
      {usbConfigProduct === "custom" && (
        <div className="ml-2 space-y-4 border-l border-slate-800/10 pl-4 dark:border-slate-300/20 ">
          <USBConfigDialog
            loading={loading}
            onSetUsbConfig={usbConfig => handleUsbConfigChange(usbConfig)}
            onRestoreToDefault={() =>
              handleUsbConfigChange(usbConfigData[usbConfigs[0].value])
            }
          />
        </div>
      )}
    </Fieldset>
  );
}

function USBConfigDialog({
  loading,
  onSetUsbConfig,
  onRestoreToDefault,
}: {
  loading: boolean;
  onSetUsbConfig: (usbConfig: USBConfig) => void;
  onRestoreToDefault: () => void;
}) {
  const [usbConfigState, setUsbConfigState] = useState<USBConfig>({
    vendor_id: "",
    product_id: "",
    serial_number: "",
    manufacturer: "",
    product: "",
  });

  const { send } = useJsonRpc();

  const syncUsbConfig = useCallback(() => {
    send("getUsbConfig", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to load USB Config:", resp.error);
      } else {
        setUsbConfigState(resp.result as UsbConfigState);
      }
    });
  }, [send, setUsbConfigState]);

  // Load stored usb config from the backend
  useEffect(() => {
    syncUsbConfig();
  }, [syncUsbConfig]);

  const handleUsbVendorIdChange = (value: string) => {
    setUsbConfigState({ ...usbConfigState, vendor_id: value });
  };

  const handleUsbProductIdChange = (value: string) => {
    setUsbConfigState({ ...usbConfigState, product_id: value });
  };

  const handleUsbSerialChange = (value: string) => {
    setUsbConfigState({ ...usbConfigState, serial_number: value });
  };

  const handleUsbManufacturer = (value: string) => {
    setUsbConfigState({ ...usbConfigState, manufacturer: value });
  };

  const handleUsbProduct = (value: string) => {
    setUsbConfigState({ ...usbConfigState, product: value });
  };

  return (
    <div className="">
      <div className="grid grid-cols-2 gap-4">
        <InputFieldWithLabel
          required
          label={m.usb_config_vendor_id_label()}
          placeholder={m.usb_config_vendor_id_placeholder()}
          pattern="^0[xX][\da-fA-F]{4}$"
          defaultValue={usbConfigState?.vendor_id}
          onChange={e => handleUsbVendorIdChange(e.target.value)}
        />
        <InputFieldWithLabel
          required
          label={m.usb_config_product_id_label()}
          placeholder={m.usb_config_product_id_placeholder()}
          pattern="^0[xX][\da-fA-F]{4}$"
          defaultValue={usbConfigState?.product_id}
          onChange={e => handleUsbProductIdChange(e.target.value)}
        />
        <InputFieldWithLabel
          required
          label={m.usb_config_serial_number_label()}
          placeholder={m.usb_config_serial_number_placeholder()}
          defaultValue={usbConfigState?.serial_number}
          onChange={e => handleUsbSerialChange(e.target.value)}
        />
        <InputFieldWithLabel
          required
          label={m.usb_config_manufacturer_label()}
          placeholder={m.usb_config_manufacturer_placeholder()}
          defaultValue={usbConfigState?.manufacturer}
          onChange={e => handleUsbManufacturer(e.target.value)}
        />
        <InputFieldWithLabel
          required
          label={m.usb_config_product_name_label()}
          placeholder={m.usb_config_product_name_placeholder()}
          defaultValue={usbConfigState?.product}
          onChange={e => handleUsbProduct(e.target.value)}
        />
      </div>
      <div className="mt-6 flex gap-x-2">
        <Button
          loading={loading}
          size="SM"
          theme="primary"
          text={m.usb_config_update_identifiers()}
          onClick={() => onSetUsbConfig(usbConfigState)}
        />
        <Button
          size="SM"
          theme="light"
          text={m.usb_config_restore_default()}
          onClick={onRestoreToDefault}
        />
      </div>
    </div>
  );
}
