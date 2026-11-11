import { useEffect, useState } from "react";
import { LuHdmiPort, LuSettings2 } from "react-icons/lu";

import { m } from "@localizations/messages.js";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { Button } from "@components/Button";
import Card from "@components/Card";
import { InputFieldWithLabel } from "@components/InputField";
import LoadingSpinner from "@components/LoadingSpinner";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import notifications from "@/notifications";

interface HdmiSwitchState {
  activeInput: number; // 0-based index into labels, -1 = unknown
  online: boolean;
  portCount: number;
}

interface HdmiSwitchConfig {
  host: string;
  port: number;
  port_count: number;
  labels: string[];
}

function normalizeLabels(labels: string[], portCount: number): string[] {
  return Array.from({ length: portCount }, (_, i) => {
    const label = labels[i];
    if (label === undefined || label === "") return `PC${i + 1}`;
    return label;
  });
}

export function HdmiSwitchControl() {
  const [hdmiSwitchState, setHdmiSwitchState] = useState<HdmiSwitchState | null>(null);
  const [config, setConfig] = useState<HdmiSwitchConfig | null>(null);
  const [isEditingSettings, setIsEditingSettings] = useState(false);
  const [editedConfig, setEditedConfig] = useState<HdmiSwitchConfig | null>(null);
  const [pendingInput, setPendingInput] = useState<number | null>(null);

  const { send } = useJsonRpc(function onRequest(resp) {
    if (resp.method === "hdmiSwitchState") {
      setHdmiSwitchState(resp.params as HdmiSwitchState);
    }
  });

  // Request initial state and config
  useEffect(() => {
    send("getHdmiSwitchState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.hdmi_switch_get_state_error({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      setHdmiSwitchState(resp.result as HdmiSwitchState);
    });
    send("getHdmiSwitchConfig", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.hdmi_switch_get_config_error({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      setConfig(resp.result as HdmiSwitchConfig);
    });
  }, [send]);

  const handleSwitchInput = (input: number) => {
    if (!config) return;
    setPendingInput(input);
    send("setHdmiSwitchInput", { input }, (resp: JsonRpcResponse) => {
      setPendingInput(null);
      if ("error" in resp) {
        notifications.error(
          m.hdmi_switch_switch_error({
            port: config.labels[input] || `PC${input + 1}`,
            error: resp.error.data || m.unknown_error(),
          }),
        );
      }
    });
  };

  const handleEditSettings = () => {
    if (!config) return;
    setEditedConfig({ ...config, labels: [...config.labels] });
    setIsEditingSettings(true);
  };

  const handleSaveSettings = () => {
    if (!editedConfig) return;
    send("setHdmiSwitchConfig", { config: editedConfig }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.hdmi_switch_set_config_error({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      const saved = resp.result as HdmiSwitchConfig | undefined;
      if (saved) {
        setConfig(saved);
        setIsEditingSettings(false);
        setEditedConfig(null);
      }
    });
  };

  const activeLabel =
    hdmiSwitchState && hdmiSwitchState.activeInput >= 0 && config
      ? config.labels[hdmiSwitchState.activeInput] || `PC${hdmiSwitchState.activeInput + 1}`
      : null;

  return (
    <div className="space-y-4">
      <SettingsPageHeader title={m.hdmi_switch()} description={m.hdmi_switch_description()} />

      {hdmiSwitchState === null || config === null ? (
        <Card className="flex min-h-[120px] items-center justify-center p-3">
          <LoadingSpinner className="h-6 w-6 text-blue-500 dark:text-blue-400" />
        </Card>
      ) : (
        <Card className="min-h-[120px] animate-fadeIn opacity-0">
          <div className="space-y-4 p-3">
            {/* Status */}
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <span className="text-sm text-slate-600 dark:text-slate-400">
                  <LuHdmiPort
                    strokeWidth={3}
                    className={`mr-1 inline ${hdmiSwitchState.online ? "text-green-600" : "text-slate-300"}`}
                  />
                  {m.hdmi_switch_active_input()}
                </span>
              </div>
              <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">
                {!hdmiSwitchState.online
                  ? config.host
                    ? m.hdmi_switch_offline()
                    : m.hdmi_switch_not_configured()
                  : activeLabel || m.hdmi_switch_unknown_input()}
              </span>
            </div>

            {isEditingSettings && editedConfig ? (
              /* Settings form */
              <div className="space-y-3">
                <InputFieldWithLabel
                  size="SM"
                  label={m.hdmi_switch_host()}
                  placeholder="192.168.1.10"
                  value={editedConfig.host}
                  onChange={e => setEditedConfig({ ...editedConfig, host: e.target.value.trim() })}
                />
                <div className="grid grid-cols-2 gap-2">
                  <InputFieldWithLabel
                    size="SM"
                    label={m.hdmi_switch_tcp_port()}
                    type="number"
                    min={1}
                    max={65535}
                    value={editedConfig.port}
                    onChange={e =>
                      setEditedConfig({ ...editedConfig, port: Number(e.target.value) })
                    }
                  />
                  <InputFieldWithLabel
                    size="SM"
                    label={m.hdmi_switch_port_count()}
                    type="number"
                    min={1}
                    max={16}
                    value={editedConfig.port_count}
                    onChange={e => {
                      const portCount = Math.max(1, Math.min(16, Number(e.target.value)));
                      setEditedConfig({
                        ...editedConfig,
                        port_count: portCount,
                        labels: normalizeLabels(editedConfig.labels, portCount),
                      });
                    }}
                  />
                </div>
                <div>
                  <p className="mb-2 text-[13px] font-medium text-slate-900 dark:text-slate-100">
                    {m.hdmi_switch_port_labels()}
                  </p>
                  <div className="grid grid-cols-2 gap-2">
                    {editedConfig.labels.map((label, i) => (
                      <InputFieldWithLabel
                        key={i}
                        size="XS"
                        label={`PC${i + 1}`}
                        value={label}
                        maxLength={24}
                        onChange={e => {
                          const labels = [...editedConfig.labels];
                          labels[i] = e.target.value;
                          setEditedConfig({ ...editedConfig, labels });
                        }}
                      />
                    ))}
                  </div>
                </div>
                <div className="flex items-center justify-end space-x-2">
                  <Button
                    size="SM"
                    theme="light"
                    text={m.cancel()}
                    onClick={() => {
                      setIsEditingSettings(false);
                      setEditedConfig(null);
                    }}
                  />
                  <Button
                    size="SM"
                    theme="primary"
                    text={m.hdmi_switch_save_settings()}
                    onClick={handleSaveSettings}
                  />
                </div>
              </div>
            ) : (
              <>
                {/* Port selection grid */}
                <div className="grid grid-cols-2 gap-2">
                  {config.labels.map((label, i) => {
                    const isActive = hdmiSwitchState.activeInput === i;
                    const isPending = pendingInput === i;
                    return (
                      <Button
                        key={i}
                        size="SM"
                        theme={isActive ? "primary" : "light"}
                        text={label || `PC${i + 1}`}
                        loading={isPending}
                        disabled={pendingInput !== null || !hdmiSwitchState.online}
                        onClick={() => handleSwitchInput(i)}
                      />
                    );
                  })}
                </div>

                <hr className="border-slate-700/30 dark:border-slate-600/30" />

                {/* Settings entry */}
                <div className="flex items-center justify-between">
                  <p className="text-xs text-slate-500 dark:text-slate-400">
                    {config.host
                      ? `${config.host}:${config.port} · ${m.hdmi_switch_port_count()}: ${config.port_count}`
                      : m.hdmi_switch_not_configured()}
                  </p>
                  <Button
                    size="XS"
                    theme="light"
                    LeadingIcon={LuSettings2}
                    text={m.hdmi_switch_settings()}
                    onClick={handleEditSettings}
                  />
                </div>
              </>
            )}
          </div>
        </Card>
      )}
    </div>
  );
}
