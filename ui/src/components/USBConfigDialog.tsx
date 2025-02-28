import { Button } from "@components/Button";
import { InputFieldWithLabel } from "./InputField";
import { UsbConfigState } from "@/hooks/stores";
import { useEffect, useCallback, useState } from "react";
import { useJsonRpc } from "../hooks/useJsonRpc";
import { USBConfig } from "./UsbConfigSetting";

export default function UpdateUsbConfigModal({
  onSetUsbConfig,
  onRestoreToDefault,
}: {
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

  const [send] = useJsonRpc();

  const syncUsbConfig = useCallback(() => {
    send("getUsbConfig", {}, resp => {
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
    <div className="space-y-6">
      <div className="grid grid-cols-2 gap-4">
        <InputFieldWithLabel
          required
          label="Vendor ID"
          placeholder="Enter Vendor ID"
          pattern="^0[xX][\da-fA-F]{4}$"
          defaultValue={usbConfigState?.vendor_id}
          onChange={e => handleUsbVendorIdChange(e.target.value)}
        />
        <InputFieldWithLabel
          required
          label="Product ID"
          placeholder="Enter Product ID"
          pattern="^0[xX][\da-fA-F]{4}$"
          defaultValue={usbConfigState?.product_id}
          onChange={e => handleUsbProductIdChange(e.target.value)}
        />
        <InputFieldWithLabel
          required
          label="Serial Number"
          placeholder="Enter Serial Number"
          defaultValue={usbConfigState?.serial_number}
          onChange={e => handleUsbSerialChange(e.target.value)}
        />
        <InputFieldWithLabel
          required
          label="Manufacturer"
          placeholder="Enter Manufacturer"
          defaultValue={usbConfigState?.manufacturer}
          onChange={e => handleUsbManufacturer(e.target.value)}
        />
        <InputFieldWithLabel
          required
          label="Product Name"
          placeholder="Enter Product Name"
          defaultValue={usbConfigState?.product}
          onChange={e => handleUsbProduct(e.target.value)}
        />
      </div>
      <div className="flex gap-x-2">
        <Button
          size="SM"
          theme="primary"
          text="Update USB Config"
          onClick={() => onSetUsbConfig(usbConfigState)}
        />
        <Button
          size="SM"
          theme="light"
          text="Restore to Default"
          onClick={onRestoreToDefault}
        />
      </div>
    </div>
  );
}
