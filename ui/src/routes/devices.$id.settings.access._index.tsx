import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SettingsItem } from "./devices.$id.settings";
import { useLoaderData } from "react-router-dom";
import { Button, LinkButton } from "../components/Button";
import { CLOUD_APP, DEVICE_API } from "../ui.config";
import api from "../api";
import { LocalDevice } from "./devices.$id";
import { useDeviceUiNavigation } from "../hooks/useAppNavigation";
import { GridCard } from "../components/Card";
import { ShieldCheckIcon } from "@heroicons/react/24/outline";
import notifications from "../notifications";
import { useCallback, useEffect, useState } from "react";
import { useJsonRpc } from "../hooks/useJsonRpc";
import { InputFieldWithLabel } from "../components/InputField";
import { SelectMenuBasic } from "../components/SelectMenuBasic";
import { SettingsSectionHeader } from "../components/SettingsSectionHeader";
import { isOnDevice } from "../main";

export const loader = async () => {
  if (isOnDevice) {
    const status = await api
      .GET(`${DEVICE_API}/device`)
      .then(res => res.json() as Promise<LocalDevice>);
    return status;
  }
  return null;
};

export default function SettingsAccessIndexRoute() {
  const loaderData = useLoaderData() as LocalDevice | null;

  const { navigateTo } = useDeviceUiNavigation();

  const [send] = useJsonRpc();

  const [isAdopted, setAdopted] = useState(false);
  const [deviceId, setDeviceId] = useState<string | null>(null);
  const [cloudUrl, setCloudUrl] = useState("");
  const [cloudProviders, setCloudProviders] = useState<
    { value: string; label: string }[] | null
  >([{ value: "https://api.jetkvm.com", label: "JetKVM Cloud" }]);

  // The default value is just there so it doesn't flicker while we fetch the default Cloud URL and available providers
  const [selectedUrlOption, setSelectedUrlOption] = useState<string>(
    "https://api.jetkvm.com",
  );

  const [defaultCloudUrl, setDefaultCloudUrl] = useState<string>("");

  const syncCloudUrl = useCallback(() => {
    send("getCloudUrl", {}, resp => {
      if ("error" in resp) return;
      const url = resp.result as string;
      setCloudUrl(url);
      // Check if the URL matches any predefined option
      if (cloudProviders?.some(provider => provider.value === url)) {
        setSelectedUrlOption(url);
      } else {
        setSelectedUrlOption("custom");
        // setCustomCloudUrl(url);
      }
    });
  }, [cloudProviders, send]);

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

  const onCloudAdoptClick = useCallback(
    (url: string) => {
      if (!deviceId) {
        notifications.error("No device ID available");
        return;
      }

      send("setCloudUrl", { url }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to update cloud URL: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        syncCloudUrl();
        notifications.success("Cloud URL updated successfully");

        const returnTo = new URL(window.location.href);
        returnTo.pathname = "/adopt";
        returnTo.search = "";
        returnTo.hash = "";
        window.location.href =
          CLOUD_APP + "/signup?deviceId=" + deviceId + `&returnTo=${returnTo.toString()}`;
      });
    },
    [deviceId, syncCloudUrl, send],
  );

  useEffect(() => {
    if (!defaultCloudUrl) return;
    setSelectedUrlOption(defaultCloudUrl);
    setCloudProviders([
      { value: defaultCloudUrl, label: "JetKVM Cloud" },
      { value: "custom", label: "Custom" },
    ]);
  }, [defaultCloudUrl]);

  useEffect(() => {
    getCloudState();

    send("getDeviceID", {}, async resp => {
      if ("error" in resp) return console.error(resp.error);
      setDeviceId(resp.result as string);
    });
  }, [send, getCloudState]);

  useEffect(() => {
    send("getDefaultCloudUrl", {}, resp => {
      if ("error" in resp) return console.error(resp.error);
      setDefaultCloudUrl(resp.result as string);
    });
  }, [cloudProviders, syncCloudUrl, send]);

  useEffect(() => {
    if (!cloudProviders?.length) return;
    syncCloudUrl();
  }, [cloudProviders, syncCloudUrl]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Access"
        description="Manage the Access Control of the device"
      />

      {loaderData?.authMode && (
        <>
          <div className="space-y-4">
            <SettingsSectionHeader
              title="Local"
              description="Manage the mode of local access to the device"
            />
            <SettingsItem
              title="Authentication Mode"
              description={`Current mode: ${loaderData.authMode === "password" ? "Password protected" : "No password"}`}
            >
              {loaderData.authMode === "password" ? (
                <Button
                  size="SM"
                  theme="light"
                  text="Disable Protection"
                  onClick={() => {
                    navigateTo("./local-auth", { state: { init: "deletePassword" } });
                  }}
                />
              ) : (
                <Button
                  size="SM"
                  theme="light"
                  text="Enable Password"
                  onClick={() => {
                    navigateTo("./local-auth", { state: { init: "createPassword" } });
                  }}
                />
              )}
            </SettingsItem>

            {loaderData.authMode === "password" && (
              <SettingsItem
                title="Change Password"
                description="Update your device access password"
              >
                <Button
                  size="SM"
                  theme="light"
                  text="Change Password"
                  onClick={() => {
                    navigateTo("./local-auth", { state: { init: "updatePassword" } });
                  }}
                />
              </SettingsItem>
            )}
          </div>
          <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />
        </>
      )}

      <div className="space-y-4">
        <SettingsSectionHeader
          title="Remote"
          description="Manage the mode of Remote access to the device"
        />

        <div className="space-y-4">
          {!isAdopted && (
            <>
              <SettingsItem
                title="Cloud Provider"
                description="Select the cloud provider for your device"
              >
                <SelectMenuBasic
                  size="SM"
                  value={selectedUrlOption}
                  onChange={e => {
                    const value = e.target.value;
                    setSelectedUrlOption(value);
                  }}
                  options={cloudProviders ?? []}
                />
              </SettingsItem>

              {selectedUrlOption === "custom" && (
                <div className="mt-4 flex items-end gap-x-2 space-y-4">
                  <InputFieldWithLabel
                    size="SM"
                    label="Custom Cloud URL"
                    value={cloudUrl}
                    onChange={e => setCloudUrl(e.target.value)}
                    placeholder="https://api.example.com"
                  />
                </div>
              )}
            </>
          )}

          {/*
                We do the harcoding here to avoid flickering when the default Cloud URL being fetched.
                I've tried to avoid harcoding api.jetkvm.com, but it's the only reasonable way I could think of to avoid flickering for now.
              */}
          {selectedUrlOption === (defaultCloudUrl || "https://api.jetkvm.com") && (
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
          )}

          {!isAdopted ? (
            <div className="flex items-end gap-x-2">
              <Button
                onClick={() => onCloudAdoptClick(cloudUrl)}
                size="SM"
                theme="primary"
                text="Adopt KVM to Cloud"
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
                    size="SM"
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
      </div>
    </div>
  );
}
