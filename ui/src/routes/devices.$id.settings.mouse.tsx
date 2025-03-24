import MouseIcon from "@/assets/mouse-icon.svg";
import PointingFinger from "@/assets/pointing-finger.svg";
import { GridCard } from "@/components/Card";
import { Checkbox } from "@/components/Checkbox";
import { useDeviceSettingsStore, useSettingsStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { CheckCircleIcon } from "@heroicons/react/16/solid";
import { useCallback, useEffect, useState } from "react";
import { FeatureFlag } from "../components/FeatureFlag";
import { SelectMenuBasic } from "../components/SelectMenuBasic";
import { useFeatureFlag } from "../hooks/useFeatureFlag";
import { SettingsItem } from "./devices.$id.settings";
import {InputFieldWithLabel} from "@components/InputField";

type ScrollSensitivity = "low" | "default" | "high";

export interface JigglerConfig {
  active_after_seconds: number;
  jitter_enabled: boolean;
  jitter_percentage: number;
  schedule_cron_tab: string;
}

export default function SettingsKeyboardMouseRoute() {
  const hideCursor = useSettingsStore(state => state.isCursorHidden);
  const setHideCursor = useSettingsStore(state => state.setCursorVisibility);

  const mouseMode = useSettingsStore(state => state.mouseMode);
  const setMouseMode = useSettingsStore(state => state.setMouseMode);

  const scrollSensitivity = useDeviceSettingsStore(state => state.scrollSensitivity);
  const setScrollSensitivity = useDeviceSettingsStore(
    state => state.setScrollSensitivity,
  );

  const { isEnabled: isScrollSensitivityEnabled } = useFeatureFlag("0.3.8");

  const [jiggler, setJiggler] = useState(false);
  const [jigglerConfig, setJigglerConfig] = useState<JigglerConfig>({
    active_after_seconds: 0,
    jitter_enabled: false,
    jitter_percentage: 0.0,
    schedule_cron_tab: "*/20 * * * * *"
  });

  const [send] = useJsonRpc();

  useEffect(() => {
    send("getJigglerState", {}, resp => {
      if ("error" in resp) return;
      setJiggler(resp.result as boolean);
    });

    if (isScrollSensitivityEnabled) {
      send("getScrollSensitivity", {}, resp => {
        if ("error" in resp) return;
        setScrollSensitivity(resp.result as ScrollSensitivity);
      });
    }

    send("getJigglerConfig", {}, resp => {
      if ("error" in resp) return;
      setJiggler(resp.result as boolean);
    });
  }, [isScrollSensitivityEnabled, send, setScrollSensitivity]);

  const handleJigglerChange = (enabled: boolean) => {
    send("setJigglerState", { enabled }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set jiggler state: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setJiggler(enabled);
    });
  };

  // const handleJigglerActiveAfterSecondsChange = (value: number) => {
  //   setJigglerConfig({ ...jigglerConfig, active_after_seconds: value });
  // };
  //
  // const handleJigglerJitterEnabledChange = (value: boolean) => {
  //   setJigglerConfig({ ...jigglerConfig, jitter_enabled: value });
  // };
  //
  // const handleJigglerJitterPercentageChange = (value: number) => {
  //   setJigglerConfig({ ...jigglerConfig, jitter_percentage: value });
  // };

  const handleJigglerScheduleCronTabChange = (value: string) => {
    setJigglerConfig({ ...jigglerConfig, schedule_cron_tab: value });
  };


  const onScrollSensitivityChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const sensitivity = e.target.value as ScrollSensitivity;
      send("setScrollSensitivity", { sensitivity }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set scroll sensitivity: ${resp.error.data || "Unknown error"}`,
          );
        }
        notifications.success("Scroll sensitivity set successfully");
        setScrollSensitivity(sensitivity);
      });
    },
    [send, setScrollSensitivity],
  );

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Mouse"
        description="Configure cursor behavior and interaction settings for your device"
      />

      <div className="space-y-4">
        <SettingsItem
          title="Hide Cursor"
          description="Hide the cursor when sending mouse movements"
        >
          <Checkbox
            checked={hideCursor}
            onChange={e => setHideCursor(e.target.checked)}
          />
        </SettingsItem>

        <FeatureFlag minAppVersion="0.3.8" name="Scroll Sensitivity">
          <SettingsItem
            title="Scroll Sensitivity"
            description="Adjust the scroll sensitivity"
          >
            <SelectMenuBasic
              size="SM"
              label=""
              fullWidth
              value={scrollSensitivity}
              onChange={onScrollSensitivityChange}
              options={
                [
                  { label: "Low", value: "low" },
                  { label: "Default", value: "default" },
                  { label: "High", value: "high" },
                ] as { label: string; value: ScrollSensitivity }[]
              }
            />
          </SettingsItem>
        </FeatureFlag>

        <SettingsItem
          title="Jiggler"
          description="Simulate movement of a computer mouse. Prevents sleep mode, standby mode or the screensaver from activating"
        >
          <Checkbox
            checked={jiggler}
            onChange={e => handleJigglerChange(e.target.checked)}
          />
        </SettingsItem>
        <SettingsItem
          title="Jiggler Schedule"
          description="Schedule for jiggler being triggered. Uses standard crontab format."
        >
          <InputFieldWithLabel
            required
            label="Jiggler Schedule"
            placeholder="Enter Crontab"
            defaultValue={jigglerConfig?.schedule_cron_tab}
            onChange={e => handleJigglerScheduleCronTabChange(e.target.value)}
          />
        </SettingsItem>
        <div className="space-y-4">
          <SettingsItem title="Modes" description="Choose the mouse input mode" />
          <div className="flex items-center gap-4">
            <button
              className="block group grow"
              onClick={() => { setMouseMode("absolute"); }}
            >
              <GridCard>
                <div className="flex items-center px-4 py-3 group gap-x-4">
                  <img
                    className="w-6 shrink-0 dark:invert"
                    src={PointingFinger}
                    alt="Finger touching a screen"
                  />
                  <div className="flex items-center justify-between grow">
                    <div className="text-left">
                      <h3 className="text-sm font-semibold text-black dark:text-white">
                        Absolute
                      </h3>
                      <p className="text-xs leading-none text-slate-800 dark:text-slate-300">
                        Most convenient
                      </p>
                    </div>
                    {mouseMode === "absolute" && (
                      <CheckCircleIcon className="w-4 h-4 text-blue-700 dark:text-blue-500" />
                    )}
                  </div>
                </div>
              </GridCard>
            </button>
            <button
              className="block group grow"
              onClick={() => { setMouseMode("relative"); }}
            >
              <GridCard>
                <div className="flex items-center px-4 py-3 gap-x-4">
                  <img className="w-6 shrink-0 dark:invert" src={MouseIcon} alt="Mouse icon" />
                  <div className="flex items-center justify-between grow">
                    <div className="text-left">
                      <h3 className="text-sm font-semibold text-black dark:text-white">
                        Relative
                      </h3>
                      <p className="text-xs leading-none text-slate-800 dark:text-slate-300">
                        Most Compatible (Beta)
                      </p>
                    </div>
                    {mouseMode === "relative" && (
                      <CheckCircleIcon className="w-4 h-4 text-blue-700 dark:text-blue-500" />
                    )}
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
