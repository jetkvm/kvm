import { useCallback, useEffect, useState } from "react";

import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { useSettingsStore } from "@/hooks/stores";
import { GridCard } from "@components/Card";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import Checkbox from "@components/Checkbox";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";
import { isSecureContext } from "@/utils";

export default function AudioPopover() {
  const { send } = useJsonRpc();
  const { microphoneEnabled, setMicrophoneEnabled } = useSettingsStore();
  const [audioOutputEnabled, setAudioOutputEnabled] = useState<boolean>(true);
  const [usbAudioEnabled, setUsbAudioEnabled] = useState<boolean>(false);
  const [loading, setLoading] = useState(false);
  const [micLoading, setMicLoading] = useState(false);
  const isHttps = isSecureContext();

  // Helper function to handle RPC errors consistently
  const handleRpcError = (resp: JsonRpcResponse, errorMsg?: string): boolean => {
    if ("error" in resp) {
      notifications.error(errorMsg || String(resp.error.data || m.unknown_error()));
      return true;
    }
    return false;
  };

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
        setMicrophoneEnabled(resp.result as boolean);
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
  }, [send, setMicrophoneEnabled]);

  const handleAudioOutputEnabledToggle = useCallback(
    (enabled: boolean) => {
      setLoading(true);
      send("setAudioOutputEnabled", { enabled }, (resp: JsonRpcResponse) => {
        setLoading(false);
        if ("error" in resp) {
          const errorMsg = enabled
            ? m.audio_output_failed_enable({ error: String(resp.error.data || m.unknown_error()) })
            : m.audio_output_failed_disable({
                error: String(resp.error.data || m.unknown_error()),
              });
          handleRpcError(resp, errorMsg);
          return;
        }

        setAudioOutputEnabled(enabled);
        const successMsg = enabled ? m.audio_output_enabled() : m.audio_output_disabled();
        notifications.success(successMsg);
      });
    },
    [send],
  );

  const handleMicrophoneToggle = useCallback(
    (enabled: boolean) => {
      setMicLoading(true);
      send("setAudioInputEnabled", { enabled }, (resp: JsonRpcResponse) => {
        setMicLoading(false);
        if ("error" in resp) {
          const errorMsg = enabled
            ? m.audio_input_failed_enable({ error: String(resp.error.data || m.unknown_error()) })
            : m.audio_input_failed_disable({ error: String(resp.error.data || m.unknown_error()) });
          handleRpcError(resp, errorMsg);
          return;
        }

        setMicrophoneEnabled(enabled);
      });
    },
    [send, setMicrophoneEnabled],
  );

  return (
    <GridCard>
      <div className="space-y-4 p-4 py-3">
        <div className="space-y-4">
          <SettingsPageHeader
            title={m.audio_popover_title()}
            description={m.audio_popover_description()}
          />

          <div className="space-y-3">
            <SettingsItem
              loading={loading}
              title={m.audio_speakers_title()}
              description={m.audio_speakers_description()}
            >
              <Checkbox
                checked={audioOutputEnabled}
                onChange={e => handleAudioOutputEnabledToggle(e.target.checked)}
              />
            </SettingsItem>

            {usbAudioEnabled && (
              <>
                <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />

                <SettingsItem
                  loading={micLoading}
                  title={m.audio_microphone_title()}
                  description={m.audio_microphone_description()}
                  badge={!isHttps ? m.audio_https_only() : undefined}
                  badgeVariant="info"
                  badgeLink={!isHttps ? "settings/access" : undefined}
                >
                  <Checkbox
                    checked={microphoneEnabled}
                    disabled={!isHttps}
                    onChange={e => handleMicrophoneToggle(e.target.checked)}
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
