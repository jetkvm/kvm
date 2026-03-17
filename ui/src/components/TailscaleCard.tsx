import { LuRefreshCcw } from "react-icons/lu";
import { useCallback, useEffect, useState } from "react";

import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import { TailscaleStatus } from "@hooks/stores";
import { useJsonRpc } from "@hooks/useJsonRpc";

export default function TailscaleCard() {
  const { send } = useJsonRpc();

  const [status, setStatus] = useState<TailscaleStatus | null>(null);

  const refreshStatus = useCallback(() => {
    send("getTailscaleStatus", {}, resp => {
      if ("error" in resp) {
        setStatus(null);
        return;
      }
      setStatus(resp.result as TailscaleStatus);
    });
  }, [send]);

  useEffect(() => {
    refreshStatus();
  }, [refreshStatus]);

  // Don't render the card at all if Tailscale is not installed
  if (status === null || !status.installed) {
    return null;
  }

  const ipv4 = status.self?.tailscaleIPs?.find(ip => !ip.includes(":"));
  const ipv6 = status.self?.tailscaleIPs?.find(ip => ip.includes(":"));

  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black opacity-0 animation-duration-500 dark:text-white">
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-x-2">
              <h3 className="text-base font-bold text-slate-900 dark:text-white">Tailscale</h3>
              <StatusBadge status={status} />
            </div>

            <div>
              <Button
                size="XS"
                theme="light"
                type="button"
                text="Refresh"
                LeadingIcon={LuRefreshCcw}
                onClick={refreshStatus}
              />
            </div>
          </div>

          {status.running && status.self && (
            <div className="flex-1 space-y-2">
              {status.self.hostName && (
                <div className="flex justify-between border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">Hostname</span>
                  <span className="text-sm font-medium">{status.self.hostName}</span>
                </div>
              )}

              {ipv4 && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">IPv4</span>
                  <span className="font-mono text-[13px] font-medium">{ipv4}</span>
                </div>
              )}

              {ipv6 && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">IPv6</span>
                  <span className="font-mono text-[13px] font-medium">{ipv6}</span>
                </div>
              )}

              {status.self.dnsName && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">DNS Name</span>
                  <span className="text-sm font-medium">
                    {status.self.dnsName.replace(/\.$/, "")}
                  </span>
                </div>
              )}
            </div>
          )}

          {status.backendState === "NeedsLogin" && status.authURL && (
            <div className="space-y-2 pt-2">
              <p className="text-sm text-slate-600 dark:text-slate-400">
                Tailscale requires authentication. Open the link below to log in.
              </p>
              <a
                href={status.authURL}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-block text-sm font-medium text-blue-500 hover:text-blue-600 dark:text-blue-400 dark:hover:text-blue-300"
              >
                {status.authURL}
              </a>
            </div>
          )}

          {!status.running && status.backendState !== "NeedsLogin" && (
            <p className="pt-2 text-sm text-slate-600 dark:text-slate-400">
              Tailscale is installed but not running.
              {status.backendState && ` State: ${status.backendState}`}
            </p>
          )}
        </div>
      </div>
    </GridCard>
  );
}

function StatusBadge({ status }: { status: TailscaleStatus }) {
  if (status.running) {
    return (
      <span className="rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-700 dark:bg-green-900/30 dark:text-green-400">
        Connected
      </span>
    );
  }
  if (status.backendState === "NeedsLogin") {
    return (
      <span className="rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
        Needs Login
      </span>
    );
  }
  return (
    <span className="rounded-full bg-slate-100 px-2 py-0.5 text-xs font-medium text-slate-600 dark:bg-slate-700 dark:text-slate-400">
      Stopped
    </span>
  );
}
