import { CheckCircleIcon } from "@heroicons/react/16/solid";
import { useEffect, useState } from "react";

import MouseIcon from "@/assets/mouse-icon.svg";
import PointingFinger from "@/assets/pointing-finger.svg";
import { GridCard } from "@/components/Card";
import { Checkbox } from "@/components/Checkbox";
import { useSettingsStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SelectMenuBasic } from "@components/SelectMenuBasic";

import { useFeatureFlag } from "../hooks/useFeatureFlag";
import { cx } from "../cva.config";

import { SettingsItem } from "./devices.$id.settings";

export default function SettingsMouseRoute() {
  const hideCursor = useSettingsStore(state => state.isCursorHidden);
  const setHideCursor = useSettingsStore(state => state.setCursorVisibility);

  const mouseMode = useSettingsStore(state => state.mouseMode);
  const setMouseMode = useSettingsStore(state => state.setMouseMode);

  const { isEnabled: isScrollSensitivityEnabled } = useFeatureFlag("0.3.8");

  const [jiggler, setJiggler] = useState(false);

  const scrollThrottling = useSettingsStore(state => state.scrollThrottling);
  const setScrollThrottling = useSettingsStore(
    state => state.setScrollThrottling,
  );

  const scrollThrottlingOptions = [
    { value: "0", label: "Off" },
    { value: "10", label: "Low" },
    { value: "25", label: "Medium" },
    { value: "50", label: "High" },
    { value: "100", label: "Very High" },
  ];

  const [send] = useJsonRpc();

  useEffect(() => {
    send("getJigglerState", {}, resp => {
      if ("error" in resp) return;
      setJiggler(resp.result as boolean);
    });
  }, [isScrollSensitivityEnabled, send]);

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

      <SettingsItem
        title="Scroll Throttling"
        description="Reduce the frequency of scroll events"
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
          title="Jiggler"
          description="Simulate movement of a computer mouse. Prevents sleep mode, standby mode or the screensaver from activating"
        >
          <Checkbox
            checked={jiggler}
            onChange={e => handleJigglerChange(e.target.checked)}
          />
        </SettingsItem>
        <div className="space-y-4">
          <SettingsItem title="Modes" description="Choose the mouse input mode" />
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
                    alt="Finger touching a screen"
                  />
                  <div className="flex grow items-center justify-between">
                    <div className="text-left">
                      <h3 className="text-sm font-semibold text-black dark:text-white">
                        Absolute
                      </h3>
                      <p className="text-xs leading-none text-slate-800 dark:text-slate-300">
                        Most convenient
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
                    alt="Mouse icon"
                  />
                  <div className="flex grow items-center justify-between">
                    <div className="text-left">
                      <h3 className="text-sm font-semibold text-black dark:text-white">
                        Relative
                      </h3>
                      <p className="text-xs leading-none text-slate-800 dark:text-slate-300">
                        Most Compatible
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
