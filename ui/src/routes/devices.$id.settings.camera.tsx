import { useEffect, useState } from "react";

import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { m } from "@localizations/messages.js";

import notifications from "../notifications";

interface CameraSettingsResult {
  resolution: string;
  frameRate: number;
  h264Bitrate: number;
  mjpegQuality: number;
}

const CAMERA_DEFAULTS = {
  resolution: "1080p",
  frameRate: 24,
  h264Bitrate: 3,
  mjpegQuality: 35,
} as const;

export default function SettingsCameraRoute() {
  const { send } = useJsonRpc();

  const [h264Bitrate, setH264Bitrate] = useState<number>(CAMERA_DEFAULTS.h264Bitrate);
  const [mjpegQuality, setMjpegQuality] = useState<number>(CAMERA_DEFAULTS.mjpegQuality);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    send("getCameraSettings", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.camera_settings_failed_load({ error: String(resp.error.data || m.unknown_error()) }));
        setIsLoading(false);
        return;
      }
      const settings = resp.result as CameraSettingsResult;
      setH264Bitrate(settings.h264Bitrate);
      setMjpegQuality(settings.mjpegQuality);
      setIsLoading(false);
    });
  }, [send]);

  const handleSaveSettings = () => {
    send(
      "setCameraSettings",
      {
        // Resolution and frameRate are determined by UVC host negotiation,
        // so we pass defaults here. The backend still expects these fields.
        resolution: CAMERA_DEFAULTS.resolution,
        frameRate: CAMERA_DEFAULTS.frameRate,
        h264Bitrate,
        mjpegQuality,
      },
      (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(m.camera_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }));
          return;
        }
        notifications.success(m.camera_settings_saved());
      }
    );
  };

  if (isLoading) {
    return (
      <div className="space-y-4">
        <SettingsPageHeader
          title={m.camera_settings_title()}
          description={m.camera_settings_encoder_description()}
        />
        <div className="flex items-center justify-center py-8">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={m.camera_settings_title()}
        description={m.camera_settings_encoder_description()}
      />

      <div className="space-y-4">
        <h3 className="text-sm font-medium text-slate-900 dark:text-white">
          {m.camera_settings_encoder_section()}
        </h3>
        <p className="text-sm text-slate-500 dark:text-slate-400">
          {m.camera_settings_encoder_section_description()}
        </p>

        <SettingsItem
          title={m.camera_settings_h264_bitrate_title()}
          description={m.camera_settings_h264_bitrate_description()}
        >
          <SelectMenuBasic
            size="SM"
            value={String(h264Bitrate)}
            options={[
              { value: "1", label: "1 Mbps" },
              { value: "2", label: "2 Mbps" },
              { value: "3", label: `3 Mbps${h264Bitrate === CAMERA_DEFAULTS.h264Bitrate ? m.camera_settings_default_suffix() : ""}` },
              { value: "4", label: "4 Mbps" },
              { value: "5", label: "5 Mbps" },
              { value: "6", label: "6 Mbps" },
              { value: "8", label: "8 Mbps" },
              { value: "10", label: "10 Mbps" },
            ]}
            onChange={(e) => setH264Bitrate(parseInt(e.target.value))}
          />
        </SettingsItem>

        <SettingsItem
          title={m.camera_settings_mjpeg_quality_title()}
          description={m.camera_settings_mjpeg_quality_description()}
        >
          <SelectMenuBasic
            size="SM"
            value={String(mjpegQuality)}
            options={[
              { value: "20", label: "20% (Low)" },
              { value: "35", label: `35%${mjpegQuality === CAMERA_DEFAULTS.mjpegQuality ? m.camera_settings_default_suffix() : ""}` },
              { value: "50", label: "50% (Medium)" },
              { value: "75", label: "75% (High)" },
              { value: "100", label: "100% (Maximum)" },
            ]}
            onChange={(e) => setMjpegQuality(parseInt(e.target.value))}
          />
        </SettingsItem>
      </div>

      <div className="pt-4">
        <button
          onClick={handleSaveSettings}
          className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500"
        >
          {m.camera_settings_save_button()}
        </button>
      </div>
    </div>
  );
}
