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

export default function SettingsHardwareRoute() {
  const { send } = useJsonRpc();
  const settings = useSettingsStore();
  const { displayRotation, setDisplayRotation } = useSettingsStore();
  const [powerSavingEnabled, setPowerSavingEnabled] = useState(false);

  const handleDisplayRotationChange = (rotation: string) => {
    setDisplayRotation(rotation);
    handleDisplayRotationSave();
  };

  const handleDisplayRotationSave = () => {
    send("setDisplayRotation", { params: { rotation: displayRotation } }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set display orientation: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("Display orientation updated successfully");
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
          `Failed to set backlight settings: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("Backlight settings updated successfully");
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
        notifications.error(`Failed to set power saving mode: ${resp.error.data || "Unknown error"}`);
        setPowerSavingEnabled(!enabled); // Attempt to revert on error
        return;
      }
      notifications.success(enabled ? 'Power saving mode enabled' : 'Power saving mode disabled');
    });
  };

  useEffect(() => {
    send("getBacklightSettings", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        return notifications.error(
          `Failed to get backlight settings: ${resp.error.data || "Unknown error"}`,
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

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Hardware"
        description={"Configure display settings and hardware options for your JetKVM device"}
      />
      <div className="space-y-4">
        <SettingsItem
          title="Display Orientation"
          description="Set the orientation of the display"
        >
          <SelectMenuBasic
            size="SM"
            label=""
            value={settings.displayRotation.toString()}
            options={[
              { value: "270", label: "Normal" },
              { value: "90", label: "Inverted" },
            ]}
            onChange={e => {
              handleDisplayRotationChange(e.target.value);
            }}
          />
        </SettingsItem>
        <SettingsItem
          title="Display Brightness"
          description="Set the brightness of the display"
        >
          <SelectMenuBasic
            size="SM"
            label=""
            value={backlightSettings.max_brightness.toString()}
            options={[
              { value: "0", label: "Off" },
              { value: "10", label: "Low" },
              { value: "35", label: "Medium" },
              { value: "64", label: "High" },
            ]}
            onChange={e => {
              handleBacklightMaxBrightnessChange(Number.parseInt(e.target.value));
            }}
          />
        </SettingsItem>
        {backlightSettings.max_brightness != 0 && (
          <NestedSettingsGroup>
            <SettingsItem
              title="Dim Display After"
              description="Set how long to wait before dimming the display"
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={backlightSettings.dim_after.toString()}
                options={[
                  { value: "0", label: "Never" },
                  { value: "60", label: "1 minute" },
                  { value: "300", label: "5 minutes" },
                  { value: "600", label: "10 minutes" },
                  { value: "1800", label: "30 minutes" },
                  { value: "3600", label: "1 hour" },
                ]}
                onChange={e => {
                  handleBacklightDimAfterChange(Number.parseInt(e.target.value));
                }}
              />
            </SettingsItem>
            <SettingsItem
              title="Turn Off Display After"
              description="Period of inactivity before display automatically turns off"
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={backlightSettings.off_after.toString()}
                options={[
                  { value: "0", label: "Never" },
                  { value: "300", label: "5 minutes" },
                  { value: "600", label: "10 minutes" },
                  { value: "1800", label: "30 minutes" },
                  { value: "3600", label: "1 hour" },
                ]}
                onChange={e => {
                  handleBacklightOffAfterChange(Number.parseInt(e.target.value));
                }}
              />
            </SettingsItem>
          </NestedSettingsGroup>
        )}
        <p className="text-xs text-slate-600 dark:text-slate-400">
          The display will wake up when the connection state changes, or when touched.
        </p>
      </div>

      <FeatureFlag minAppVersion="0.4.9">
        <div className="space-y-4">
          <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />
          <SettingsSectionHeader
            title="Power Saving"
            description="Reduce power consumption when not in use"
          />
          <SettingsItem
            badge="Experimental"
            title="HDMI Sleep Mode"
            description="Reduce power consumption when the HDMI port is not in use"
          >
            <Checkbox
              checked={powerSavingEnabled}
              onChange={(e) => handlePowerSavingChange(e.target.checked)}
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