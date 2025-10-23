import { useCallback, useEffect, useState } from "react";
import { LuVolume2 } from "react-icons/lu";

import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { GridCard } from "@components/Card";
import { SettingsItem } from "@components/SettingsItem";
import { Button } from "@components/Button";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

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
        const errorMsg = enabled
          ? m.audio_output_failed_enable({ error: String(resp.error.data || m.unknown_error()) })
          : m.audio_output_failed_disable({ error: String(resp.error.data || m.unknown_error()) });
        notifications.error(errorMsg);
      } else {
        setAudioOutputEnabled(enabled);
        const successMsg = enabled ? m.audio_output_enabled() : m.audio_output_disabled();
        notifications.success(successMsg);
      }
    });
  }, [send, audioOutputEnabled]);

  const handleAudioInputEnabledToggle = useCallback(() => {
    const enabled = !audioInputEnabled;
    setLoading(true);
    send("setAudioInputEnabled", { enabled }, (resp: JsonRpcResponse) => {
      setLoading(false);
      if ("error" in resp) {
        const errorMsg = enabled
          ? m.audio_input_failed_enable({ error: String(resp.error.data || m.unknown_error()) })
          : m.audio_input_failed_disable({ error: String(resp.error.data || m.unknown_error()) });
        notifications.error(errorMsg);
      } else {
        setAudioInputEnabled(enabled);
        const successMsg = enabled ? m.audio_input_enabled() : m.audio_input_disabled();
        notifications.success(successMsg);
      }
    });
  }, [send, audioInputEnabled]);

  return (
    <GridCard>
      <div className="space-y-4 p-4 py-3">
        <div className="space-y-4">
          <div className="flex items-center gap-2 text-slate-900 dark:text-slate-100">
            <LuVolume2 className="h-5 w-5" />
            <h3 className="font-semibold">{m.audio_popover_title()}</h3>
          </div>

          <div className="space-y-3">
            <SettingsItem
              loading={loading}
              title={m.audio_output_title()}
              description={m.audio_output_description()}
            >
              <Button
                size="SM"
                theme={audioOutputEnabled ? "light" : "primary"}
                text={audioOutputEnabled ? m.audio_disable() : m.audio_enable()}
                onClick={handleAudioOutputEnabledToggle}
              />
            </SettingsItem>

            {usbAudioEnabled && (
              <>
                <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />

                <SettingsItem
                  loading={loading}
                  title={m.audio_input_title()}
                  description={m.audio_input_description()}
                >
                  <Button
                    size="SM"
                    theme={audioInputEnabled ? "light" : "primary"}
                    text={audioInputEnabled ? m.audio_disable() : m.audio_enable()}
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
