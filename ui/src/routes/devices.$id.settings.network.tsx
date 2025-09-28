import { useCallback, useEffect, useRef, useState } from "react";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import { LuEthernetPort } from "react-icons/lu";
import { useTranslation } from "react-i18next";

import {
  IPv4Mode,
  IPv6Mode,
  LLDPMode,
  mDNSMode,
  NetworkSettings,
  NetworkState,
  TimeSyncMode,
  useNetworkStateStore,
} from "@/hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import InputField, { InputFieldWithLabel } from "@components/InputField";
import { SelectMenuBasic } from "@/components/SelectMenuBasic";
import { SettingsPageHeader } from "@/components/SettingsPageheader";
import Fieldset from "@/components/Fieldset";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import notifications from "@/notifications";

import Ipv6NetworkCard from "../components/Ipv6NetworkCard";
import EmptyCard from "../components/EmptyCard";
import AutoHeight from "../components/AutoHeight";
import DhcpLeaseCard from "../components/DhcpLeaseCard";

import { SettingsItem } from "./devices.$id.settings";

dayjs.extend(relativeTime);

const defaultNetworkSettings: NetworkSettings = {
  hostname: "",
  http_proxy: "",
  domain: "",
  ipv4_mode: "unknown",
  ipv6_mode: "unknown",
  lldp_mode: "unknown",
  lldp_tx_tlvs: [],
  mdns_mode: "unknown",
  time_sync_mode: "unknown",
};

export function LifeTimeLabel({ lifetime }: { lifetime: string }) {
  const { t } = useTranslation();
  const [remaining, setRemaining] = useState<string | null>(null);

  useEffect(() => {
    setRemaining(dayjs(lifetime).fromNow());

    const interval = setInterval(() => {
      setRemaining(dayjs(lifetime).fromNow());
    }, 1000 * 30);
    return () => clearInterval(interval);
  }, [lifetime]);

  if (lifetime == "") {
    return <strong>{t('N/A')}</strong>;
  }

  return (
    <>
      <span className="text-sm font-medium">{remaining && <> {remaining}</>}</span>
      <span className="text-xs text-slate-700 dark:text-slate-300">
        {" "}
        ({dayjs(lifetime).format("YYYY-MM-DD HH:mm")})
      </span>
    </>
  );
}

export default function SettingsNetworkRoute() {
  const { send } = useJsonRpc();
  const { t } = useTranslation();
  const [networkState, setNetworkState] = useNetworkStateStore(state => [
    state,
    state.setNetworkState,
  ]);

  const [networkSettings, setNetworkSettings] =
    useState<NetworkSettings>(defaultNetworkSettings);

  // We use this to determine whether the settings have changed
  const firstNetworkSettings = useRef<NetworkSettings | undefined>(undefined);

  const [networkSettingsLoaded, setNetworkSettingsLoaded] = useState(false);

  const [customDomain, setCustomDomain] = useState<string>("");
  const [selectedDomainOption, setSelectedDomainOption] = useState<string>("dhcp");

  useEffect(() => {
    if (networkSettings.domain && networkSettingsLoaded) {
      // Check if the domain is one of the predefined options
      const predefinedOptions = ["dhcp", "local"];
      if (predefinedOptions.includes(networkSettings.domain)) {
        setSelectedDomainOption(networkSettings.domain);
      } else {
        setSelectedDomainOption("custom");
        setCustomDomain(networkSettings.domain);
      }
    }
  }, [networkSettings.domain, networkSettingsLoaded]);

  const getNetworkSettings = useCallback(() => {
    setNetworkSettingsLoaded(false);
    send("getNetworkSettings", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const networkSettings = resp.result as NetworkSettings;
      console.debug("Network settings: ", networkSettings);
      setNetworkSettings(networkSettings);

      if (!firstNetworkSettings.current) {
        firstNetworkSettings.current = networkSettings;
      }
      setNetworkSettingsLoaded(true);
    });
  }, [send]);

  const getNetworkState = useCallback(() => {
    send("getNetworkState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const networkState = resp.result as NetworkState;
      console.debug("Network state:", networkState);
      setNetworkState(networkState);
    });
  }, [send, setNetworkState]);

  const setNetworkSettingsRemote = useCallback(
    (settings: NetworkSettings) => {
      setNetworkSettingsLoaded(false);
      send("setNetworkSettings", { settings }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            t('Failed_to_save_network_settings_msg',{msg:(resp.error.data ? resp.error.data : resp.error.message)})
          );
          setNetworkSettingsLoaded(true);
          return;
        }
        const networkSettings = resp.result as NetworkSettings;
        // We need to update the firstNetworkSettings ref to the new settings so we can use it to determine if the settings have changed
        firstNetworkSettings.current = networkSettings;
        setNetworkSettings(networkSettings);
        getNetworkState();
        setNetworkSettingsLoaded(true);
        notifications.success(t('Network_settings_saved'));
      });
    },
    [getNetworkState, send],
  );

  const handleRenewLease = useCallback(() => {
    send("renewDHCPLease", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(t('Failed_to_renew_lease_msg',{msg:resp.error.message}));
      } else {
        notifications.success(t('DHCP_lease_renewed'));
      }
    });
  }, [send]);

  useEffect(() => {
    getNetworkState();
    getNetworkSettings();
  }, [getNetworkState, getNetworkSettings]);

  const handleIpv4ModeChange = (value: IPv4Mode | string) => {
    setNetworkSettingsRemote({ ...networkSettings, ipv4_mode: value as IPv4Mode });
  };

  const handleIpv6ModeChange = (value: IPv6Mode | string) => {
    setNetworkSettingsRemote({ ...networkSettings, ipv6_mode: value as IPv6Mode });
  };

  const handleLldpModeChange = (value: LLDPMode | string) => {
    setNetworkSettings({ ...networkSettings, lldp_mode: value as LLDPMode });
  };

  const handleMdnsModeChange = (value: mDNSMode | string) => {
    setNetworkSettings({ ...networkSettings, mdns_mode: value as mDNSMode });
  };

  const handleTimeSyncModeChange = (value: TimeSyncMode | string) => {
    setNetworkSettings({ ...networkSettings, time_sync_mode: value as TimeSyncMode });
  };

  const handleHostnameChange = (value: string) => {
    setNetworkSettings({ ...networkSettings, hostname: value });
  };

  const handleProxyChange = (value: string) => {
    setNetworkSettings({ ...networkSettings, http_proxy: value });
  };

  const handleDomainChange = (value: string) => {
    setNetworkSettings({ ...networkSettings, domain: value });
  };

  const handleDomainOptionChange = (value: string) => {
    setSelectedDomainOption(value);
    if (value !== "custom") {
      handleDomainChange(value);
    }
  };

  const handleCustomDomainChange = (value: string) => {
    setCustomDomain(value);
    handleDomainChange(value);
  };

  const filterUnknown = useCallback(
    (options: { value: string; label: string }[]) => {
      if (!networkSettingsLoaded) return options;
      return options.filter(option => option.value !== "unknown");
    },
    [networkSettingsLoaded],
  );

  const [showRenewLeaseConfirm, setShowRenewLeaseConfirm] = useState(false);

  return (
    <>
      <Fieldset disabled={!networkSettingsLoaded} className="space-y-4">
        <SettingsPageHeader
          title={t('Network')}
          description={t('Configure_your_network_settings')}
        />
        <div className="space-y-4">
          <SettingsItem
            title={t('MAC_Address')}
            description={t('Hardware_identifier_for_the_network_interface')}
          >
            <InputField
              type="text"
              size="SM"
              value={networkState?.mac_address}
              error={""}
              readOnly={true}
              className="dark:!text-opacity-60"
            />
          </SettingsItem>
        </div>
        <div className="space-y-4">
          <SettingsItem
            title={t('Hostname')}
            description={t('Device_identifier_on_the_network_Blank_for_system_default')}
          >
            <div className="relative">
              <div>
                <InputField
                  size="SM"
                  type="text"
                  placeholder="jetkvm"
                  defaultValue={networkSettings.hostname}
                  onChange={e => {
                    handleHostnameChange(e.target.value);
                  }}
                />
              </div>
            </div>
          </SettingsItem>
        </div>
        <div className="space-y-4">
          <SettingsItem
            title={t('HTTP_Proxy')}
            description={t('Proxy_server_for_outgoing_HTTP_S_requests_from_the_device_Blank_for_none')}
          >
            <div className="relative">
              <div>
                <InputField
                  size="SM"
                  type="text"
                  placeholder="http://proxy.example.com:8080/"
                  defaultValue={networkSettings.http_proxy}
                  onChange={e => {
                    handleProxyChange(e.target.value);
                  }}
                />
              </div>
            </div>
          </SettingsItem>
        </div>

        <div className="space-y-4">
          <div className="space-y-1">
            <SettingsItem
              title={t('Domain')}
              description={t('Network_domain_suffix_for_the_device')}
            >
              <div className="space-y-2">
                <SelectMenuBasic
                  size="SM"
                  value={selectedDomainOption}
                  onChange={e => handleDomainOptionChange(e.target.value)}
                  options={[
                    { value: "dhcp", label: t('DHCP_provided') },
                    { value: "local", label: ".local" },
                    { value: "custom", label: t('Custom') },
                  ]}
                />
              </div>
            </SettingsItem>
            {selectedDomainOption === "custom" && (
              <div className="mt-2 w-1/3 border-l border-slate-800/10 pl-4 dark:border-slate-300/20">
                <InputFieldWithLabel
                  size="SM"
                  type="text"
                  label={t('Custom_Domain')}
                  placeholder="home"
                  value={customDomain}
                  onChange={e => {
                    setCustomDomain(e.target.value);
                    handleCustomDomainChange(e.target.value);
                  }}
                />
              </div>
            )}
          </div>
          <div className="space-y-4">
            <SettingsItem
              title="mDNS"
              description={t('Control_mDNS_multicast_DNS_operational_mode')}
            >
              <SelectMenuBasic
                size="SM"
                value={networkSettings.mdns_mode}
                onChange={e => handleMdnsModeChange(e.target.value)}
                options={filterUnknown([
                  { value: "disabled", label: t('Disabled') },
                  { value: "auto", label: t('Auto') },
                  { value: "ipv4_only", label: t('IPv4_only') },
                  { value: "ipv6_only", label: t('IPv6_only') },
                ])}
              />
            </SettingsItem>
          </div>

          <div className="space-y-4">
            <SettingsItem
              title={t('Time_synchronization')}
              description={t('Configure_time_synchronization_settings')}
            >
              <SelectMenuBasic
                size="SM"
                value={networkSettings.time_sync_mode}
                onChange={e => {
                  handleTimeSyncModeChange(e.target.value);
                }}
                options={filterUnknown([
                  { value: "unknown", label: "..." },
                  // { value: "auto", label: "Auto" },
                  { value: "ntp_only", label: t('NTP_only') },
                  { value: "ntp_and_http", label: t('NTP_and_HTTP') },
                  { value: "http_only", label: t('HTTP_only') },
                  // { value: "custom", label: "Custom" },
                ])}
              />
            </SettingsItem>
          </div>

          <Button
            size="SM"
            theme="primary"
            disabled={firstNetworkSettings.current === networkSettings}
            text={t('Save_Settings')}
            onClick={() => setNetworkSettingsRemote(networkSettings)}
          />
        </div>

        <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />

        <div className="space-y-4">
          <SettingsItem title={t('IPv4_Mode')} description={t('Configure_the_IPv4_mode')}>
            <SelectMenuBasic
              size="SM"
              value={networkSettings.ipv4_mode}
              onChange={e => handleIpv4ModeChange(e.target.value)}
              options={filterUnknown([
                { value: "dhcp", label: "DHCP" },
                // { value: "static", label: "Static" },
              ])}
            />
          </SettingsItem>
          <AutoHeight>
            {!networkSettingsLoaded && !networkState?.dhcp_lease ? (
              <GridCard>
                <div className="p-4">
                  <div className="space-y-4">
                    <h3 className="text-base font-bold text-slate-900 dark:text-white">
                        {t('DHCP_Lease_Information')}
                    </h3>
                    <div className="animate-pulse space-y-3">
                      <div className="h-4 w-1/3 rounded bg-slate-200 dark:bg-slate-700" />
                      <div className="h-4 w-1/2 rounded bg-slate-200 dark:bg-slate-700" />
                      <div className="h-4 w-1/3 rounded bg-slate-200 dark:bg-slate-700" />
                    </div>
                  </div>
                </div>
              </GridCard>
            ) : networkState?.dhcp_lease && networkState.dhcp_lease.ip ? (
              <DhcpLeaseCard
                networkState={networkState}
                setShowRenewLeaseConfirm={setShowRenewLeaseConfirm}
              />
            ) : (
              <EmptyCard
                IconElm={LuEthernetPort}
                headline={t('DHCP_Information')}
                description={t('No_DHCP_lease_information_available')}
              />
            )}
          </AutoHeight>
        </div>
        <div className="space-y-4">
          <SettingsItem title={t('IPv6_Mode')} description={t('Configure_the_IPv6_mode')}>
            <SelectMenuBasic
              size="SM"
              value={networkSettings.ipv6_mode}
              onChange={e => handleIpv6ModeChange(e.target.value)}
              options={filterUnknown([
                { value: "disabled", label: t('Disabled') },
                { value: "slaac", label: "SLAAC" },
                // { value: "dhcpv6", label: "DHCPv6" },
                // { value: "slaac_and_dhcpv6", label: "SLAAC and DHCPv6" },
                // { value: "static", label: "Static" },
                // { value: "link_local", label: "Link-local only" },
              ])}
            />
          </SettingsItem>
          <AutoHeight>
            {!networkSettingsLoaded &&
            !(networkState?.ipv6_addresses && networkState.ipv6_addresses.length > 0) ? (
              <GridCard>
                <div className="p-4">
                  <div className="space-y-4">
                    <h3 className="text-base font-bold text-slate-900 dark:text-white">
                        {t('IPv6_Information')}
                    </h3>
                    <div className="animate-pulse space-y-3">
                      <div className="h-4 w-1/3 rounded bg-slate-200 dark:bg-slate-700" />
                      <div className="h-4 w-1/2 rounded bg-slate-200 dark:bg-slate-700" />
                      <div className="h-4 w-1/3 rounded bg-slate-200 dark:bg-slate-700" />
                    </div>
                  </div>
                </div>
              </GridCard>
            ) : networkState?.ipv6_addresses && networkState.ipv6_addresses.length > 0 ? (
              <Ipv6NetworkCard networkState={networkState} />
            ) : (
              <EmptyCard
                IconElm={LuEthernetPort}
                headline={t('IPv6_Information')}
                description={t('No_IPv6_addresses_configured')}
              />
            )}
          </AutoHeight>
        </div>
        <div className="hidden space-y-4">
          <SettingsItem
            title="LLDP"
            description={t('Control_which_TLVs_will_be_sent_over_Link_Layer_Discovery_Protocol')}
          >
            <SelectMenuBasic
              size="SM"
              value={networkSettings.lldp_mode}
              onChange={e => handleLldpModeChange(e.target.value)}
              options={filterUnknown([
                { value: "disabled", label: t('Disabled') },
                { value: "basic", label: t('Basic') },
                { value: "all", label: t('All') },
              ])}
            />
          </SettingsItem>
        </div>
      </Fieldset>
      <ConfirmDialog
        open={showRenewLeaseConfirm}
        onClose={() => setShowRenewLeaseConfirm(false)}
        title={t('Renew_DHCP_Lease')}
        description={t('This_will_request_a_new_IP_address_from_your_DHCP_server_Your_device_may_temporarily_lose_network_connectivity_during_this_process')}
        variant="danger"
        confirmText={t('Renew_Lease')}
        onConfirm={() => {
          handleRenewLease();
          setShowRenewLeaseConfirm(false);
        }}
      />
    </>
  );
}
