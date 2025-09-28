import { LuPlus, LuSend, LuTrash2 } from "react-icons/lu";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/Button";
import Card from "@/components/Card";
import { FieldError } from "@/components/InputField";

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
  const { t } = useTranslation();
  return (
    <div className="space-y-4">
      <Card className="animate-fadeIn opacity-0">
        <div className="w-full divide-y divide-slate-700/30 dark:divide-slate-600/30">
          {storedDevices.map((device, index) => (
            <div key={index} className="flex items-center justify-between gap-x-2 p-3">
              <div className="space-y-0.5">
                <p className="text-sm font-semibold leading-none text-slate-900 dark:text-slate-100">
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
                  text={t('Wake')}
                  LeadingIcon={LuSend}
                  onClick={() => onSendMagicPacket(device.macAddress)}
                />
                <Button
                  size="XS"
                  theme="danger"
                  LeadingIcon={LuTrash2}
                  onClick={() => onDeleteDevice(index)}
                  aria-label={t('Delete_device')}
                />
              </div>
            </div>
          ))}
        </div>
      </Card>
      <div
        className="flex animate-fadeIn opacity-0 items-center justify-end space-x-2"
        style={{
          animationDuration: "0.7s",
          animationDelay: "0.2s",
        }}
      >
        <Button size="SM" theme="blank" text={t('Close')} onClick={onCancelWakeOnLanModal} />
        <Button
          size="SM"
          theme="primary"
          text={t('Add_New_Device')}
          onClick={() => setShowAddForm(true)}
          LeadingIcon={LuPlus}
        />
      </div>
    </div>
  );
}
