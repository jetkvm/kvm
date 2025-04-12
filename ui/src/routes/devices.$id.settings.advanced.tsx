
import { useCallback, useState, useEffect } from "react";

import { GridCard } from "@components/Card";

import { SettingsPageHeader } from "../components/SettingsPageheader";
import Checkbox from "../components/Checkbox";
import { useJsonRpc } from "../hooks/useJsonRpc";
import notifications from "../notifications";
import { TextAreaWithLabel } from "../components/TextArea";
import { isOnDevice } from "../main";
import { Button } from "../components/Button";
import { useSettingsStore } from "../hooks/stores";


import { SettingsItem } from "./devices.$id.settings";

export default function SettingsAdvancedRoute() {
  const [send] = useJsonRpc();

  const [sshKey, setSSHKey] = useState<string>("");
  const setDeveloperMode = useSettingsStore(state => state.setDeveloperMode);
  const [devChannel, setDevChannel] = useState(false);
  const [usbEmulationEnabled, setUsbEmulationEnabled] = useState(false);

  const settings = useSettingsStore();

  useEffect(() => {
    send("getDevModeState", {}, resp => {
      if ("error" in resp) return;
      const result = resp.result as { enabled: boolean };
      setDeveloperMode(result.enabled);
    });

    send("getSSHKeyState", {}, resp => {
      if ("error" in resp) return;
      setSSHKey(resp.result as string);
    });

    send("getUsbEmulationState", {}, resp => {
      if ("error" in resp) return;
      setUsbEmulationEnabled(resp.result as boolean);
    });

    send("getDevChannelState", {}, resp => {
      if ("error" in resp) return;
      setDevChannel(resp.result as boolean);
    });
  }, [send, setDeveloperMode]);

  const getUsbEmulationState = useCallback(() => {
    send("getUsbEmulationState", {}, resp => {
      if ("error" in resp) return;
      setUsbEmulationEnabled(resp.result as boolean);
    });
  }, [send]);

  const handleUsbEmulationToggle = useCallback(
    (enabled: boolean) => {
      send("setUsbEmulationState", { enabled: enabled }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to ${enabled ? "enable" : "disable"} USB emulation: ${resp.error.data || "Unknown error"}`,
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
    send("resetConfig", {}, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to reset configuration: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("Configuration reset to default successfully");
    });
  }, [send]);

  const handleUpdateSSHKey = useCallback(() => {
    send("setSSHKeyState", { sshKey }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to update SSH key: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("SSH key updated successfully");
    });
  }, [send, sshKey]);

  const handleDevModeChange = useCallback(
    (developerMode: boolean) => {
      send("setDevModeState", { enabled: developerMode }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to set dev mode: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        setDeveloperMode(developerMode);
      });
    },
    [send, setDeveloperMode],
  );

  const handleDevChannelChange = (enabled: boolean) => {
    send("setDevChannelState", { enabled }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set dev channel state: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setDevChannel(enabled);
    });
  };

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

        {settings.developerMode && (
          <GridCard>
            <div className="flex select-none items-start gap-x-4 p-4">
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
        )}

        {isOnDevice && settings.developerMode && (
          <div className="space-y-4">
            <SettingsItem
              title="SSH Access"
              description="Add your SSH public key to enable secure remote access to the device"
            />
            <div className="space-y-4">
              <TextAreaWithLabel
                label="SSH Public Key"
                value={sshKey || ""}
                rows={3}
                onChange={e => setSSHKey(e.target.value)}
                placeholder="Enter your SSH public key"
              />
              <p className="text-xs text-slate-600 dark:text-slate-400">
                The default SSH user is <strong>root</strong>.
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
          </div>
        )}

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
          <>
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
                onClick={() => {
                  handleResetConfig();
                  window.location.reload();
                }}
              />
            </SettingsItem>
          </>
        )}
      </div>
    </div>
  );
}
