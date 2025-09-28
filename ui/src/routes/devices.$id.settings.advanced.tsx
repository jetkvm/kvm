import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { GridCard } from "@components/Card";

import { Button } from "../components/Button";
import Checkbox from "../components/Checkbox";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { SettingsPageHeader } from "../components/SettingsPageheader";
import { TextAreaWithLabel } from "../components/TextArea";
import { useSettingsStore } from "../hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "../hooks/useJsonRpc";
import { isOnDevice } from "../main";
import notifications from "../notifications";

import { SettingsItem } from "./devices.$id.settings";

export default function SettingsAdvancedRoute() {
  const { send } = useJsonRpc();
  const { t } = useTranslation();
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
              t('Failed_to_set_USB_emulation_msg',{set:(enabled ? t('enable') : t('disable')),msg:resp.error.data || t('Unknown_error')})
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
            t('Failed_to_reset_configuration_msg',{msg:resp.error.data || t('Unknown_error')})
        );
        return;
      }
      notifications.success(t('Configuration_reset_to_default_successfully'));
    });
  }, [send]);

  const handleUpdateSSHKey = useCallback(() => {
    send("setSSHKeyState", { sshKey }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
            t('Failed_to_update_SSH_key_msg',{msg:resp.error.data || t('Unknown_error')})
        );
        return;
      }
      notifications.success(t('SSH_key_updated_successfully'));
    });
  }, [send, sshKey]);

  const handleDevModeChange = useCallback(
    (developerMode: boolean) => {
      send("setDevModeState", { enabled: developerMode }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            t('Failed_to_set_dev_mode_msg',{msg:resp.error.data || t('Unknown_error')})
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
            t('Failed_to_set_dev_channel_state_msg', {msg:resp.error.data || t('Unknown_error')})
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
              t('Failed_to_set_loopback-only_mode_msg',{state:enabled ? t('enable') : t('disable'),msg:resp.error.data || t('Unknown_error')})
          );
          return;
        }
        setLocalLoopbackOnly(enabled);
        if (enabled) {
          notifications.success(
            t('Loopback-only_mode_enabled_Restart_your_device_to_apply')
          );
        } else {
          notifications.success(
            t('Loopback-only_mode_enabled_Restart_your_device_to_apply'),
          );
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
        title={t('Advanced')}
        description={t('Access_additional_settings_for_troubleshooting_and_customization')}
      />

      <div className="space-y-4">
        <SettingsItem
          title={t('Dev_Channel_Updates')}
          description={t('Receive_early_updates_from_the_development_channel')}
        >
          <Checkbox
            checked={devChannel}
            onChange={e => {
              handleDevChannelChange(e.target.checked);
            }}
          />
        </SettingsItem>
        <SettingsItem
          title={t('Developer_Mode')}
          description={t('Enable_advanced_features_for_developers')}
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
                      {t('Developer_Mode_Enabled')}
                  </h3>
                  <div>
                    <ul className="list-disc space-y-1 pl-5 text-xs text-slate-700 dark:text-slate-300">
                      <li>{t('Security_is_weakened_while_active')}</li>
                      <li>{t('Only_use_if_you_understand_the_risks')}</li>
                    </ul>
                  </div>
                </div>

                <div className="text-xs text-slate-700 dark:text-slate-300">
                    {t('For_advanced_users_only_Not_for_production_use')}
                </div>
              </div>
            </div>
          </GridCard>
        )}

        <SettingsItem
          title={t('Loopback-Only_Mode')}
          description={t('Restrict_web_interface_access_to_localhost_only_127_0_0_1')}
        >
          <Checkbox
            checked={localLoopbackOnly}
            onChange={e => handleLoopbackOnlyModeChange(e.target.checked)}
          />
        </SettingsItem>

        {isOnDevice && settings.developerMode && (
          <div className="space-y-4">
            <SettingsItem
              title={t('SSH_Access')}
              description={t('Add_your_SSH_public_key_to_enable_secure_remote_access_to_the_device')}
            />
            <div className="space-y-4">
              <TextAreaWithLabel
                label={t('SSH_Public_Key')}
                value={sshKey || ""}
                rows={3}
                onChange={e => setSSHKey(e.target.value)}
                placeholder={t('Enter_your_SSH_public_key')}
              />
              <p className="text-xs text-slate-600 dark:text-slate-400">
                  {t('The_default_SSH_user_is')} <strong>root</strong>
              </p>
              <div className="flex items-center gap-x-2">
                <Button
                  size="SM"
                  theme="primary"
                  text={t('Update_SSH_Key')}
                  onClick={handleUpdateSSHKey}
                />
              </div>
            </div>
          </div>
        )}

        <SettingsItem
          title={t('Troubleshooting_Mode')}
          description={t('Diagnostic_tools_and_additional_controls_for_troubleshooting_and_development_purposes')}
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
              title={t('USB_Emulation')}
              description={t('Control_the_USB_emulation_state')}
            >
              <Button
                size="SM"
                theme="light"
                text={
                  usbEmulationEnabled ? t('Disable_USB_Emulation') : t('Enable_USB_Emulation')
                }
                onClick={() => handleUsbEmulationToggle(!usbEmulationEnabled)}
              />
            </SettingsItem>

            <SettingsItem
              title={t('Reset_Configuration')}
              description={t('Reset_configuration_to_default_This_will_log_you_out')}
            >
              <Button
                size="SM"
                theme="light"
                text={t('Reset_Config')}
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
        title={t('Enable_Loopback-Only_Mode')}
        description={
          <>
            <p>
                {t('WARNING_This_will_restrict_web_interface_access_to_localhost_127_0_0_1_only')}
            </p>
            <p>{t('Before_enabling_this_feature_make_sure_you_have_either')}</p>
            <ul className="list-disc space-y-1 pl-5 text-xs text-slate-700 dark:text-slate-300">
              <li>{t('SSH_access_configured_and_tested')}</li>
              <li>{t('Cloud_access_enabled_and_working')}</li>
            </ul>
          </>
        }
        variant="warning"
        confirmText={t('I_Understand_Enable_Anyway')}
        onConfirm={confirmLoopbackModeEnable}
      />
    </div>
  );
}
