import { useCallback, useEffect, useState, useMemo } from "react";
import clsx from "clsx";

import { useSettingsStore } from "@hooks/stores";
import { JsonRpcError, JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { Button, LinkButton } from "@components/Button";
import Checkbox, { CheckboxWithLabel } from "@components/Checkbox";
import { ConfirmDialog } from "@components/ConfirmDialog";
import { GridCard } from "@components/Card";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { NestedSettingsGroup } from "@components/NestedSettingsGroup";
import { TextAreaWithLabel } from "@components/TextArea";
import { InputFieldWithLabel } from "@components/InputField";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { isOnDevice } from "@/main";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";
import { sleep } from "@/utils";
import { checkUpdateComponents, UpdateComponents } from "@/utils/jsonrpc";
import { SystemVersionInfo } from "@hooks/useVersion";

import { FeatureFlag } from "../components/FeatureFlag";

// LogLevelState from the backend
interface LogLevelState {
  globalLevel: string;
  subsystemLevels: Record<string, string>;
  availableLevels: string[];
  subsystems: string[];
  overrides: string;
}

// Parsed subsystem override tag
interface SubsystemOverride {
  subsystem: string;
  level: string;
}

// Parse overrides string into global level and subsystem overrides
function parseOverrides(overrides: string): {
  global: string | null;
  subsystems: SubsystemOverride[];
} {
  const result: { global: string | null; subsystems: SubsystemOverride[] } = {
    global: null,
    subsystems: [],
  };

  if (!overrides) return result;

  const parts = overrides.split(",");
  for (const part of parts) {
    const trimmed = part.trim();
    if (!trimmed) continue;

    if (trimmed.includes(":")) {
      const [subsystem, level] = trimmed.split(":", 2);
      result.subsystems.push({
        subsystem: subsystem.trim().toLowerCase(),
        level: level.trim().toUpperCase(),
      });
    } else {
      result.global = trimmed.toUpperCase();
    }
  }

  return result;
}

// Build overrides string from global level and subsystem overrides
function buildOverrides(global: string | null, subsystems: SubsystemOverride[]): string {
  const parts: string[] = [];
  if (global) {
    parts.push(global);
  }
  for (const sub of subsystems) {
    parts.push(`${sub.subsystem}:${sub.level}`);
  }
  return parts.join(",");
}

// Get translated label for log level
function getLevelLabel(level: string): string {
  switch (level) {
    case "DISABLE":
      return m.advanced_logging_level_disable();
    case "ERROR":
      return m.advanced_logging_level_error();
    case "WARN":
      return m.advanced_logging_level_warn();
    case "INFO":
      return m.advanced_logging_level_info();
    case "DEBUG":
      return m.advanced_logging_level_debug();
    case "TRACE":
      return m.advanced_logging_level_trace();
    default:
      return level;
  }
}

// Get color class for log level tag
function getLevelColorClass(level: string): string {
  switch (level) {
    case "TRACE":
      return "text-purple-600 dark:text-purple-400";
    case "DEBUG":
      return "text-blue-600 dark:text-blue-400";
    case "INFO":
      return "text-green-600 dark:text-green-400";
    case "WARN":
      return "text-amber-600 dark:text-amber-400";
    case "ERROR":
      return "text-red-600 dark:text-red-400";
    case "DISABLE":
      return "text-slate-500 dark:text-slate-400";
    default:
      return "text-slate-700 dark:text-slate-300";
  }
}

export default function SettingsAdvancedRoute() {
  const { send } = useJsonRpc();
  const { navigateTo } = useDeviceUiNavigation();

  const [sshKey, setSSHKey] = useState<string>("");
  const { setDeveloperMode } = useSettingsStore();
  const [devChannel, setDevChannel] = useState(false);
  const [usbEmulationEnabled, setUsbEmulationEnabled] = useState(false);
  const [showLoopbackWarning, setShowLoopbackWarning] = useState(false);
  const [localLoopbackOnly, setLocalLoopbackOnly] = useState(false);
  const [updateTarget, setUpdateTarget] = useState<string>("app");
  const [appVersion, setAppVersion] = useState<string>("");
  const [systemVersion, setSystemVersion] = useState<string>("");
  const [resetConfig, setResetConfig] = useState(false);
  const [versionChangeAcknowledged, setVersionChangeAcknowledged] = useState(false);
  const [customVersionUpdateLoading, setCustomVersionUpdateLoading] = useState(false);
  const settings = useSettingsStore();

  // Logging state
  const [logLevelState, setLogLevelState] = useState<LogLevelState | null>(null);
  const [logGlobalLevel, setLogGlobalLevel] = useState<string>("");
  const [logSubsystemOverrides, setLogSubsystemOverrides] = useState<SubsystemOverride[]>([]);
  const [newSubsystem, setNewSubsystem] = useState<string>("");
  const [newSubsystemLevel, setNewSubsystemLevel] = useState<string>("DEBUG");

  // Parse current overrides when state changes
  const currentOverrides = useMemo(() => {
    return buildOverrides(logGlobalLevel || null, logSubsystemOverrides);
  }, [logGlobalLevel, logSubsystemOverrides]);

  useEffect(() => {
    send("getDevModeState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const result = resp.result as { enabled: boolean };
      setDeveloperMode(result.enabled);
    });

    send("getSSHKeyState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setSSHKey(resp.result as string);
    });

    send("getUsbEmulationState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setUsbEmulationEnabled(resp.result as boolean);
    });

    send("getDevChannelState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setDevChannel(resp.result as boolean);
    });

    send("getLocalLoopbackOnly", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setLocalLoopbackOnly(resp.result as boolean);
    });

    // Load log level state
    send("getLogLevelState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const state = resp.result as LogLevelState;
      setLogLevelState(state);
      const parsed = parseOverrides(state.overrides);
      setLogGlobalLevel(parsed.global || "");
      setLogSubsystemOverrides(parsed.subsystems);
    });
  }, [send, setDeveloperMode]);

  const getUsbEmulationState = useCallback(() => {
    send("getUsbEmulationState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setUsbEmulationEnabled(resp.result as boolean);
    });
  }, [send]);

  const handleUsbEmulationToggle = useCallback(
    (enabled: boolean) => {
      send("setUsbEmulationState", { enabled: enabled }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            enabled
              ? m.advanced_error_usb_emulation_enable({
                  error: resp.error.data || m.unknown_error(),
                })
              : m.advanced_error_usb_emulation_disable({
                  error: resp.error.data || m.unknown_error(),
                }),
          );
          return;
        }
        setUsbEmulationEnabled(enabled);
        getUsbEmulationState();
      });
    },
    [getUsbEmulationState, send],
  );

  const handleResetConfig = useCallback(() => {
    send("resetConfig", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.advanced_error_reset_config({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      notifications.success(m.advanced_success_reset_config());
    });
  }, [send]);

  const handleUpdateSSHKey = useCallback(() => {
    send("setSSHKeyState", { sshKey }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.advanced_error_update_ssh_key({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      notifications.success(m.advanced_success_update_ssh_key());
    });
  }, [send, sshKey]);

  const handleDevModeChange = useCallback(
    (developerMode: boolean) => {
      send("setDevModeState", { enabled: developerMode }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            m.advanced_error_set_dev_mode({ error: resp.error.data || m.unknown_error() }),
          );
          return;
        }
        setDeveloperMode(developerMode);
      });
    },
    [send, setDeveloperMode],
  );

  const handleDevChannelChange = useCallback(
    (enabled: boolean) => {
      send("setDevChannelState", { enabled }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            m.advanced_error_set_dev_channel({ error: resp.error.data || m.unknown_error() }),
          );
          return;
        }
        setDevChannel(enabled);
      });
    },
    [send, setDevChannel],
  );

  const applyLoopbackOnlyMode = useCallback(
    (enabled: boolean) => {
      send("setLocalLoopbackOnly", { enabled }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            enabled
              ? m.advanced_error_loopback_enable({ error: resp.error.data || m.unknown_error() })
              : m.advanced_error_loopback_disable({ error: resp.error.data || m.unknown_error() }),
          );
          return;
        }
        setLocalLoopbackOnly(enabled);
        if (enabled) {
          notifications.success(m.advanced_success_loopback_enabled());
        } else {
          notifications.success(m.advanced_success_loopback_disabled());
        }
      });
    },
    [send, setLocalLoopbackOnly],
  );

  const handleLoopbackOnlyModeChange = useCallback(
    (enabled: boolean) => {
      // If trying to enable loopback-only mode, show warning first
      if (enabled) {
        setShowLoopbackWarning(true);
      } else {
        // If disabling, just proceed
        applyLoopbackOnlyMode(false);
      }
    },
    [applyLoopbackOnlyMode, setShowLoopbackWarning],
  );

  const confirmLoopbackModeEnable = useCallback(() => {
    applyLoopbackOnlyMode(true);
    setShowLoopbackWarning(false);
  }, [applyLoopbackOnlyMode, setShowLoopbackWarning]);

  const handleVersionUpdateError = useCallback((error?: JsonRpcError | string) => {
    notifications.error(
      m.advanced_error_version_update({
        error:
          typeof error === "string" ? error : (error?.data ?? error?.message ?? m.unknown_error()),
      }),
      { duration: 1000 * 15 }, // 15 seconds
    );
    setCustomVersionUpdateLoading(false);
  }, []);

  const handleCustomVersionUpdate = useCallback(async () => {
    const components: UpdateComponents = {};
    if (["app", "both"].includes(updateTarget) && appVersion) components.app = appVersion;
    if (["system", "both"].includes(updateTarget) && systemVersion)
      components.system = systemVersion;
    let versionInfo: SystemVersionInfo | undefined;

    try {
      // we do not need to set it to false if check succeeds,
      // because it will be redirected to the update page later
      setCustomVersionUpdateLoading(true);
      versionInfo = await checkUpdateComponents({ components }, devChannel);
    } catch (error: unknown) {
      const jsonRpcError = error as JsonRpcError;
      handleVersionUpdateError(jsonRpcError);
      return;
    }

    let hasUpdate = false;

    const pageParams = new URLSearchParams();
    if (components.app && versionInfo?.remote?.appVersion && versionInfo?.appUpdateAvailable) {
      hasUpdate = true;
      pageParams.set("custom_app_version", versionInfo.remote?.appVersion);
    }
    if (
      components.system &&
      versionInfo?.remote?.systemVersion &&
      versionInfo?.systemUpdateAvailable
    ) {
      hasUpdate = true;
      pageParams.set("custom_system_version", versionInfo.remote?.systemVersion);
    }
    pageParams.set("reset_config", resetConfig.toString());

    if (!hasUpdate) {
      handleVersionUpdateError("No update available");
      return;
    }

    // Navigate to update page
    navigateTo(`/settings/general/update?${pageParams.toString()}`);
  }, [
    appVersion,
    devChannel,
    handleVersionUpdateError,
    navigateTo,
    resetConfig,
    systemVersion,
    updateTarget,
  ]);

  // Log level handlers
  const applyLogLevelChanges = useCallback(
    (global: string, subsystems: SubsystemOverride[]) => {
      const overrides = buildOverrides(global || null, subsystems);
      send("setLogLevel", { params: { overrides } }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            m.advanced_logging_error({ error: resp.error.data || m.unknown_error() }),
          );
          return;
        }
        notifications.success(m.advanced_logging_updated());
        // Refresh state
        send("getLogLevelState", {}, (refreshResp: JsonRpcResponse) => {
          if ("error" in refreshResp) return;
          setLogLevelState(refreshResp.result as LogLevelState);
        });
      });
    },
    [send],
  );

  const handleGlobalLevelChange = useCallback(
    (level: string) => {
      setLogGlobalLevel(level);
      applyLogLevelChanges(level, logSubsystemOverrides);
    },
    [applyLogLevelChanges, logSubsystemOverrides],
  );

  const handleAddSubsystemOverride = useCallback(() => {
    if (!newSubsystem.trim()) return;
    const subsystem = newSubsystem.trim().toLowerCase();
    // Check if already exists
    if (logSubsystemOverrides.some(s => s.subsystem === subsystem)) {
      notifications.error(m.advanced_logging_subsystem_exists());
      return;
    }
    const newOverrides = [...logSubsystemOverrides, { subsystem, level: newSubsystemLevel }];
    setLogSubsystemOverrides(newOverrides);
    setNewSubsystem("");
    applyLogLevelChanges(logGlobalLevel, newOverrides);
  }, [
    newSubsystem,
    newSubsystemLevel,
    logSubsystemOverrides,
    logGlobalLevel,
    applyLogLevelChanges,
  ]);

  const handleRemoveSubsystemOverride = useCallback(
    (subsystem: string) => {
      const newOverrides = logSubsystemOverrides.filter(s => s.subsystem !== subsystem);
      setLogSubsystemOverrides(newOverrides);
      applyLogLevelChanges(logGlobalLevel, newOverrides);
    },
    [logSubsystemOverrides, logGlobalLevel, applyLogLevelChanges],
  );

  const handleSubsystemLevelChange = useCallback(
    (subsystem: string, level: string) => {
      const newOverrides = logSubsystemOverrides.map(s =>
        s.subsystem === subsystem ? { ...s, level } : s,
      );
      setLogSubsystemOverrides(newOverrides);
      applyLogLevelChanges(logGlobalLevel, newOverrides);
    },
    [logSubsystemOverrides, logGlobalLevel, applyLogLevelChanges],
  );

  return (
    <div className="space-y-4">
      <SettingsPageHeader title={m.advanced_title()} description={m.advanced_description()} />

      <div className="space-y-4">
        <SettingsItem
          title={m.advanced_dev_channel_title()}
          description={m.advanced_dev_channel_description()}
        >
          <Checkbox
            checked={devChannel}
            onChange={e => {
              handleDevChannelChange(e.target.checked);
            }}
          />
        </SettingsItem>
        <SettingsItem
          title={m.advanced_developer_mode_title()}
          description={m.advanced_developer_mode_description()}
        >
          <Checkbox
            checked={settings.developerMode}
            onChange={e => handleDevModeChange(e.target.checked)}
          />
        </SettingsItem>
        {settings.developerMode ? (
          <NestedSettingsGroup>
            <GridCard>
              <div className="flex items-start gap-x-4 p-4 select-none">
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  viewBox="0 0 24 24"
                  fill="currentColor"
                  className="mt-1 h-8 w-8 shrink-0 text-amber-600 dark:text-amber-500"
                >
                  <path
                    fillRule="evenodd"
                    d="M9.401 3.003c1.155-2 4.043-2 5.197 0l7.355 12.748c1.154 2-.29 4.5-2.599 4.5H4.645c-2.309 0-3.752-2.5-2.598-4.5L9.4 3.003zM12 8.25a.75.75 0 01.75.75v3.75a.75.75 0 01-1.5 0V9a.75.75 0 01.75-.75zm0 8.25a.75.75 0 100-1.5.75.75 0 000 1.5z"
                    clipRule="evenodd"
                  />
                </svg>
                <div className="space-y-3">
                  <div className="space-y-2">
                    <h3 className="text-base font-bold text-slate-900 dark:text-white">
                      {m.advanced_developer_mode_enabled_title()}
                    </h3>
                    <div>
                      <ul className="list-disc space-y-1 pl-5 text-xs text-slate-700 dark:text-slate-300">
                        <li>{m.advanced_developer_mode_warning_security()}</li>
                        <li>{m.advanced_developer_mode_warning_risks()}</li>
                      </ul>
                    </div>
                  </div>
                  <div className="text-xs text-slate-700 dark:text-slate-300">
                    {m.advanced_developer_mode_warning_advanced()}
                  </div>
                </div>
              </div>
            </GridCard>

            {isOnDevice && (
              <div className="space-y-4">
                <SettingsItem
                  title={m.advanced_ssh_access_title()}
                  description={m.advanced_ssh_access_description()}
                />
                <TextAreaWithLabel
                  label={m.advanced_ssh_public_key_label()}
                  value={sshKey || ""}
                  rows={3}
                  onChange={e => setSSHKey(e.target.value)}
                  placeholder={m.advanced_ssh_public_key_placeholder()}
                />
                <p className="text-xs text-slate-600 dark:text-slate-400">
                  {m.advanced_ssh_default_user()}
                  <strong>root</strong>.
                </p>
                <div className="flex items-center gap-x-2">
                  <Button
                    size="SM"
                    theme="primary"
                    text={m.advanced_update_ssh_key_button()}
                    onClick={handleUpdateSSHKey}
                  />
                </div>
              </div>
            )}

            <FeatureFlag minAppVersion="0.4.10" name="version-update">
              <div className="space-y-4">
                <SettingsItem
                  title={m.advanced_version_update_title()}
                  description={m.advanced_version_update_description()}
                />

                <SelectMenuBasic
                  label={m.advanced_version_update_target_label()}
                  options={[
                    { value: "app", label: m.advanced_version_update_target_app() },
                    { value: "system", label: m.advanced_version_update_target_system() },
                    { value: "both", label: m.advanced_version_update_target_both() },
                  ]}
                  value={updateTarget}
                  onChange={e => setUpdateTarget(e.target.value)}
                />

                {(updateTarget === "app" || updateTarget === "both") && (
                  <InputFieldWithLabel
                    label={m.advanced_version_update_app_label()}
                    placeholder="0.4.9"
                    value={appVersion}
                    onChange={e => setAppVersion(e.target.value)}
                  />
                )}

                {(updateTarget === "system" || updateTarget === "both") && (
                  <InputFieldWithLabel
                    label={m.advanced_version_update_system_label()}
                    placeholder="0.4.9"
                    value={systemVersion}
                    onChange={e => setSystemVersion(e.target.value)}
                  />
                )}

                <p className="text-xs text-slate-600 dark:text-slate-400">
                  {m.advanced_version_update_helper()}{" "}
                  <a
                    href="https://github.com/jetkvm/kvm/releases"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-medium text-blue-700 hover:underline dark:text-blue-500"
                  >
                    {m.advanced_version_update_github_link()}
                  </a>
                </p>

                <div>
                  <CheckboxWithLabel
                    label={m.advanced_version_update_reset_config_label()}
                    description={m.advanced_version_update_reset_config_description()}
                    checked={resetConfig}
                    onChange={e => setResetConfig(e.target.checked)}
                  />
                </div>

                <div>
                  <CheckboxWithLabel
                    label="I understand version changes may break my device and require factory reset"
                    checked={versionChangeAcknowledged}
                    onChange={e => setVersionChangeAcknowledged(e.target.checked)}
                  />
                </div>

                <Button
                  size="SM"
                  theme="primary"
                  text={m.advanced_version_update_button()}
                  disabled={
                    (updateTarget === "app" && !appVersion) ||
                    (updateTarget === "system" && !systemVersion) ||
                    (updateTarget === "both" && (!appVersion || !systemVersion)) ||
                    !versionChangeAcknowledged ||
                    customVersionUpdateLoading
                  }
                  loading={customVersionUpdateLoading}
                  onClick={handleCustomVersionUpdate}
                />
              </div>
            </FeatureFlag>
          </NestedSettingsGroup>
        ) : null}

        <SettingsItem
          title={m.advanced_loopback_only_title()}
          description={m.advanced_loopback_only_description()}
        >
          <Checkbox
            checked={localLoopbackOnly}
            onChange={e => handleLoopbackOnlyModeChange(e.target.checked)}
          />
        </SettingsItem>

        <SettingsItem
          title={m.advanced_troubleshooting_mode_title()}
          description={m.advanced_troubleshooting_mode_description()}
        >
          <Checkbox
            defaultChecked={settings.debugMode}
            onChange={e => {
              settings.setDebugMode(e.target.checked);
            }}
          />
        </SettingsItem>

        {settings.debugMode && (
          <NestedSettingsGroup>
            <SettingsItem
              title={m.advanced_usb_emulation_title()}
              description={m.advanced_usb_emulation_description()}
            >
              <Button
                size="SM"
                theme="light"
                text={
                  usbEmulationEnabled
                    ? m.advanced_disable_usb_emulation()
                    : m.advanced_enable_usb_emulation()
                }
                onClick={() => handleUsbEmulationToggle(!usbEmulationEnabled)}
              />
            </SettingsItem>

            <SettingsItem
              title={m.advanced_reset_config_title()}
              description={m.advanced_reset_config_description()}
            >
              <Button
                size="SM"
                theme="light"
                text={m.advanced_reset_config_button()}
                onClick={async () => {
                  handleResetConfig();
                  // Add 2s delay between resetting the configuration and calling reload() to prevent reload from interrupting the RPC call to reset things.
                  await sleep(2000);
                  window.location.reload();
                }}
              />
            </SettingsItem>

            <SettingsItem
              title={m.advanced_download_diagnostics_title()}
              description={m.advanced_download_diagnostics_description()}
            >
              <LinkButton
                to="/diagnostics"
                reloadDocument
                download
                size="SM"
                theme="light"
                text={m.advanced_download_diagnostics_button()}
              />
            </SettingsItem>

            {/* Logging Configuration */}
            <div className="space-y-4 pt-2">
              <SettingsItem
                title={m.advanced_logging_title()}
                description={m.advanced_logging_description()}
              />

              {logLevelState && (
                <div className="space-y-4">
                  {/* Global Log Level */}
                  <SelectMenuBasic
                    label={m.advanced_logging_global_level()}
                    options={[
                      { value: "", label: m.advanced_logging_level_default() },
                      ...(logLevelState.availableLevels.map(level => ({
                        value: level,
                        label: getLevelLabel(level),
                      })) || []),
                    ]}
                    value={logGlobalLevel}
                    onChange={e => handleGlobalLevelChange(e.target.value)}
                  />

                  {/* Subsystem Overrides */}
                  <div className="space-y-2">
                    <p className="text-sm font-medium text-slate-700 dark:text-slate-300">
                      {m.advanced_logging_overrides()}
                    </p>
                    <p className="text-xs text-slate-600 dark:text-slate-400">
                      {m.advanced_logging_overrides_description()}
                    </p>

                    {/* Current overrides as tags */}
                    {logSubsystemOverrides.length > 0 && (
                      <div className="flex flex-wrap gap-2 pt-1">
                        {logSubsystemOverrides.map(override => (
                          <div
                            key={override.subsystem}
                            className={clsx(
                              "flex items-center gap-1 rounded-md px-2 py-1",
                              "bg-blue-50 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300",
                              "border border-blue-200 dark:border-blue-700",
                            )}
                          >
                            <span className="font-mono text-xs font-medium">
                              {override.subsystem}
                            </span>
                            <span className="text-blue-400">:</span>
                            <select
                              className={clsx(
                                "cursor-pointer appearance-none border-none bg-transparent text-xs font-semibold focus:outline-none",
                                getLevelColorClass(override.level),
                              )}
                              value={override.level}
                              onChange={e =>
                                handleSubsystemLevelChange(override.subsystem, e.target.value)
                              }
                            >
                              {logLevelState.availableLevels.map(level => (
                                <option key={level} value={level}>
                                  {level}
                                </option>
                              ))}
                            </select>
                            <button
                              type="button"
                              onClick={() => handleRemoveSubsystemOverride(override.subsystem)}
                              className={clsx(
                                "ml-1 rounded-full p-0.5 hover:bg-blue-200 dark:hover:bg-blue-800",
                                "text-blue-500 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-200",
                              )}
                              aria-label={`Remove ${override.subsystem} override`}
                            >
                              <svg
                                className="h-3 w-3"
                                fill="none"
                                viewBox="0 0 24 24"
                                stroke="currentColor"
                                strokeWidth={2}
                              >
                                <path
                                  strokeLinecap="round"
                                  strokeLinejoin="round"
                                  d="M6 18L18 6M6 6l12 12"
                                />
                              </svg>
                            </button>
                          </div>
                        ))}
                      </div>
                    )}

                    {/* Add new override */}
                    <div className="flex items-center gap-2 pt-1">
                      <select
                        className={clsx(
                          "h-8 rounded-md border border-slate-300 bg-white px-2 text-xs dark:border-slate-600 dark:bg-slate-800",
                          "focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none",
                        )}
                        value={newSubsystem}
                        onChange={e => setNewSubsystem(e.target.value)}
                      >
                        <option value="">{m.advanced_logging_select_subsystem()}</option>
                        {logLevelState.subsystems
                          .filter(s => !logSubsystemOverrides.some(o => o.subsystem === s))
                          .map(subsystem => (
                            <option key={subsystem} value={subsystem}>
                              {subsystem}
                            </option>
                          ))}
                      </select>
                      <select
                        className={clsx(
                          "h-8 rounded-md border border-slate-300 bg-white px-2 text-xs dark:border-slate-600 dark:bg-slate-800",
                          "focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none",
                        )}
                        value={newSubsystemLevel}
                        onChange={e => setNewSubsystemLevel(e.target.value)}
                      >
                        {logLevelState.availableLevels.map(level => (
                          <option key={level} value={level}>
                            {level}
                          </option>
                        ))}
                      </select>
                      <Button
                        size="SM"
                        theme="light"
                        text={m.advanced_logging_add_override()}
                        disabled={!newSubsystem}
                        onClick={handleAddSubsystemOverride}
                      />
                    </div>

                    {/* Current config string (read-only) */}
                    {currentOverrides && (
                      <div className="pt-2">
                        <p className="text-xs text-slate-500 dark:text-slate-400">
                          {m.advanced_logging_current_config()}:{" "}
                          <code className="font-mono text-xs text-slate-700 dark:text-slate-300">
                            {currentOverrides || m.advanced_logging_level_default()}
                          </code>
                        </p>
                      </div>
                    )}
                  </div>
                </div>
              )}
            </div>
          </NestedSettingsGroup>
        )}
      </div>

      <ConfirmDialog
        open={showLoopbackWarning}
        onClose={() => {
          setShowLoopbackWarning(false);
        }}
        title={m.advanced_loopback_warning_title()}
        description={
          <>
            <p>{m.advanced_loopback_warning_description()}</p>
            <p>{m.advanced_loopback_warning_before()}</p>
            <ul className="list-disc space-y-1 pl-5 text-xs text-slate-700 dark:text-slate-300">
              <li>{m.advanced_loopback_warning_ssh()}</li>
              <li>{m.advanced_loopback_warning_cloud()}</li>
            </ul>
          </>
        }
        variant="warning"
        confirmText={m.advanced_loopback_warning_confirm()}
        onConfirm={confirmLoopbackModeEnable}
      />
    </div>
  );
}
