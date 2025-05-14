import { useLoaderData, useNavigate } from "react-router-dom";
import { ShieldCheckIcon } from "@heroicons/react/24/outline";
import { useCallback, useEffect, useState } from "react";

import api from "@/api";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { GridCard } from "@/components/Card";
import { Button, LinkButton } from "@/components/Button";
import { InputFieldWithLabel } from "@/components/InputField";
import { SelectMenuBasic } from "@/components/SelectMenuBasic";
import { SettingsSectionHeader } from "@/components/SettingsSectionHeader";
import { useDeviceUiNavigation } from "@/hooks/useAppNavigation";
import notifications from "@/notifications";
import { DEVICE_API } from "@/ui.config";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { isOnDevice } from "@/main";
import { TextAreaWithLabel } from "@components/TextArea";

import { LocalDevice } from "./devices.$id";
import { SettingsItem } from "./devices.$id.settings";
import { CloudState } from "./adopt";

export interface TLSState {
  mode: "self-signed" | "custom" | "disabled";
  certificate?: string;
  privateKey?: string;
}

const loader = async () => {
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
  const navigate = useNavigate();

  const [send] = useJsonRpc();

  const [isAdopted, setAdopted] = useState(false);
  const [deviceId, setDeviceId] = useState<string | null>(null);
  const [cloudApiUrl, setCloudApiUrl] = useState("");
  const [cloudAppUrl, setCloudAppUrl] = useState("");

  // Use a simple string identifier for the selected provider
  const [selectedProvider, setSelectedProvider] = useState<string>("jetkvm");
  const [tlsMode, setTlsMode] = useState<string>("unknown");
  const [tlsCert, setTlsCert] = useState<string>("");
  const [tlsKey, setTlsKey] = useState<string>("");

  const getCloudState = useCallback(() => {
    send("getCloudState", {}, resp => {
      if ("error" in resp) return console.error(resp.error);
      const cloudState = resp.result as CloudState;
      setAdopted(cloudState.connected);
      setCloudApiUrl(cloudState.url);

      if (cloudState.appUrl) setCloudAppUrl(cloudState.appUrl);

      // Find if the API URL matches any of our predefined providers
      const isAPIJetKVMProd = cloudState.url === "https://api.jetkvm.com";
      const isAppJetKVMProd = cloudState.appUrl === "https://app.jetkvm.com";

      if (isAPIJetKVMProd && isAppJetKVMProd) {
        setSelectedProvider("jetkvm");
      } else {
        setSelectedProvider("custom");
      }
    });
  }, [send]);

  const getTLSState = useCallback(() => {
    send("getTLSState", {}, resp => {
      if ("error" in resp) return console.error(resp.error);
      const tlsState = resp.result as TLSState;

      setTlsMode(tlsState.mode);
      if (tlsState.certificate) setTlsCert(tlsState.certificate);
      if (tlsState.privateKey) setTlsKey(tlsState.privateKey);
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
      // In cloud mode, we need to navigate to the device overview page, as we don't a connection anymore
      if (!isOnDevice) navigate("/");
      return;
    });
  };

  const onCloudAdoptClick = useCallback(
    (cloudApiUrl: string, cloudAppUrl: string) => {
      if (!deviceId) {
        notifications.error("No device ID available");
        return;
      }

      send("setCloudUrl", { apiUrl: cloudApiUrl, appUrl: cloudAppUrl }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to update cloud URL: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }

        const returnTo = new URL(window.location.href);
        returnTo.pathname = "/adopt";
        returnTo.search = "";
        returnTo.hash = "";
        window.location.href =
          cloudAppUrl +
          "/signup?deviceId=" +
          deviceId +
          `&returnTo=${returnTo.toString()}`;
      });
    },
    [deviceId, send],
  );

  // Handle provider selection change
  const handleProviderChange = (value: string) => {
    setSelectedProvider(value);

    // If selecting a predefined provider, update both URLs
    if (value === "jetkvm") {
      setCloudApiUrl("https://api.jetkvm.com");
      setCloudAppUrl("https://app.jetkvm.com");
    } else {
      if (cloudApiUrl || cloudAppUrl) return;
      setCloudApiUrl("");
      setCloudAppUrl("");
    }
  };

  // Function to update TLS state - accepts a mode parameter
  const updateTlsState = useCallback(
    (mode: string, cert?: string, key?: string) => {
      const state = { mode } as TLSState;
      if (cert && key) {
        state.certificate = cert;
        state.privateKey = key;
      }

      send("setTLSState", { state }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to update TLS settings: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }

        notifications.success("TLS settings updated successfully");
      });
    },
    [send],
  );

  // Handle TLS mode change
  const handleTlsModeChange = (value: string) => {
    setTlsMode(value);

    // For "disabled" and "self-signed" modes, immediately apply the settings
    if (value !== "custom") {
      updateTlsState(value);
    }
  };

  const handleTlsCertChange = (value: string) => {
    setTlsCert(value);
  };

  const handleTlsKeyChange = (value: string) => {
    setTlsKey(value);
  };

  // Update the custom TLS settings button click handler
  const handleCustomTlsUpdate = () => {
    updateTlsState(tlsMode, tlsCert, tlsKey);
  };

  // Fetch device ID and cloud state on component mount
  useEffect(() => {
    getCloudState();
    getTLSState();

    send("getDeviceID", {}, async resp => {
      if ("error" in resp) return console.error(resp.error);
      setDeviceId(resp.result as string);
    });
  }, [send, getCloudState, getTLSState]);

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
            <>
              <SettingsItem
                title="HTTPS Mode"
                badge="Experimental"
                description="Configure secure HTTPS access to your device"
              >
                <SelectMenuBasic
                  size="SM"
                  value={tlsMode}
                  onChange={e => handleTlsModeChange(e.target.value)}
                  disabled={tlsMode === "unknown"}
                  options={[
                    { value: "disabled", label: "Disabled" },
                    { value: "self-signed", label: "Self-signed" },
                    { value: "custom", label: "Custom" },
                  ]}
                />
              </SettingsItem>

              {tlsMode === "custom" && (
                <div className="mt-4 space-y-4">
                  <div className="space-y-4">
                    <SettingsItem
                      title="TLS Certificate"
                      description="Paste your TLS certificate below. For certificate chains, include the entire chain (leaf, intermediate, and root certificates)."
                    />
                    <div className="space-y-4">
                      <TextAreaWithLabel
                        label="Certificate"
                        rows={3}
                        placeholder={
                          "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----"
                        }
                        value={tlsCert}
                        onChange={e => handleTlsCertChange(e.target.value)}
                      />
                    </div>

                    <div className="space-y-4">
                      <div className="space-y-4">
                        <TextAreaWithLabel
                          label="Private Key"
                          description="For security reasons, it will not be displayed after saving."
                          rows={3}
                          placeholder={
                            "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----"
                          }
                          value={tlsKey}
                          onChange={e => handleTlsKeyChange(e.target.value)}
                        />
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-x-2">
                    <Button
                      size="SM"
                      theme="primary"
                      text="Update TLS Settings"
                      onClick={handleCustomTlsUpdate}
                    />
                  </div>
                </div>
              )}

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
            </>

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
                  value={selectedProvider}
                  onChange={e => handleProviderChange(e.target.value)}
                  options={[
                    { value: "jetkvm", label: "JetKVM Cloud" },
                    { value: "custom", label: "Custom" },
                  ]}
                />
              </SettingsItem>

              {selectedProvider === "custom" && (
                <div className="mt-4 space-y-4">
                  <div className="flex items-end gap-x-2">
                    <InputFieldWithLabel
                      size="SM"
                      label="Cloud API URL"
                      value={cloudApiUrl}
                      onChange={e => setCloudApiUrl(e.target.value)}
                      placeholder="https://api.example.com"
                    />
                  </div>
                  <div className="flex items-end gap-x-2">
                    <InputFieldWithLabel
                      size="SM"
                      label="Cloud App URL"
                      value={cloudAppUrl}
                      onChange={e => setCloudAppUrl(e.target.value)}
                      placeholder="https://app.example.com"
                    />
                  </div>
                </div>
              )}
            </>
          )}

          {/* Show security info for JetKVM Cloud */}
          {selectedProvider === "jetkvm" && (
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
                onClick={() => onCloudAdoptClick(cloudApiUrl, cloudAppUrl)}
                size="SM"
                theme="primary"
                text="Adopt KVM to Cloud"
              />
            </div>
          ) : (
            <div>
              <div className="space-y-2">
                <p className="text-sm text-slate-600 dark:text-slate-300">
                  Your device is adopted to the Cloud
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

SettingsAccessIndexRoute.loader = loader;