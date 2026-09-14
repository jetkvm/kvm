import { LuRefreshCcw } from "react-icons/lu";
import { useCallback, useEffect, useState } from "react";

import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import { InputFieldWithLabel } from "@components/InputField";
import { NestedSettingsGroup } from "@components/NestedSettingsGroup";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsItem } from "@components/SettingsItem";
import { NetbirdStatus } from "@hooks/stores";
import { useJsonRpc } from "@hooks/useJsonRpc";
import { m } from "@localizations/messages.js";
import notifications from "@/notifications";

const defaultManagementURL = "https://api.netbird.io:443";
const managementModeDefault = "default";
const managementModeCustom = "custom";
type ManagementMode = typeof managementModeDefault | typeof managementModeCustom;

export default function NetbirdCard() {
  const { send } = useJsonRpc();

  const [status, setStatus] = useState<NetbirdStatus | null>(null);
  const [managementURLInput, setManagementURLInput] = useState("");
  const [managementMode, setManagementMode] = useState<ManagementMode>(managementModeDefault);
  const [isSavingManagementURL, setIsSavingManagementURL] = useState(false);

  const refreshStatus = useCallback(() => {
    send("getNetbirdStatus", {}, resp => {
      if ("error" in resp) {
        setStatus(null);
        return;
      }
      const nextStatus = resp.result as NetbirdStatus;
      setStatus(nextStatus);
      const activeManagementURL = nextStatus.managementURL ?? defaultManagementURL;
      if (activeManagementURL === defaultManagementURL) {
        setManagementMode(managementModeDefault);
        setManagementURLInput("");
      } else {
        setManagementMode(managementModeCustom);
        setManagementURLInput(activeManagementURL);
      }
    });
  }, [send]);

  const saveManagementURL = useCallback(() => {
    setIsSavingManagementURL(true);
    const nextManagementURL =
      managementMode === managementModeDefault ? "" : managementURLInput.trim();

    send("setNetbirdManagementURL", { managementURL: nextManagementURL }, resp => {
      setIsSavingManagementURL(false);
      if ("error" in resp) {
        const errorMessage =
          typeof resp.error.data === "string" ? resp.error.data : resp.error.message;
        notifications.error(m.netbird_management_url_update_failed({ error: errorMessage }));
        return;
      }

      notifications.success(m.netbird_management_url_update_success());
      refreshStatus();
    });
  }, [managementMode, managementURLInput, refreshStatus, send]);

  useEffect(() => {
    refreshStatus();
  }, [refreshStatus]);

  // Don't render the card at all if NetBird is not installed
  if (status === null || !status.installed) {
    return null;
  }

  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black opacity-0 animation-duration-500 dark:text-white">
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-x-2">
              <h3 className="text-base font-bold text-slate-900 dark:text-white">
                {m.netbird_title()}
              </h3>
              <StatusBadge status={status} />
            </div>

            <div>
              <Button
                size="XS"
                theme="light"
                type="button"
                text={m.netbird_refresh()}
                LeadingIcon={LuRefreshCcw}
                onClick={refreshStatus}
              />
            </div>
          </div>

          <div className="space-y-4 border-t border-slate-800/10 pt-3 dark:border-slate-300/20">
            <SettingsItem
              size="SM"
              title={m.netbird_management_url_title()}
              description={m.netbird_management_url_description()}
            >
              <SelectMenuBasic
                size="XS"
                label=""
                value={managementMode}
                onChange={(e: React.ChangeEvent<HTMLSelectElement>) =>
                  setManagementMode(e.target.value as ManagementMode)
                }
                options={[
                  { value: managementModeDefault, label: m.netbird_management_url_default() },
                  { value: managementModeCustom, label: m.netbird_management_url_custom() },
                ]}
              />
            </SettingsItem>

            {managementMode === managementModeCustom && (
              <NestedSettingsGroup>
                <InputFieldWithLabel
                  size="SM"
                  label={m.netbird_management_url_custom_label()}
                  placeholder={m.netbird_management_url_custom_placeholder()}
                  value={managementURLInput}
                  onChange={e => setManagementURLInput(e.target.value)}
                />
                <div className="flex items-center gap-x-2">
                  <Button
                    size="SM"
                    theme="primary"
                    type="button"
                    text={isSavingManagementURL ? m.netbird_saving() : m.netbird_save()}
                    disabled={isSavingManagementURL}
                    onClick={saveManagementURL}
                  />
                </div>
              </NestedSettingsGroup>
            )}
          </div>

          {status.running && (
            <div className="flex-1 space-y-2">
              {status.fqdn && (
                <div className="flex justify-between border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.netbird_fqdn()}
                  </span>
                  <span className="text-sm font-medium">{status.fqdn}</span>
                </div>
              )}

              {status.netbirdIp && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.netbird_ipv4()}
                  </span>
                  <span className="font-mono text-[13px] font-medium">{status.netbirdIp}</span>
                </div>
              )}

              {status.netbirdIpv6 && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.netbird_ipv6()}
                  </span>
                  <span className="font-mono text-[13px] font-medium">{status.netbirdIpv6}</span>
                </div>
              )}

              {status.peersTotal !== undefined && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.netbird_peers()}
                  </span>
                  <span className="text-sm font-medium">
                    {status.peersConnected ?? 0} / {status.peersTotal}
                  </span>
                </div>
              )}
            </div>
          )}

          {status.managementError && (
            <p className="pt-2 text-sm text-red-600 dark:text-red-400">{status.managementError}</p>
          )}

          {!status.running && (
            <p className="pt-2 text-sm text-slate-600 dark:text-slate-400">
              {status.daemonStatus === "NeedsLogin" ||
              status.daemonStatus === "LoginFailed" ||
              status.daemonStatus === "SessionExpired"
                ? m.netbird_needs_login_description()
                : m.netbird_installed_not_running()}
              {status.daemonStatus &&
                m.netbird_installed_not_running_state({ state: status.daemonStatus })}
            </p>
          )}
        </div>
      </div>
    </GridCard>
  );
}

function StatusBadge({ status }: { status: NetbirdStatus }) {
  if (status.running) {
    return (
      <span className="rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-700 dark:bg-green-900/30 dark:text-green-400">
        {m.netbird_connected()}
      </span>
    );
  }
  if (
    status.daemonStatus === "NeedsLogin" ||
    status.daemonStatus === "LoginFailed" ||
    status.daemonStatus === "SessionExpired"
  ) {
    return (
      <span className="rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
        {m.netbird_needs_login()}
      </span>
    );
  }
  return (
    <span className="rounded-full bg-slate-100 px-2 py-0.5 text-xs font-medium text-slate-600 dark:bg-slate-700 dark:text-slate-400">
      {m.netbird_stopped()}
    </span>
  );
}
