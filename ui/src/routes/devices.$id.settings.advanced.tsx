import { useCallback, useEffect, useState } from "react";
import { useSettingsStore } from "@hooks/stores";
import { JsonRpcError, JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { SystemVersionInfo } from "@hooks/useVersion";

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
import { sleep } from "@/utils";
import { checkUpdateComponents, UpdateComponents } from "@/utils/jsonrpc";

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
          const errorMsg = resp.error.data || "Unknown error";
          notifications.error(
            enabled
              ? `Failed to enable USB emulation: ${errorMsg}`
              : `Failed to disable USB emulation: ${errorMsg}`
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
          `Failed to reset configuration: ${resp.error.data || "Unknown error"}`
        );
        return;
      }
      notifications.success("Configuration reset to default successfully");
    });
  }, [send]);

  const handleUpdateSSHKey = useCallback(() => {
    send("setSSHKeyState", { sshKey }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to update SSH key: ${resp.error.data || "Unknown error"}`
        );
        return;
      }
      notifications.success("SSH key updated successfully");
    });
  }, [send, sshKey]);

  const handleDevModeChange = useCallback(
    (developerMode: boolean) => {
      send("setDevModeState", { enabled: developerMode }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set dev mode: ${resp.error.data || "Unknown error"}`
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
            `Failed to set dev channel state: ${resp.error.data || "Unknown error"}`
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
          const errorMsg = resp.error.data || "Unknown error";
          notifications.error(
            enabled
              ? `Failed to enable loopback-only mode: ${errorMsg}`
              : `Failed to disable loopback-only mode: ${errorMsg}`
          );
          return;
        }
        setLocalLoopbackOnly(enabled);
        if (enabled) {
          notifications.success("Loopback-only mode enabled. Restart your device to apply.");
        } else {
          notifications.success("Loopback-only mode disabled. Restart your device to apply.");
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
    const errorMessage = typeof error === "string" ? error : (error?.data ?? error?.message ?? "Unknown error");
    notifications.error(
      `Failed to initiate version update: ${errorMessage}`,
      { duration: 1000 * 15 } // 15 seconds
    );
    setCustomVersionUpdateLoading(false);
  }, []);

  const handleCustomVersionUpdate = useCallback(async () => {
    const components: UpdateComponents = {};
    if (["app", "both"].includes(updateTarget) && appVersion) components.app = appVersion;
    if (["system", "both"].includes(updateTarget) && systemVersion) components.system = systemVersion;
    let versionInfo: SystemVersionInfo | undefined;

    try {
      // we do not need to set it to false if check succeeds,
      // because it will be redirected to the update page later
      setCustomVersionUpdateLoading(true);
      versionInfo = await checkUpdateComponents({
        components,
      }, devChannel);
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
    if (components.system && versionInfo?.remote?.systemVersion && versionInfo?.systemUpdateAvailable) {
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
    updateTarget, appVersion, systemVersion, devChannel,
    navigateTo, resetConfig, handleVersionUpdateError,
    setCustomVersionUpdateLoading
  ]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Advanced"
        description="Access additional settings for troubleshooting and customization"
      />

      <div className="space-y-4">
        <SettingsItem
          title="Dev Channel Updates"
          description="Receive early updates from the development channel"
        >
          <Checkbox
            checked={devChannel}
            onChange={e => {
              handleDevChannelChange(e.target.checked);
            }}
          />
        </SettingsItem>
        <SettingsItem
          title="Developer Mode"
          description="Enable advanced features for developers"
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
                      Developer Mode Enabled
                    </h3>
                    <div>
                      <ul className="list-disc space-y-1 pl-5 text-xs text-slate-700 dark:text-slate-300">
                        <li>Security is weakened while active</li>
                        <li>Only use if you understand the risks</li>
                      </ul>
                    </div>
                  </div>
                  <div className="text-xs text-slate-700 dark:text-slate-300">
                    For advanced users only. Not for production use.
                  </div>
                </div>
              </div>
            </GridCard>

            {isOnDevice && (
              <div className="space-y-4">
                <SettingsItem
                  title="SSH Access"
                  description="Add your SSH public key to enable secure remote access to the device"
                />
                <TextAreaWithLabel
                  label="SSH Public Key"
                  value={sshKey || ""}
                  rows={3}
                  onChange={e => setSSHKey(e.target.value)}
                  placeholder="Enter your SSH public key"
                />
                <p className="text-xs text-slate-600 dark:text-slate-400">
                  The default SSH user is<strong>root</strong>.
                </p>
                <div className="flex items-center gap-x-2">
                  <Button
                    size="SM"
                    theme="primary"
                    text="Update SSH Key"
                    onClick={handleUpdateSSHKey}
                  />
                </div>
              </div>
            )}

            <FeatureFlag minAppVersion="0.4.10" name="version-update">
              <div className="space-y-4">
                <SettingsItem
                  title="Update to Specific Version"
                  description="Install a specific version from GitHub releases"
                />

                <SelectMenuBasic
                  label="What to update"
                  options={[
                    { value: "app", label: "App only" },
                    { value: "system", label: "System only" },
                    { value: "both", label: "Both App and System" },
                  ]}
                  value={updateTarget}
                  onChange={e => setUpdateTarget(e.target.value)}
                />

                {(updateTarget === "app" || updateTarget === "both") && (
                  <InputFieldWithLabel
                    label="App Version"
                    placeholder="0.4.9"
                    value={appVersion}
                    onChange={e => setAppVersion(e.target.value)}
                  />
                )}

                {(updateTarget === "system" || updateTarget === "both") && (
                  <InputFieldWithLabel
                    label="System Version"
                    placeholder="0.4.9"
                    value={systemVersion}
                    onChange={e => setSystemVersion(e.target.value)}
                  />
                )}

                <p className="text-xs text-slate-600 dark:text-slate-400">
                  Find available versions on the{" "}
                  <a
                    href="https://github.com/jetkvm/kvm/releases"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-medium text-blue-700 hover:underline dark:text-blue-500"
                  >
                    JetKVM releases page
                  </a>
                </p>

                <div>
                  <CheckboxWithLabel
                    label="Reset configuration"
                    description="Reset configuration after the update"
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
                  text="Update to Version"
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
          title="Loopback-Only Mode"
          description="Restrict web interface access to localhost only (127.0.0.1)"
        >
          <Checkbox
            checked={localLoopbackOnly}
            onChange={e => handleLoopbackOnlyModeChange(e.target.checked)}
          />
        </SettingsItem>



        <SettingsItem
          title="Troubleshooting Mode"
          description="Diagnostic tools and additional controls for troubleshooting and development purposes"
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
              title="USB Emulation"
              description="Control the USB emulation state"
            >
              <Button
                size="SM"
                theme="light"
                text={
                  usbEmulationEnabled ? "Disable USB Emulation" : "Enable USB Emulation"
                }
                onClick={() => handleUsbEmulationToggle(!usbEmulationEnabled)}
              />
            </SettingsItem>

            <SettingsItem
              title="Reset Configuration"
              description="Reset configuration to default. This will log you out."
            >
              <Button
                size="SM"
                theme="light"
                text="Reset Config"
                onClick={async () => {
                  handleResetConfig();
                  // Add 2s delay between resetting the configuration and calling reload() to prevent reload from interrupting the RPC call to reset things.
                  await sleep(2000);
                  window.location.reload();
                }}
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
        title="Enable Loopback-Only Mode?"
        description={
          <>
            <p>
              WARNING: This will restrict web interface access to localhost (127.0.0.1)
              only.
            </p>
            <p>Before enabling this feature, make sure you have either:</p>
            <ul className="list-disc space-y-1 pl-5 text-xs text-slate-700 dark:text-slate-300">
              <li>SSH access configured and tested</li>
              <li>Cloud access enabled and working</li>
            </ul>
          </>
        }
        variant="warning"
        confirmText="I Understand, Enable Anyway"
        onConfirm={confirmLoopbackModeEnable}
      />
    </div>
  );
}