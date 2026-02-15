import { LuPlus, LuSend, LuTrash2 } from "react-icons/lu";
import { useState } from "react";

import { m } from "@localizations/messages.js";
import { Button } from "@components/Button";
import Card from "@components/Card";
import { FieldError } from "@components/InputField";

export interface StoredDevice {
  name: string;
  macAddress: string;
}

interface DeviceListProps {
  storedDevices: StoredDevice[];
  errorMessage: string | null;
  onSendMagicPacket: (macAddress: string) => void;
  onDeleteDevice: (index: number) => void;
  onCancelWakeOnLanModal: () => void;
  setShowAddForm: (show: boolean) => void;
}

export default function DeviceList({
  storedDevices,
  errorMessage,
  onSendMagicPacket,
  onDeleteDevice,
  onCancelWakeOnLanModal,
  setShowAddForm,
}: DeviceListProps) {
  const [deleteIndex, setDeleteIndex] = useState<number | null>(null);

  const handleDelete = (index: number) => {
    setDeleteIndex(index);
  };

  const confirmDelete = () => {
    if (deleteIndex !== null) {
      onDeleteDevice(deleteIndex);
      setDeleteIndex(null);
    }
  };

  const cancelDelete = () => {
    setDeleteIndex(null);
  };

  return (
    <div className="space-y-4">
      <Card className="animate-fadeIn opacity-0">
        <div className="w-full divide-y divide-slate-700/30 dark:divide-slate-600/30">
          {storedDevices.map((device, index) => (
            <div key={index} className="flex items-center justify-between gap-x-2 p-3">
              <div className="space-y-0.5">
                <p className="text-sm leading-none font-semibold text-slate-900 dark:text-slate-100">
                  {device?.name}
                </p>
                <p className="text-sm text-slate-600 dark:text-slate-400">
                  {device.macAddress?.toLowerCase()}
                </p>
              </div>

              {errorMessage && <FieldError error={errorMessage} />}
              <div className="flex items-center space-x-2">
                <Button
                  size="XS"
                  theme="light"
                  text={m.wake_on_lan_device_list_wake()}
                  LeadingIcon={LuSend}
                  onClick={() => onSendMagicPacket(device.macAddress)}
                />
                <Button
                  size="XS"
                  theme="danger"
                  LeadingIcon={LuTrash2}
                  onClick={() => handleDelete(index)}
                  aria-label={m.wake_on_lan_device_list_delete_device()}
                />
              </div>
            </div>
          ))}
        </div>
      </Card>
      {deleteIndex !== null && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <Card className="w-full max-w-xs animate-fadeIn p-6">
            <h3 className="mb-2 text-lg font-semibold text-slate-900 dark:text-slate-100">
              {m.wake_on_lan_device_list_delete_device()}
            </h3>
            <p className="mb-4 text-sm text-slate-700 dark:text-slate-300">
              {m.wake_on_lan_device_list_confirm_delete_message({
                name: storedDevices[deleteIndex]?.name || "",
              })}
            </p>
            <div className="flex justify-end space-x-2">
              <Button size="SM" theme="blank" text={m.cancel()} onClick={cancelDelete} />
              <Button size="SM" theme="danger" text={m.delete()} onClick={confirmDelete} />
            </div>
          </Card>
        </div>
      )}
      <div
        className="flex animate-fadeIn items-center justify-end space-x-2 opacity-0"
        style={{
          animationDuration: "0.7s",
          animationDelay: "0.2s",
        }}
      >
        <Button size="SM" theme="blank" text={m.close()} onClick={onCancelWakeOnLanModal} />
        <Button
          size="SM"
          theme="primary"
          text={m.wake_on_lan_device_list_add_new_device()}
          onClick={() => setShowAddForm(true)}
          LeadingIcon={LuPlus}
        />
      </div>
    </div>
  );
}
