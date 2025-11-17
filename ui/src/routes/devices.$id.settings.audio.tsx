import { useEffect } from "react";

import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { useSettingsStore } from "@/hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import Checkbox from "@components/Checkbox";
import { m } from "@localizations/messages.js";

import notifications from "../notifications";

interface AudioConfigResult {
  bitrate: number;
  complexity: number;
  dtx_enabled: boolean;
  fec_enabled: boolean;
  buffer_periods: number;
}

export default function SettingsAudioRoute() {
  const { send } = useJsonRpc();
  const {
    setAudioOutputEnabled,
    setAudioInputAutoEnable,
    setAudioOutputSource,
    audioOutputEnabled,
    audioInputAutoEnable,
    audioOutputSource,
    audioBitrate,
    setAudioBitrate,
    audioComplexity,
    setAudioComplexity,
    audioDTXEnabled,
    setAudioDTXEnabled,
    audioFECEnabled,
    setAudioFECEnabled,
    audioBufferPeriods,
    setAudioBufferPeriods,
  } = useSettingsStore();

  useEffect(() => {
    send("getAudioOutputEnabled", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setAudioOutputEnabled(resp.result as boolean);
    });

    send("getAudioInputAutoEnable", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setAudioInputAutoEnable(resp.result as boolean);
    });

    send("getAudioOutputSource", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setAudioOutputSource(resp.result as string);
    });

    send("getAudioConfig", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const config = resp.result as AudioConfigResult;
      setAudioBitrate(config.bitrate);
      setAudioComplexity(config.complexity);
      setAudioDTXEnabled(config.dtx_enabled);
      setAudioFECEnabled(config.fec_enabled);
      setAudioBufferPeriods(config.buffer_periods);
    });
  }, [send, setAudioOutputEnabled, setAudioInputAutoEnable, setAudioOutputSource, setAudioBitrate, setAudioComplexity, setAudioDTXEnabled, setAudioFECEnabled, setAudioBufferPeriods]);

  const handleAudioOutputEnabledChange = (enabled: boolean) => {
    send("setAudioOutputEnabled", { enabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        const errorMsg = enabled
          ? m.audio_output_failed_enable({ error: String(resp.error.data || m.unknown_error()) })
          : m.audio_output_failed_disable({ error: String(resp.error.data || m.unknown_error()) });
        notifications.error(errorMsg);
        return;
      }
      setAudioOutputEnabled(enabled);
      const successMsg = enabled ? m.audio_output_enabled() : m.audio_output_disabled();
      notifications.success(successMsg);
    });
  };

  const handleAudioOutputSourceChange = (source: string) => {
    send("setAudioOutputSource", { source }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.audio_settings_output_source_failed({ error: String(resp.error.data || m.unknown_error()) }));
        return;
      }
      setAudioOutputSource(source);
      notifications.success(m.audio_settings_output_source_success());
    });
  };

  const handleAudioInputAutoEnableChange = (enabled: boolean) => {
    send("setAudioInputAutoEnable", { enabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(String(resp.error.data || m.unknown_error()));
        return;
      }
      setAudioInputAutoEnable(enabled);
      const successMsg = enabled
        ? m.audio_input_auto_enable_enabled()
        : m.audio_input_auto_enable_disabled();
      notifications.success(successMsg);
    });
  };

  const handleAudioConfigChange = (
    bitrate: number,
    complexity: number,
    dtxEnabled: boolean,
    fecEnabled: boolean,
    bufferPeriods: number
  ) => {
    send(
      "setAudioConfig",
      { bitrate, complexity, dtxEnabled, fecEnabled, bufferPeriods },
      (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(String(resp.error.data || m.unknown_error()));
          return;
        }
        setAudioBitrate(bitrate);
        setAudioComplexity(complexity);
        setAudioDTXEnabled(dtxEnabled);
        setAudioFECEnabled(fecEnabled);
        setAudioBufferPeriods(bufferPeriods);
        notifications.success(m.audio_settings_config_updated());
      }
    );
  };

  const handleRestartAudio = () => {
    send("restartAudioOutput", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(String(resp.error.data || m.unknown_error()));
        return;
      }
      notifications.success(m.audio_settings_applied());
    });
  };

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={m.audio_settings_title()}
        description={m.audio_settings_description()}
      />
      <div className="space-y-4">
        <SettingsItem
          title={m.audio_settings_output_title()}
          description={m.audio_settings_output_description()}
        >
          <Checkbox
            checked={audioOutputEnabled || false}
            onChange={(e) => handleAudioOutputEnabledChange(e.target.checked)}
          />
        </SettingsItem>

        <SettingsItem
          title={m.audio_settings_output_source_title()}
          description={m.audio_settings_output_source_description()}
        >
          <SelectMenuBasic
            size="SM"
            value={audioOutputSource || "usb"}
            options={[
              { value: "usb", label: m.audio_settings_usb_label() },
              { value: "hdmi", label: m.audio_settings_hdmi_label() },
            ]}
            onChange={(e) => handleAudioOutputSourceChange(e.target.value)}
          />
        </SettingsItem>

        <SettingsItem
          title={m.audio_settings_auto_enable_microphone_title()}
          description={m.audio_settings_auto_enable_microphone_description()}
        >
          <Checkbox
            checked={audioInputAutoEnable || false}
            onChange={(e) => handleAudioInputAutoEnableChange(e.target.checked)}
          />
        </SettingsItem>

        <SettingsItem
          title={m.audio_settings_bitrate_title()}
          description={m.audio_settings_bitrate_description()}
        >
          <SelectMenuBasic
            size="SM"
            value={String(audioBitrate)}
            options={[
              { value: "64", label: "64 kbps" },
              { value: "96", label: "96 kbps" },
              { value: "128", label: "128 kbps" },
              { value: "160", label: "160 kbps" },
              { value: "192", label: "192 kbps" },
              { value: "256", label: "256 kbps" },
            ]}
            onChange={(e) =>
              handleAudioConfigChange(
                parseInt(e.target.value),
                audioComplexity,
                audioDTXEnabled,
                audioFECEnabled,
                audioBufferPeriods
              )
            }
          />
        </SettingsItem>

        <SettingsItem
          title={m.audio_settings_complexity_title()}
          description={m.audio_settings_complexity_description()}
        >
          <SelectMenuBasic
            size="SM"
            value={String(audioComplexity)}
            options={[
              { value: "0", label: "0 (fastest)" },
              { value: "2", label: "2" },
              { value: "5", label: "5 (balanced)" },
              { value: "8", label: "8" },
              { value: "10", label: "10 (best)" },
            ]}
            onChange={(e) =>
              handleAudioConfigChange(
                audioBitrate,
                parseInt(e.target.value),
                audioDTXEnabled,
                audioFECEnabled,
                audioBufferPeriods
              )
            }
          />
        </SettingsItem>

        <SettingsItem
          title={m.audio_settings_dtx_title()}
          description={m.audio_settings_dtx_description()}
        >
          <Checkbox
            checked={audioDTXEnabled}
            onChange={(e) =>
              handleAudioConfigChange(
                audioBitrate,
                audioComplexity,
                e.target.checked,
                audioFECEnabled,
                audioBufferPeriods
              )
            }
          />
        </SettingsItem>

        <SettingsItem
          title={m.audio_settings_fec_title()}
          description={m.audio_settings_fec_description()}
        >
          <Checkbox
            checked={audioFECEnabled}
            onChange={(e) =>
              handleAudioConfigChange(
                audioBitrate,
                audioComplexity,
                audioDTXEnabled,
                e.target.checked,
                audioBufferPeriods
              )
            }
          />
        </SettingsItem>

        <SettingsItem
          title={m.audio_settings_buffer_title()}
          description={m.audio_settings_buffer_description()}
        >
          <SelectMenuBasic
            size="SM"
            value={String(audioBufferPeriods)}
            options={[
              { value: "4", label: "4 (80ms)" },
              { value: "8", label: "8 (160ms)" },
              { value: "12", label: "12 (240ms)" },
              { value: "16", label: "16 (320ms)" },
              { value: "24", label: "24 (480ms)" },
            ]}
            onChange={(e) =>
              handleAudioConfigChange(
                audioBitrate,
                audioComplexity,
                audioDTXEnabled,
                audioFECEnabled,
                parseInt(e.target.value)
              )
            }
          />
        </SettingsItem>

        <div className="pt-4">
          <button
            onClick={handleRestartAudio}
            className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500"
          >
            {m.audio_settings_apply_button()}
          </button>
        </div>
      </div>
    </div>
  );
}
