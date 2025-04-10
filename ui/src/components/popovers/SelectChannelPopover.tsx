import { useCallback, useEffect, useState } from "react";
import { useClose } from "@headlessui/react";

import { GridCard } from "@components/Card";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { RemoteKVMSwitchSelectedChannel, useUiStore } from "@/hooks/stores";


export default function SelectChannelPopover() {
  const [send] = useJsonRpc();
  const [switchChannelNames, setSwitchChannelNames] = useState<RemoteKVMSwitchSelectedChannel[]>([]);
  const remoteKvmSelectedChannel = useUiStore(state => state.remoteKvmSelectedChannel);
  const setRemoteKvmSelectedChannel = useUiStore(state => state.setRemoteKvmSelectedChannel);
  const close = useClose();

  useEffect(() => {
    send("getKvmSwitchChannels", {}, resp => {
      if ("error" in resp) {
        notifications.error(`Failed to get switch channels: ${resp.error.data || "Unknown error"}`);
        return
      }
      setSwitchChannelNames((resp.result as { name: string, id: string }[]).map(x => ({ name: x.name, id: x.id })));
    })

  }, [send]);

  const onChannelSelected = useCallback((selection: RemoteKVMSwitchSelectedChannel | null) => {
    if (selection === null) {
      close();
      return;
    }

    let selectedItem = selection!;

    send("setKvmSwitchSelectedChannel", { id: selectedItem.id }, resp => {
      if ("error" in resp) {
        notifications.error(`Failed to set switch channel: ${resp.error.data || "Unknown error"}`);
        return
      }

      notifications.success(`Remote KVM switch set to channel ${selectedItem.name}`);
      setRemoteKvmSelectedChannel(selectedItem);
      close();
    })
  }, [send, close, setRemoteKvmSelectedChannel]);

  const onChannelSelectedById = useCallback((id: string) => {
    onChannelSelected(switchChannelNames.find(x => x.id === id) || null);
  }, [onChannelSelected, switchChannelNames]);

  let options = []

  if (!remoteKvmSelectedChannel) {
    options.push({
      label: "Select Channel",
      value: ""
    });
  }

  if (switchChannelNames.length > 0) {
    options = options.concat(switchChannelNames.map(x => ({ label: x.name, value: x.id })))
  }

  return (
    <GridCard>
      <div className="space-y-4 p-4 py-3">
        <div className="grid h-full grid-rows-headerBody">
          <div className="h-full space-y-4">
            <div className="space-y-4">
              <SettingsPageHeader
                title="Select Channel"
                description="Select channel on the remote KVM"
              />

              <div
                className="animate-fadeIn space-y-2 opacity-0"
                style={{
                  animationDuration: "0.7s",
                  animationDelay: "0.1s",
                }}
              >
                <div>
                  <SelectMenuBasic
                    value={remoteKvmSelectedChannel ? remoteKvmSelectedChannel.id : ""}
                    size="MD"
                    onChange={e => onChannelSelectedById(e.target.value)}
                    options={options}
                    fullWidth
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </GridCard>
  );
}
