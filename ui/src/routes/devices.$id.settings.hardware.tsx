import { useEffect } from "react";
import { useTranslation } from "react-i18next";

import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SettingsItem } from "@routes/devices.$id.settings";
import { BacklightSettings, useSettingsStore } from "@/hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { UsbDeviceSetting } from "@components/UsbDeviceSetting";

import notifications from "../notifications";
import { UsbInfoSetting } from "../components/UsbInfoSetting";
import { FeatureFlag } from "../components/FeatureFlag";

export default function SettingsHardwareRoute() {
  const { send } = useJsonRpc();
  const { t } = useTranslation();
  const settings = useSettingsStore();
  const { setDisplayRotation } = useSettingsStore();

  const handleDisplayRotationChange = (rotation: string) => {
    setDisplayRotation(rotation);
    handleDisplayRotationSave();
  };

  const handleDisplayRotationSave = () => {
    send("setDisplayRotation", { params: { rotation: settings.displayRotation } }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          t('Failed_to_set_display_orientation_msg',{msg:resp.error.data || t('Unknown_error')})
        );
        return;
      }
      notifications.success(t('Display_orientation_updated_successfully'));
    });
  };

  const { setBacklightSettings } = useSettingsStore();

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
    send("setBacklightSettings", { params: settings.backlightSettings }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          t('Failed_to_set_backlight_settings_msg',{msg:resp.error.data || t('Unknown_error')})
        );
        return;
      }
      notifications.success(t('Backlight_settings_updated_successfully'));
    });
  };

  useEffect(() => {
    send("getBacklightSettings", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        return notifications.error(
          t('Failed_to_get_backlight_settings_msg',{msg:resp.error.data || t('Unknown_error')})
        );
      }
      const result = resp.result as BacklightSettings;
      setBacklightSettings(result);
    });
  }, [send, setBacklightSettings]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={t('Hardware')}
        description={t('Configure_display_settings_and_hardware_options_for_your_JetKVM_device')}
      />
      <div className="space-y-4">
        <SettingsItem
          title={t('Display_Orientation')}
          description={t('Set_the_orientation_of_the_display')}
        >
          <SelectMenuBasic
            size="SM"
            label=""
            value={settings.displayRotation.toString()}
            options={[
              { value: "270", label: t('Normal') },
              { value: "90", label: t('Inverted') },
            ]}
            onChange={e => {
              settings.displayRotation = e.target.value;
              handleDisplayRotationChange(settings.displayRotation);
            }}
          />
        </SettingsItem>
        <SettingsItem
          title={t('Display_Brightness')}
          description={t('Set_the_brightness_of_the_display')}
        >
          <SelectMenuBasic
            size="SM"
            label=""
            value={settings.backlightSettings.max_brightness.toString()}
            options={[
              { value: "0", label: t('Off') },
              { value: "10", label: t('Low') },
              { value: "35", label: t('Medium') },
              { value: "64", label: t('High') },
            ]}
            onChange={e => {
              settings.backlightSettings.max_brightness = parseInt(e.target.value);
              handleBacklightSettingsChange(settings.backlightSettings);
            }}
          />
        </SettingsItem>
        {settings.backlightSettings.max_brightness != 0 && (
          <>
            <SettingsItem
              title={t('Dim_Display_After')}
              description={t('Set_how_long_to_wait_before_dimming_the_display')}
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={settings.backlightSettings.dim_after.toString()}
                options={[
                  { value: "0", label: t('Never') },
                  { value: "60", label: t('num_minute',{num:1}) },
                  { value: "300", label: t('num_minute',{num:5}) },
                  { value: "600", label: t('num_minute',{num:10}) },
                  { value: "1800", label: t('num_minute',{num:30}) },
                  { value: "3600", label: t('1Hour') },
                ]}
                onChange={e => {
                  settings.backlightSettings.dim_after = parseInt(e.target.value);
                  handleBacklightSettingsChange(settings.backlightSettings);
                }}
              />
            </SettingsItem>
            <SettingsItem
              title={t('Turn_off_Display_After')}
              description={t('Period_of_inactivity_before_display_automatically_turns_off')}
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={settings.backlightSettings.off_after.toString()}
                options={[
                  { value: "0", label: t('Never') },
                  { value: "300", label: t('num_minute',{num:5}) },
                  { value: "600", label: t('num_minute',{num:10}) },
                  { value: "1800", label: t('num_minute',{num:30}) },
                  { value: "3600", label: t('1Hour') },
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
            {t('The_display_will_wake_up_when_the_connection_state_changes_or_when_touched')}
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
