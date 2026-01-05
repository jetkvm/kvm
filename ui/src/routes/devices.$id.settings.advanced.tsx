import { useCallback, useEffect, useState } from "react";

import { useSettingsStore } from "@hooks/stores";
import { JsonRpcError, JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { Button } from "@components/Button";
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
import { callJsonRpc, checkUpdateComponents, UpdateComponents } from "@/utils/jsonrpc";
import { SystemVersionInfo } from "@hooks/useVersion";

import { FeatureFlag } from "../components/FeatureFlag";

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
  const [diagnosticsLoading, setDiagnosticsLoading] = useState(false);
  const settings = useSettingsStore();

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

  const handleDownloadDiagnostics = useCallback(async () => {
    setDiagnosticsLoading(true);

    try {
      const response = await callJsonRpc<string>({
        method: "getDiagnostics",
        attemptTimeoutMs: 20000, // 20s - diagnostics collects a lot of data
        maxAttempts: 1,
      });

      if (response.error) {
        notifications.error(
          m.advanced_error_download_diagnostics({
            error: response.error.data || m.unknown_error(),
          }),
        );
        return;
      }

      const logContent = response.result;
      const timestamp = new Date().toISOString().replace(/[:.]/g, "-");
      const filename = `jetkvm-diagnostics-${timestamp}.txt`;

      const blob = new Blob([logContent], { type: "text/plain" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);

      notifications.success(m.advanced_success_download_diagnostics());
    } catch (error) {
      notifications.error(
        m.advanced_error_download_diagnostics({
          error: error instanceof Error ? error.message : m.unknown_error(),
        }),
      );
    } finally {
      setDiagnosticsLoading(false);
    }
  }, []);

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
              <Button
                size="SM"
                disabled={diagnosticsLoading}
                theme="light"
                text={m.advanced_download_diagnostics_button()}
                loading={diagnosticsLoading}
                onClick={handleDownloadDiagnostics}
              />
            </SettingsItem>
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
