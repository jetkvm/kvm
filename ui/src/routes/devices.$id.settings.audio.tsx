import { useParams } from "react-router";
import { microphoneStorageKey } from "@/webrtc/microphone";
import { useCallback, useEffect, useState } from "react";
import { LinkButton } from "@components/Button";
import { Checkbox } from "@components/Checkbox";
import { GridCard } from "@components/Card";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useRTCStore } from "@hooks/stores";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";
import { LuInfo } from "react-icons/lu";

interface AudioConfig {
  enabled: boolean;
  microphone_enabled: boolean;
}

interface UsbDeviceConfig {
  audio?: boolean;
}

export default function SettingsAudioRoute() {
  const { send } = useJsonRpc();
  const { id = "" } = useParams();
  const [microphoneEnabled, setMicrophoneEnabled] = useState(false);
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [usbAudioEnabled, setUsbAudioEnabled] = useState<boolean | null>(null);

  useEffect(() => {
    send("getAudioConfig", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return console.error(resp.error);
      setEnabled((resp.result as AudioConfig).enabled);
      setMicrophoneEnabled((resp.result as AudioConfig).microphone_enabled ?? false);
    });

    send("getUsbDevices", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error(resp.error);
        setUsbAudioEnabled(true);
        return;
      }
      setUsbAudioEnabled((resp.result as UsbDeviceConfig).audio !== false);
    });
  }, [send]);

  const handleChange = useCallback(
    (next: boolean) => {
      const previous = enabled;
      setEnabled(next);
      send(
        "setAudioConfig",
        { params: { enabled: next, microphone_enabled: microphoneEnabled } },
        (resp: JsonRpcResponse) => {
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
        },
      );
    },
    [enabled, microphoneEnabled, send],
  );

  const handleMicrophoneChange = (next: boolean) => {
    send(
      "setAudioConfig",
      { params: { enabled: enabled ?? false, microphone_enabled: next } },
      (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(resp.error.data || m.unknown_error());
          return;
        }
        const key = microphoneStorageKey(id);
        if (next) window.sessionStorage.setItem(key, "true");
        else {
          window.sessionStorage.removeItem(key);
          useRTCStore
            .getState()
            .peerConnection?.getSenders()
            .forEach((sender) => sender.track?.stop());
        }
        useRTCStore.getState().peerConnection?.close();
        window.location.reload();
      },
    );
  };

  const audioBlockedByUsb = usbAudioEnabled === false;

  return (
    <div className="space-y-4">
      <SettingsPageHeader title={m.audio_title()} description={m.audio_page_description()} />
      <div className="space-y-3">
        <SettingsItem
          title={m.audio_enable_title()}
          badge="Experimental"
          description={m.audio_enable_description()}
        >
          <Checkbox
            checked={enabled ?? false}
            disabled={enabled === null || usbAudioEnabled === null || audioBlockedByUsb}
            aria-describedby={audioBlockedByUsb ? "audio-usb-disabled-hint" : undefined}
            onChange={(e) => handleChange(e.target.checked)}
          />
        </SettingsItem>
        <SettingsItem
          title={m.audio_microphone_title()}
          badge="Experimental"
          description={m.audio_microphone_description()}
        >
          <Checkbox
            checked={
              microphoneEnabled &&
              window.sessionStorage.getItem(microphoneStorageKey(id)) === "true"
            }
            aria-label={m.audio_microphone_title()}
            disabled={enabled === null || usbAudioEnabled !== true}
            onChange={(e) => handleMicrophoneChange(e.target.checked)}
          />
        </SettingsItem>
        {audioBlockedByUsb && (
          <GridCard>
            <div
              id="audio-usb-disabled-hint"
              className="flex items-center justify-between gap-x-4 p-2 px-3"
            >
              <div className="flex items-center gap-x-2">
                <LuInfo className="h-4 w-4 shrink-0" />
                <div className="text-sm text-black dark:text-white">
                  {m.audio_usb_device_disabled_hint()}
                </div>
              </div>
              <LinkButton
                to="../hardware"
                size="XS"
                theme="light"
                text={m.audio_usb_device_disabled_link()}
              />
            </div>
          </GridCard>
        )}
      </div>
    </div>
  );
}
