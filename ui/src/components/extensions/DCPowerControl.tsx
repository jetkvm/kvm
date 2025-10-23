import { useCallback, useEffect, useState } from "react";
import { LuPower } from "react-icons/lu";

import { m } from "@localizations/messages.js";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { Button } from "@components/Button";
import Card from "@components/Card";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import FieldLabel from "@components/FieldLabel";
import LoadingSpinner from "@components/LoadingSpinner";
import {SelectMenuBasic} from "@components/SelectMenuBasic";
import notifications from "@/notifications";

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
        notifications.error(m.dc_power_control_get_state_error({ error: resp.error.data || m.unknown_error() }));
        return;
      }
      setPowerState(resp.result as DCPowerState);
    });
  }, [send]);

  const handlePowerToggle = (enabled: boolean) => {
    send("setDCPowerState", { enabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.dc_power_control_set_power_state_error({ enabled: enabled, error: resp.error.data || m.unknown_error() }));
        return;
      }
      getDCPowerState(); // Refresh state after change
    });
  };
  const handleRestoreChange = (state: number) => {
    // const state = powerState?.restoreState === 0 ? 1 : powerState?.restoreState === 1 ? 2 : 0;
    send("setDCRestoreState", { state }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.dc_power_control_set_restore_state_error({ state: state, error: resp.error.data || m.unknown_error() }));
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
        title={m.extensions_dc_power_control()}
        description={m.extensions_dc_power_control_description()}
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
                text={m.dc_power_control_power_on_button()}
                onClick={() => handlePowerToggle(true)}
                disabled={powerState.isOn}
              />
              <Button
                size="SM"
                theme="light"
                LeadingIcon={LuPower}
                text={m.dc_power_control_power_off_button()}
                disabled={!powerState.isOn}
                onClick={() => handlePowerToggle(false)}
              />
            </div>
            {powerState.restoreState > -1 ? (
              <div className="flex items-center">
                <SelectMenuBasic
                    size="SM"
                    label={m.dc_power_control_restore_power_state()}
                    value={powerState.restoreState}
                    onChange={e => handleRestoreChange(parseInt(e.target.value))}
                    options={[
                      { value: '0', label: m.dc_power_control_power_off_state()},
                      { value: '1', label: m.dc_power_control_power_on_state()},
                      { value: '2', label: m.dc_power_control_restore_last_state()},
                    ]}
                />
              </div>
            ) : null}
            <hr className="border-slate-700/30 dark:border-slate-600/30" />

            {/* Status Display */}
            <div className="grid grid-cols-3 gap-4">
              <div className="space-y-1">
                <FieldLabel label={m.dc_power_control_voltage()} />
                <p className="text-sm font-medium text-slate-900 dark:text-slate-100">
                  {powerState.voltage.toFixed(1)}&nbsp;{m.dc_power_control_voltage_unit()}
                </p>
              </div>
              <div className="space-y-1">
                <FieldLabel label={m.dc_power_control_current()} />
                <p className="text-sm font-medium text-slate-900 dark:text-slate-100">
                  {powerState.current.toFixed(1)}&nbsp;{m.dc_power_control_current_unit()}
                </p>
              </div>
              <div className="space-y-1">
                <FieldLabel label={m.dc_power_control_power()}/>
                <p className="text-sm font-medium text-slate-900 dark:text-slate-100">
                  {powerState.power.toFixed(1)}&nbsp;{m.dc_power_control_power_unit()}
                </p>
              </div>
            </div>
          </div>
        </Card>
      )}
    </div>
  );
}
