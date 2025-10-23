import { useCallback, useEffect, useState } from "react";

import { useSettingsStore } from "@hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { Button } from "@components/Button";
import Checkbox from "@components/Checkbox";
import { ConfirmDialog } from "@components/ConfirmDialog";
import { GridCard } from "@components/Card";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { TextAreaWithLabel } from "@components/TextArea";
import { isOnDevice } from "@/main";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

export default function SettingsAdvancedRoute() {
  const { send } = useJsonRpc();

  const [sshKey, setSSHKey] = useState<string>("");
  const { setDeveloperMode } = useSettingsStore();
  const [devChannel, setDevChannel] = useState(false);
  const [usbEmulationEnabled, setUsbEmulationEnabled] = useState(false);
  const [showLoopbackWarning, setShowLoopbackWarning] = useState(false);
  const [localLoopbackOnly, setLocalLoopbackOnly] = useState(false);

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
              ? m.advanced_error_usb_emulation_enable({ error: resp.error.data || m.unknown_error() })
              : m.advanced_error_usb_emulation_disable({ error: resp.error.data || m.unknown_error() })
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
          m.advanced_error_reset_config({ error: resp.error.data || m.unknown_error() })
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
          m.advanced_error_update_ssh_key({ error: resp.error.data || m.unknown_error() })
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
            m.advanced_error_set_dev_mode({ error: resp.error.data || m.unknown_error() })
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
            m.advanced_error_set_dev_channel({ error: resp.error.data || m.unknown_error() })
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
              : m.advanced_error_loopback_disable({ error: resp.error.data || m.unknown_error() })
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

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={m.advanced_title()}
        description={m.advanced_description()}
      />

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

        {settings.developerMode && (
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
        )}

        <SettingsItem
          title={m.advanced_loopback_only_title()}
          description={m.advanced_loopback_only_description()}
        >
          <Checkbox
            checked={localLoopbackOnly}
            onChange={e => handleLoopbackOnlyModeChange(e.target.checked)}
          />
        </SettingsItem>

        {isOnDevice && settings.developerMode && (
          <div className="space-y-4">
            <SettingsItem
              title={m.advanced_ssh_access_title()}
              description={m.advanced_ssh_access_description()}
            />
            <div className="space-y-4">
              <TextAreaWithLabel
                label={m.advanced_ssh_public_key_label()}
                value={sshKey || ""}
                rows={3}
                onChange={e => setSSHKey(e.target.value)}
                placeholder={m.advanced_ssh_public_key_placeholder()}
              />
              <p className="text-xs text-slate-600 dark:text-slate-400">
                {m.advanced_ssh_default_user()}<strong>root</strong>.
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
          </div>
        )}

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
          <>
            <SettingsItem
              title={m.advanced_usb_emulation_title()}
              description={m.advanced_usb_emulation_description()}
            >
              <Button
                size="SM"
                theme="light"
                text={
                  usbEmulationEnabled ? m.advanced_disable_usb_emulation() : m.advanced_enable_usb_emulation()
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
                onClick={() => {
                  handleResetConfig();
                  window.location.reload();
                }}
              />
            </SettingsItem>
          </>
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
            <p>
              {m.advanced_loopback_warning_description()}
            </p>
            <p>
              {m.advanced_loopback_warning_before()}
            </p>
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
