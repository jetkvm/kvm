import { SectionHeader } from "../components/SectionHeader";

import { SettingsItem } from "./devices.$id.settings";
import { useCallback, useState } from "react";
import { useEffect } from "react";
import { SystemVersionInfo } from "./devices.$id.settings.general.update";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { Button, LinkButton } from "../components/Button";
import { GridCard } from "../components/Card";
import { ShieldCheckIcon } from "@heroicons/react/24/outline";
import { CLOUD_APP } from "../ui.config";
import notifications from "../notifications";
import { isOnDevice } from "../main";
import Checkbox from "../components/Checkbox";
import { useDeviceUiNavigation } from "../hooks/useAppNavigation";

export default function SettingsGeneralRoute() {
  const [send] = useJsonRpc();
  const { navigateTo } = useDeviceUiNavigation();

  const [devChannel, setDevChannel] = useState(false);
  const [autoUpdate, setAutoUpdate] = useState(true);
  const [deviceId, setDeviceId] = useState<string | null>(null);
  const [isAdopted, setAdopted] = useState(false);
  const [currentVersions, setCurrentVersions] = useState<{
    appVersion: string;
    systemVersion: string;
  } | null>(null);

  const getCloudState = useCallback(() => {
    send("getCloudState", {}, resp => {
      if ("error" in resp) return console.error(resp.error);
      const cloudState = resp.result as { connected: boolean };
      setAdopted(cloudState.connected);
    });
  }, [send]);

  const deregisterDevice = async () => {
    send("deregisterDevice", {}, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to de-register device: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      getCloudState();
      return;
    });
  };

  const getCurrentVersions = useCallback(() => {
    send("getUpdateStatus", {}, resp => {
      if ("error" in resp) return;
      const result = resp.result as SystemVersionInfo;
      setCurrentVersions({
        appVersion: result.local.appVersion,
        systemVersion: result.local.systemVersion,
      });
    });
  }, [send]);

  useEffect(() => {
    getCurrentVersions();
    getCloudState();
    send("getDeviceID", {}, async resp => {
      if ("error" in resp) return console.error(resp.error);
      setDeviceId(resp.result as string);
    });

    send("getAutoUpdateState", {}, resp => {
      if ("error" in resp) return;
      setAutoUpdate(resp.result as boolean);
    });

    send("getDevChannelState", {}, resp => {
      if ("error" in resp) return;
      setDevChannel(resp.result as boolean);
    });
  }, [getCurrentVersions, getCloudState, send]);

  const handleAutoUpdateChange = (enabled: boolean) => {
    send("setAutoUpdateState", { enabled }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set auto-update: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setAutoUpdate(enabled);
    });
  };

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
      <SectionHeader
        title="General"
        description="Configure device settings and update preferences"
      />

      <div className="space-y-4">
        <div className="space-y-4 pb-2">
          <div className="mt-2 flex items-center justify-between gap-x-2">
            <SettingsItem
              title="Check for Updates"
              description={
                currentVersions ? (
                  <>
                    App: {currentVersions.appVersion}
                    <br />
                    System: {currentVersions.systemVersion}
                  </>
                ) : (
                  <>
                    App: Loading...
                    <br />
                    System: Loading...
                  </>
                )
              }
            />
            <div>
              <Button
                size="SM"
                theme="light"
                text="Check for Updates"
                onClick={() => navigateTo("./update")}
              />
            </div>
          </div>
          <div className="space-y-4">
            <SettingsItem
              title="Auto Update"
              description="Automatically update the device to the latest version"
            >
              <Checkbox
                checked={autoUpdate}
                onChange={e => {
                  handleAutoUpdateChange(e.target.checked);
                }}
              />
            </SettingsItem>
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
          </div>
        </div>

        {isOnDevice && (
          <>
            <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />

            <div className="space-y-4">
              <SettingsItem
                title="JetKVM Cloud"
                description="Connect your device to the cloud for secure remote access and management"
              />

              <GridCard>
                <div className="flex items-start gap-x-4 p-4">
                  <ShieldCheckIcon className="mt-1 h-8 w-8 shrink-0 text-blue-600 dark:text-blue-500" />
                  <div className="space-y-3">
                    <div className="space-y-2">
                      <h3 className="text-base font-bold text-slate-900 dark:text-white">
                        Cloud Security
                      </h3>
                      <div>
                        <ul className="list-disc space-y-1 pl-5 text-xs text-slate-700 dark:text-slate-300">
                          <li>End-to-end encryption using WebRTC (DTLS and SRTP)</li>
                          <li>Zero Trust security model</li>
                          <li>OIDC (OpenID Connect) authentication</li>
                          <li>All streams encrypted in transit</li>
                        </ul>
                      </div>

                      <div className="text-xs text-slate-700 dark:text-slate-300">
                        All cloud components are open-source and available on{" "}
                        <a
                          href="https://github.com/jetkvm"
                          target="_blank"
                          rel="noopener noreferrer"
                          className="font-medium text-blue-600 hover:text-blue-800 dark:text-blue-500 dark:hover:text-blue-400"
                        >
                          GitHub
                        </a>
                        .
                      </div>
                    </div>
                    <hr className="block w-full dark:border-slate-600" />

                    <div>
                      <LinkButton
                        to="https://jetkvm.com/docs/networking/remote-access"
                        size="SM"
                        theme="light"
                        text="Learn about our cloud security"
                      />
                    </div>
                  </div>
                </div>
              </GridCard>

              {!isAdopted ? (
                <div>
                  <LinkButton
                    to={
                      CLOUD_APP +
                      "/signup?deviceId=" +
                      deviceId +
                      `&returnTo=${location.href}adopt`
                    }
                    size="SM"
                    theme="primary"
                    text="Adopt KVM to Cloud account"
                  />
                </div>
              ) : (
                <div>
                  <div className="space-y-2">
                    <p className="text-sm text-slate-600 dark:text-slate-300">
                      Your device is adopted to JetKVM Cloud
                    </p>
                    <div>
                      <Button
                        size="MD"
                        theme="light"
                        text="De-register from Cloud"
                        className="text-red-600"
                        onClick={() => {
                          if (deviceId) {
                            if (
                              window.confirm(
                                "Are you sure you want to de-register this device?",
                              )
                            ) {
                              deregisterDevice();
                            }
                          } else {
                            notifications.error("No device ID available");
                          }
                        }}
                      />
                    </div>
                  </div>
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
