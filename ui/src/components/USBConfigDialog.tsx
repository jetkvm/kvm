import { GridCard } from "@/components/Card";
import {useCallback, useEffect, useState} from "react";
import { Button } from "@components/Button";
import LogoBlueIcon from "@/assets/logo-blue.svg";
import LogoWhiteIcon from "@/assets/logo-white.svg";
import Modal from "@components/Modal";
import { InputFieldWithLabel } from "./InputField";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { useUsbConfigModalStore } from "@/hooks/stores";
import ExtLink from "@components/ExtLink";
import { UsbConfigState } from "@/hooks/stores"

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
        setError(`Failed to update USB Config: ${resp.error.data || "Unknown error"}`);
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
  const [usbConfigState, setUsbConfigState] = useState<UsbConfigState>({
    vendor_id: '',
    product_id: '',
    serial_number: '',
    manufacturer: '',
    product: ''
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
    setUsbConfigState({... usbConfigState, vendor_id: value})
  };

  const handleUsbProductIdChange = (value: string) => {
    setUsbConfigState({... usbConfigState, product_id: value})
  };

  const handleUsbSerialChange = (value: string) => {
    setUsbConfigState({... usbConfigState, serial_number: value})
  };

  const handleUsbManufacturer = (value: string) => {
    setUsbConfigState({... usbConfigState, manufacturer: value})
  };

  const handleUsbProduct = (value: string) => {
    setUsbConfigState({... usbConfigState, product: value})
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
          <div className="flex justify-start mt-4 text-xs text-slate-500 dark:text-slate-400">
            <ExtLink
              href={`https://the-sz.com/products/usbid/index.php`}
              className="hover:underline"
            >
              Look up USB Device IDs here
            </ExtLink>
          </div>
        </div>
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
        <div className="flex gap-x-2">
          <Button
            size="SM"
            theme="primary"
            text="Update USB Config"
            onClick={() => onSetUsbConfig(usbConfigState)}
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