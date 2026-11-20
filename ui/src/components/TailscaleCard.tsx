import { LuRefreshCcw } from "react-icons/lu";
import { useCallback, useEffect, useState } from "react";

import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import Checkbox from "@components/Checkbox";
import { InputFieldWithLabel } from "@components/InputField";
import { NestedSettingsGroup } from "@components/NestedSettingsGroup";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsItem } from "@components/SettingsItem";
import { TailscaleStatus } from "@hooks/stores";
import { JsonRpcRequest, JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { m } from "@localizations/messages.js";
import notifications from "@/notifications";

const defaultControlURL = "https://controlplane.tailscale.com";
const controlServerModeDefault = "default";
const controlServerModeCustom = "custom";
type ControlServerMode = typeof controlServerModeDefault | typeof controlServerModeCustom;

function rpcErrorMessage(resp: JsonRpcResponse) {
  if (!("error" in resp)) return null;
  return typeof resp.error.data === "string" ? resp.error.data : resp.error.message;
}

export default function TailscaleCard() {
  const [status, setStatus] = useState<TailscaleStatus | null>(null);
  const [controlURLInput, setControlURLInput] = useState("");
  const [controlServerMode, setControlServerMode] =
    useState<ControlServerMode>(controlServerModeDefault);
  const [isSavingControlURL, setIsSavingControlURL] = useState(false);
  const [authKey, setAuthKey] = useState("");
  const [isBusy, setIsBusy] = useState(false);

  const onStatusEvent = useCallback((payload: JsonRpcRequest) => {
    if (payload.method === "tailscaleStatus") setStatus(payload.params as TailscaleStatus);
  }, []);
  const { send } = useJsonRpc(onStatusEvent);

  const refreshStatus = useCallback(() => {
    send("getTailscaleStatus", {}, resp => {
      setStatus("error" in resp ? null : (resp.result as TailscaleStatus));
    });
  }, [send]);

  const activeControlURL = status?.controlURL ?? defaultControlURL;
  useEffect(() => {
    if (activeControlURL === defaultControlURL) {
      setControlServerMode(controlServerModeDefault);
      setControlURLInput("");
    } else {
      setControlServerMode(controlServerModeCustom);
      setControlURLInput(activeControlURL);
    }
  }, [activeControlURL]);

  const saveControlURL = useCallback(() => {
    setIsSavingControlURL(true);
    const nextControlURL =
      controlServerMode === controlServerModeDefault ? "" : controlURLInput.trim();

    send("setTailscaleControlURL", { controlURL: nextControlURL }, resp => {
      setIsSavingControlURL(false);
      const error = rpcErrorMessage(resp);
      if (error) {
        notifications.error(m.tailscale_control_server_update_failed({ error }));
        return;
      }

      notifications.success(m.tailscale_control_server_update_success());
      refreshStatus();
    });
  }, [controlServerMode, controlURLInput, refreshStatus, send]);

  const runAction = useCallback(
    (method: string, params: object) => {
      setIsBusy(true);
      send(method, params, resp => {
        setIsBusy(false);
        const error = rpcErrorMessage(resp);
        if (error) notifications.error(m.tailscale_update_failed({ error }));
        refreshStatus();
      });
    },
    [refreshStatus, send],
  );

  useEffect(() => {
    refreshStatus();
  }, [refreshStatus]);

  // Don't render the card at all if Tailscale is not installed
  if (status === null || !status.installed) {
    return null;
  }

  const ipv4 = status.self?.tailscaleIPs?.find(ip => !ip.includes(":"));
  const ipv6 = status.self?.tailscaleIPs?.find(ip => ip.includes(":"));
  const needsLogin = status.backendState === "NeedsLogin";
  const needsApproval = status.backendState === "NeedsMachineAuth";
  const active = !status.builtin || status.enabled;

  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black opacity-0 animation-duration-500 dark:text-white">
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-x-2">
              <h3 className="text-base font-bold text-slate-900 dark:text-white">
                {m.tailscale_title()}
              </h3>
              {active && <StatusBadge status={status} />}
            </div>

            {!status.builtin && (
              <div>
                <Button
                  size="XS"
                  theme="light"
                  type="button"
                  text={m.tailscale_refresh()}
                  LeadingIcon={LuRefreshCcw}
                  onClick={refreshStatus}
                />
              </div>
            )}
          </div>

          <div className="space-y-4 border-t border-slate-800/10 pt-3 dark:border-slate-300/20">
            {status.builtin ? (
              <SettingsItem
                size="SM"
                title={m.tailscale_enable_title()}
                description={m.tailscale_enable_description()}
              >
                <Checkbox
                  aria-label={m.tailscale_enable_title()}
                  checked={status.enabled}
                  disabled={isBusy}
                  onChange={e => runAction("setTailscaleEnabled", { enabled: e.target.checked })}
                />
              </SettingsItem>
            ) : (
              status.builtin === false && (
                <p className="text-sm text-slate-600 dark:text-slate-400">
                  {m.tailscale_external_description()}
                </p>
              )
            )}

            <SettingsItem
              size="SM"
              title={m.tailscale_control_server_title()}
              description={m.tailscale_control_server_description()}
            >
              <SelectMenuBasic
                size="XS"
                label=""
                value={controlServerMode}
                onChange={(e: React.ChangeEvent<HTMLSelectElement>) =>
                  setControlServerMode(e.target.value as ControlServerMode)
                }
                options={[
                  { value: controlServerModeDefault, label: m.tailscale_control_server_default() },
                  { value: controlServerModeCustom, label: m.tailscale_control_server_custom() },
                ]}
              />
            </SettingsItem>

            {(controlServerMode === controlServerModeCustom ||
              activeControlURL !== defaultControlURL) && (
              <NestedSettingsGroup>
                {controlServerMode === controlServerModeCustom && (
                  <InputFieldWithLabel
                    size="SM"
                    label={m.tailscale_control_server_custom_url_label()}
                    placeholder={m.tailscale_control_server_custom_url_placeholder()}
                    value={controlURLInput}
                    onChange={e => setControlURLInput(e.target.value)}
                  />
                )}
                <div className="flex items-center gap-x-2">
                  <Button
                    size="SM"
                    theme="primary"
                    type="button"
                    text={isSavingControlURL ? m.tailscale_saving() : m.tailscale_save()}
                    disabled={isSavingControlURL}
                    onClick={saveControlURL}
                  />
                </div>
              </NestedSettingsGroup>
            )}
          </div>

          {status.error && (
            <p className="pt-2 text-sm text-red-600 dark:text-red-400">
              {m.tailscale_start_failed({ error: status.error })}
            </p>
          )}

          {status.running && status.self && (
            <div className="flex-1 space-y-2">
              {status.self.hostName && (
                <div className="flex justify-between border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.tailscale_hostname()}
                  </span>
                  <span className="text-sm font-medium">{status.self.hostName}</span>
                </div>
              )}

              {status.tailnet && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.tailscale_tailnet()}
                  </span>
                  <span className="text-sm font-medium">{status.tailnet}</span>
                </div>
              )}

              {ipv4 && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.tailscale_ipv4()}
                  </span>
                  <span className="font-mono text-[13px] font-medium">{ipv4}</span>
                </div>
              )}

              {ipv6 && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.tailscale_ipv6()}
                  </span>
                  <span className="font-mono text-[13px] font-medium">{ipv6}</span>
                </div>
              )}

              {status.self.dnsName && (
                <div className="flex justify-between border-t border-slate-800/10 pt-2 dark:border-slate-300/20">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    {m.tailscale_dns_name()}
                  </span>
                  <span className="text-sm font-medium">
                    {status.self.dnsName.replace(/\.$/, "")}
                  </span>
                </div>
              )}
            </div>
          )}

          {needsLogin && (
            <div className="space-y-2 pt-2">
              <p className="text-sm text-slate-600 dark:text-slate-400">
                {m.tailscale_auth_description()}
              </p>
              {status.authURL && (
                <a
                  href={status.authURL}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-block text-sm font-medium break-all text-blue-500 hover:text-blue-600 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  {status.authURL}
                </a>
              )}
              {status.builtin && !status.authURL && (
                <Button
                  size="SM"
                  theme="light"
                  type="button"
                  text={m.tailscale_get_login_link()}
                  disabled={isBusy}
                  onClick={() => runAction("loginTailscale", { authKey: "" })}
                />
              )}
              {status.builtin && (
                <div className="flex items-end gap-x-2">
                  <InputFieldWithLabel
                    id="tailscale-auth-key"
                    size="SM"
                    type="password"
                    autoComplete="off"
                    label={m.tailscale_auth_key_label()}
                    placeholder="tskey-auth-..."
                    value={authKey}
                    onChange={e => setAuthKey(e.target.value)}
                  />
                  <Button
                    size="SM"
                    theme="primary"
                    type="button"
                    text={m.tailscale_connect()}
                    disabled={isBusy || authKey.trim() === ""}
                    onClick={() => {
                      runAction("loginTailscale", { authKey });
                      setAuthKey("");
                    }}
                  />
                </div>
              )}
            </div>
          )}

          {needsApproval && (
            <p className="pt-2 text-sm text-slate-600 dark:text-slate-400">
              {m.tailscale_needs_approval_description()}
            </p>
          )}

          {!status.builtin && !status.running && !needsLogin && (
            <p className="pt-2 text-sm text-slate-600 dark:text-slate-400">
              {m.tailscale_installed_not_running()}
              {status.backendState &&
                m.tailscale_installed_not_running_state({ state: status.backendState })}
            </p>
          )}

          {status.builtin && (status.running || needsApproval) && (
            <div className="pt-2">
              <Button
                size="SM"
                theme="light"
                type="button"
                text={m.log_out()}
                disabled={isBusy}
                onClick={() => runAction("logoutTailscale", {})}
              />
            </div>
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
        {m.tailscale_connected()}
      </span>
    );
  }
  if (status.backendState === "NeedsLogin" || status.backendState === "NeedsMachineAuth") {
    return (
      <span className="rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
        {status.backendState === "NeedsLogin"
          ? m.tailscale_needs_login()
          : m.tailscale_needs_approval()}
      </span>
    );
  }
  return (
    <span className="rounded-full bg-slate-100 px-2 py-0.5 text-xs font-medium text-slate-600 dark:bg-slate-700 dark:text-slate-400">
      {status.builtin && !status.error ? m.tailscale_connecting() : m.tailscale_stopped()}
    </span>
  );
}
