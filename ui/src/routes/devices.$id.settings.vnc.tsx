import { useCallback, useEffect, useState } from "react";

import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { m } from "@localizations/messages.js";

import notifications from "../notifications";

interface VNCStateResult {
  enabled: boolean;
  running: boolean;
  port: number;
  quality: number;
  connectionCount: number;
  tlsEnabled: boolean;
}

const VNC_DEFAULTS = {
  port: 5900,
  quality: 80,
} as const;

export default function SettingsVNCRoute() {
  const { send } = useJsonRpc();

  const [enabled, setEnabled] = useState<boolean>(false);
  const [running, setRunning] = useState<boolean>(false);
  const [port, setPort] = useState<number>(VNC_DEFAULTS.port);
  const [quality, setQuality] = useState<number>(VNC_DEFAULTS.quality);
  const [connectionCount, setConnectionCount] = useState<number>(0);
  const [tlsEnabled, setTlsEnabled] = useState<boolean>(false);
  const [isLoading, setIsLoading] = useState(true);

  const loadVNCState = useCallback(() => {
    send("getVNCState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.vnc_settings_failed_load({ error: String(resp.error.data || m.unknown_error()) }),
        );
        setIsLoading(false);
        return;
      }
      const state = resp.result as VNCStateResult;
      setEnabled(state.enabled);
      setRunning(state.running);
      setPort(state.port);
      setQuality(state.quality);
      setConnectionCount(state.connectionCount);
      setTlsEnabled(state.tlsEnabled);
      setIsLoading(false);
    });
  }, [send]);

  useEffect(() => {
    loadVNCState();
    const interval = setInterval(loadVNCState, 5000);
    return () => clearInterval(interval);
  }, [loadVNCState]);

  const handleToggleEnabled = () => {
    const newEnabled = !enabled;
    send("setVNCEnabled", { enabled: newEnabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.vnc_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setEnabled(newEnabled);
      notifications.success(newEnabled ? m.vnc_settings_enabled() : m.vnc_settings_disabled());
      // Reload state to get running status
      setTimeout(loadVNCState, 500);
    });
  };

  const handlePortChange = (newPort: number) => {
    send("setVNCPort", { port: newPort }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.vnc_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setPort(newPort);
      notifications.success(m.vnc_settings_port_changed());
    });
  };

  const handleQualityChange = (newQuality: number) => {
    send("setVNCQuality", { quality: newQuality }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.vnc_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setQuality(newQuality);
      notifications.success(m.vnc_settings_quality_changed());
    });
  };

  const handleTlsToggle = () => {
    const newTlsEnabled = !tlsEnabled;
    send("setVNCTLS", { enabled: newTlsEnabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.vnc_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setTlsEnabled(newTlsEnabled);
      notifications.success(m.vnc_settings_tls_changed());
    });
  };

  if (isLoading) {
    return (
      <div className="space-y-4">
        <SettingsPageHeader
          title={m.vnc_settings_title()}
          description={m.vnc_settings_description()}
        />
        <div className="flex items-center justify-center py-8">
          <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-blue-600"></div>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={m.vnc_settings_title()}
        description={m.vnc_settings_description()}
      />

      <div className="space-y-4">
        <SettingsItem
          title={m.vnc_settings_enable_title()}
          description={m.vnc_settings_enable_description()}
        >
          <label className="relative inline-flex cursor-pointer items-center">
            <input
              type="checkbox"
              checked={enabled}
              onChange={handleToggleEnabled}
              className="peer sr-only"
            />
            <div className="peer h-6 w-11 rounded-full bg-slate-200 peer-checked:bg-blue-600 peer-focus:ring-4 peer-focus:ring-blue-300 peer-focus:outline-none after:absolute after:top-[2px] after:left-[2px] after:h-5 after:w-5 after:rounded-full after:border after:border-slate-300 after:bg-white after:transition-all after:content-[''] peer-checked:after:translate-x-full peer-checked:after:border-white dark:border-slate-600 dark:bg-slate-700 dark:peer-focus:ring-blue-800"></div>
          </label>
        </SettingsItem>

        {enabled && (
          <>
            <div className="rounded-md bg-slate-50 p-4 dark:bg-slate-700">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm font-medium text-slate-900 dark:text-white">
                    {m.vnc_settings_status_title()}
                  </p>
                  <p className="text-sm text-slate-500 dark:text-slate-400">
                    {running
                      ? m.vnc_settings_status_running({ port })
                      : m.vnc_settings_status_stopped()}
                  </p>
                </div>
                <div className="text-right">
                  <span
                    className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${
                      running
                        ? "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200"
                        : "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200"
                    }`}
                  >
                    {running ? m.vnc_settings_running() : m.vnc_settings_stopped()}
                  </span>
                  {connectionCount > 0 && (
                    <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
                      {m.vnc_settings_connections({ count: connectionCount })}
                    </p>
                  )}
                </div>
              </div>
            </div>

            <SettingsItem
              title={m.vnc_settings_port_title()}
              description={m.vnc_settings_port_description()}
            >
              <SelectMenuBasic
                size="SM"
                value={String(port)}
                options={[
                  {
                    value: "5900",
                    label: `5900${port === VNC_DEFAULTS.port ? m.vnc_settings_default_suffix() : ""}`,
                  },
                  { value: "5901", label: "5901" },
                  { value: "5902", label: "5902" },
                  { value: "5903", label: "5903" },
                  { value: "5904", label: "5904" },
                  { value: "5905", label: "5905" },
                ]}
                onChange={e => handlePortChange(parseInt(e.target.value))}
              />
            </SettingsItem>

            <SettingsItem
              title={m.vnc_settings_quality_title()}
              description={m.vnc_settings_quality_description()}
            >
              <SelectMenuBasic
                size="SM"
                value={String(quality)}
                options={[
                  { value: "30", label: `30% (${m.vnc_settings_quality_low()})` },
                  { value: "50", label: `50% (${m.vnc_settings_quality_medium()})` },
                  {
                    value: "80",
                    label: `80%${quality === VNC_DEFAULTS.quality ? m.vnc_settings_default_suffix() : ""} (${m.vnc_settings_quality_high()})`,
                  },
                  { value: "95", label: `95% (${m.vnc_settings_quality_maximum()})` },
                ]}
                onChange={e => handleQualityChange(parseInt(e.target.value))}
              />
            </SettingsItem>

            <SettingsItem
              title={m.vnc_settings_tls_title()}
              description={m.vnc_settings_tls_description()}
            >
              <label className="relative inline-flex cursor-pointer items-center">
                <input
                  type="checkbox"
                  checked={tlsEnabled}
                  onChange={handleTlsToggle}
                  className="peer sr-only"
                />
                <div className="peer h-6 w-11 rounded-full bg-slate-200 peer-checked:bg-blue-600 peer-focus:ring-4 peer-focus:ring-blue-300 peer-focus:outline-none after:absolute after:top-[2px] after:left-[2px] after:h-5 after:w-5 after:rounded-full after:border after:border-slate-300 after:bg-white after:transition-all after:content-[''] peer-checked:after:translate-x-full peer-checked:after:border-white dark:border-slate-600 dark:bg-slate-700 dark:peer-focus:ring-blue-800"></div>
              </label>
            </SettingsItem>
          </>
        )}
      </div>

      <div className="rounded-md bg-amber-50 p-4 dark:bg-amber-900/20">
        <p className="text-sm text-amber-700 dark:text-amber-300">
          {m.vnc_settings_password_note()}
        </p>
      </div>
    </div>
  );
}
