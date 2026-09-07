import { useCallback, useEffect, useState } from "react";

import { useSettingsStore } from "@hooks/stores";
import { Button } from "@components/Button";
import { Checkbox } from "@components/Checkbox";
import { TextAreaWithLabel } from "@components/TextArea";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { NestedSettingsGroup } from "@components/NestedSettingsGroup";
import Fieldset from "@components/Fieldset";
import notifications from "@/notifications";
import { isLinuxDesktop } from "@/utils";
import { m } from "@localizations/messages.js";

interface EDIDPreset {
  name: string;
  edid: string;
}

const streamQualityOptions = [
  { value: "1", label: m.video_quality_high() },
  { value: "0.5", label: m.video_quality_medium() },
  { value: "0.1", label: m.video_quality_low() },
];

const allCodecOptions = [
  { value: "auto", label: m.video_codec_auto() },
  { value: "h265", label: m.video_codec_h265() },
  { value: "h264", label: m.video_codec_h264() },
];

const h265Supported = (() => {
  // Linux browsers frequently advertise H.265 they cannot actually decode,
  // leaving users stuck on a black "Loading video stream..." screen. See
  // jetkvm/kvm#1413.
  if (isLinuxDesktop()) return false;
  const caps = RTCRtpReceiver.getCapabilities?.("video");
  return caps?.codecs.some(c => c.mimeType === "video/H265") ?? false;
})();

const browserCodecOptions = h265Supported
  ? allCodecOptions
  : allCodecOptions.filter(o => o.value !== "h265");

export default function SettingsVideoRoute() {
  const { send } = useJsonRpc();
  const [streamQuality, setStreamQuality] = useState("1");
  const [streamQualityLoading, setStreamQualityLoading] = useState(true);
  const [codecPreference, setCodecPreference] = useState("auto");
  const [supportedCodecs, setSupportedCodecs] = useState<string[] | null>(null);
  const [codecLoadFailed, setCodecLoadFailed] = useState(false);
  const [codecLoadAttempt, setCodecLoadAttempt] = useState(0);
  const codecOptions = allCodecOptions
    .filter(
      option =>
        option.value === codecPreference ||
        (browserCodecOptions.includes(option) &&
          (option.value === "auto" || supportedCodecs?.includes(option.value))),
    )
    .map(option => ({
      ...option,
      disabled:
        option.value !== "auto" &&
        (!browserCodecOptions.includes(option) || !supportedCodecs?.includes(option.value)),
    }));

  const [disableHostDisplayWhenIdle, setDisableHostDisplayWhenIdle] = useState(false);
  const [disableHostDisplayWhenIdleLoading, setDisableHostDisplayWhenIdleLoading] = useState(true);
  const [customEdidValue, setCustomEdidValue] = useState<string | null>(null);
  const [edid, setEdid] = useState<string | null>(null);
  const [edidLoading, setEdidLoading] = useState(true);
  // The device owns the preset list, so the UI has no copy of it.
  const [edidPresets, setEdidPresets] = useState<EDIDPreset[]>([]);
  const { debugMode } = useSettingsStore();
  // Video enhancement settings from store
  const {
    videoSaturation,
    setVideoSaturation,
    videoBrightness,
    setVideoBrightness,
    videoContrast,
    setVideoContrast,
  } = useSettingsStore();

  useEffect(() => {
    let active = true;
    setSupportedCodecs(null);
    setCodecLoadFailed(false);
    const timeout = window.setTimeout(() => setCodecLoadFailed(true), 10000);
    void send("getSupportedVideoCodecs", {}, (resp: JsonRpcResponse) => {
      if (!active) return;
      window.clearTimeout(timeout);
      if (
        "error" in resp ||
        !Array.isArray(resp.result) ||
        !resp.result.every(codec => typeof codec === "string")
      ) {
        setCodecLoadFailed(true);
        return;
      }
      setCodecLoadFailed(false);
      setSupportedCodecs(resp.result as string[]);
    });
    return () => {
      active = false;
      window.clearTimeout(timeout);
    };
  }, [send, codecLoadAttempt]);

  useEffect(() => {
    void send("getStreamQualityFactor", {}, (resp: JsonRpcResponse) => {
      setStreamQualityLoading(false);
      if ("error" in resp) return;
      setStreamQuality(String(resp.result as number));
    });

    void send("getVideoCodecPreference", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const codec = resp.result as string;
      const isAvailable = allCodecOptions.some(o => o.value === codec);
      setCodecPreference(isAvailable ? codec : "auto");
    });

    void send("getHostDisplayIdleMode", {}, (resp: JsonRpcResponse) => {
      setDisableHostDisplayWhenIdleLoading(false);
      if ("error" in resp) return;
      const result = resp.result as { enabled: boolean };
      setDisableHostDisplayWhenIdle(result.enabled);
    });
  }, [send]);

  useEffect(() => {
    let active = true;
    setEdidLoading(true);
    // Classify the current value only after the device's preset list arrives.
    void send("getEDIDPresets", {}, (resp: JsonRpcResponse) => {
      if (!active) return;
      if ("error" in resp) {
        setEdidLoading(false);
        notifications.error(
          m.video_failed_get_edid({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      const presets = resp.result as EDIDPreset[];
      setEdidPresets(presets);
      void send("getEDID", {}, (resp: JsonRpcResponse) => {
        if (!active) return;
        setEdidLoading(false);
        if ("error" in resp) {
          notifications.error(
            m.video_failed_get_edid({ error: resp.error.data || m.unknown_error() }),
          );
          return;
        }
        const value = resp.result as string;
        const preset = presets.find(p => p.edid.toLowerCase() === value.toLowerCase());
        setEdid(preset?.edid ?? "custom");
        setCustomEdidValue(preset ? null : value);
      });
    });
    return () => {
      active = false;
    };
  }, [send]);

  const handleStreamQualityChange = (factor: string) => {
    setStreamQualityLoading(true);
    void send("setStreamQualityFactor", { factor: Number(factor) }, (resp: JsonRpcResponse) => {
      setStreamQualityLoading(false);
      if ("error" in resp) {
        notifications.error(
          m.video_failed_set_stream_quality({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }

      notifications.success(
        m.video_stream_quality_set({
          quality: streamQualityOptions.find(x => x.value === factor)?.label || "Unknown",
        }),
      );
      setStreamQuality(factor);
    });
  };

  const handleCodecChange = (codec: string) => {
    if (codec === codecPreference) return;
    void send("setVideoCodecPreference", { codec }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.video_failed_set_codec({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      // Full page reload to renegotiate WebRTC with the new codec.
      window.location.reload();
    });
  };

  const handleDisableHostDisplayWhenIdleChange = (enabled: boolean) => {
    const previous = disableHostDisplayWhenIdle;
    setDisableHostDisplayWhenIdle(enabled);
    setDisableHostDisplayWhenIdleLoading(true);
    void send("setHostDisplayIdleMode", { enabled }, (resp: JsonRpcResponse) => {
      setDisableHostDisplayWhenIdleLoading(false);
      if ("error" in resp) {
        setDisableHostDisplayWhenIdle(previous);
        notifications.error(
          m.video_idle_display_failed({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }

      notifications.success(
        enabled ? m.video_idle_display_enabled() : m.video_idle_display_disabled(),
      );
    });
  };

  const handleEDIDChange = (newEdid: string) => {
    const matched = edidPresets.find(p => p.edid.toLowerCase() === newEdid.toLowerCase());
    setEdidLoading(true);
    void send("setEDID", { edid: newEdid }, (resp: JsonRpcResponse) => {
      setEdidLoading(false);
      if ("error" in resp) {
        notifications.error(
          m.video_failed_set_edid({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      setEdid(matched?.edid ?? "custom");
      setCustomEdidValue(matched ? null : newEdid);
      notifications.success(
        m.video_edid_set_success({
          edid: matched?.name ?? "the custom EDID",
        }),
      );
    });
  };

  const [debugInfo, setDebugInfo] = useState<string | null>(null);
  const [debugInfoLoading, setDebugInfoLoading] = useState(false);
  const getDebugInfo = useCallback(() => {
    setDebugInfoLoading(true);
    void send("getVideoLogStatus", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.video_failed_get_debug_info({ error: resp.error.data || m.unknown_error() }),
        );
        setDebugInfoLoading(false);
        return;
      }
      const data = resp.result as string;
      setDebugInfo(
        data
          .split("\n")
          .map(line => line.trim().replace(/^\[\s*\d+\.\d+\]\s*/, ""))
          .join("\n"),
      );
      setDebugInfoLoading(false);
    });
  }, [send]);

  return (
    <div className="space-y-3">
      <div className="space-y-4">
        <SettingsPageHeader title={m.video_title()} description={m.video_description()} />

        <div className="space-y-4">
          <div className="space-y-4">
            <SettingsItem
              title={m.video_stream_quality_title()}
              description={m.video_stream_quality_description()}
              loading={streamQualityLoading}
            >
              <SelectMenuBasic
                size="SM"
                label=""
                disabled={streamQualityLoading}
                value={streamQuality}
                options={streamQualityOptions}
                onChange={e => handleStreamQualityChange(e.target.value)}
              />
            </SettingsItem>

            <SettingsItem
              title={m.video_codec_title()}
              description={m.video_codec_description()}
              loading={supportedCodecs === null && !codecLoadFailed}
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={codecPreference}
                options={codecOptions}
                disabled={supportedCodecs === null}
                onChange={e => handleCodecChange(e.target.value)}
              />
              {codecLoadFailed && (
                <Button
                  data-testid="video-codec-retry"
                  size="SM"
                  theme="light"
                  text={m.retry()}
                  onClick={() => setCodecLoadAttempt(attempt => attempt + 1)}
                />
              )}
            </SettingsItem>

            <SettingsItem
              title={m.video_idle_display_title()}
              description={m.video_idle_display_description()}
              loading={disableHostDisplayWhenIdleLoading}
            >
              <Checkbox
                checked={disableHostDisplayWhenIdle}
                disabled={disableHostDisplayWhenIdleLoading}
                onChange={e => handleDisableHostDisplayWhenIdleChange(e.target.checked)}
              />
            </SettingsItem>

            {/* Video Enhancement Settings */}
            <SettingsItem
              title={m.video_enhancement_title()}
              description={m.video_enhancement_description()}
            />

            <NestedSettingsGroup>
              <SettingsItem
                title={m.video_saturation_title()}
                description={m.video_saturation_description({ value: videoSaturation.toFixed(1) })}
              >
                <input
                  type="range"
                  min="0.5"
                  max="2.0"
                  step="0.1"
                  value={videoSaturation}
                  onChange={e => setVideoSaturation(Number.parseFloat(e.target.value))}
                  className="h-2 w-32 cursor-pointer appearance-none rounded-lg bg-gray-200 dark:bg-gray-700"
                />
              </SettingsItem>

              <SettingsItem
                title={m.video_brightness_title()}
                description={m.video_brightness_description({ value: videoBrightness.toFixed(1) })}
              >
                <input
                  type="range"
                  min="0.5"
                  max="1.5"
                  step="0.1"
                  value={videoBrightness}
                  onChange={e => setVideoBrightness(Number.parseFloat(e.target.value))}
                  className="h-2 w-32 cursor-pointer appearance-none rounded-lg bg-gray-200 dark:bg-gray-700"
                />
              </SettingsItem>

              <SettingsItem
                title={m.video_contrast_title()}
                description={m.video_contrast_description({ value: videoContrast.toFixed(1) })}
              >
                <input
                  type="range"
                  min="0.5"
                  max="2.0"
                  step="0.1"
                  value={videoContrast}
                  onChange={e => setVideoContrast(Number.parseFloat(e.target.value))}
                  className="h-2 w-32 cursor-pointer appearance-none rounded-lg bg-gray-200 dark:bg-gray-700"
                />
              </SettingsItem>

              <div className="flex gap-2">
                <Button
                  size="SM"
                  theme="light"
                  text={m.video_reset_to_default()}
                  onClick={() => {
                    setVideoSaturation(1);
                    setVideoBrightness(1);
                    setVideoContrast(1);
                  }}
                />
              </div>
            </NestedSettingsGroup>

            <Fieldset disabled={edidLoading} className="space-y-2">
              <SettingsItem
                title={m.video_edid_title()}
                description={m.video_edid_description()}
                loading={edidLoading}
              >
                <SelectMenuBasic
                  size="SM"
                  label=""
                  fullWidth
                  disabled={edidLoading || edid === null}
                  value={edid || ""}
                  onChange={e => {
                    if (e.target.value === "custom") {
                      setEdid("custom");
                      setCustomEdidValue("");
                    } else {
                      handleEDIDChange(e.target.value);
                    }
                  }}
                  options={[
                    ...edidPresets.map(p => ({ value: p.edid, label: p.name })),
                    { value: "custom", label: m.video_edid_custom() },
                  ]}
                />
              </SettingsItem>
              {customEdidValue !== null && (
                <>
                  <SettingsItem
                    title={m.video_custom_edid_title()}
                    description={m.video_custom_edid_description()}
                  />
                  <TextAreaWithLabel
                    label={m.video_edid_file_label()}
                    placeholder="00F..."
                    rows={3}
                    value={customEdidValue}
                    disabled={edidLoading}
                    onChange={e => setCustomEdidValue(e.target.value)}
                  />
                  <div className="flex justify-start gap-x-2">
                    <Button
                      size="SM"
                      theme="primary"
                      text={m.video_set_custom_edid()}
                      loading={edidLoading}
                      onClick={() => handleEDIDChange(customEdidValue)}
                    />
                    <Button
                      size="SM"
                      theme="light"
                      text={m.video_restore_to_default()}
                      loading={edidLoading}
                      disabled={!edidPresets.length}
                      onClick={() => handleEDIDChange(edidPresets[0].edid)}
                    />
                  </div>
                </>
              )}
            </Fieldset>
          </div>

          {debugMode && (
            <div className="space-y-4">
              <SettingsItem
                title={m.video_debugging_info_title()}
                description={m.video_debugging_info_description()}
              >
                <Button
                  size="SM"
                  theme="primary"
                  text={m.video_get_debugging_info()}
                  loading={debugInfoLoading}
                  disabled={debugInfoLoading}
                  onClick={() => {
                    getDebugInfo();
                  }}
                />
              </SettingsItem>
              {debugInfo && (
                <div className="max-h-64 overflow-y-auto rounded-md bg-gray-100 p-2 font-mono text-xs dark:bg-gray-800">
                  <pre className="whitespace-pre-wrap">{debugInfo}</pre>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
