import { useCallback, useEffect, useState } from "react";

import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { useSettingsStore } from "@/hooks/stores";
import { GridCard } from "@components/Card";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";
import { isSecureContext } from "@/utils";

type UVCSource = "hdmi" | "camera";

const getUvcSourceOptions = () => [
  { value: "hdmi" as UVCSource, label: m.camera_source_hdmi() },
  { value: "camera" as UVCSource, label: m.camera_source_browser() },
];

export default function CameraPopover() {
  const { send } = useJsonRpc();
  const { setCameraEnabled } = useSettingsStore();
  const [uvcEnabled, setUvcEnabled] = useState<boolean>(false);
  const [uvcSource, setUvcSource] = useState<UVCSource>("hdmi");
  const isHttps = isSecureContext();

  useEffect(() => {
    // Load UVC enabled status
    send("getUsbDevices", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to load USB devices:", resp.error);
      } else {
        const usbDevices = resp.result as { uvc: boolean };
        setUvcEnabled(usbDevices.uvc || false);
      }
    });

    // Load current UVC source
    send("getUVCSource", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to load UVC source:", resp.error);
      } else {
        const source = resp.result as UVCSource;
        setUvcSource(source);
        // Sync camera enabled state based on current source
        setCameraEnabled(source === "camera");
      }
    });
  }, [send, setCameraEnabled]);

  const handleSourceChange = useCallback(
    (source: UVCSource) => {
      // Check HTTPS requirement for camera source
      if (source === "camera" && !isHttps) {
        notifications.error(m.camera_source_https_required());
        return;
      }

      // Enable/disable camera based on source selection
      const enableCamera = source === "camera";

      // First set the camera enabled state
      send("setCameraEnabled", { enabled: enableCamera }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            m.camera_failed_enable({ error: String(resp.error.data || resp.error.message) }),
          );
          return;
        }
        setCameraEnabled(enableCamera);

        // Then set the UVC source
        send("setUVCSource", { source }, (resp: JsonRpcResponse) => {
          if ("error" in resp) {
            notifications.error(
              m.camera_source_change_failed({ error: String(resp.error.message) }),
            );
            // Rollback camera state on failure
            send("setCameraEnabled", { enabled: !enableCamera }, () => {
              // Ignore rollback response
            });
            setCameraEnabled(!enableCamera);
            return;
          }
          setUvcSource(source);
          notifications.success(
            source === "hdmi" ? m.camera_source_changed_hdmi() : m.camera_source_changed_browser(),
          );
        });
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
                {/* UVC Source Selector */}
                <SettingsItem
                  title={m.camera_source_title()}
                  description={m.camera_source_description()}
                >
                  <SelectMenuBasic
                    size="SM"
                    value={uvcSource}
                    onChange={e => handleSourceChange(e.target.value as UVCSource)}
                    options={getUvcSourceOptions()}
                  />
                </SettingsItem>

                {/* Info text based on source */}
                <div className="text-sm text-slate-500 dark:text-slate-400">
                  {uvcSource === "hdmi"
                    ? m.camera_source_hdmi_active()
                    : m.camera_source_browser_active()}
                </div>

                {/* HTTPS warning for camera option */}
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
