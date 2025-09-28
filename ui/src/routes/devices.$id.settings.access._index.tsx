import { useLoaderData, useNavigate } from "react-router";
import type { LoaderFunction } from "react-router";
import { ShieldCheckIcon } from "@heroicons/react/24/outline";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

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
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
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
  const { t } = useTranslation();

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
        notifications.error(t('Failed_to_de-register_device_msg',{ msg:resp.error.data || t('Unknown_error') }));
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
        notifications.error(t('No_device_ID_available'));
        return;
      }

      send("setCloudUrl", { apiUrl: cloudApiUrl, appUrl: cloudAppUrl }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            t('Failed_to_update_cloud_URL_msg',{msg:resp.error.data || t('Unknown_error')}),
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
            t('Failed_to_update_TLS_settings_msg',{msg:resp.error.data || t('Unknown_error')}),
          );
          return;
        }

        notifications.success(t('TLS_settings_updated_successfully'));
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
        title={t('Access')}
        description={t('Manage_the_Access_Control_of_the_device')}
      />

      {loaderData?.authMode && (
        <>
          <div className="space-y-4">
            <SettingsSectionHeader
              title={t('Local')}
              description={t('Manage_the_mode_of_local_access_to_the_device')}
            />
            <>
              <SettingsItem
                title={t('HTTPS_Mode')}
                badge={t('Experimental')}
                description={t('Configure_secure_HTTPS_access_to_your_device')}
              >
                <SelectMenuBasic
                  size="SM"
                  value={tlsMode}
                  onChange={e => handleTlsModeChange(e.target.value)}
                  disabled={tlsMode === "unknown"}
                  options={[
                    { value: "disabled", label: t('Disabled') },
                    { value: "self-signed", label: t('Self-signed') },
                    { value: "custom", label: t('Custom') },
                  ]}
                />
              </SettingsItem>

              {tlsMode === "custom" && (
                <div className="mt-4 space-y-4">
                  <div className="space-y-4">
                    <SettingsItem
                      title={t('TLS_Certificate')}
                      description={t('Paste_your_TLS_certificate_below_For_certificate_chains')}
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
                          label={t('Private_Key')}
                          description={t('For_security_reasons_it_will_not_be_displayed_after_saving')}
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
                      text={t('Update_TLS_Settings')}
                      onClick={handleCustomTlsUpdate}
                    />
                  </div>
                </div>
              )}

              <SettingsItem
                title={t('Authentication_Mode')}
                description={t('Current_mode_state',{state:loaderData.authMode === "password" ? t('Password_protected') : t('No_password')})}
              >
                {loaderData.authMode === "password" ? (
                  <Button
                    size="SM"
                    theme="light"
                    text={t('Disable_Protection')}
                    onClick={() => {
                      navigateTo("./local-auth", { state: { init: "deletePassword" } });
                    }}
                  />
                ) : (
                  <Button
                    size="SM"
                    theme="light"
                    text={t('Enable_Password')}
                    onClick={() => {
                      navigateTo("./local-auth", { state: { init: "createPassword" } });
                    }}
                  />
                )}
              </SettingsItem>
            </>

            {loaderData.authMode === "password" && (
              <SettingsItem
                title={t('Change_Password')}
                description={t('Update_your_device_access_password')}
              >
                <Button
                  size="SM"
                  theme="light"
                  text={t('Change_Password')}
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
          title={t('Remote')}
          description={t('Manage_the_mode_of_Remote_access_to_the_device')}
        />

        <div className="space-y-4">
          {!isAdopted && (
            <>
              <SettingsItem
                title={t('Cloud_Provider')}
                description={t('Select_the_cloud_provider_for_your_device')}
              >
                <SelectMenuBasic
                  size="SM"
                  value={selectedProvider}
                  onChange={e => handleProviderChange(e.target.value)}
                  options={[
                    { value: "jetkvm", label: t('JetKVM_Cloud') },
                    { value: "custom", label: t('Custom') },
                  ]}
                />
              </SettingsItem>

              {selectedProvider === "custom" && (
                <div className="mt-4 space-y-4">
                  <div className="flex items-end gap-x-2">
                    <InputFieldWithLabel
                      size="SM"
                      label={t('Cloud_API_URL')}
                      value={cloudApiUrl}
                      onChange={e => setCloudApiUrl(e.target.value)}
                      placeholder="https://api.example.com"
                    />
                  </div>
                  <div className="flex items-end gap-x-2">
                    <InputFieldWithLabel
                      size="SM"
                      label={t('Cloud_App_URL')}
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
                        {t('Cloud_Security')}
                    </h3>
                    <div>
                      <ul className="list-disc space-y-1 pl-5 text-xs text-slate-700 dark:text-slate-300">
                        <li>{t('End-to-end_encryption using_WebRTC_DTLS_and_SRTP')}</li>
                        <li>{t('Zero_Trust_security_model')}</li>
                        <li>{t('OIDC_OpenID_Connect_authentication')}</li>
                        <li>{t('All_streams_encrypted_in_transit')}</li>
                      </ul>
                    </div>

                    <div className="text-xs text-slate-700 dark:text-slate-300">
                        {t('All_cloud_components_are_open-source_and_available_on')}
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
                  <hr className="block w-full border-slate-800/20 dark:border-slate-300/20" />

                  <div>
                    <LinkButton
                      to="https://jetkvm.com/docs/networking/remote-access"
                      size="SM"
                      theme="light"
                      text={t('Learn_about_our_cloud_security')}
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
                text={t('Adopt_KVM_to_Cloud')}
              />
            </div>
          ) : (
            <div>
              <div className="space-y-2">
                <p className="text-sm text-slate-600 dark:text-slate-300">
                    {t('Your_device_is_adopted_to_the_Cloud')}
                </p>
                <div>
                  <Button
                    size="SM"
                    theme="light"
                    text={t('De-register_from_Cloud')}
                    className="text-red-600"
                    onClick={() => {
                      if (deviceId) {
                        if (
                          window.confirm(
                            t('Are_you_sure_you_want_to_de-register_this_device'),
                          )
                        ) {
                          deregisterDevice();
                        }
                      } else {
                        notifications.error(t('No_device_ID_available'));
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
