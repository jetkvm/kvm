import { LuRefreshCcw } from "react-icons/lu";
import { useCallback, useEffect, useState } from "react";

import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import { PublicIP } from "@hooks/stores";
import { m } from "@localizations/messages.js";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import notifications from "@/notifications";
import { formatters } from "@/utils";

const TimeAgoLabel = ({ date }: { date: Date }) => {
  const [timeAgo, setTimeAgo] = useState<string | undefined>(formatters.timeAgo(date));
  useEffect(() => {
    const interval = setInterval(() => {
      setTimeAgo(formatters.timeAgo(date));
    }, 1000);
    return () => clearInterval(interval);
  }, [date]);

  return <span className="text-sm text-slate-600 select-none dark:text-slate-400">{timeAgo}</span>;
};

export default function PublicIPCard() {
  const { send } = useJsonRpc();

  const [publicIPs, setPublicIPs] = useState<PublicIP[]>([]);
  const refreshPublicIPs = useCallback(() => {
    send("getPublicIPAddresses", { refresh: true }, (resp: JsonRpcResponse) => {
      setPublicIPs([]);
      if ("error" in resp) {
        notifications.error(
          m.public_ip_card_refresh_error({ error: resp.error.data || m.unknown_error() }),
        );
        return;
      }
      const publicIPs = resp.result as PublicIP[];
      // sort the public IPs by IP address
      // IPv6 addresses are sorted after IPv4 addresses
      setPublicIPs(
        publicIPs.sort(({ ip: aIp }, { ip: bIp }) => {
          const aIsIPv6 = aIp.includes(":");
          const bIsIPv6 = bIp.includes(":");
          if (aIsIPv6 && !bIsIPv6) return 1;
          if (!aIsIPv6 && bIsIPv6) return -1;
          return aIp.localeCompare(bIp);
        }),
      );
    });
  }, [send, setPublicIPs]);

  useEffect(() => {
    refreshPublicIPs();
  }, [refreshPublicIPs]);

  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black opacity-0 animation-duration-500 dark:text-white">
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-base font-bold text-slate-900 dark:text-white">
              {m.public_ip_card_header()}
            </h3>

            <div>
              <Button
                size="XS"
                theme="light"
                type="button"
                className="text-red-500"
                text={m.public_ip_card_refresh()}
                LeadingIcon={LuRefreshCcw}
                onClick={refreshPublicIPs}
              />
            </div>
          </div>
          {publicIPs.length === 0 ? (
            <div>
              <div className="space-y-4">
                <div className="animate-pulse space-y-2">
                  <div className="h-4 w-1/4 rounded bg-slate-200 dark:bg-slate-700" />
                  <div className="h-4 w-1/3 rounded bg-slate-200 dark:bg-slate-700" />
                  <div className="h-4 w-1/2 rounded bg-slate-200 dark:bg-slate-700" />
                </div>
              </div>
            </div>
          ) : (
            <div className="flex gap-x-6 gap-y-2">
              <div className="flex-1 space-y-2">
                {publicIPs?.map(ip => (
                  <div
                    key={ip.ip}
                    className="flex justify-between border-slate-800/10 pt-2 dark:border-slate-300/20"
                  >
                    <span className="text-sm font-medium">{ip.ip}</span>
                    {ip.last_updated && <TimeAgoLabel date={new Date(ip.last_updated)} />}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </GridCard>
  );
}
