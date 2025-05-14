import { useCallback, useEffect, useRef, useState } from "react";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import { ArrowPathIcon } from "@heroicons/react/24/outline";

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
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import InputField from "@components/InputField";

import { SettingsPageHeader } from "../components/SettingsPageheader";
import { SelectMenuBasic } from "../components/SelectMenuBasic";
import Fieldset from "../components/Fieldset";
import { ConfirmDialog } from "../components/ConfirmDialog";

import { SettingsItem } from "./devices.$id.settings";

dayjs.extend(relativeTime);

const defaultNetworkSettings: NetworkSettings = {
  hostname: "",
  domain: "",
  ipv4_mode: "unknown",
  ipv6_mode: "unknown",
  lldp_mode: "unknown",
  lldp_tx_tlvs: [],
  mdns_mode: "unknown",
  time_sync_mode: "unknown",
};

export function LifeTimeLabel({ lifetime }: { lifetime: string }) {
  const [remaining, setRemaining] = useState<string | null>(null);

  useEffect(() => {
    setRemaining(dayjs(lifetime).fromNow());

    const interval = setInterval(() => {
      setRemaining(dayjs(lifetime).fromNow());
    }, 1000 * 30);
    return () => clearInterval(interval);
  }, [lifetime]);

  return (
    <>
      <span>{dayjs(lifetime).format("YYYY-MM-DD HH:mm")}</span>
      {remaining && (
        <>
          {" "}
          <span className="text-xs text-slate-700 dark:text-slate-300">
            ({remaining})
          </span>
        </>
      )}
    </>
  );
}

export default function SettingsNetworkRoute() {
  const [send] = useJsonRpc();
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
    send("getNetworkSettings", {}, resp => {
      if ("error" in resp) return;
      console.log(resp.result);
      setNetworkSettings(resp.result as NetworkSettings);

      if (!firstNetworkSettings.current) {
        firstNetworkSettings.current = resp.result as NetworkSettings;
      }
      setNetworkSettingsLoaded(true);
    });
  }, [send]);

  const setNetworkSettingsRemote = useCallback(
    (settings: NetworkSettings) => {
      setNetworkSettingsLoaded(false);
      send("setNetworkSettings", { settings }, resp => {
        if ("error" in resp) {
          notifications.error(
            "Failed to save network settings: " +
              (resp.error.data ? resp.error.data : resp.error.message),
          );
          setNetworkSettingsLoaded(true);
          return;
        }
        // We need to update the firstNetworkSettings ref to the new settings so we can use it to determine if the settings have changed
        firstNetworkSettings.current = resp.result as NetworkSettings;
        setNetworkSettings(resp.result as NetworkSettings);
        setNetworkSettingsLoaded(true);
        notifications.success("Network settings saved");
      });
    },
    [send],
  );

  const getNetworkState = useCallback(() => {
    send("getNetworkState", {}, resp => {
      if ("error" in resp) return;
      console.log(resp.result);
      setNetworkState(resp.result as NetworkState);
    });
  }, [send, setNetworkState]);

  const handleRenewLease = useCallback(() => {
    send("renewDHCPLease", {}, resp => {
      if ("error" in resp) {
        notifications.error("Failed to renew lease: " + resp.error.message);
      } else {
        notifications.success("DHCP lease renewed");
      }
    });
  }, [send]);

  useEffect(() => {
    getNetworkState();
    getNetworkSettings();
  }, [getNetworkState, getNetworkSettings]);

  const handleIpv4ModeChange = (value: IPv4Mode | string) => {
    setNetworkSettings({ ...networkSettings, ipv4_mode: value as IPv4Mode });
  };

  const handleIpv6ModeChange = (value: IPv6Mode | string) => {
    setNetworkSettings({ ...networkSettings, ipv6_mode: value as IPv6Mode });
  };

  const handleLldpModeChange = (value: LLDPMode | string) => {
    setNetworkSettings({ ...networkSettings, lldp_mode: value as LLDPMode });
  };

  // const handleLldpTxTlvsChange = (value: string[]) => {
  //   setNetworkSettings({ ...networkSettings, lldp_tx_tlvs: value });
  // };

  const handleMdnsModeChange = (value: mDNSMode | string) => {
    setNetworkSettings({ ...networkSettings, mdns_mode: value as mDNSMode });
  };

  const handleTimeSyncModeChange = (value: TimeSyncMode | string) => {
    setNetworkSettings({ ...networkSettings, time_sync_mode: value as TimeSyncMode });
  };

  const handleHostnameChange = (value: string) => {
    setNetworkSettings({ ...networkSettings, hostname: value });
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
          title="Network"
          description="Configure your network settings"
        />
        <div className="space-y-4">
          <SettingsItem
            title="MAC Address"
            description="Hardware identifier for the network interface"
          >
            <InputField
              type="text"
              size="SM"
              value={networkState?.mac_address}
              error={""}
              disabled={true}
              readOnly={true}
              className="dark:!text-opacity-60"
            />
          </SettingsItem>
        </div>
        <div className="space-y-4">
          <SettingsItem
            title="Hostname"
            description="Device identifier on the network. Blank for system default"
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
          <div className="space-y-4">
            <SettingsItem
              title="Domain"
              description="Network domain suffix for the device"
            >
              <div className="space-y-2">
                <SelectMenuBasic
                  size="SM"
                  value={selectedDomainOption}
                  onChange={e => handleDomainOptionChange(e.target.value)}
                  options={[
                    { value: "dhcp", label: "DHCP provided" },
                    { value: "local", label: ".local" },
                    { value: "custom", label: "Custom" },
                  ]}
                />
              </div>
            </SettingsItem>
            {selectedDomainOption === "custom" && (
              <div className="flex items-center justify-between gap-x-2">
                <InputField
                  size="SM"
                  type="text"
                  placeholder="home"
                  value={customDomain}
                  onChange={e => setCustomDomain(e.target.value)}
                />
                <Button
                  size="SM"
                  theme="primary"
                  text="Save Domain"
                  onClick={() => handleCustomDomainChange(customDomain)}
                />
              </div>
            )}
          </div>
          <div className="space-y-4">
            <SettingsItem
              title="mDNS"
              description="Control mDNS (multicast DNS) operational mode"
            >
              <SelectMenuBasic
                size="SM"
                value={networkSettings.mdns_mode}
                onChange={e => handleMdnsModeChange(e.target.value)}
                options={filterUnknown([
                  { value: "disabled", label: "Disabled" },
                  { value: "auto", label: "Auto" },
                  { value: "ipv4_only", label: "IPv4 only" },
                  { value: "ipv6_only", label: "IPv6 only" },
                ])}
              />
            </SettingsItem>
          </div>

          <div className="space-y-4">
            <SettingsItem
              title="Time synchronization"
              description="Configure time synchronization settings"
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
                  { value: "ntp_only", label: "NTP only" },
                  { value: "ntp_and_http", label: "NTP and HTTP" },
                  { value: "http_only", label: "HTTP only" },
                  // { value: "custom", label: "Custom" },
                ])}
              />
            </SettingsItem>
          </div>

          <Button
            size="SM"
            theme="primary"
            disabled={firstNetworkSettings.current === networkSettings}
            text="Save Settings"
            onClick={() => setNetworkSettingsRemote(networkSettings)}
          />
        </div>

        <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />

        <div className="space-y-4">
          <SettingsItem title="IPv4 Mode" description="Configure the IPv4 mode">
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
          {networkState?.dhcp_lease && (
            <GridCard>
              <div className="p-4">
                <div className="space-y-4">
                  <h3 className="text-base font-bold text-slate-900 dark:text-white">
                    DHCP Lease
                  </h3>

                  <div className="flex gap-x-6 gap-y-2">
                    <div className="flex-1 space-y-2">
                      {networkState?.dhcp_lease?.ip && (
                        <div className="flex justify-between border-slate-800/10 pt-2 dark:border-slate-300/20">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            IP Address
                          </span>
                          <span className="text-sm font-medium">
                            {networkState?.dhcp_lease?.ip}
                          </span>
                        </div>
                      )}

                      {networkState?.dhcp_lease?.netmask && (
                        <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            Subnet Mask
                          </span>
                          <span className="text-sm font-medium">
                            {networkState?.dhcp_lease?.netmask}
                          </span>
                        </div>
                      )}

                      {networkState?.dhcp_lease?.dns && (
                        <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            DNS Servers
                          </span>
                          <span className="text-right text-sm font-medium">
                            {networkState?.dhcp_lease?.dns.map(dns => (
                              <div key={dns}>{dns}</div>
                            ))}
                          </span>
                        </div>
                      )}

                      {networkState?.dhcp_lease?.broadcast && (
                        <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            Broadcast
                          </span>
                          <span className="text-sm font-medium">
                            {networkState?.dhcp_lease?.broadcast}
                          </span>
                        </div>
                      )}

                      {networkState?.dhcp_lease?.domain && (
                        <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            Domain
                          </span>
                          <span className="text-sm font-medium">
                            {networkState?.dhcp_lease?.domain}
                          </span>
                        </div>
                      )}

                      {networkState?.dhcp_lease?.ntp_servers &&
                        networkState?.dhcp_lease?.ntp_servers.length > 0 && (
                          <div className="flex justify-between gap-x-8 border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                            <div className="w-full grow text-sm text-slate-600 dark:text-slate-400">
                              NTP Servers
                            </div>
                            <div className="shrink text-right text-sm font-medium">
                              {networkState?.dhcp_lease?.ntp_servers.map(server => (
                                <div key={server}>{server}</div>
                              ))}
                            </div>
                          </div>
                        )}

                      {networkState?.dhcp_lease?.hostname && (
                        <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            Hostname
                          </span>
                          <span className="text-sm font-medium">
                            {networkState?.dhcp_lease?.hostname}
                          </span>
                        </div>
                      )}
                    </div>

                    <div className="flex-1 space-y-2">
                      {networkState?.dhcp_lease?.routers &&
                        networkState?.dhcp_lease?.routers.length > 0 && (
                          <div className="flex justify-between pt-2">
                            <span className="text-sm text-slate-600 dark:text-slate-400">
                              Gateway
                            </span>
                            <span className="text-right text-sm font-medium">
                              {networkState?.dhcp_lease?.routers.map(router => (
                                <div key={router}>{router}</div>
                              ))}
                            </span>
                          </div>
                        )}

                      {networkState?.dhcp_lease?.server_id && (
                        <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            DHCP Server
                          </span>
                          <span className="text-sm font-medium">
                            {networkState?.dhcp_lease?.server_id}
                          </span>
                        </div>
                      )}

                      {networkState?.dhcp_lease?.lease_expiry && (
                        <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            Lease Expires
                          </span>
                          <span className="text-sm font-medium">
                            <LifeTimeLabel
                              lifetime={`${networkState?.dhcp_lease?.lease_expiry}`}
                            />
                          </span>
                        </div>
                      )}

                      {networkState?.dhcp_lease?.mtu && (
                        <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            MTU
                          </span>
                          <span className="text-sm font-medium">
                            {networkState?.dhcp_lease?.mtu}
                          </span>
                        </div>
                      )}

                      {networkState?.dhcp_lease?.ttl && (
                        <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            TTL
                          </span>
                          <span className="text-sm font-medium">
                            {networkState?.dhcp_lease?.ttl}
                          </span>
                        </div>
                      )}

                      {networkState?.dhcp_lease?.bootp_next_server && (
                        <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            Boot Next Server
                          </span>
                          <span className="text-sm font-medium">
                            {networkState?.dhcp_lease?.bootp_next_server}
                          </span>
                        </div>
                      )}

                      {networkState?.dhcp_lease?.bootp_server_name && (
                        <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            Boot Server Name
                          </span>
                          <span className="text-sm font-medium">
                            {networkState?.dhcp_lease?.bootp_server_name}
                          </span>
                        </div>
                      )}

                      {networkState?.dhcp_lease?.bootp_file && (
                        <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                          <span className="text-sm text-slate-600 dark:text-slate-400">
                            Boot File
                          </span>
                          <span className="text-sm font-medium">
                            {networkState?.dhcp_lease?.bootp_file}
                          </span>
                        </div>
                      )}
                    </div>
                  </div>

                  <div>
                    <Button
                      size="SM"
                      theme="light"
                      className="text-red-500"
                      text="Renew DHCP Lease"
                      LeadingIcon={ArrowPathIcon}
                      onClick={() => setShowRenewLeaseConfirm(true)}
                    />
                  </div>
                </div>
              </div>
            </GridCard>
          )}
        </div>
        <div className="space-y-4">
          <SettingsItem title="IPv6 Mode" description="Configure the IPv6 mode">
            <SelectMenuBasic
              size="SM"
              value={networkSettings.ipv6_mode}
              onChange={e => handleIpv6ModeChange(e.target.value)}
              options={filterUnknown([
                // { value: "disabled", label: "Disabled" },
                { value: "slaac", label: "SLAAC" },
                // { value: "dhcpv6", label: "DHCPv6" },
                // { value: "slaac_and_dhcpv6", label: "SLAAC and DHCPv6" },
                // { value: "static", label: "Static" },
                // { value: "link_local", label: "Link-local only" },
              ])}
            />
          </SettingsItem>
          {networkState?.ipv6_addresses && (
            <GridCard>
              <div className="p-4">
                <div className="space-y-4">
                  <h3 className="text-base font-bold text-slate-900 dark:text-white">
                    IPv6 Information
                  </h3>

                  <div className="grid grid-cols-2 gap-x-6 gap-y-2">
                    {networkState?.dhcp_lease?.ip && (
                      <div className="flex flex-col justify-between">
                        <span className="text-sm text-slate-600 dark:text-slate-400">
                          Link-local
                        </span>
                        <span className="text-sm font-medium">
                          {networkState?.ipv6_link_local}
                        </span>
                      </div>
                    )}
                  </div>

                  <div className="space-y-3 pt-2">
                    {networkState?.ipv6_addresses &&
                      networkState?.ipv6_addresses.length > 0 && (
                        <div className="space-y-3">
                          <h4 className="text-sm font-semibold">IPv6 Addresses</h4>
                          {networkState.ipv6_addresses.map(addr => (
                            <div
                              key={addr.address}
                              className="rounded-md rounded-l-none border border-slate-500/10 border-l-blue-700/50 bg-slate-100/40 p-4 pl-4 dark:border-blue-500 dark:bg-slate-900"
                            >
                              <div className="grid grid-cols-2 gap-x-8 gap-y-4">
                                <div className="col-span-2 flex flex-col justify-between">
                                  <span className="text-sm text-slate-600 dark:text-slate-400">
                                    Address
                                  </span>
                                  <span className="text-sm font-medium">
                                    {addr.address}
                                  </span>
                                </div>

                                {addr.valid_lifetime && (
                                  <div className="flex flex-col justify-between">
                                    <span className="text-sm text-slate-600 dark:text-slate-400">
                                      Valid Lifetime
                                    </span>
                                    <span className="text-sm font-medium">
                                      {addr.valid_lifetime === "" ? (
                                        <span className="text-slate-400 dark:text-slate-600">
                                          N/A
                                        </span>
                                      ) : (
                                        <LifeTimeLabel
                                          lifetime={`${addr.valid_lifetime}`}
                                        />
                                      )}
                                    </span>
                                  </div>
                                )}
                                {addr.preferred_lifetime && (
                                  <div className="flex flex-col justify-between">
                                    <span className="text-sm text-slate-600 dark:text-slate-400">
                                      Preferred Lifetime
                                    </span>
                                    <span className="text-sm font-medium">
                                      {addr.preferred_lifetime === "" ? (
                                        <span className="text-slate-400 dark:text-slate-600">
                                          N/A
                                        </span>
                                      ) : (
                                        <LifeTimeLabel
                                          lifetime={`${addr.preferred_lifetime}`}
                                        />
                                      )}
                                    </span>
                                  </div>
                                )}
                              </div>
                            </div>
                          ))}
                        </div>
                      )}
                  </div>
                </div>
              </div>
            </GridCard>
          )}
        </div>
        <div className="hidden space-y-4">
          <SettingsItem
            title="LLDP"
            description="Control which TLVs will be sent over Link Layer Discovery Protocol"
          >
            <SelectMenuBasic
              size="SM"
              value={networkSettings.lldp_mode}
              onChange={e => handleLldpModeChange(e.target.value)}
              options={filterUnknown([
                { value: "disabled", label: "Disabled" },
                { value: "basic", label: "Basic" },
                { value: "all", label: "All" },
              ])}
            />
          </SettingsItem>
        </div>
      </Fieldset>
      <ConfirmDialog
        open={showRenewLeaseConfirm}
        onClose={() => setShowRenewLeaseConfirm(false)}
        title="Renew DHCP Lease"
        description="This will request a new IP address from your DHCP server. Your device may temporarily lose network connectivity during this process."
        variant="danger"
        confirmText="Renew Lease"
        onConfirm={() => {
          handleRenewLease();
          setShowRenewLeaseConfirm(false);
        }}
      />
    </>
  );
}
