import { useCallback, useEffect, useState } from "react";

import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { useSettingsStore } from "@/hooks/stores";
import { GridCard } from "@components/Card";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import Checkbox from "@components/Checkbox";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";
import { isSecureContext } from "@/utils";

type UVCSource = "hdmi" | "camera";

const uvcSourceOptions = [
  { value: "hdmi" as UVCSource, label: "HDMI Input" },
  { value: "camera" as UVCSource, label: "Browser Camera" },
];

export default function CameraPopover() {
  const { send } = useJsonRpc();
  const { cameraEnabled, setCameraEnabled } = useSettingsStore();
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
        setUvcSource(resp.result as UVCSource);
      }
    });

    // Sync camera enabled state from device (important for remote connections)
    send("getCameraEnabled", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to load camera enabled state:", resp.error);
      } else {
        setCameraEnabled(resp.result as boolean);
      }
    });
  }, [send, setCameraEnabled]);

  const handleSourceChange = useCallback((source: UVCSource) => {
    send("setUVCSource", { source }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(`Failed to change UVC source: ${resp.error.message}`);
        return;
      }
      setUvcSource(source);
      notifications.success(`UVC source changed to ${source === "hdmi" ? "HDMI Input" : "Browser Camera"}`);
    });
  }, [send]);

  const handleCameraToggle = useCallback((enabled: boolean) => {
    send("setCameraEnabled", { enabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.camera_failed_enable({ error: String(resp.error.data || resp.error.message) }));
        return;
      }
      setCameraEnabled(enabled);
      const successMsg = enabled ? m.camera_enabled() : m.camera_disabled();
      notifications.success(successMsg);
    });
  }, [send, setCameraEnabled]);

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
                  title="UVC Video Source"
                  description="Select which video source to send to the target PC via UVC"
                >
                  <SelectMenuBasic
                    size="SM"
                    value={uvcSource}
                    onChange={(e) => handleSourceChange(e.target.value as UVCSource)}
                    options={uvcSourceOptions}
                  />
                </SettingsItem>

                {/* Camera Toggle - only shown when source is camera */}
                {uvcSource === "camera" && (
                  <SettingsItem
                    title={m.camera_title()}
                    description={m.camera_description()}
                    badge={!isHttps ? m.camera_https_only() : undefined}
                    badgeVariant="info"
                    badgeLink={!isHttps ? "settings/access" : undefined}
                  >
                    <Checkbox
                      checked={cameraEnabled}
                      disabled={!isHttps}
                      onChange={(e) => handleCameraToggle(e.target.checked)}
                    />
                  </SettingsItem>
                )}

                {/* Info when HDMI is selected */}
                {uvcSource === "hdmi" && (
                  <div className="text-sm text-slate-500 dark:text-slate-400">
                    HDMI input is being mirrored to the target PC via UVC.
                  </div>
                )}
              </>
            ) : (
              <div className="text-sm text-slate-500 dark:text-slate-400">
                UVC (webcam) is not enabled. Enable it in USB Device settings to use camera passthrough.
              </div>
            )}
          </div>
        </div>
      </div>
    </GridCard>
  );
}
