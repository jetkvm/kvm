import { useEffect } from "react";

import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { useSettingsStore } from "@/hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import Checkbox from "@components/Checkbox";

import notifications from "../notifications";

export default function SettingsAudioRoute() {
  const { send } = useJsonRpc();
  const settings = useSettingsStore();

  // Fetch current audio settings on mount
  useEffect(() => {
    send("getAudioOutputSource", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        return;
      }
      const source = resp.result as string;
      settings.audioOutputSource = source;
    });

    send("getAudioMode", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        return;
      }
      const mode = resp.result as string;
      settings.audioMode = mode;
    });

    send("getAudioOutputEnabled", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        return;
      }
      settings.audioOutputEnabled = resp.result as boolean;
    });

    send("getAudioInputEnabled", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        return;
      }
      settings.audioInputEnabled = resp.result as boolean;
    });
  }, [send]);

  const handleAudioOutputSourceChange = (source: string) => {
    send("setAudioOutputSource", { source }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set audio output source: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      settings.audioOutputSource = source;
      notifications.success("Audio output source updated successfully");
    });
  };

  const handleAudioModeChange = (mode: string) => {
    send("setAudioMode", { mode }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set audio mode: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      settings.audioMode = mode;
      notifications.success("Audio mode updated successfully. Changes will take effect on next connection.");
    });
  };

  const handleAudioOutputEnabledChange = (enabled: boolean) => {
    send("setAudioOutputEnabled", { enabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to ${enabled ? "enable" : "disable"} audio output: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      settings.audioOutputEnabled = enabled;
      notifications.success(`Audio output ${enabled ? "enabled" : "disabled"} successfully`);
    });
  };

  const handleAudioInputEnabledChange = (enabled: boolean) => {
    send("setAudioInputEnabled", { enabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to ${enabled ? "enable" : "disable"} audio input: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      settings.audioInputEnabled = enabled;
      notifications.success(`Audio input ${enabled ? "enabled" : "disabled"} successfully`);
    });
  };

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Audio"
        description="Configure audio input and output settings for your JetKVM device"
      />
      <div className="space-y-4">
        <SettingsItem
          title="Audio Output"
          description="Enable or disable audio from the remote computer"
        >
          <Checkbox
            checked={settings.audioOutputEnabled || false}
            onChange={(e) => handleAudioOutputEnabledChange(e.target.checked)}
          />
        </SettingsItem>

        {settings.audioOutputEnabled && (
          <SettingsItem
            title="Audio Output Source"
            description="Select the audio capture device (HDMI or USB)"
          >
            <SelectMenuBasic
              size="SM"
              label=""
              value={settings.audioOutputSource || "usb"}
              options={[
                { value: "hdmi", label: "HDMI" },
                { value: "usb", label: "USB" },
              ]}
              onChange={e => {
                handleAudioOutputSourceChange(e.target.value);
              }}
            />
          </SettingsItem>
        )}

        <SettingsItem
          title="Audio Input"
          description="Enable or disable microphone audio to the remote computer"
        >
          <Checkbox
            checked={settings.audioInputEnabled || false}
            onChange={(e) => handleAudioInputEnabledChange(e.target.checked)}
          />
        </SettingsItem>

        <div className="border-t border-slate-200 pt-4 dark:border-slate-700">
          <h3 className="mb-2 text-sm font-medium">Advanced</h3>
          <SettingsItem
            title="Audio Processing Mode"
            description="In-process mode uses less CPU but subprocess mode provides better isolation"
          >
            <SelectMenuBasic
              size="SM"
              label=""
              value={settings.audioMode || "subprocess"}
              options={[
                { value: "subprocess", label: "Subprocess (Recommended)" },
                { value: "in-process", label: "In-Process" },
              ]}
              onChange={e => {
                handleAudioModeChange(e.target.value);
              }}
            />
          </SettingsItem>
          <p className="mt-2 text-xs text-slate-600 dark:text-slate-400">
            Changing the audio mode will take effect when the next WebRTC connection is established.
          </p>
        </div>
      </div>
    </div>
  );
}
