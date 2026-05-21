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
}

export default function SettingsAudioRoute() {
  const { send } = useJsonRpc();
  const [enabled, setEnabled] = useState<boolean | null>(null);

  useEffect(() => {
    send("getAudioConfig", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return console.error(resp.error);
      setEnabled((resp.result as AudioConfig).enabled);
    });
  }, [send]);

  const handleChange = useCallback(
    (next: boolean) => {
      const previous = enabled;
      setEnabled(next);
      send("setAudioConfig", { params: { enabled: next } }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(resp.error.data || m.unknown_error());
          setEnabled(previous);
          return;
        }
        // Close the WebRTC connection before reloading. Firefox's soft
        // reload doesn't always tear it down, which leaves the new page in
        // a half-renegotiated state (tracks land on receivers but never
        // attach to a MediaStream). Closing first guarantees a clean start.
        useRTCStore.getState().peerConnection?.close();
        window.location.reload();
      });
    },
    [enabled, send],
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
          checked={enabled ?? false}
          disabled={enabled === null}
          onChange={e => handleChange(e.target.checked)}
        />
      </SettingsItem>
    </div>
  );
}
