import { useCallback, useEffect, useState } from "react";

import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SettingsItem } from "@routes/devices.$id.settings";
import { BacklightSettings, useSettingsStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import Checkbox from "@components/Checkbox";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { UsbDeviceSetting } from "@components/UsbDeviceSetting";

import notifications from "../notifications";
import { UsbInfoSetting } from "../components/UsbInfoSetting";
import { FeatureFlag } from "../components/FeatureFlag";

export interface ActionBarConfig {
  ctrlAltDel: boolean;
}

const defaultActionBarConfig: ActionBarConfig = {
  ctrlAltDel: false,
};

export default function SettingsHardwareRoute() {
  const [send] = useJsonRpc();
  const settings = useSettingsStore();

  const setDisplayRotation = useSettingsStore(state => state.setDisplayRotation);

  const handleDisplayRotationChange = (rotation: string) => {
    setDisplayRotation(rotation);
    handleDisplayRotationSave();
  };

  const handleDisplayRotationSave = () => {
    send("setDisplayRotation", { params: { rotation: settings.displayRotation } }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set display orientation: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("Display orientation updated successfully");
    });
  };

  const setBacklightSettings = useSettingsStore(state => state.setBacklightSettings);

  const handleBacklightSettingsChange = (settings: BacklightSettings) => {
    // If the user has set the display to dim after it turns off, set the dim_after
    // value to never.
    if (settings.dim_after > settings.off_after && settings.off_after != 0) {
      settings.dim_after = 0;
    }

    setBacklightSettings(settings);
    handleBacklightSettingsSave();
  };

  const handleBacklightSettingsSave = () => {
    send("setBacklightSettings", { params: settings.backlightSettings }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set backlight settings: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("Backlight settings updated successfully");
    });
  };

  useEffect(() => {
    send("getBacklightSettings", {}, resp => {
      if ("error" in resp) {
        return notifications.error(
          `Failed to get backlight settings: ${resp.error.data || "Unknown error"}`,
        );
      }
      const result = resp.result as BacklightSettings;
      setBacklightSettings(result);
    });
  }, [send, setBacklightSettings]);

  const [actionBarConfig, setActionBarConfig] = useState<ActionBarConfig>(defaultActionBarConfig);
  
  const onActionBarItemChange = useCallback(
      (key: keyof ActionBarConfig) => (e: React.ChangeEvent<HTMLInputElement>) => {
        setActionBarConfig(prev => ({
          ...prev,
          [key]: e.target.checked,
        }));
      },
      [],
    );

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Hardware"
        description="Configure display settings and hardware options for your JetKVM device"
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
              settings.displayRotation = e.target.value;
              handleDisplayRotationChange(settings.displayRotation);
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
            value={settings.backlightSettings.max_brightness.toString()}
            options={[
              { value: "0", label: "Off" },
              { value: "10", label: "Low" },
              { value: "35", label: "Medium" },
              { value: "64", label: "High" },
            ]}
            onChange={e => {
              settings.backlightSettings.max_brightness = parseInt(e.target.value);
              handleBacklightSettingsChange(settings.backlightSettings);
            }}
          />
        </SettingsItem>
        <SettingsItem
          title="Enable Ctrl+Alt+Del Action Bar"
          description="Enable or disable the action bar action for sending a Ctrl+Alt+Del to the host"
        >
          <Checkbox
            checked={actionBarConfig.ctrlAltDel}
            onChange={onActionBarItemChange("ctrlAltDel")}
          />
        </SettingsItem>
        {settings.backlightSettings.max_brightness != 0 && (
          <>
            <SettingsItem
              title="Dim Display After"
              description="Set how long to wait before dimming the display"
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={settings.backlightSettings.dim_after.toString()}
                options={[
                  { value: "0", label: "Never" },
                  { value: "60", label: "1 Minute" },
                  { value: "300", label: "5 Minutes" },
                  { value: "600", label: "10 Minutes" },
                  { value: "1800", label: "30 Minutes" },
                  { value: "3600", label: "1 Hour" },
                ]}
                onChange={e => {
                  settings.backlightSettings.dim_after = parseInt(e.target.value);
                  handleBacklightSettingsChange(settings.backlightSettings);
                }}
              />
            </SettingsItem>
            <SettingsItem
              title="Turn off Display After"
              description="Period of inactivity before display automatically turns off"
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={settings.backlightSettings.off_after.toString()}
                options={[
                  { value: "0", label: "Never" },
                  { value: "300", label: "5 Minutes" },
                  { value: "600", label: "10 Minutes" },
                  { value: "1800", label: "30 Minutes" },
                  { value: "3600", label: "1 Hour" },
                ]}
                onChange={e => {
                  settings.backlightSettings.off_after = parseInt(e.target.value);
                  handleBacklightSettingsChange(settings.backlightSettings);
                }}
              />
            </SettingsItem>
          </>
        )}
        <p className="text-xs text-slate-600 dark:text-slate-400">
          The display will wake up when the connection state changes, or when touched.
        </p>
      </div>

      <FeatureFlag minAppVersion="0.3.8">
        <UsbDeviceSetting />
      </FeatureFlag>

      <FeatureFlag minAppVersion="0.3.8">
        <UsbInfoSetting />
      </FeatureFlag>
    </div>
  );
}
