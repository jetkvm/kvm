import { LuRefreshCcw } from "react-icons/lu";

import { Button } from "@components/Button";
import EmptyCard from "@components/EmptyCard";
import { GridCard } from "@components/Card";
import { LifeTimeLabel } from "@routes/devices.$id.settings.network";
import { NetworkState } from "@hooks/stores";
import { m } from "@localizations/messages.js";

export default function DhcpLeaseCard({
  networkState,
  setShowRenewLeaseConfirm,
}: {
  networkState: NetworkState | null;
  setShowRenewLeaseConfirm: (show: boolean) => void;
}) {
  const isDhcpLeaseEmpty = Object.keys(networkState?.dhcp_lease || {}).length === 0;

  if (isDhcpLeaseEmpty) {
    return (
      <EmptyCard
        headline={m.dhcp_empty_lease_headline()}
        description={m.dhcp_empty_lease_description()}
      />
    );
  }

  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black opacity-0 animation-duration-500 dark:text-white">
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-base font-bold text-slate-900 dark:text-white">
              {m.dhcp_lease_header()}
            </h3>

            <div>
              <Button
                size="XS"
                theme="light"
                type="button"
                className="text-red-500"
                text={m.dhcp_lease_renew()}
                LeadingIcon={LuRefreshCcw}
                onClick={() => setShowRenewLeaseConfirm(true)}
              />
            </div>
          </div>

          <div className="flex gap-x-6 gap-y-2">
            <div className="flex-1 space-y-2">
              {networkState?.dhcp_lease?.ip && (
                <div className="flex justify-between border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.ip_address()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">{networkState?.dhcp_lease?.ip}</span>
                </div>
              )}

              {networkState?.dhcp_lease?.netmask && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.subnet_mask()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">{networkState?.dhcp_lease?.netmask}</span>
                </div>
              )}

              {networkState?.dhcp_lease?.dns_servers && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.dns_servers()}
                  </span>
                  &nbsp;
                  <span className="text-right text-sm font-medium">
                    {networkState?.dhcp_lease?.dns_servers.map(dns => (
                      <div key={dns}>{dns}</div>
                    ))}
                  </span>
                </div>
              )}

              {networkState?.dhcp_lease?.broadcast && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.dhcp_lease_broadcast()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">{networkState?.dhcp_lease?.broadcast}</span>
                </div>
              )}

              {networkState?.dhcp_lease?.domain && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.dhcp_lease_domain()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">{networkState?.dhcp_lease?.domain}</span>
                </div>
              )}

              {networkState?.dhcp_lease?.ntp_servers &&
                networkState?.dhcp_lease?.ntp_servers.length > 0 && (
                  <div className="flex justify-between gap-x-8 border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                    <div className="w-full grow text-sm text-slate-600 dark:text-slate-400">
                      {m.ntp_servers()}
                    </div>
                    &nbsp;
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
                    {m.dhcp_lease_hostname()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">{networkState?.dhcp_lease?.hostname}</span>
                </div>
              )}
            </div>

            <div className="flex-1 space-y-2">
              {networkState?.dhcp_lease?.routers &&
                networkState?.dhcp_lease?.routers.length > 0 && (
                  <div className="flex justify-between pt-2">
                    <span className="text-sm text-slate-600 dark:text-slate-400">
                      {m.dhcp_lease_gateway()}
                    </span>
                    &nbsp;
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
                    {m.dhcp_server()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">{networkState?.dhcp_lease?.server_id}</span>
                </div>
              )}

              {networkState?.dhcp_lease?.lease_expiry && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.dhcp_lease_lease_expires()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">
                    <LifeTimeLabel lifetime={`${networkState?.dhcp_lease?.lease_expiry}`} />
                  </span>
                </div>
              )}

              {networkState?.dhcp_lease?.broadcast && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.dhcp_lease_broadcast()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">{networkState?.dhcp_lease?.broadcast}</span>
                </div>
              )}

              {networkState?.dhcp_lease?.mtu && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.dhcp_lease_maximum_transfer_unit()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">{networkState?.dhcp_lease?.mtu}</span>
                </div>
              )}

              {networkState?.dhcp_lease?.ttl && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.dhcp_lease_time_to_live()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">{networkState?.dhcp_lease?.ttl}</span>
                </div>
              )}

              {networkState?.dhcp_lease?.bootp_next_server && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.dhcp_lease_boot_next_server()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">
                    {networkState?.dhcp_lease?.bootp_next_server}
                  </span>
                </div>
              )}

              {networkState?.dhcp_lease?.bootp_server_name && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.dhcp_lease_boot_server_name()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">
                    {networkState?.dhcp_lease?.bootp_server_name}
                  </span>
                </div>
              )}

              {networkState?.dhcp_lease?.bootp_file && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.dhcp_lease_boot_file()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">
                    {networkState?.dhcp_lease?.bootp_file}
                  </span>
                </div>
              )}

              {networkState?.dhcp_lease?.dhcp_client && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.network_dhcp_client_title()}
                  </span>
                  &nbsp;
                  <span className="text-sm font-medium">
                    {networkState?.dhcp_lease?.dhcp_client}
                  </span>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </GridCard>
  );
}
