import { useCallback, useEffect, useState } from "react";
import { LuVolume2 } from "react-icons/lu";

import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { GridCard } from "@components/Card";
import { SettingsItem } from "@components/SettingsItem";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { Button } from "@components/Button";
import notifications from "@/notifications";

export default function AudioPopover() {
  const { send } = useJsonRpc();
  const [audioOutputSource, setAudioOutputSource] = useState<string>("hdmi");
  const [audioOutputEnabled, setAudioOutputEnabled] = useState<boolean>(true);
  const [audioInputEnabled, setAudioInputEnabled] = useState<boolean>(true);
  const [usbAudioEnabled, setUsbAudioEnabled] = useState<boolean>(false);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    // Load current audio settings
    send("getAudioOutputSource", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to load audio output source:", resp.error);
      } else {
        setAudioOutputSource(resp.result as string);
      }
    });

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

  const handleAudioOutputSourceChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const newSource = e.target.value;
      setLoading(true);
      send("setAudioOutputSource", { source: newSource }, (resp: JsonRpcResponse) => {
        setLoading(false);
        if ("error" in resp) {
          notifications.error(
            `Failed to set audio output source: ${resp.error.data || "Unknown error"}`,
          );
        } else {
          setAudioOutputSource(newSource);
          notifications.success(`Audio output source set to ${newSource.toUpperCase()}`);
        }
      });
    },
    [send],
  );

  const handleAudioOutputEnabledToggle = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const enabled = e.target.checked;
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
    },
    [send],
  );

  const handleAudioInputEnabledToggle = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const enabled = e.target.checked;
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
    },
    [send],
  );

  return (
    <GridCard>
      <div className="space-y-4 p-4 py-3">
        <div className="space-y-4">
          <div className="flex items-center gap-2 text-slate-900 dark:text-slate-100">
            <LuVolume2 className="h-5 w-5" />
            <h3 className="font-semibold">Audio Settings</h3>
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
                onClick={() => handleAudioOutputEnabledToggle({ target: { checked: !audioOutputEnabled } } as any)}
              />
            </SettingsItem>

            <SettingsItem
              loading={loading}
              title="Audio Output Source"
              description={usbAudioEnabled ? "Select where to capture audio from" : "Enable USB Audio to use USB as source"}
            >
              <SelectMenuBasic
                size="SM"
                label=""
                className="max-w-[180px]"
                value={audioOutputSource}
                fullWidth
                disabled={!audioOutputEnabled}
                onChange={handleAudioOutputSourceChange}
                options={
                  usbAudioEnabled
                    ? [
                        { label: "HDMI", value: "hdmi" },
                        { label: "USB", value: "usb" },
                      ]
                    : [{ label: "HDMI", value: "hdmi" }]
                }
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
                    onClick={() => handleAudioInputEnabledToggle({ target: { checked: !audioInputEnabled } } as any)}
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
