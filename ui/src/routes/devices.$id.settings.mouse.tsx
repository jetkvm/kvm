import { useCallback, useEffect, useState } from "react";
import { CheckCircleIcon } from "@heroicons/react/16/solid";

import { cx } from "@/cva.config";
import MouseIcon from "@assets/mouse-icon.svg";
import PointingFinger from "@assets/pointing-finger.svg";
import { useJsonRpc } from "@hooks/useJsonRpc";
import { useSettingsStore } from "@hooks/stores";
import type { JsonRpcResponse } from "@hooks/useJsonRpc";
import type { MouseMode } from "@hooks/stores";
import { GridCard } from "@components/Card";
import { Checkbox } from "@components/Checkbox";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsItem } from "@components/SettingsItem";
import SettingsNestedSection from "@components/SettingsNestedSection";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JigglerSetting } from "@components/JigglerSetting";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

export interface JigglerConfig {
  inactivity_limit_seconds: number;
  jitter_percentage: number;
  schedule_cron_tab: string;
  timezone?: string;
}

const jigglerOptions = [
  { value: "disabled", label: m.mouse_jiggler_disabled(), config: null },
  {
    value: "frequent",
    label: m.mouse_jiggler_frequent(),
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
    label: m.mouse_jiggler_standard(),
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
    label: m.mouse_jiggler_light(),
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

const mouseModeOptions: {
  mode: MouseMode;
  title: () => string;
  description: () => string;
  icon: string;
  alt: () => string;
}[] = [
  {
    mode: "absolute",
    title: m.mouse_mode_absolute,
    description: m.mouse_mode_absolute_description,
    icon: PointingFinger,
    alt: m.mouse_alt_finger,
  },
  {
    mode: "relative",
    title: m.mouse_mode_relative,
    description: m.mouse_mode_relative_description,
    icon: MouseIcon,
    alt: m.mouse_alt_mouse,
  },
  {
    mode: "digitizer",
    title: m.mouse_mode_digitizer,
    description: m.mouse_mode_digitizer_description,
    icon: PointingFinger,
    alt: m.mouse_alt_finger,
  },
];

export default function SettingsMouseRoute() {
  const {
    isCursorHidden,
    setCursorVisibility,
    mouseMode,
    setMouseMode,
    scrollThrottling,
    setScrollThrottling,
    invertScroll,
    setInvertScroll,
  } = useSettingsStore();

  const [selectedJigglerOption, setSelectedJigglerOption] = useState<JigglerValues | null>(null);
  const [currentJigglerConfig, setCurrentJigglerConfig] = useState<JigglerConfig | null>(null);

  const scrollThrottlingOptions = [
    { value: "0", label: m.mouse_scroll_off() },
    { value: "10", label: m.mouse_scroll_low() },
    { value: "25", label: m.mouse_scroll_medium() },
    { value: "50", label: m.mouse_scroll_high() },
    { value: "100", label: m.mouse_scroll_very_high() },
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
            m.mouse_jiggler_failed_state({ error: resp.error.data || m.unknown_error() }),
          );
        }
      });

      send("setJigglerConfig", { jigglerConfig }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          const errorMsg = resp.error.data || m.unknown_error();

          // Check for cron syntax errors and provide user-friendly message
          if (
            errorMsg.includes("invalid syntax") ||
            errorMsg.includes("parse failure") ||
            errorMsg.includes("invalid cron")
          ) {
            return notifications.error(m.mouse_jiggler_invalid_cron());
          }

          return notifications.error(m.mouse_jiggler_error_config({ error: errorMsg }));
        }

        notifications.success(m.mouse_jiggler_config_updated());
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
            m.mouse_jiggler_failed_state({ error: resp.error.data || m.unknown_error() }),
          );
        }
      });

      notifications.success(m.mouse_jiggler_config_updated());
      return setSelectedJigglerOption("disabled");
    }

    const jigglerConfig = jigglerOptions.find(o => o.value === option)?.config;
    if (!jigglerConfig) {
      return notifications.error(m.mouse_jiggler_error_config());
    }

    saveJigglerConfig(jigglerConfig);
  };

  return (
    <div className="space-y-4">
      <SettingsPageHeader title={m.mouse_title()} description={m.mouse_description()} />

      <div className="space-y-4">
        <SettingsItem
          title={m.mouse_hide_cursor_title()}
          description={m.mouse_hide_cursor_description()}
        >
          <Checkbox
            checked={isCursorHidden}
            onChange={e => setCursorVisibility(e.target.checked)}
          />
        </SettingsItem>

        <SettingsItem
          title={m.mouse_scroll_throttling_title()}
          description={m.mouse_scroll_throttling_description()}
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

        <SettingsItem
          title={m.mouse_scroll_invert_title()}
          description={m.mouse_scroll_invert_description()}
        >
          <Checkbox checked={invertScroll} onChange={e => setInvertScroll(e.target.checked)} />
        </SettingsItem>

        <SettingsItem title={m.mouse_jiggler_title()} description={m.mouse_jiggler_description()}>
          <SelectMenuBasic
            size="SM"
            label=""
            value={selectedJigglerOption || "disabled"}
            options={[
              ...jigglerOptions.map(option => ({
                value: option.value,
                label: option.label,
              })),
              { value: "custom", label: m.mouse_jiggler_custom() },
            ]}
            onChange={e => {
              handleJigglerChange(e.target.value as (typeof jigglerOptions)[number]["value"]);
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
          <SettingsItem title={m.mouse_modes_title()} description={m.mouse_modes_description()} />
          <div className="grid grid-cols-3 gap-4">
            {mouseModeOptions.map(option => {
              const selected = mouseMode === option.mode;
              return (
                <button
                  key={option.mode}
                  className={cx(
                    "group block rounded-lg border text-left transition",
                    selected
                      ? "border-blue-600 bg-blue-50 ring-2 ring-blue-600/20 dark:border-blue-500 dark:bg-blue-950/40"
                      : "border-transparent",
                  )}
                  aria-pressed={selected}
                  onClick={() => {
                    setMouseMode(option.mode);
                  }}
                >
                  <GridCard>
                    <div className="group flex w-full flex-col gap-2 px-3 py-3 sm:flex-row sm:items-center sm:gap-x-4 sm:px-4">
                      <img
                        className="h-6 w-6 shrink-0 dark:invert"
                        src={option.icon}
                        alt={option.alt()}
                      />
                      <div className="flex grow items-start justify-between gap-2 sm:items-center">
                        <div className="text-left">
                          <h3 className="text-sm font-semibold text-black dark:text-white">
                            {option.title()}
                          </h3>
                          <p className="text-xs leading-none text-slate-800 dark:text-slate-300">
                            {option.description()}
                          </p>
                        </div>
                        <CheckCircleIcon
                          className={cx(
                            "h-5 w-5 shrink-0 text-blue-700 opacity-0 transition dark:text-blue-500",
                            { "opacity-100": selected },
                          )}
                        />
                      </div>
                    </div>
                  </GridCard>
                </button>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
