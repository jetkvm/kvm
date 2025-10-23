import { useEffect } from "react";

import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { useSettingsStore } from "@/hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import Checkbox from "@components/Checkbox";
import { m } from "@localizations/messages.js";

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
      settings.setAudioOutputSource(resp.result as string);
    });

    send("getAudioOutputEnabled", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        return;
      }
      settings.setAudioOutputEnabled(resp.result as boolean);
    });

    send("getAudioInputEnabled", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        return;
      }
      settings.setAudioInputEnabled(resp.result as boolean);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [send]);

  const handleAudioOutputSourceChange = (source: string) => {
    // Update UI immediately for better responsiveness
    settings.setAudioOutputSource(source);

    send("setAudioOutputSource", { source }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        // Revert on error by fetching current value from backend
        send("getAudioOutputSource", {}, (getResp: JsonRpcResponse) => {
          if ("result" in getResp) {
            settings.setAudioOutputSource(getResp.result as string);
          }
        });
        notifications.error(
          m.audio_settings_output_source_failed({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      notifications.success(m.audio_settings_output_source_success());
    });
  };

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

  const handleAudioInputEnabledChange = (enabled: boolean) => {
    send("setAudioInputEnabled", { enabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        const errorMsg = enabled
          ? m.audio_input_failed_enable({ error: String(resp.error.data || m.unknown_error()) })
          : m.audio_input_failed_disable({ error: String(resp.error.data || m.unknown_error()) });
        notifications.error(errorMsg);
        return;
      }
      settings.setAudioInputEnabled(enabled);
      const successMsg = enabled ? m.audio_input_enabled() : m.audio_input_disabled();
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

        {settings.audioOutputEnabled && (
          <SettingsItem
            title={m.audio_settings_output_source_title()}
            description={m.audio_settings_output_source_description()}
          >
            <SelectMenuBasic
              size="SM"
              label=""
              value={settings.audioOutputSource || "usb"}
              options={[
                { value: "hdmi", label: m.audio_settings_hdmi_label() },
                { value: "usb", label: m.audio_settings_usb_label() },
              ]}
              onChange={e => {
                handleAudioOutputSourceChange(e.target.value);
              }}
            />
          </SettingsItem>
        )}

        <SettingsItem
          title={m.audio_settings_input_title()}
          description={m.audio_settings_input_description()}
        >
          <Checkbox
            checked={settings.audioInputEnabled || false}
            onChange={(e) => handleAudioInputEnabledChange(e.target.checked)}
          />
        </SettingsItem>
      </div>
    </div>
  );
}
