import { GridCard } from "@/components/Card";
import {useCallback, useState} from "react";
import { Button } from "@components/Button";
import LogoBlueIcon from "@/assets/logo-blue.svg";
import LogoWhiteIcon from "@/assets/logo-white.svg";
import Modal from "@components/Modal";
import { InputFieldWithLabel } from "./InputField";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { useUsbConfigModalStore } from "@/hooks/stores";

export default function USBConfigDialog({
  open,
  setOpen,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
}) {
  return (
    <Modal open={open} onClose={() => setOpen(false)}>
      <Dialog setOpen={setOpen} />
    </Modal>
  );
}

export function Dialog({ setOpen }: { setOpen: (open: boolean) => void }) {
  const { modalView, setModalView } = useUsbConfigModalStore();
  const [error, setError] = useState<string | null>(null);

  const [send] = useJsonRpc();

  const handleUsbConfigChange = useCallback((usbConfig: object) => {
    send("setUsbConfig", { usbConfig }, resp => {
      if ("error" in resp) {
        setError(`Failed to update USB Config: ${resp.error.data || "Unknown error"}`,);
        return;
      }
      setModalView("updateUsbConfigSuccess");
    });
  }, [send, setModalView]);

  return (
    <GridCard cardClassName="relative max-w-lg mx-auto text-left pointer-events-auto dark:bg-slate-800">
      <div className="p-10">
        {modalView === "updateUsbConfig" && (
          <UpdateUsbConfigModal
            onSetUsbConfig={handleUsbConfigChange}
            onCancel={() => setOpen(false)}
            error={error}
          />
        )}

        {modalView === "updateUsbConfigSuccess" && (
          <SuccessModal
            headline="USB Configuration Updated Successfully"
            description="You've successfully updated the USB Configuration"
            onClose={() => setOpen(false)}
          />
        )}
      </div>
    </GridCard>
  );
}

function UpdateUsbConfigModal({
  onSetUsbConfig,
  onCancel,
  error,
}: {
  onSetUsbConfig: (usb_config: object) => void;
  onCancel: () => void;
  error: string | null;
}) {
  const [usbConfig, setUsbConfig] = useState({
    usb_vendor_id: '',
    usb_product_id: '',
    usb_serial_number: '',
    usb_manufacturer: '',
    usb_product: '',
  })
  const handleUsbVendorIdChange = (vendorId: string) => {
    setUsbConfig({... usbConfig, usb_vendor_id: vendorId})
  };

  const handleUsbProductIdChange = (productId: string) => {
    setUsbConfig({... usbConfig, usb_product_id: productId})
  };

  const handleUsbSerialChange = (serialNumber: string) => {
    setUsbConfig({... usbConfig, usb_serial_number: serialNumber})
  };

  const handleUsbManufacturer = (manufacturer: string) => {
    setUsbConfig({... usbConfig, usb_manufacturer: manufacturer})
  };

  const handleUsbProduct = (name: string) => {
    setUsbConfig({... usbConfig, usb_product: name})
  };

  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div>
        <img src={LogoWhiteIcon} alt="" className="h-[24px] hidden dark:block" />
        <img src={LogoBlueIcon} alt="" className="h-[24px] dark:hidden" />
      </div>
      <div className="space-y-4">
        <div>
          <h2 className="text-lg font-semibold dark:text-white">USB Emulation Configuration</h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            Set custom USB parameters to control how the USB device is emulated.
            The device will rebind once the parameters are updated.
          </p>
        </div>
        <InputFieldWithLabel
          required
          label="Vendor ID"
          placeholder="Enter Vendor ID"
          value={usbConfig.usb_vendor_id || ""}
          onChange={e => handleUsbVendorIdChange(e.target.value)}
        />
        <InputFieldWithLabel
          required
          label="Product ID"
          placeholder="Enter Product ID"
          value={usbConfig.usb_product_id || ""}
          onChange={e => handleUsbProductIdChange(e.target.value)}
        />
        <InputFieldWithLabel
          required
          label="Serial Number"
          placeholder="Enter Serial Number"
          value={usbConfig.usb_serial_number || ""}
          onChange={e => handleUsbSerialChange(e.target.value)}
        />
        <InputFieldWithLabel
          required
          label="Manufacturer"
          placeholder="Enter Manufacturer"
          value={usbConfig.usb_manufacturer || ""}
          onChange={e => handleUsbManufacturer(e.target.value)}
        />
        <InputFieldWithLabel
          required
          label="Product Name"
          placeholder="Enter Product Name"
          value={usbConfig.usb_product || ""}
          onChange={e => handleUsbProduct(e.target.value)}
        />
        <div className="flex gap-x-2">
          <Button
            size="SM"
            theme="primary"
            text="Update USB Config"
            onClick={() => onSetUsbConfig(usbConfig)}
          />
          <Button size="SM" theme="light" text="Not Now" onClick={onCancel} />
        </div>
        {error && <p className="text-sm text-red-500">{error}</p>}
      </div>
    </div>
  );
}

function SuccessModal({
  headline,
  description,
  onClose,
}: {
  headline: string;
  description: string;
  onClose: () => void;
}) {
  return (
    <div className="flex flex-col items-start justify-start w-full max-w-lg space-y-4 text-left">
      <div>
        <img src={LogoWhiteIcon} alt="" className="h-[24px] hidden dark:block" />
        <img src={LogoBlueIcon} alt="" className="h-[24px] dark:hidden" />
      </div>
      <div className="space-y-4">
        <div>
          <h2 className="text-lg font-semibold dark:text-white">{headline}</h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">{description}</p>
        </div>
        <Button size="SM" theme="primary" text="Close" onClick={onClose} />
      </div>
    </div>
  );
}
