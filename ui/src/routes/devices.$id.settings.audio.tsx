import { useEffect } from "react";

import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { useSettingsStore } from "@/hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
// import { SelectMenuBasic } from "@components/SelectMenuBasic";
import Checkbox from "@components/Checkbox";
import { m } from "@localizations/messages.js";

import notifications from "../notifications";

export default function SettingsAudioRoute() {
  const { send } = useJsonRpc();
  const settings = useSettingsStore();

  useEffect(() => {
    send("getAudioOutputEnabled", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      settings.setAudioOutputEnabled(resp.result as boolean);
    });

    send("getAudioInputAutoEnable", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      settings.setAudioInputAutoEnable(resp.result as boolean);
    });
  }, [send, settings]);

  const handleAudioOutputEnabledChange = (enabled: boolean) => {
    send("setAudioOutputEnabled", { enabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        const errorMsg = enabled
          ? m.audio_output_failed_enable({ error: String(resp.error.data || m.unknown_error()) })
          : m.audio_output_failed_disable({ error: String(resp.error.data || m.unknown_error()) });
        notifications.error(errorMsg);
        return;
      }
      settings.setAudioOutputEnabled(enabled);
      const successMsg = enabled ? m.audio_output_enabled() : m.audio_output_disabled();
      notifications.success(successMsg);
    });
  };

  const handleAudioInputAutoEnableChange = (enabled: boolean) => {
    send("setAudioInputAutoEnable", { enabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(String(resp.error.data || m.unknown_error()));
        return;
      }
      settings.setAudioInputAutoEnable(enabled);
      const successMsg = enabled
        ? m.audio_input_auto_enable_enabled()
        : m.audio_input_auto_enable_disabled();
      notifications.success(successMsg);
    });
  };

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={m.audio_settings_title()}
        description={m.audio_settings_description()}
      />
      <div className="space-y-4">
        <SettingsItem
          title={m.audio_settings_output_title()}
          description={m.audio_settings_output_description()}
        >
          <Checkbox
            checked={settings.audioOutputEnabled || false}
            onChange={(e) => handleAudioOutputEnabledChange(e.target.checked)}
          />
        </SettingsItem>

        <SettingsItem
          title={m.audio_settings_auto_enable_microphone_title()}
          description={m.audio_settings_auto_enable_microphone_description()}
        >
          <Checkbox
            checked={settings.audioInputAutoEnable || false}
            onChange={(e) => handleAudioInputAutoEnableChange(e.target.checked)}
          />
        </SettingsItem>
      </div>
    </div>
  );
}
