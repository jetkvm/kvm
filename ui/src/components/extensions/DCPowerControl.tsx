import { LuPower } from "react-icons/lu";
import { useCallback, useEffect, useState } from "react";

import { Button } from "@components/Button";
import Card from "@components/Card";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import FieldLabel from "@components/FieldLabel";
import LoadingSpinner from "@components/LoadingSpinner";
import {SelectMenuBasic} from "@components/SelectMenuBasic";

interface DCPowerState {
  isOn: boolean;
  voltage: number;
  current: number;
  power: number;
  restoreState: number;
}

export function DCPowerControl() {
  const { send } = useJsonRpc();
  const [powerState, setPowerState] = useState<DCPowerState | null>(null);

  const getDCPowerState = useCallback(() => {
    send("getDCPowerState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to get DC power state: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setPowerState(resp.result as DCPowerState);
    });
  }, [send]);

  const handlePowerToggle = (enabled: boolean) => {
    send("setDCPowerState", { enabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set DC power state: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      getDCPowerState(); // Refresh state after change
    });
  };
  const handleRestoreChange = (state: number) => {
    // const state = powerState?.restoreState === 0 ? 1 : powerState?.restoreState === 1 ? 2 : 0;
    send("setDCRestoreState", { state }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set DC power state: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      getDCPowerState(); // Refresh state after change
    });
  };



  useEffect(() => {
    getDCPowerState();
    // Set up polling interval to update status
    const interval = setInterval(getDCPowerState, 1000);
    return () => clearInterval(interval);
  }, [getDCPowerState]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="DC Power Control"
        description="Control your DC power settings"
      />

      {powerState === null ? (
        <Card className="flex h-[160px] justify-center p-3">
          <LoadingSpinner className="h-6 w-6 text-blue-500 dark:text-blue-400" />
        </Card>
      ) : (
        <Card className="animate-fadeIn opacity-0">
          <div className="space-y-4 p-3">
            {/* Power Controls */}
            <div className="flex items-center space-x-2">
              <Button
                size="SM"
                theme="light"
                LeadingIcon={LuPower}
                text="Power On"
                onClick={() => handlePowerToggle(true)}
                disabled={powerState.isOn}
              />
              <Button
                size="SM"
                theme="light"
                LeadingIcon={LuPower}
                text="Power Off"
                disabled={!powerState.isOn}
                onClick={() => handlePowerToggle(false)}
              />
            </div>
            {powerState.restoreState > -1 ? (
              <div className="flex items-center">
                <SelectMenuBasic
                    size="SM"
                    label="Restore Power Loss"
                    value={powerState.restoreState}
                    onChange={e => handleRestoreChange(parseInt(e.target.value))}
                    options={[
                      { value: '0', label: "Power OFF" },
                      { value: '1', label: "Power ON" },
                      { value: '2', label: "Last State" },
                    ]}
                />
              </div>
            ) : null}
            <hr className="border-slate-700/30 dark:border-slate-600/30" />

            {/* Status Display */}
            <div className="grid grid-cols-3 gap-4">
              <div className="space-y-1">
                <FieldLabel label="Voltage" />
                <p className="text-sm font-medium text-slate-900 dark:text-slate-100">
                  {powerState.voltage.toFixed(1)}V
                </p>
              </div>
              <div className="space-y-1">
                <FieldLabel label="Current" />
                <p className="text-sm font-medium text-slate-900 dark:text-slate-100">
                  {powerState.current.toFixed(1)}A
                </p>
              </div>
              <div className="space-y-1">
                <FieldLabel label="Power" />
                <p className="text-sm font-medium text-slate-900 dark:text-slate-100">
                  {powerState.power.toFixed(1)}W
                </p>
              </div>
            </div>
          </div>
        </Card>
      )}
    </div>
  );
}
