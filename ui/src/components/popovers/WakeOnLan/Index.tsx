import { useCallback, useEffect, useState } from "react";
import { useClose } from "@headlessui/react";

import { m } from "@localizations/messages.js";
import { GridCard } from "@components/Card";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useRTCStore, useUiStore } from "@hooks/stores";
import notifications from "@/notifications";

import EmptyStateCard from "./EmptyStateCard";
import DeviceList, { StoredDevice } from "./DeviceList";
import AddDeviceForm from "./AddDeviceForm";

export default function WakeOnLanModal() {
  const [storedDevices, setStoredDevices] = useState<StoredDevice[]>([]);
  const [showAddForm, setShowAddForm] = useState(false);
  const { setDisableVideoFocusTrap } = useUiStore();
  const { rpcDataChannel } = useRTCStore();
  const { send } = useJsonRpc();
  const close = useClose();
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [addDeviceErrorMessage, setAddDeviceErrorMessage] = useState<string | null>(null);

  const onCancelWakeOnLanModal = useCallback(() => {
    setDisableVideoFocusTrap(false);
    close();
  }, [close, setDisableVideoFocusTrap]);

  const onSendMagicPacket = useCallback(
    (macAddress: string) => {
      setErrorMessage(null);
      if (rpcDataChannel?.readyState !== "open") return;

      send("sendWOLMagicPacket", { macAddress }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          const isInvalid = resp.error.data?.includes("invalid MAC address");
          if (isInvalid) {
            setErrorMessage(m.wake_on_lan_invalid_mac());
          } else {
            setErrorMessage(m.wake_on_lan_failed_send_magic());
          }
        } else {
          notifications.success(m.wake_on_lan_magic_sent_success());
          setDisableVideoFocusTrap(false);
          close();
        }
      });
    },
    [close, rpcDataChannel?.readyState, send, setDisableVideoFocusTrap],
  );

  const syncStoredDevices = useCallback(() => {
    send("getWakeOnLanDevices", {}, (resp: JsonRpcResponse) => {
      if ("result" in resp) {
        setStoredDevices(resp.result as StoredDevice[]);
      } else {
        console.error("Failed to load Wake-on-LAN devices:", resp.error);
      }
    });
  }, [send, setStoredDevices]);

  // Load stored devices from the backend
  useEffect(() => {
    syncStoredDevices();
  }, [syncStoredDevices]);

  const onDeleteDevice = useCallback(
    (index: number) => {
      const updatedDevices = storedDevices.filter((_, i) => i !== index);

      send("setWakeOnLanDevices", { params: { devices: updatedDevices } }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          console.error("Failed to update Wake-on-LAN devices:", resp.error);
        } else {
          syncStoredDevices();
        }
      });
    },
    [send, storedDevices, syncStoredDevices],
  );

  const onAddDevice = useCallback(
    (name: string, macAddress: string) => {
      if (!name || !macAddress) return;
      const updatedDevices = [...storedDevices, { name, macAddress }];
      console.log("updatedDevices", updatedDevices);
      send("setWakeOnLanDevices", { params: { devices: updatedDevices } }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          console.error("Failed to add Wake-on-LAN device:", resp.error);
          setAddDeviceErrorMessage(m.wake_on_lan_failed_add_device());
        } else {
          setShowAddForm(false);
          syncStoredDevices();
        }
      });
    },
    [send, storedDevices, syncStoredDevices],
  );

  return (
    <GridCard>
      <div className="space-y-4 p-4 py-3">
        <div className="grid h-full grid-rows-(--grid-headerBody)">
          <div className="space-y-4">
            <SettingsPageHeader
              title={m.wake_on_lan()}
              description={m.wake_on_lan_description()}
            />

            {showAddForm ? (
              <AddDeviceForm
                setShowAddForm={setShowAddForm}
                errorMessage={addDeviceErrorMessage}
                setErrorMessage={setAddDeviceErrorMessage}
                onAddDevice={onAddDevice}
              />
            ) : storedDevices.length === 0 ? (
              <EmptyStateCard
                onCancelWakeOnLanModal={onCancelWakeOnLanModal}
                setShowAddForm={setShowAddForm}
              />
            ) : (
              <DeviceList
                storedDevices={storedDevices}
                errorMessage={errorMessage}
                onSendMagicPacket={onSendMagicPacket}
                onDeleteDevice={onDeleteDevice}
                onCancelWakeOnLanModal={onCancelWakeOnLanModal}
                setShowAddForm={setShowAddForm}
              />
            )}
          </div>
        </div>
      </div>
    </GridCard>
  );
}
