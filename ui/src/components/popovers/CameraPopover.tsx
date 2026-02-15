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

export default function CameraPopover() {
  const { send } = useJsonRpc();
  const { setCameraEnabled } = useSettingsStore();
  const [uvcEnabled, setUvcEnabled] = useState<boolean>(false);
  const [cameraEnabled, setCameraEnabledLocal] = useState<boolean>(false);
  const isHttps = isSecureContext();

  useEffect(() => {
    send("getUsbDevices", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to load USB devices:", resp.error);
        notifications.error(m.camera_failed_load_usb_devices());
      } else {
        const usbDevices = resp.result as { uvc: boolean };
        setUvcEnabled(usbDevices.uvc || false);
      }
    });

    send("getCameraEnabled", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to load camera state:", resp.error);
        notifications.error(m.camera_failed_load_state());
      } else {
        const enabled = resp.result as boolean;
        setCameraEnabledLocal(enabled);
        setCameraEnabled(enabled);
      }
    });
  }, [send, setCameraEnabled]);

  const handleCameraToggle = useCallback(
    (enabled: boolean) => {
      if (enabled && !isHttps) {
        notifications.error(m.camera_source_https_required());
        return;
      }

      send("setCameraEnabled", { enabled }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            m.camera_failed_enable({ error: String(resp.error.data || resp.error.message) }),
          );
          return;
        }
        setCameraEnabledLocal(enabled);
        setCameraEnabled(enabled);
        notifications.success(enabled ? m.camera_enabled() : m.camera_disabled());
      });
    },
    [send, setCameraEnabled, isHttps],
  );

  return (
    <GridCard>
      <div className="space-y-4 p-4 py-3">
        <div className="space-y-4">
          <SettingsPageHeader
            title={m.camera_popover_title()}
            description={m.camera_popover_description()}
          />

          <div className="space-y-3">
            {uvcEnabled ? (
              <>
                <SettingsItem
                  title={m.camera_passthrough_title()}
                  description={m.camera_passthrough_description()}
                >
                  <Checkbox
                    checked={cameraEnabled}
                    onChange={e => handleCameraToggle(e.target.checked)}
                  />
                </SettingsItem>

                {cameraEnabled && (
                  <div className="text-sm text-slate-500 dark:text-slate-400">
                    {m.camera_passthrough_active()}
                  </div>
                )}

                {!isHttps && (
                  <div className="text-sm text-amber-600 dark:text-amber-400">
                    {m.camera_source_https_warning()}
                  </div>
                )}
              </>
            ) : (
              <div className="text-sm text-slate-500 dark:text-slate-400">
                {m.camera_source_uvc_disabled()}
              </div>
            )}
          </div>
        </div>
      </div>
    </GridCard>
  );
}
