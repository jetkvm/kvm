import { useCallback, useEffect, useState } from "react";

import { SettingsPageHeader } from "../components/SettingsPageheader";
import { SelectMenuBasic } from "../components/SelectMenuBasic";

import { SettingsItem } from "./devices.$id.settings";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { Button } from "@components/Button";
import notifications from "@/notifications";

interface DhcpLease {
  ip?: string;
  netmask?: string;
  broadcast?: string;
  ttl?: string;
  mtu?: string;
  hostname?: string;
  domain?: string;
  bootp_next_server?: string;
  bootp_server_name?: string;
  bootp_file?: string;
  timezone?: string;
  routers?: string[];
  dns?: string[];
  ntp_servers?: string[];
  lpr_servers?: string[];
  _time_servers?: string[];
  _name_servers?: string[];
  _log_servers?: string[];
  _cookie_servers?: string[];
  _wins_servers?: string[];
  _swap_server?: string;
  boot_size?: string;
  root_path?: string;
  lease?: string;
  dhcp_type?: string;
  server_id?: string;
  message?: string;
  tftp?: string;
  bootfile?: string;
}


interface NetworkState {
  interface_name?: string;
  mac_address?: string;
  ipv4?: string;
  ipv6?: string;
  dhcp_lease?: DhcpLease;
}

export default function SettingsNetworkRoute() {
  const [send] = useJsonRpc();
  const [networkState, setNetworkState] = useState<NetworkState | null>(null);


  const getNetworkState = useCallback(() => {
    send("getNetworkState", {}, resp => {
      if ("error" in resp) return;
      setNetworkState(resp.result as NetworkState);
    });
  }, [send]);

  const handleRenewLease = useCallback(() => {
    send("renewDHCPLease", {}, resp => {
      if ("error" in resp) {
        notifications.error("Failed to renew lease: " + resp.error.message);
      } else {
        notifications.success("DHCP lease renewed");
        getNetworkState();
      }
    });
  }, [send, getNetworkState]);

  useEffect(() => {
    getNetworkState();
  }, [getNetworkState]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Network"
        description="Configure your network settings"
      />
      <div className="space-y-4">
        <SettingsItem
          title="IPv4 Address"
          description={
            <span className="select-text font-mono">{networkState?.ipv4}</span>
          }
        />
      </div>
      <div className="space-y-4">
        <SettingsItem
          title="IPv6 Address"
          description={<span className="select-text font-mono">{networkState?.ipv6}</span>}
        />
      </div>
      <div className="space-y-4">
        <SettingsItem
          title="MAC Address"
          description={<span className="select-auto font-mono">{networkState?.mac_address}</span>}
        />
      </div>
      <div className="space-y-4">
        <SettingsItem
          title="DHCP Lease"
          description={<>
          <ul>
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
          </ul>
          </>}
        >
          <Button
              size="SM"
              theme="light"
              text="Renew lease"
              onClick={() => {
                handleRenewLease();
              }}
          />
        </SettingsItem>
      </div>
    </div>
  );
}
