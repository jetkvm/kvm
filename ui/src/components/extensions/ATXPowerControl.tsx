import { Button } from "@components/Button";
import { LuHardDrive, LuPower, LuRotateCcw } from "react-icons/lu";
import Card from "@components/Card";
import { SectionHeader } from "@components/SectionHeader";
import { useEffect, useState } from "react";
import notifications from "@/notifications";
import { useJsonRpc } from "../../hooks/useJsonRpc";
import LoadingSpinner from "../LoadingSpinner";

const LONG_PRESS_DURATION = 3000; // 3 seconds for long press

interface ATXState {
  power: boolean;
  hdd: boolean;
}

export function ATXPowerControl() {
  const [isPowerPressed, setIsPowerPressed] = useState(false);
  const [powerPressTimer, setPowerPressTimer] = useState<ReturnType<
    typeof setTimeout
  > | null>(null);
  const [atxState, setAtxState] = useState<ATXState | null>(null);

  const [send] = useJsonRpc(function onRequest(resp) {
    if (resp.method === "atxState") {
      setAtxState(resp.params as ATXState);
    }
  });

  // Request initial state
  useEffect(() => {
    send("getATXState", {}, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to get ATX state: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setAtxState(resp.result as ATXState);
    });
  }, [send]);

  const handlePowerPress = (pressed: boolean) => {
    // Prevent phantom releases
    if (!pressed && !isPowerPressed) return;

    setIsPowerPressed(pressed);

    // Handle button press
    if (pressed) {
      // Start long press timer
      const timer = setTimeout(() => {
        // Send long press action
        console.log("Sending long press ATX power action");
        send("setATXPowerAction", { action: "power-long" }, resp => {
          if ("error" in resp) {
            notifications.error(
              `Failed to send ATX power action: ${resp.error.data || "Unknown error"}`,
            );
          }
          setIsPowerPressed(false);
        });
      }, LONG_PRESS_DURATION);

      setPowerPressTimer(timer);
    }
    // Handle button release
    else {
      // If timer exists, was a short press
      if (powerPressTimer) {
        clearTimeout(powerPressTimer);
        setPowerPressTimer(null);

        // Send short press action
        console.log("Sending short press ATX power action");
        send("setATXPowerAction", { action: "power-short" }, resp => {
          if ("error" in resp) {
            notifications.error(
              `Failed to send ATX power action: ${resp.error.data || "Unknown error"}`,
            );
          }
        });
      }
    }
  };

  // Cleanup timer on unmount
  useEffect(() => {
    return () => {
      if (powerPressTimer) {
        clearTimeout(powerPressTimer);
      }
    };
  }, [powerPressTimer]);

  return (
    <div className="space-y-4">
      <SectionHeader
        title="ATX Power Control"
        description="Control your ATX power settings"
      />

      {atxState === null ? (
        <Card className="flex h-[120px] items-center justify-center p-3">
          <LoadingSpinner className="w-6 h-6 text-blue-500 dark:text-blue-400" />
        </Card>
      ) : (
        <Card className="h-[120px] animate-fadeIn opacity-0">
          <div className="p-3 space-y-4">
            {/* Control Buttons */}
            <div className="flex items-center space-x-2">
              <Button
                size="SM"
                theme="light"
                LeadingIcon={LuPower}
                text="Power"
                onMouseDown={() => handlePowerPress(true)}
                onMouseUp={() => handlePowerPress(false)}
                onMouseLeave={() => handlePowerPress(false)}
                className={isPowerPressed ? "opacity-75" : ""}
              />
              <Button
                size="SM"
                theme="light"
                LeadingIcon={LuRotateCcw}
                text="Reset"
                onClick={() => {
                  send("setATXPowerAction", { action: "reset" }, resp => {
                    if ("error" in resp) {
                      notifications.error(
                        `Failed to send ATX power action: ${resp.error.data || "Unknown error"}`,
                      );
                      return;
                    }
                  });
                }}
              />
            </div>

            <hr className="border-slate-700/30 dark:border-slate-600/30" />
            {/* Status Indicators */}
            <div className="flex items-center space-x-4">
              <div className="flex items-center space-x-2">
                <span className="text-sm text-slate-600 dark:text-slate-400">
                  <LuPower
                    strokeWidth={3}
                    className={`mr-1 inline ${
                      atxState?.power ? "text-green-600" : "text-slate-300"
                    }`}
                  />
                  Power LED
                </span>
              </div>
              <div className="flex items-center space-x-2">
                <span className="text-sm text-slate-600 dark:text-slate-400">
                  <LuHardDrive
                    strokeWidth={3}
                    className={`mr-1 inline ${
                      atxState?.hdd ? "text-blue-400" : "text-slate-300"
                    }`}
                  />
                  HDD LED
                </span>
              </div>
            </div>
          </div>
        </Card>
      )}
    </div>
  );
}
