import { useCallback, useEffect, useRef, useState } from "react";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import { LuEthernetPort } from "react-icons/lu";

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

  if (lifetime == "") {
    return <strong>N/A</strong>;
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

  const getNetworkState = useCallback(() => {
    send("getNetworkState", {}, resp => {
      if ("error" in resp) return;
      console.log(resp.result);
      setNetworkState(resp.result as NetworkState);
    });
  }, [send, setNetworkState]);

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
        getNetworkState();
        setNetworkSettingsLoaded(true);
        notifications.success("Network settings saved");
      });
    },
    [getNetworkState, send],
  );

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
          <div className="space-y-1">
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
              <div className="mt-2 w-1/3 border-l border-slate-800/10 pl-4 dark:border-slate-300/20">
                <InputFieldWithLabel
                  size="SM"
                  type="text"
                  label="Custom Domain"
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

        <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />

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
          <AutoHeight>
            {!networkSettingsLoaded ? (
              <GridCard>
                <div className="p-4">
                  <div className="space-y-4">
                    <h3 className="text-base font-bold text-slate-900 dark:text-white">
                      DHCP Lease Information
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
                headline="DHCP Information"
                description="No DHCP lease information available"
              />
            )}
          </AutoHeight>
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
          <AutoHeight>
            {!networkSettingsLoaded ? (
              <GridCard>
                <div className="p-4">
                  <div className="space-y-4">
                    <h3 className="text-base font-bold text-slate-900 dark:text-white">
                      IPv6 Information
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
                headline="IPv6 Information"
                description="No IPv6 addresses configured"
              />
            )}
          </AutoHeight>
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
