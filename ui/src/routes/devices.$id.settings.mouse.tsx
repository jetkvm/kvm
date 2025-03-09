import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SettingsItem } from "./devices.$id.settings";
import { Checkbox } from "@/components/Checkbox";
import { GridCard } from "@/components/Card";
import PointingFinger from "@/assets/pointing-finger.svg";
import { CheckCircleIcon } from "@heroicons/react/16/solid";
import { useDeviceSettingsStore, useSettingsStore } from "@/hooks/stores";
import notifications from "@/notifications";
import { useCallback, useEffect, useState } from "react";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { cx } from "../cva.config";
import { SelectMenuBasic } from "../components/SelectMenuBasic";

type ScrollSensitivity = "low" | "default" | "high";

export default function SettingsKeyboardMouseRoute() {
  const hideCursor = useSettingsStore(state => state.isCursorHidden);
  const setHideCursor = useSettingsStore(state => state.setCursorVisibility);
  const scrollSensitivity = useDeviceSettingsStore(state => state.scrollSensitivity);
  const setScrollSensitivity = useDeviceSettingsStore(
    state => state.setScrollSensitivity,
  );

  const [jiggler, setJiggler] = useState(false);

  const [send] = useJsonRpc();

  useEffect(() => {
    send("getJigglerState", {}, resp => {
      if ("error" in resp) return;
      setJiggler(resp.result as boolean);
    });

    send("getScrollSensitivity", {}, resp => {
      if ("error" in resp) return;
      setScrollSensitivity(resp.result as ScrollSensitivity);
    });
  }, [send, setScrollSensitivity]);

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
          <div className="flex flex-col items-center gap-4 md:flex-row">
            <button
              className="group block w-full grow"
              onClick={() => console.log("Absolute mouse mode clicked")}
            >
              <GridCard>
                <div className="group flex items-center gap-x-4 px-4 py-3">
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
                        "h-4 w-4 text-blue-700 transition-opacity duration-300 dark:text-blue-500",
                      )}
                    />
                  </div>
                </div>
              </GridCard>
            </button>
            <button
              className="group block w-full grow cursor-not-allowed opacity-50"
              disabled
            >
              <GridCard>
                <div className="group flex items-center gap-x-4 px-4 py-3">
                  <img
                    className="w-6 shrink-0 dark:invert"
                    src={PointingFinger}
                    alt="Finger touching a screen"
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
                        "hidden",
                        "h-4 w-4 text-blue-700 transition-opacity duration-300 dark:text-blue-500",
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
