import { useCallback, useEffect, useState } from "react";
import { LuVolume2 } from "react-icons/lu";

import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { GridCard } from "@components/Card";
import { SettingsItem } from "@components/SettingsItem";
import { Button } from "@components/Button";
import notifications from "@/notifications";

export default function AudioPopover() {
  const { send } = useJsonRpc();
  const [audioOutputEnabled, setAudioOutputEnabled] = useState<boolean>(true);
  const [audioInputEnabled, setAudioInputEnabled] = useState<boolean>(true);
  const [usbAudioEnabled, setUsbAudioEnabled] = useState<boolean>(false);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    send("getAudioOutputEnabled", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to load audio output enabled:", resp.error);
      } else {
        setAudioOutputEnabled(resp.result as boolean);
      }
    });

    send("getAudioInputEnabled", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to load audio input enabled:", resp.error);
      } else {
        setAudioInputEnabled(resp.result as boolean);
      }
    });

    send("getUsbDevices", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to load USB devices:", resp.error);
      } else {
        const usbDevices = resp.result as { audio: boolean };
        setUsbAudioEnabled(usbDevices.audio || false);
      }
    });
  }, [send]);

  const handleAudioOutputEnabledToggle = useCallback(() => {
    const enabled = !audioOutputEnabled;
    setLoading(true);
    send("setAudioOutputEnabled", { enabled }, (resp: JsonRpcResponse) => {
      setLoading(false);
      if ("error" in resp) {
        notifications.error(
          `Failed to ${enabled ? "enable" : "disable"} audio output: ${resp.error.data || "Unknown error"}`,
        );
      } else {
        setAudioOutputEnabled(enabled);
        notifications.success(`Audio output ${enabled ? "enabled" : "disabled"}`);
      }
    });
  }, [send, audioOutputEnabled]);

  const handleAudioInputEnabledToggle = useCallback(() => {
    const enabled = !audioInputEnabled;
    setLoading(true);
    send("setAudioInputEnabled", { enabled }, (resp: JsonRpcResponse) => {
      setLoading(false);
      if ("error" in resp) {
        notifications.error(
          `Failed to ${enabled ? "enable" : "disable"} audio input: ${resp.error.data || "Unknown error"}`,
        );
      } else {
        setAudioInputEnabled(enabled);
        notifications.success(`Audio input ${enabled ? "enabled" : "disabled"}`);
      }
    });
  }, [send, audioInputEnabled]);

  return (
    <GridCard>
      <div className="space-y-4 p-4 py-3">
        <div className="space-y-4">
          <div className="flex items-center gap-2 text-slate-900 dark:text-slate-100">
            <LuVolume2 className="h-5 w-5" />
            <h3 className="font-semibold">Audio</h3>
          </div>

          <div className="space-y-3">
            <SettingsItem
              loading={loading}
              title="Audio Output"
              description="Enable audio from target to speakers"
            >
              <Button
                size="SM"
                theme={audioOutputEnabled ? "light" : "primary"}
                text={audioOutputEnabled ? "Disable" : "Enable"}
                onClick={handleAudioOutputEnabledToggle}
              />
            </SettingsItem>

            {usbAudioEnabled && (
              <>
                <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />

                <SettingsItem
                  loading={loading}
                  title="Audio Input (Microphone)"
                  description="Enable microphone input to target"
                >
                  <Button
                    size="SM"
                    theme={audioInputEnabled ? "light" : "primary"}
                    text={audioInputEnabled ? "Disable" : "Enable"}
                    onClick={handleAudioInputEnabledToggle}
                  />
                </SettingsItem>
              </>
            )}
          </div>
        </div>
      </div>
    </GridCard>
  );
}
