import { useCallback, useEffect, useState } from "react";
import { useLoaderData, useNavigate, type LoaderFunction } from "react-router";
import { ShieldCheckIcon } from "@heroicons/react/24/outline";

import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { GridCard } from "@components/Card";
import { Button, LinkButton } from "@components/Button";
import { InputFieldWithLabel } from "@components/InputField";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SettingsSectionHeader } from "@components/SettingsSectionHeader";
import { TextAreaWithLabel } from "@components/TextArea";
import api from "@/api";
import notifications from "@/notifications";
import { DEVICE_API } from "@/ui.config";
import { isOnDevice } from "@/main";
import { m } from "@localizations/messages.js";

import { LocalDevice } from "./devices.$id";
import { CloudState } from "./adopt";

export interface TLSState {
  mode: "self-signed" | "custom" | "disabled";
  certificate?: string;
  privateKey?: string;
}

const loader: LoaderFunction = async () => {
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

  const { send } = useJsonRpc();

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
    send("getCloudState", {}, (resp: JsonRpcResponse) => {
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
    send("getTLSState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return console.error(resp.error);
      const tlsState = resp.result as TLSState;

      setTlsMode(tlsState.mode);
      if (tlsState.certificate) setTlsCert(tlsState.certificate);
      if (tlsState.privateKey) setTlsKey(tlsState.privateKey);
    });
  }, [send]);

  const deregisterDevice = () => {
    send("deregisterDevice", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.access_failed_deregister({ error: resp.error.data || m.unknown_error() }),
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
        notifications.error(m.access_no_device_id());
        return;
      }

      send("setCloudUrl", { apiUrl: cloudApiUrl, appUrl: cloudAppUrl }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            m.access_failed_update_cloud_url({ error: resp.error.data || m.unknown_error() }),
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

      send("setTLSState", { state }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            m.access_failed_update_tls({ error: resp.error.data || m.unknown_error() }),
          );
          return;
        }

        notifications.success(m.access_tls_updated());
      });
    }, [send]);

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

    send("getDeviceID", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return console.error(resp.error);
      setDeviceId(resp.result as string);
    });
  }, [send, getCloudState, getTLSState]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={m.access_title()}
        description={m.access_description()}
      />

      {loaderData?.authMode && (
        <>
          <div className="space-y-4">
            <SettingsSectionHeader
              title={m.access_local_title()}
              description={m.access_local_description()}
            />
            <>
              <SettingsItem
                title={m.access_https_mode_title()}
                badge="Experimental"
                description={m.access_https_description()}
              >
                <SelectMenuBasic
                  size="SM"
                  value={tlsMode}
                  onChange={e => handleTlsModeChange(e.target.value)}
                  disabled={tlsMode === "unknown"}
                  options={[
                    { value: "disabled", label: m.access_tls_disabled() },
                    { value: "self-signed", label: m.access_tls_self_signed() },
                    { value: "custom", label: m.access_tls_custom() },
                  ]}
                />
              </SettingsItem>

              {tlsMode === "custom" && (
                <div className="mt-4 space-y-4">
                  <div className="space-y-4">
                    <SettingsItem
                      title={m.access_tls_certificate_title()}
                      description={m.access_tls_certificate_description()}
                    />
                    <div className="space-y-4">
                      <TextAreaWithLabel
                        label={m.access_certificate_label()}
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
                          label={m.access_private_key_label()}
                          description={m.access_private_key_description()}
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
                      text={m.access_update_tls_settings()}
                      onClick={handleCustomTlsUpdate}
                    />
                  </div>
                </div>
              )}

              <SettingsItem
                title={m.access_authentication_mode_title()}
                description={loaderData.authMode === "password" ? m.access_auth_mode_password() : m.access_auth_mode_no_password()}
              >
                {loaderData.authMode === "password" ? (
                  <Button
                    size="SM"
                    theme="light"
                    text={m.access_disable_protection()}
                    onClick={() => {
                      navigateTo("./local-auth", { state: { init: "deletePassword" } });
                    }}
                  />
                ) : (
                  <Button
                    size="SM"
                    theme="light"
                    text={m.access_enable_password()}
                    onClick={() => {
                      navigateTo("./local-auth", { state: { init: "createPassword" } });
                    }}
                  />
                )}
              </SettingsItem>
            </>

            {loaderData.authMode === "password" && (
              <SettingsItem
                title={m.access_change_password_title()}
                description={m.access_change_password_description()}
              >
                <Button
                  size="SM"
                  theme="light"
                  text={m.access_change_password_button()}
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
          description={m.access_remote_description()}
        />

        <div className="space-y-4">
          {!isAdopted && (
            <>
              <SettingsItem
                title={m.access_cloud_provider_title()}
                description={m.access_cloud_provider_description()}
              >
                <SelectMenuBasic
                  size="SM"
                  value={selectedProvider}
                  onChange={e => handleProviderChange(e.target.value)}
                  options={[
                    { value: "jetkvm", label: m.access_provider_jetkvm() },
                    { value: "custom", label: m.access_provider_custom() },
                  ]}
                />
              </SettingsItem>

              {selectedProvider === "custom" && (
                <div className="mt-4 space-y-4">
                  <div className="flex items-end gap-x-2">
                    <InputFieldWithLabel
                      size="SM"
                      label={m.access_cloud_api_url_label()}
                      value={cloudApiUrl}
                      onChange={e => setCloudApiUrl(e.target.value)}
                      placeholder="https://api.example.com"
                    />
                  </div>
                  <div className="flex items-end gap-x-2">
                    <InputFieldWithLabel
                      size="SM"
                      label={m.access_cloud_app_url_label()}
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
                      {m.access_cloud_security_title()}
                    </h3>
                    <div>
                      <ul className="list-disc space-y-1 pl-5 text-xs text-slate-700 dark:text-slate-300">
                        <li>{m.access_security_encryption()}</li>
                        <li>{m.access_security_zero_trust()}</li>
                        <li>{m.access_security_oidc()}</li>
                        <li>{m.access_security_streams()}</li>
                      </ul>
                    </div>

                    <div className="text-xs text-slate-700 dark:text-slate-300">
                      {m.access_security_open_source()}{" "}
                      <a
                        href="https://github.com/jetkvm"
                        target="_blank"
                        rel="noopener noreferrer"
                        className="font-medium text-blue-600 hover:text-blue-800 dark:text-blue-500 dark:hover:text-blue-400"
                      >
                        {m.access_github_link()}
                      </a>
                      .
                    </div>
                  </div>
                  <hr className="block w-full border-slate-800/20 dark:border-slate-300/20" />

                  <div>
                    <LinkButton
                      to="https://jetkvm.com/docs/networking/remote-access"
                      size="SM"
                      theme="light"
                      text={m.access_learn_security()}
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
                text={m.access_adopt_kvm()}
              />
            </div>
          ) : (
            <div>
              <div className="space-y-2">
                <p className="text-sm text-slate-600 dark:text-slate-300">
                  {m.access_adopted_message()}
                </p>
                <div>
                  <Button
                    size="SM"
                    theme="light"
                    text={m.access_deregister()}
                    className="text-red-600"
                    onClick={() => {
                      if (deviceId) {
                        if (
                          window.confirm(
                            m.access_confirm_deregister(),
                          )
                        ) {
                          deregisterDevice();
                        }
                      } else {
                        notifications.error(m.access_no_device_id());
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
