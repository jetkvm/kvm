import { useCallback, useEffect, useState } from "react";

import { SelectMenuBasic } from "../components/SelectMenuBasic";
import { SettingsPageHeader } from "../components/SettingsPageheader";

import { IPv4Mode, IPv6Mode, LLDPMode, mDNSMode, NetworkSettings, NetworkState, TimeSyncMode, useNetworkStateStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import InputField from "@components/InputField";
import { SettingsItem } from "./devices.$id.settings";

import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';

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
}

export function LifeTimeLabel({ lifetime }: { lifetime: string }) {
  if (lifetime == "") {
    return <strong>N/A</strong>;
  }

  const [remaining, setRemaining] = useState<string | null>(null);

  useEffect(() => {
    setRemaining(dayjs(lifetime).fromNow());

    const interval = setInterval(() => {
      setRemaining(dayjs(lifetime).fromNow());
    }, 1000 * 30);
    return () => clearInterval(interval);
  }, [lifetime]);

  return <>
    <strong>{dayjs(lifetime).format()}</strong>
    {remaining && <>
      {" "}<span className="text-xs text-slate-700 dark:text-slate-300">
        ({remaining})
      </span>
    </>}
  </>
}

export default function SettingsNetworkRoute() {
  const [send] = useJsonRpc();
  const [networkState, setNetworkState] = useNetworkStateStore(state => [state, state.setNetworkState]);

  const [networkSettings, setNetworkSettings] = useState<NetworkSettings>(defaultNetworkSettings);
  const [networkSettingsLoaded, setNetworkSettingsLoaded] = useState(false);

  const getNetworkSettings = useCallback(() => {
    setNetworkSettingsLoaded(false);
    send("getNetworkSettings", {}, resp => {
      if ("error" in resp) return;
      console.log(resp.result);
      setNetworkSettings(resp.result as NetworkSettings);
      setNetworkSettingsLoaded(true);
    });
  }, [send]);

  const setNetworkSettingsRemote = useCallback((settings: NetworkSettings) => {
    setNetworkSettingsLoaded(false);
    send("setNetworkSettings", { settings }, resp => {
      if ("error" in resp) {
        notifications.error("Failed to save network settings: " + (resp.error.data ? resp.error.data : resp.error.message));
        setNetworkSettingsLoaded(true);
        return;
      }
      setNetworkSettings(resp.result as NetworkSettings);
      setNetworkSettingsLoaded(true);
      notifications.success("Network settings saved");
    });
  }, [send]);

  const getNetworkState = useCallback(() => {
    send("getNetworkState", {}, resp => {
      if ("error" in resp) return;
      console.log(resp.result);
      setNetworkState(resp.result as NetworkState);
    });
  }, [send]);

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

  const filterUnknown = useCallback((options: { value: string; label: string; }[]) => {
    if (!networkSettingsLoaded) return options;
    return options.filter(option => option.value !== "unknown");
  }, [networkSettingsLoaded]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Network"
        description="Configure your network settings"
      />
      <div className="space-y-4">
        <SettingsItem
          title="MAC Address"
          description={<></>}
        >
          <span className="select-auto font-mono text-xs text-slate-700 dark:text-slate-300">
            {networkState?.mac_address}
          </span>
        </SettingsItem>
      </div>
      <div className="space-y-4">
        <SettingsItem
          title="Hostname"
          description={
            <>
              Hostname for the device
              <br />
              <span className="text-xs text-slate-700 dark:text-slate-300">
                Leave blank for default
              </span>
            </>
          }
        >
          <InputField
            type="text"
            placeholder="jetkvm"
            value={networkSettings.hostname}
            error={""}
            onChange={e => {
              setNetworkSettings({ ...networkSettings, hostname: e.target.value });
            }}
            disabled={!networkSettingsLoaded}
          />
        </SettingsItem>
      </div>
      <div className="space-y-4">
        <SettingsItem
          title="Domain"
          description={
            <>
              Domain for the device
              <br />
              <span className="text-xs text-slate-700 dark:text-slate-300">
                Leave blank to use DHCP provided domain, if there is no domain, use <span className="font-mono">local</span>
              </span>
            </>
          }
        >
          <InputField
            type="text"
            placeholder="local"
            value={networkSettings.domain}
            error={""}
            onChange={e => {
              setNetworkSettings({ ...networkSettings, domain: e.target.value });
            }}
            disabled={!networkSettingsLoaded}
          />
        </SettingsItem>
      </div>
      <div className="space-y-4">
        <SettingsItem
          title="IPv4 Mode"
          description="Configure the IPv4 mode"
        >
          <SelectMenuBasic
            size="SM"
            value={networkSettings.ipv4_mode}
            onChange={e => handleIpv4ModeChange(e.target.value)}
            disabled={!networkSettingsLoaded}
            options={filterUnknown([
              { value: "dhcp", label: "DHCP" },
              // { value: "static", label: "Static" },
            ])}
          />
        </SettingsItem>
        {networkState?.dhcp_lease && (
          <GridCard>
            <div className="flex items-start gap-x-4 p-4">
              <div className="space-y-3 w-full">
                <div className="space-y-2">
                  <h3 className="text-base font-bold text-slate-900 dark:text-white">
                    Current DHCP Lease
                  </h3>
                  <div>
                    <ul className="list-none space-y-1 text-xs text-slate-700 dark:text-slate-300">
                      {networkState?.dhcp_lease?.ip && <li>IP: <strong>{networkState?.dhcp_lease?.ip}</strong></li>}
                      {networkState?.dhcp_lease?.netmask && <li>Subnet: <strong>{networkState?.dhcp_lease?.netmask}</strong></li>}
                      {networkState?.dhcp_lease?.broadcast && <li>Broadcast: <strong>{networkState?.dhcp_lease?.broadcast}</strong></li>}
                      {networkState?.dhcp_lease?.ttl && <li>TTL: <strong>{networkState?.dhcp_lease?.ttl}</strong></li>}
                      {networkState?.dhcp_lease?.mtu && <li>MTU: <strong>{networkState?.dhcp_lease?.mtu}</strong></li>}
                      {networkState?.dhcp_lease?.hostname && <li>Hostname: <strong>{networkState?.dhcp_lease?.hostname}</strong></li>}
                      {networkState?.dhcp_lease?.domain && <li>Domain: <strong>{networkState?.dhcp_lease?.domain}</strong></li>}
                      {networkState?.dhcp_lease?.routers && <li>Gateway: <strong>{networkState?.dhcp_lease?.routers.join(", ")}</strong></li>}
                      {networkState?.dhcp_lease?.dns && <li>DNS: <strong>{networkState?.dhcp_lease?.dns.join(", ")}</strong></li>}
                      {networkState?.dhcp_lease?.ntp_servers && <li>NTP Servers: <strong>{networkState?.dhcp_lease?.ntp_servers.join(", ")}</strong></li>}
                      {networkState?.dhcp_lease?.server_id && <li>Server ID: <strong>{networkState?.dhcp_lease?.server_id}</strong></li>}
                      {networkState?.dhcp_lease?.bootp_next_server && <li>BootP Next Server: <strong>{networkState?.dhcp_lease?.bootp_next_server}</strong></li>}
                      {networkState?.dhcp_lease?.bootp_server_name && <li>BootP Server Name: <strong>{networkState?.dhcp_lease?.bootp_server_name}</strong></li>}
                      {networkState?.dhcp_lease?.bootp_file && <li>Boot File: <strong>{networkState?.dhcp_lease?.bootp_file}</strong></li>}
                      {networkState?.dhcp_lease?.lease_expiry && <li>
                        Lease Expiry: <LifeTimeLabel lifetime={`${networkState?.dhcp_lease?.lease_expiry}`} />
                      </li>}
                      {/* {JSON.stringify(networkState?.dhcp_lease)} */}
                    </ul>
                  </div>
                </div>
                <hr className="block w-full dark:border-slate-600" />
                <div>
                  <Button
                    size="SM"
                    theme="danger"
                    text="Renew lease"
                    onClick={() => {
                      handleRenewLease();
                    }}
                  />
                </div>
              </div>
            </div>
          </GridCard>
        )}
      </div>
      <div className="space-y-4">
        <SettingsItem
          title="IPv6 Mode"
          description="Configure the IPv6 mode"
        >
          <SelectMenuBasic
            size="SM"
            value={networkSettings.ipv6_mode}
            onChange={e => handleIpv6ModeChange(e.target.value)}
            disabled={!networkSettingsLoaded}
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
            <div className="flex items-start gap-x-4 p-4">
              <div className="space-y-3 w-full">
                <div className="space-y-2">
                  <h3 className="text-base font-bold text-slate-900 dark:text-white">
                    IPv6 Information
                  </h3>
                  <div className="space-y-2">
                    <div>
                      <h4 className="text-sm font-bold text-slate-900 dark:text-white">
                        IPv6 Link-local
                      </h4>
                      <p className="text-xs text-slate-700 dark:text-slate-300">
                        {networkState?.ipv6_link_local}
                      </p>
                    </div>
                    <div>
                      <h4 className="text-sm font-bold text-slate-900 dark:text-white">
                        IPv6 Addresses
                      </h4>
                      <ul className="list-none space-y-1 text-xs text-slate-700 dark:text-slate-300">
                        {networkState?.ipv6_addresses && networkState?.ipv6_addresses.map(addr => (
                          <li key={addr.address}>
                            {addr.address}
                            {addr.valid_lifetime && <>
                              <br />
                              - valid_lft: {" "}
                              <span className="text-xs text-slate-700 dark:text-slate-300">
                                <LifeTimeLabel lifetime={`${addr.valid_lifetime}`} />
                              </span>
                            </>}
                            {addr.preferred_lifetime && <>
                              <br />
                              - pref_lft: {" "}
                              <span className="text-xs text-slate-700 dark:text-slate-300">
                                <LifeTimeLabel lifetime={`${addr.preferred_lifetime}`} />
                              </span>
                            </>}
                          </li>
                        ))}
                      </ul>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </GridCard>
        )}
      </div>
      <div className="space-y-4 hidden">
        <SettingsItem
          title="LLDP"
          description="Control which TLVs will be sent over Link Layer Discovery Protocol"
        >
          <SelectMenuBasic
            size="SM"
            value={networkSettings.lldp_mode}
            onChange={e => handleLldpModeChange(e.target.value)}
            disabled={!networkSettingsLoaded}
            options={filterUnknown([
              { value: "disabled", label: "Disabled" },
              { value: "basic", label: "Basic" },
              { value: "all", label: "All" },
            ])}
          />
        </SettingsItem>
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
            disabled={!networkSettingsLoaded}
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
            onChange={e => handleTimeSyncModeChange(e.target.value)}
            disabled={!networkSettingsLoaded}
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
      <div className="flex items-end gap-x-2">
        <Button
          onClick={() => {
            setNetworkSettingsRemote(networkSettings);
          }}
          size="SM"
          theme="light"
          text="Save Settings"
        />
      </div>
    </div>
  );
}
