import { useEffect, useState } from "react";

import { BacklightSettings, useSettingsStore } from "@hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { Checkbox } from "@components/Checkbox";
import { FeatureFlag } from "@components/FeatureFlag";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SettingsSectionHeader } from "@components/SettingsSectionHeader";
import { NestedSettingsGroup } from "@components/NestedSettingsGroup";
import { UsbDeviceSetting } from "@components/UsbDeviceSetting";
import { UsbInfoSetting } from "@components/UsbInfoSetting";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

export default function SettingsHardwareRoute() {
  const { send } = useJsonRpc();
  const settings = useSettingsStore();
  const { setDisplayRotation } = useSettingsStore();
  const [powerSavingEnabled, setPowerSavingEnabled] = useState(false);

  const handleDisplayRotationChange = (rotation: string) => {
    setDisplayRotation(rotation);

    send("setDisplayRotation", { params: { rotation } }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.hardware_display_orientation_error({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      notifications.success(m.hardware_display_orientation_success());
    });
  };

  const { backlightSettings, setBacklightSettings } = useSettingsStore();

  const handleBacklightSettingsChange = (settings: BacklightSettings) => {
    // If the user has set the display to dim after it turns off, set the dim_after
    // value to never.
    if (settings.dim_after > settings.off_after && settings.off_after != 0) {
      settings.dim_after = 0;
    }

    setBacklightSettings(settings);
    handleBacklightSettingsSave(settings);
  };

  const handleBacklightSettingsSave = (backlightSettings: BacklightSettings) => {
    send("setBacklightSettings", { params: backlightSettings }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.hardware_backlight_settings_error({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      notifications.success(m.hardware_backlight_settings_success());
    });
  };

  const handleBacklightMaxBrightnessChange = (max_brightness: number) => {
    const settings = { ...backlightSettings, max_brightness };
    handleBacklightSettingsChange(settings);
  };

  const handleBacklightDimAfterChange = (dim_after: number) => {
    const settings = { ...backlightSettings, dim_after };
    handleBacklightSettingsChange(settings);
  };

  const handleBacklightOffAfterChange = (off_after: number) => {
    const settings = { ...backlightSettings, off_after };
    handleBacklightSettingsChange(settings);
  };

  const handlePowerSavingChange = (enabled: boolean) => {
    setPowerSavingEnabled(enabled);
    const duration = enabled ? 90 : -1;
    send("setVideoSleepMode", { duration }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.hardware_power_saving_failed_error({ error: resp.error.data || m.unknown_error() }),
        );
        setPowerSavingEnabled(!enabled); // Attempt to revert on error
        return;
      }
      notifications.success(
        enabled ? m.hardware_power_saving_enabled() : m.hardware_power_saving_disabled(),
      );
    });
  };

  useEffect(() => {
    send("getBacklightSettings", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        return notifications.error(
          m.hardware_backlight_settings_get_error({ error: resp.error.data || m.unknown_error() }),
        );
      }
      const result = resp.result as BacklightSettings;
      setBacklightSettings(result);
    });
  }, [send, setBacklightSettings]);

  useEffect(() => {
    send("getVideoSleepMode", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to get power saving mode:", resp.error);
        return;
      }
      const result = resp.result as { enabled: boolean; duration: number };
      setPowerSavingEnabled(result.duration >= 0);
    });
  }, [send]);

  useEffect(() => {
    send("getDisplayRotation", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to get display rotation:", resp.error);
        return;
      }
      const result = resp.result as { rotation: string };
      setDisplayRotation(result.rotation);
    });
  }, [send, setDisplayRotation]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader title={m.hardware_title()} description={m.hardware_page_description()} />
      <div className="space-y-4">
        <SettingsItem
          title={m.hardware_display_orientation_title()}
          description={m.hardware_display_orientation_description()}
        >
          <SelectMenuBasic
            size="SM"
            label=""
            value={settings.displayRotation.toString()}
            options={[
              { value: "270", label: m.hardware_display_orientation_normal() },
              { value: "90", label: m.hardware_display_orientation_inverted() },
            ]}
            onChange={e => {
              handleDisplayRotationChange(e.target.value);
            }}
          />
        </SettingsItem>
        <SettingsItem
          title={m.hardware_display_brightness_title()}
          description={m.hardware_display_brightness_description()}
        >
          <SelectMenuBasic
            size="SM"
            label=""
            value={backlightSettings.max_brightness.toString()}
            options={[
              { value: "0", label: m.hardware_display_brightness_off() },
              { value: "10", label: m.hardware_display_brightness_low() },
              { value: "35", label: m.hardware_display_brightness_medium() },
              { value: "64", label: m.hardware_display_brightness_high() },
            ]}
            onChange={e => {
              handleBacklightMaxBrightnessChange(Number.parseInt(e.target.value));
            }}
          />
        </SettingsItem>
        {backlightSettings.max_brightness != 0 && (
          <NestedSettingsGroup>
            <SettingsItem
              title={m.hardware_dim_display_after_title()}
              description={m.hardware_dim_display_after_description()}
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={backlightSettings.dim_after.toString()}
                options={[
                  { value: "0", label: m.hardware_time_never() },
                  { value: "60", label: m.hardware_time_1_minute() },
                  { value: "300", label: m.hardware_time_5_minutes() },
                  { value: "600", label: m.hardware_time_10_minutes() },
                  { value: "1800", label: m.hardware_time_30_minutes() },
                  { value: "3600", label: m.hardware_time_1_hour() },
                ]}
                onChange={e => {
                  handleBacklightDimAfterChange(Number.parseInt(e.target.value));
                }}
              />
            </SettingsItem>
            <SettingsItem
              title={m.hardware_turn_off_display_after_title()}
              description={m.hardware_turn_off_display_after_description()}
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={backlightSettings.off_after.toString()}
                options={[
                  { value: "0", label: m.hardware_time_never() },
                  { value: "300", label: m.hardware_time_5_minutes() },
                  { value: "600", label: m.hardware_time_10_minutes() },
                  { value: "1800", label: m.hardware_time_30_minutes() },
                  { value: "3600", label: m.hardware_time_1_hour() },
                ]}
                onChange={e => {
                  handleBacklightOffAfterChange(Number.parseInt(e.target.value));
                }}
              />
            </SettingsItem>
          </NestedSettingsGroup>
        )}
        <p className="text-xs text-slate-600 dark:text-slate-400">
          {m.hardware_display_wake_up_note()}
        </p>
      </div>

      <FeatureFlag minAppVersion="0.4.9">
        <div className="space-y-4">
          <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />
          <SettingsSectionHeader
            title={m.hardware_power_saving_title()}
            description={m.hardware_power_saving_description()}
          />
          <SettingsItem
            badge={m.experimental()}
            title={m.hardware_power_saving_hdmi_sleep_title()}
            description={m.hardware_power_saving_hdmi_sleep_description()}
          >
            <Checkbox
              checked={powerSavingEnabled}
              onChange={e => handlePowerSavingChange(e.target.checked)}
            />
          </SettingsItem>
        </div>
      </FeatureFlag>

      <FeatureFlag minAppVersion="0.3.8">
        <UsbDeviceSetting />
      </FeatureFlag>

      <FeatureFlag minAppVersion="0.3.8">
        <UsbInfoSetting />
      </FeatureFlag>
    </div>
  );
}
