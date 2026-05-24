import { useCallback, useEffect, useState } from "react";

import { Checkbox } from "@components/Checkbox";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useRTCStore } from "@hooks/stores";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

interface AudioConfig {
  enabled: boolean;
  microphone_enabled: boolean;
}

const MICROPHONE_ENABLED_STORAGE_KEY = "jetkvm.microphone.enabled";

export default function SettingsAudioRoute() {
  const { send } = useJsonRpc();
  const [audioConfig, setAudioConfig] = useState<AudioConfig | null>(null);

  useEffect(() => {
    send("getAudioConfig", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return console.error(resp.error);
      const result = resp.result as AudioConfig;
      setAudioConfig({
        enabled: result.enabled,
        microphone_enabled: result.microphone_enabled ?? false,
      });
    });
  }, [send]);

  const handleChange = useCallback(
    (next: Partial<AudioConfig>) => {
      if (!audioConfig) return;
      const previous = audioConfig;
      const updated = { ...audioConfig, ...next };
      setAudioConfig(updated);
      send("setAudioConfig", { params: updated }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(resp.error.data || m.unknown_error());
          setAudioConfig(previous);
          return;
        }
        if (updated.microphone_enabled) {
          window.localStorage.setItem(MICROPHONE_ENABLED_STORAGE_KEY, "true");
        } else {
          window.localStorage.removeItem(MICROPHONE_ENABLED_STORAGE_KEY);
        }
        // Close the WebRTC connection before reloading. Firefox's soft
        // reload doesn't always tear it down, which leaves the new page in
        // a half-renegotiated state (tracks land on receivers but never
        // attach to a MediaStream). Closing first guarantees a clean start.
        useRTCStore.getState().peerConnection?.close();
        window.location.reload();
      });
    },
    [audioConfig, send],
  );

  return (
    <div className="space-y-4">
      <SettingsPageHeader title={m.audio_title()} description={m.audio_page_description()} />
      <SettingsItem
        title={m.audio_enable_title()}
        badge="Experimental"
        description={m.audio_enable_description()}
      >
        <Checkbox
          checked={audioConfig?.enabled ?? false}
          disabled={audioConfig === null}
          onChange={e => handleChange({ enabled: e.target.checked })}
        />
      </SettingsItem>
      <SettingsItem
        title="Enable Microphone"
        badge="Experimental"
        description="Send this browser microphone to the host as a JetKVM USB microphone."
      >
        <Checkbox
          checked={audioConfig?.microphone_enabled ?? false}
          disabled={audioConfig === null}
          onChange={e => handleChange({ microphone_enabled: e.target.checked })}
        />
      </SettingsItem>
    </div>
  );
}
