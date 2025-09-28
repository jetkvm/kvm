import { CheckCircleIcon } from "@heroicons/react/16/solid";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import MouseIcon from "@/assets/mouse-icon.svg";
import PointingFinger from "@/assets/pointing-finger.svg";
import { GridCard } from "@/components/Card";
import { Checkbox } from "@/components/Checkbox";
import { useSettingsStore } from "@/hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { JigglerSetting } from "@components/JigglerSetting";

import { cx } from "../cva.config";
import notifications from "../notifications";
import SettingsNestedSection from "../components/SettingsNestedSection";

import { SettingsItem } from "./devices.$id.settings";

export interface JigglerConfig {
  inactivity_limit_seconds: number;
  jitter_percentage: number;
  schedule_cron_tab: string;
  timezone?: string;
}

export default function SettingsMouseRoute() {
  const { t } = useTranslation();
  const jigglerOptions = [
        { value: "disabled", label: t('Disabled'), config: null },
        {
            value: "frequent",
            label: t('Frequent_30s'),
            config: {
                inactivity_limit_seconds: 30,
                jitter_percentage: 25,
                schedule_cron_tab: "*/30 * * * * *",
                // We don't care about the timezone for this preset
                // timezone: "UTC",
            },
        },
        {
            value: "standard",
            label: t('Standard_1m'),
            config: {
                inactivity_limit_seconds: 60,
                jitter_percentage: 25,
                schedule_cron_tab: "0 * * * * *",
                // We don't care about the timezone for this preset
                // timezone: "UTC",
            },
        },
        {
            value: "light",
            label: t('Light_5m'),
            config: {
                inactivity_limit_seconds: 300,
                jitter_percentage: 25,
                schedule_cron_tab: "0 */5 * * * *",
                // We don't care about the timezone for this preset
                // timezone: "UTC",
            },
        },
  ] as const;
  type JigglerValues = (typeof jigglerOptions)[number]["value"] | "custom";
  const {
    isCursorHidden, setCursorVisibility,
    mouseMode, setMouseMode,
    scrollThrottling, setScrollThrottling
  } = useSettingsStore();

  const [selectedJigglerOption, setSelectedJigglerOption] =
    useState<JigglerValues | null>(null);
  const [currentJigglerConfig, setCurrentJigglerConfig] = useState<JigglerConfig | null>(
    null,
  );

  const scrollThrottlingOptions = [
    { value: "0", label: t('Off') },
    { value: "10", label: t('Low') },
    { value: "25", label: t('Medium') },
    { value: "50", label: t('High') },
    { value: "100", label: t('Very_High') },
  ];

  const { send } = useJsonRpc();

  const syncJigglerSettings = useCallback(() => {
    send("getJigglerState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const isEnabled = resp.result as boolean;
      console.log("Jiggler is enabled:", isEnabled);

      // If the jiggler is disabled, set the selected option to "disabled" and nothing else
      if (!isEnabled) return setSelectedJigglerOption("disabled");

      send("getJigglerConfig", {}, (resp: JsonRpcResponse) => {
        if ("error" in resp) return;
        const result = resp.result as JigglerConfig;
        setCurrentJigglerConfig(result);

        const value = jigglerOptions.find(
          o =>
            o?.config?.inactivity_limit_seconds === result.inactivity_limit_seconds &&
            o?.config?.jitter_percentage === result.jitter_percentage &&
            o?.config?.schedule_cron_tab === result.schedule_cron_tab,
        )?.value;

        setSelectedJigglerOption(value || "custom");
      });
    });
  }, [send]);

  useEffect(() => {
    syncJigglerSettings();
  }, [syncJigglerSettings]);

  const saveJigglerConfig = useCallback(
    (jigglerConfig: JigglerConfig) => {
      // We assume the jiggler should be set to enabled if the config is being updated
      send("setJigglerState", { enabled: true }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          return notifications.error(
            t('Failed_to_set_jiggler_state_msg',{msg:resp.error.data || "Unknown error"})
          );
        }
      });

      send("setJigglerConfig", { jigglerConfig }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          const errorMsg = resp.error.data || "Unknown error";

          // Check for cron syntax errors and provide user-friendly message
          if (
            errorMsg.includes("invalid syntax") ||
            errorMsg.includes("parse failure") ||
            errorMsg.includes("invalid cron")
          ) {
            return notifications.error(
              t('Invalid_cron_expression_error')
            );
          }

          return notifications.error(t('Failed_to_set_jiggler_config_msg',{msg:errorMsg}));
        }

        notifications.success(`Jiggler Config successfully updated`);
        syncJigglerSettings();
      });
    },
    [send, syncJigglerSettings],
  );

  const handleJigglerChange = (option: JigglerValues) => {
    if (option === "custom") {
      setSelectedJigglerOption("custom");
      // We don't need to sync the jiggler settings when the option is "custom". The user will press "Save" to save the custom settings.
      return;
    }

    // We don't need to update the device jiggler state when the option is "disabled"
    if (option === "disabled") {
      send("setJigglerState", { enabled: false }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          return notifications.error(
            t('Failed_to_set_jiggler_state_msg',{msg:resp.error.data || "Unknown error"})
          );
        }
      });

      notifications.success(t('Jiggler_Config_successfully_updated'));
      return setSelectedJigglerOption("disabled");
    }

    const jigglerConfig = jigglerOptions.find(o => o.value === option)?.config;
    if (!jigglerConfig) {
      return notifications.error(t('There_was_an_error_setting_the_jiggler_config'));
    }

    saveJigglerConfig(jigglerConfig);
  };

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={t('Mouse')}
        description={t('Configure_cursor_behavior_and_interaction_settings_for_your_device')}
      />

      <div className="space-y-4">
        <SettingsItem
          title={t('Hide_Cursor')}
          description={t('Hide_the_cursor_when_sending_mouse_movements')}
        >
          <Checkbox
            checked={isCursorHidden}
            onChange={e => setCursorVisibility(e.target.checked)}
          />
        </SettingsItem>

        <SettingsItem
          title={t('Scroll_Throttling')}
          description={t('Reduce_the_frequency_of_scroll_events')}
        >
          <SelectMenuBasic
            size="SM"
            label=""
            className="max-w-[292px]"
            value={scrollThrottling}
            fullWidth
            onChange={e => setScrollThrottling(parseInt(e.target.value))}
            options={scrollThrottlingOptions}
          />
        </SettingsItem>

        <SettingsItem title={t('Jiggler')} description={t('Simulate_movement_of_a_computer_mouse')}>
          <SelectMenuBasic
            size="SM"
            label=""
            value={selectedJigglerOption || "disabled"}
            options={[
              ...jigglerOptions.map(option => ({
                value: option.value,
                label: option.label,
              })),
              { value: "custom", label: t('Custom') },
            ]}
            onChange={e => {
              handleJigglerChange(
                e.target.value as (typeof jigglerOptions)[number]["value"],
              );
            }}
          />
        </SettingsItem>

        {selectedJigglerOption === "custom" && (
          <SettingsNestedSection>
            <JigglerSetting
              onSave={saveJigglerConfig}
              defaultJigglerState={currentJigglerConfig || undefined}
            />
          </SettingsNestedSection>
        )}
        <div className="space-y-4">
          <SettingsItem title={t('Modes')} description={t('Choose_the_mouse_input_mode')} />
          <div className="flex items-center gap-4">
            <button
              className="group block grow"
              onClick={() => {
                setMouseMode("absolute");
              }}
            >
              <GridCard>
                <div className="group flex w-full items-center gap-x-4 px-4 py-3">
                  <img
                    className="w-6 shrink-0 dark:invert"
                    src={PointingFinger}
                    alt={t('Finger_touching_a_screen')}
                  />
                  <div className="flex grow items-center justify-between">
                    <div className="text-left">
                      <h3 className="text-sm font-semibold text-black dark:text-white">
                          {t('Absolute')}
                      </h3>
                      <p className="text-xs leading-none text-slate-800 dark:text-slate-300">
                          {t('Most_convenient')}
                      </p>
                    </div>
                    <CheckCircleIcon
                      className={cx(
                        "h-4 w-4 text-blue-700 opacity-0 transition dark:text-blue-500",
                        { "opacity-100": mouseMode === "absolute" },
                      )}
                    />
                  </div>
                </div>
              </GridCard>
            </button>
            <button
              className="group block grow"
              onClick={() => {
                setMouseMode("relative");
              }}
            >
              <GridCard>
                <div className="flex w-full items-center gap-x-4 px-4 py-3">
                  <img
                    className="w-6 shrink-0 dark:invert"
                    src={MouseIcon}
                    alt={t('Mouse_icon')}
                  />
                  <div className="flex grow items-center justify-between">
                    <div className="text-left">
                      <h3 className="text-sm font-semibold text-black dark:text-white">
                          {t('Relative')}
                      </h3>
                      <p className="text-xs leading-none text-slate-800 dark:text-slate-300">
                          {t('Most_Compatible')}
                      </p>
                    </div>
                    <CheckCircleIcon
                      className={cx(
                        "h-4 w-4 text-blue-700 opacity-0 transition dark:text-blue-500",
                        { "opacity-100": mouseMode === "relative" },
                      )}
                    />
                  </div>
                </div>
              </GridCard>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
