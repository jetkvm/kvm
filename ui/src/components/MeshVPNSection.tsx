import { useCallback, useEffect, useState } from "react";
import { GlobeAltIcon, CheckCircleIcon, XCircleIcon } from "@heroicons/react/24/outline";

import { JsonRpcRequest, JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import {
  useMeshVPNStore,
  MeshVPNProviderInfo,
  MeshVPNProviderStatus,
  MeshVPNConfig,
  MeshVPNExitNode,
  MeshVPNVersionInfo,
} from "@hooks/stores";
import { GridCard } from "@components/Card";
import { Button } from "@components/Button";
import { InputFieldWithLabel } from "@components/InputField";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsSectionHeader } from "@components/SettingsSectionHeader";
import LoadingSpinner from "@components/LoadingSpinner";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";
import { Checkbox } from "@components/Checkbox";

// Helper to get state display label
function getStateLabel(state: string): string {
  switch (state) {
    case "not_installed":
      return m.meshvpn_not_installed();
    case "installing":
      return m.meshvpn_install_progress({ progress: "..." });
    case "stopped":
      return m.meshvpn_stopped();
    case "connecting":
      return m.meshvpn_connecting();
    case "needs_auth":
      return m.meshvpn_needs_auth();
    case "connected":
      return m.meshvpn_connected();
    case "error":
      return m.meshvpn_status_error();
    default:
      return state;
  }
}

// Helper to get status text color class
function getStatusTextColorClass(state: string | undefined): string {
  if (state === "connected") {
    return "text-green-600 dark:text-green-400";
  }
  if (state === "needs_auth") {
    return "text-amber-600 dark:text-amber-400";
  }
  return "text-slate-500 dark:text-slate-400";
}

interface ProviderCardProps {
  provider: MeshVPNProviderInfo;
  status: MeshVPNProviderStatus | null;
  config: MeshVPNConfig | null;
  exitNodes: MeshVPNExitNode[];
  installProgress: number | null;
  updateProgress: number | null;
  versionInfo: MeshVPNVersionInfo | null;
  isAuthDialogOpen: boolean;
  onInstall: (provider: string) => void;
  onUninstall: (provider: string) => void;
  onConnect: (provider: string, opts: { controlServer?: string; authKey?: string }) => void;
  onDisconnect: (provider: string) => void;
  onLogout: (provider: string) => void;
  onUpdate: (provider: string) => void;
  onSetExitNode: (provider: string, hostname: string, allowLan: boolean) => void;
  onClearExitNode: (provider: string) => void;
  onSetTUNMode: (provider: string, mode: "userspace" | "kernel") => void;
  onSetAdvertiseExitNode: (provider: string, advertise: boolean) => void;
  onAuthDialogClose: () => void;
}

function ProviderCard({
  provider,
  status,
  config,
  exitNodes,
  installProgress,
  updateProgress,
  versionInfo,
  isAuthDialogOpen,
  onInstall,
  onUninstall,
  onConnect,
  onDisconnect,
  onLogout,
  onUpdate,
  onSetExitNode,
  onClearExitNode,
  onSetTUNMode,
  onSetAdvertiseExitNode,
  onAuthDialogClose,
}: ProviderCardProps) {
  const [controlServer, setControlServer] = useState("");
  const [authKey, setAuthKey] = useState("");
  const [networkId, setNetworkId] = useState("");
  const [selectedExitNode, setSelectedExitNode] = useState("");
  const [allowLanAccess, setAllowLanAccess] = useState(false);
  const [advertiseExitNode, setAdvertiseExitNode] = useState(false);
  const [tunMode, setTunMode] = useState<"userspace" | "kernel">("userspace");
  const [actionLoading, setActionLoading] = useState(false);
  const [isExpanded, setIsExpanded] = useState(false);

  // Load config when it changes
  useEffect(() => {
    if (provider.name === "tailscale" && config?.tailscale) {
      setControlServer(config.tailscale.controlServer || "");
      setAuthKey(config.tailscale.authKey || "");
      setSelectedExitNode(config.tailscale.exitNode || "");
      setAllowLanAccess(config.tailscale.exitNodeAllowLanAccess || false);
      setAdvertiseExitNode(config.tailscale.advertiseExitNode || false);
      setTunMode(config.tailscale.tunMode || "userspace");
    }
    if (provider.name === "zerotier" && config?.zerotier) {
      setNetworkId(config.zerotier.networkId || "");
    }
  }, [provider.name, config]);

  const isInstalled = status?.installed ?? provider.installed;
  const isConnected = status?.state === "connected";
  const isConnecting = status?.state === "connecting";
  const needsAuth = status?.state === "needs_auth";

  const handleConnect = () => {
    setActionLoading(true);
    const connectOpts =
      provider.name === "zerotier"
        ? { controlServer: networkId || undefined }
        : { controlServer: controlServer || undefined, authKey: authKey || undefined };
    onConnect(provider.name, connectOpts);
    setTimeout(() => setActionLoading(false), 1000);
  };

  const handleDisconnect = () => {
    setActionLoading(true);
    onDisconnect(provider.name);
    setTimeout(() => setActionLoading(false), 1000);
  };

  const handleLogout = () => {
    if (!window.confirm(m.meshvpn_logout_confirm_description())) return;
    setActionLoading(true);
    onLogout(provider.name);
    setTimeout(() => setActionLoading(false), 1000);
  };

  const handleInstall = () => {
    setActionLoading(true);
    onInstall(provider.name);
  };

  const handleUninstall = () => {
    if (!window.confirm(m.meshvpn_uninstall_confirm_description())) return;
    setActionLoading(true);
    onUninstall(provider.name);
    setTimeout(() => setActionLoading(false), 1000);
  };

  const handleUpdate = () => {
    setActionLoading(true);
    onUpdate(provider.name);
  };

  const handleExitNodeChange = (hostname: string) => {
    setSelectedExitNode(hostname);
    if (!hostname) {
      onClearExitNode(provider.name);
    } else {
      onSetExitNode(provider.name, hostname, allowLanAccess);
    }
  };

  const handleAllowLanChange = (checked: boolean) => {
    setAllowLanAccess(checked);
    if (selectedExitNode) {
      onSetExitNode(provider.name, selectedExitNode, checked);
    }
  };

  const handleTunModeChange = (mode: "userspace" | "kernel") => {
    setTunMode(mode);
    onSetTUNMode(provider.name, mode);
  };

  const handleAdvertiseExitNodeChange = (checked: boolean) => {
    setAdvertiseExitNode(checked);
    onSetAdvertiseExitNode(provider.name, checked);
  };

  return (
    <GridCard>
      <div className="p-4">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-x-3">
            <div
              className={`flex h-10 w-10 items-center justify-center rounded-lg ${
                isConnected
                  ? "bg-green-100 dark:bg-green-900/30"
                  : "bg-slate-100 dark:bg-slate-700"
              }`}
            >
              {isConnected ? (
                <CheckCircleIcon className="h-6 w-6 text-green-600 dark:text-green-400" />
              ) : isInstalled ? (
                <GlobeAltIcon className="h-6 w-6 text-slate-500 dark:text-slate-400" />
              ) : (
                <XCircleIcon className="h-6 w-6 text-slate-400 dark:text-slate-500" />
              )}
            </div>
            <div>
              <h3 className="font-semibold text-slate-900 dark:text-white">
                {provider.displayName}
              </h3>
              <p className={`text-sm ${getStatusTextColorClass(status?.state)}`}>
                {status ? getStateLabel(status.state) : m.meshvpn_not_installed()}
              </p>
            </div>
          </div>

          {/* Quick Action Buttons */}
          <div className="flex items-center gap-x-2">
            {!isInstalled && (
              <Button
                size="SM"
                theme="primary"
                text={m.meshvpn_install_button()}
                onClick={handleInstall}
                disabled={actionLoading || installProgress !== null}
              />
            )}
            {isInstalled && !isConnected && !isConnecting && !needsAuth && (
              <Button
                size="SM"
                theme="primary"
                text={m.meshvpn_connect_button()}
                onClick={handleConnect}
                disabled={actionLoading}
              />
            )}
            {(isConnected || isConnecting || needsAuth) && (
              <Button
                size="SM"
                theme="light"
                text={m.meshvpn_disconnect_button()}
                onClick={handleDisconnect}
                disabled={actionLoading}
              />
            )}
            {isInstalled && (
              <Button
                size="XS"
                theme="light"
                text={isExpanded ? "−" : "+"}
                onClick={() => setIsExpanded(!isExpanded)}
              />
            )}
          </div>
        </div>

        {/* Status Info (when connected) */}
        {isConnected && status && (
          <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-sm">
            {status.ip && (
              <span className="text-slate-600 dark:text-slate-400">
                IP: <span className="font-mono text-slate-800 dark:text-slate-200">{status.ip}</span>
              </span>
            )}
            {status.hostname && (
              <span className="text-slate-600 dark:text-slate-400">
                Node: <span className="font-mono text-slate-800 dark:text-slate-200">{status.hostname}</span>
              </span>
            )}
          </div>
        )}

        {/* Install Progress */}
        {installProgress !== null && (
          <div className="mt-3 rounded bg-blue-50 p-2 dark:bg-blue-900/20">
            <div className="flex items-center gap-x-2">
              <LoadingSpinner className="h-4 w-4 text-blue-500" />
              <span className="text-sm text-blue-700 dark:text-blue-300">
                {m.meshvpn_install_progress({ progress: Math.round(installProgress * 100).toString() })}
              </span>
            </div>
          </div>
        )}

        {/* Update Progress */}
        {updateProgress !== null && (
          <div className="mt-3 rounded bg-blue-50 p-2 dark:bg-blue-900/20">
            <div className="flex items-center gap-x-2">
              <LoadingSpinner className="h-4 w-4 text-blue-500" />
              <span className="text-sm text-blue-700 dark:text-blue-300">
                {m.meshvpn_update_progress({ progress: Math.round(updateProgress * 100).toString() })}
              </span>
            </div>
          </div>
        )}

        {/* Auth Dialog for Tailscale */}
        {isAuthDialogOpen && status?.authUrl && provider.name === "tailscale" && (
          <div className="mt-3 rounded bg-amber-50 p-3 dark:bg-amber-900/20">
            <div className="space-y-2">
              <p className="text-sm font-medium text-amber-800 dark:text-amber-200">
                {m.meshvpn_auth_dialog_title()}
              </p>
              <p className="text-sm text-amber-700 dark:text-amber-300">
                {m.meshvpn_auth_dialog_description()}
              </p>
              <div className="rounded bg-white/50 p-2 font-mono text-xs break-all text-slate-800 dark:bg-slate-800/50 dark:text-slate-200">
                {status.authUrl}
              </div>
              <div className="flex gap-x-2">
                <Button
                  size="XS"
                  theme="primary"
                  text={m.meshvpn_auth_dialog_copy()}
                  onClick={() => {
                    navigator.clipboard.writeText(status.authUrl!);
                    notifications.success("URL copied");
                  }}
                />
                <Button
                  size="XS"
                  theme="light"
                  text={m.meshvpn_auth_dialog_open()}
                  onClick={() => window.open(status.authUrl, "_blank")}
                />
                <Button size="XS" theme="light" text={m.cancel()} onClick={onAuthDialogClose} />
              </div>
            </div>
          </div>
        )}

        {/* ZeroTier Needs Auth */}
        {needsAuth && provider.name === "zerotier" && (
          <div className="mt-3 rounded bg-amber-50 p-3 dark:bg-amber-900/20">
            <p className="text-sm text-amber-700 dark:text-amber-300">
              {m.meshvpn_zerotier_needs_auth()}
            </p>
            {status?.hostname && (
              <p className="mt-1 text-sm text-amber-700 dark:text-amber-300">
                {m.meshvpn_zerotier_node_id()}:{" "}
                <span className="font-mono font-bold">{status.hostname}</span>
              </p>
            )}
          </div>
        )}

        {/* Error Message */}
        {status?.errorMessage && (
          <div className="mt-3 rounded bg-red-50 p-2 text-sm text-red-600 dark:bg-red-900/20 dark:text-red-400">
            {status.errorMessage}
          </div>
        )}

        {/* Expanded Settings */}
        {isExpanded && isInstalled && (
          <div className="mt-4 space-y-4 border-t border-slate-200 pt-4 dark:border-slate-700">
            {/* Tailscale Configuration */}
            {provider.name === "tailscale" && !isConnected && (
              <>
                <InputFieldWithLabel
                  size="SM"
                  label={m.meshvpn_control_server()}
                  description={m.meshvpn_control_server_description()}
                  value={controlServer}
                  onChange={e => setControlServer(e.target.value)}
                  placeholder={m.meshvpn_control_server_placeholder()}
                  disabled={actionLoading || isConnecting}
                />
                <InputFieldWithLabel
                  size="SM"
                  label={m.meshvpn_auth_key()}
                  description={m.meshvpn_auth_key_description()}
                  value={authKey}
                  onChange={e => setAuthKey(e.target.value)}
                  type="password"
                  disabled={actionLoading || isConnecting}
                />
                <SettingsItem
                  title={m.meshvpn_tun_mode_title()}
                  description={m.meshvpn_tun_mode_description()}
                >
                  <SelectMenuBasic
                    size="SM"
                    value={tunMode}
                    onChange={e => handleTunModeChange(e.target.value as "userspace" | "kernel")}
                    disabled={actionLoading || isConnecting}
                    options={[
                      { value: "userspace", label: m.meshvpn_tun_mode_userspace() },
                      { value: "kernel", label: m.meshvpn_tun_mode_kernel() },
                    ]}
                  />
                </SettingsItem>
              </>
            )}

            {/* ZeroTier Configuration */}
            {provider.name === "zerotier" && !isConnected && (
              <InputFieldWithLabel
                size="SM"
                label={m.meshvpn_zerotier_network_id()}
                description={m.meshvpn_zerotier_network_id_description()}
                value={networkId}
                onChange={e => setNetworkId(e.target.value)}
                placeholder={m.meshvpn_zerotier_network_id_placeholder()}
                disabled={actionLoading || isConnecting}
              />
            )}

            {/* Exit Nodes (Tailscale only, when connected) */}
            {isConnected && provider.supportsExitNodes && exitNodes.length > 0 && (
              <>
                <SettingsItem
                  title={m.meshvpn_exit_node_title()}
                  description={m.meshvpn_exit_node_description()}
                >
                  <SelectMenuBasic
                    size="SM"
                    value={selectedExitNode}
                    onChange={e => handleExitNodeChange(e.target.value)}
                    options={[
                      { value: "", label: m.meshvpn_exit_node_none() },
                      ...exitNodes
                        .filter(n => n.online)
                        .map(n => ({ value: n.hostName, label: n.name || n.hostName })),
                    ]}
                  />
                </SettingsItem>
                {selectedExitNode && (
                  <div className="flex items-center gap-x-2">
                    <Checkbox
                      checked={allowLanAccess}
                      onChange={e => handleAllowLanChange(e.target.checked)}
                    />
                    <span className="text-sm text-slate-700 dark:text-slate-300">
                      {m.meshvpn_exit_node_allow_lan()}
                    </span>
                  </div>
                )}
              </>
            )}

            {/* Advertise as Exit Node (Tailscale only, when connected) */}
            {isConnected && provider.supportsExitNodes && (
              <div className="flex items-center gap-x-2">
                <Checkbox
                  checked={advertiseExitNode}
                  onChange={e => handleAdvertiseExitNodeChange(e.target.checked)}
                />
                <div>
                  <span className="text-sm font-medium text-slate-900 dark:text-white">
                    {m.meshvpn_advertise_exit_node_title()}
                  </span>
                  <p className="text-xs text-slate-600 dark:text-slate-400">
                    {m.meshvpn_advertise_exit_node_description()}
                  </p>
                </div>
              </div>
            )}

            {/* Version Info */}
            {versionInfo && versionInfo.latestVersion && (
              <div
                className={`rounded p-3 ${
                  versionInfo.updateAvailable
                    ? "bg-amber-50 dark:bg-amber-900/20"
                    : "bg-slate-50 dark:bg-slate-800"
                }`}
              >
                <div className="flex items-center justify-between">
                  <div className="space-y-1 text-sm">
                    <div className="text-slate-600 dark:text-slate-400">
                      {m.meshvpn_current_version()}:{" "}
                      <span className="font-mono">{versionInfo.currentVersion || status?.version || "?"}</span>
                    </div>
                    <div className="text-slate-600 dark:text-slate-400">
                      {m.meshvpn_latest_version()}:{" "}
                      <span className="font-mono">{versionInfo.latestVersion}</span>
                    </div>
                  </div>
                  {versionInfo.updateAvailable && (
                    <Button
                      size="SM"
                      theme="primary"
                      text={m.meshvpn_update_button()}
                      onClick={handleUpdate}
                      disabled={actionLoading || isConnecting}
                    />
                  )}
                </div>
              </div>
            )}

            {/* Advanced Actions */}
            <div className="flex flex-wrap gap-2 pt-2">
              {isConnected && (
                <Button
                  size="SM"
                  theme="light"
                  text={m.meshvpn_logout_button()}
                  onClick={handleLogout}
                  disabled={actionLoading}
                />
              )}
              {!isConnected && !isConnecting && (
                <Button
                  size="SM"
                  theme="light"
                  text={m.meshvpn_uninstall_button()}
                  onClick={handleUninstall}
                  disabled={actionLoading}
                  className="text-red-600"
                />
              )}
            </div>
          </div>
        )}
      </div>
    </GridCard>
  );
}

export function MeshVPNSection() {
  const {
    providers,
    providerStatuses,
    config,
    providerExitNodes,
    providerInstallProgress,
    providerUpdateProgress,
    authDialogProvider,
    providerVersionInfo,
    setProviders,
    setProviderStatus,
    setConfig,
    setProviderExitNodes,
    setProviderInstallProgress,
    setProviderUpdateProgress,
    setAuthDialogProvider,
    setProviderVersionInfo,
  } = useMeshVPNStore();

  const handleRpcEvent = useCallback(
    (req: JsonRpcRequest) => {
      if (req.method === "meshVPNState") {
        const params = req.params as MeshVPNProviderStatus & { provider?: string };
        const providerName = params.provider || "tailscale";
        setProviderStatus(providerName, params);
        if (params.authUrl && params.state === "needs_auth") {
          setAuthDialogProvider(providerName);
        }
      } else if (req.method === "meshVPNInstallProgress") {
        const { provider, progress } = req.params as { provider: string; progress: number };
        setProviderInstallProgress(provider, progress);
      } else if (req.method === "meshVPNUpdateProgress") {
        const { provider, progress } = req.params as { provider: string; progress: number };
        setProviderUpdateProgress(provider, progress);
      }
    },
    [setProviderStatus, setAuthDialogProvider, setProviderInstallProgress, setProviderUpdateProgress],
  );

  const { send } = useJsonRpc(handleRpcEvent);

  // Fetch providers
  const fetchProviders = useCallback(() => {
    send("getMeshVPNProviders", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.meshvpn_get_providers_error({ error: String(resp.error.message) }));
        return;
      }
      setProviders(resp.result as MeshVPNProviderInfo[]);
    });
  }, [send, setProviders]);

  // Fetch status for a specific provider
  const fetchProviderStatus = useCallback(
    (providerName: string) => {
      send("getMeshVPNStatus", { params: { provider: providerName } }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          return;
        }
        setProviderStatus(providerName, resp.result as MeshVPNProviderStatus);
      });
    },
    [send, setProviderStatus],
  );

  // Fetch config
  const fetchConfig = useCallback(() => {
    send("getMeshVPNConfig", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        return;
      }
      setConfig(resp.result as MeshVPNConfig);
    });
  }, [send, setConfig]);

  // Fetch exit nodes for a provider
  const fetchExitNodes = useCallback(
    (providerName: string) => {
      send("meshVPNGetExitNodes", {}, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          return;
        }
        setProviderExitNodes(providerName, resp.result as MeshVPNExitNode[]);
      });
    },
    [send, setProviderExitNodes],
  );

  // Fetch version info for a provider
  const fetchVersionInfo = useCallback(
    (providerName: string) => {
      send("meshVPNGetVersionInfo", { params: { provider: providerName } }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          return;
        }
        setProviderVersionInfo(providerName, resp.result as MeshVPNVersionInfo);
      });
    },
    [send, setProviderVersionInfo],
  );

  // Initial load
  useEffect(() => {
    fetchProviders();
    fetchConfig();
  }, [fetchProviders, fetchConfig]);

  // Fetch status for all providers when providers list changes
  useEffect(() => {
    providers.forEach(p => {
      fetchProviderStatus(p.name);
      if (p.installed) {
        fetchVersionInfo(p.name);
      }
    });
  }, [providers, fetchProviderStatus, fetchVersionInfo]);

  // Fetch exit nodes when a provider is connected
  useEffect(() => {
    providers.forEach(p => {
      const status = providerStatuses[p.name];
      if (status?.state === "connected" && p.supportsExitNodes) {
        fetchExitNodes(p.name);
      }
    });
  }, [providers, providerStatuses, fetchExitNodes]);

  // Action handlers
  const handleInstall = (providerName: string) => {
    setProviderInstallProgress(providerName, 0);
    send("meshVPNInstall", { provider: providerName }, (resp: JsonRpcResponse) => {
      setProviderInstallProgress(providerName, null);
      if ("error" in resp) {
        notifications.error(m.meshvpn_install_error({ error: String(resp.error.message) }));
        return;
      }
      fetchProviderStatus(providerName);
      fetchProviders();
    });
  };

  const handleUninstall = (providerName: string) => {
    send("meshVPNUninstall", { provider: providerName }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.meshvpn_uninstall_error({ error: String(resp.error.message) }));
        return;
      }
      fetchProviderStatus(providerName);
      fetchProviders();
    });
  };

  const handleConnect = (providerName: string, opts: { controlServer?: string; authKey?: string }) => {
    send(
      "meshVPNConnect",
      { params: { provider: providerName, controlServer: opts.controlServer, authKey: opts.authKey } },
      (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(m.meshvpn_connect_error({ error: String(resp.error.message) }));
          return;
        }
        const result = resp.result as { success: boolean; authUrl?: string };
        if (result.authUrl) {
          setProviderStatus(providerName, {
            ...(providerStatuses[providerName] || { state: "needs_auth", installed: true, running: false }),
            authUrl: result.authUrl,
            state: "needs_auth",
          });
          setAuthDialogProvider(providerName);
        }
        fetchProviderStatus(providerName);
      },
    );
  };

  const handleDisconnect = (providerName: string) => {
    send("meshVPNDisconnect", { params: { provider: providerName } }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.meshvpn_disconnect_error({ error: String(resp.error.message) }));
        return;
      }
      setAuthDialogProvider(null);
      const newStatus = resp.result as MeshVPNProviderStatus;
      setProviderStatus(providerName, newStatus);
      fetchProviders();
    });
  };

  const handleLogout = (providerName: string) => {
    send("meshVPNLogout", { params: { provider: providerName } }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.meshvpn_logout_error({ error: String(resp.error.message) }));
        return;
      }
      setAuthDialogProvider(null);
      fetchProviderStatus(providerName);
      fetchProviders();
    });
  };

  const handleUpdate = (providerName: string) => {
    setProviderUpdateProgress(providerName, 0);
    send("meshVPNUpdate", { params: { provider: providerName } }, (resp: JsonRpcResponse) => {
      setProviderUpdateProgress(providerName, null);
      if ("error" in resp) {
        notifications.error(m.meshvpn_update_error({ error: String(resp.error.message) }));
        return;
      }
      notifications.success(m.meshvpn_update_success());
      fetchProviderStatus(providerName);
      fetchVersionInfo(providerName);
    });
  };

  const handleSetExitNode = (providerName: string, hostname: string, allowLan: boolean) => {
    send("meshVPNSetExitNode", { params: { hostname, allowLan } }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.meshvpn_exit_node_set_error({ error: String(resp.error.message) }));
        return;
      }
      fetchProviderStatus(providerName);
    });
  };

  const handleClearExitNode = (providerName: string) => {
    send("meshVPNClearExitNode", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.meshvpn_exit_node_set_error({ error: String(resp.error.message) }));
        return;
      }
      fetchProviderStatus(providerName);
    });
  };

  const handleSetTUNMode = (providerName: string, mode: "userspace" | "kernel") => {
    send("meshVPNSetTUNMode", { params: { provider: providerName, mode } }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.meshvpn_tun_mode_error({ error: String(resp.error.message) }));
        fetchConfig();
        return;
      }
      notifications.success(m.meshvpn_tun_mode_updated());
      fetchProviderStatus(providerName);
    });
  };

  const handleSetAdvertiseExitNode = (providerName: string, advertise: boolean) => {
    send(
      "meshVPNSetAdvertiseExitNode",
      { params: { provider: providerName, advertise } },
      (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(m.meshvpn_advertise_exit_node_error({ error: String(resp.error.message) }));
          fetchConfig();
          return;
        }
        notifications.success(
          advertise ? m.meshvpn_advertise_exit_node_enabled() : m.meshvpn_advertise_exit_node_disabled(),
        );
      },
    );
  };

  return (
    <div className="space-y-4">
      <SettingsSectionHeader title={m.meshvpn_title()} description={m.meshvpn_description()} />

      {/* Provider Cards */}
      <div className="space-y-3">
        {providers.map(provider => (
          <ProviderCard
            key={provider.name}
            provider={provider}
            status={providerStatuses[provider.name] || null}
            config={config}
            exitNodes={providerExitNodes[provider.name] || []}
            installProgress={providerInstallProgress[provider.name] ?? null}
            updateProgress={providerUpdateProgress[provider.name] ?? null}
            versionInfo={providerVersionInfo[provider.name] || null}
            isAuthDialogOpen={authDialogProvider === provider.name}
            onInstall={handleInstall}
            onUninstall={handleUninstall}
            onConnect={handleConnect}
            onDisconnect={handleDisconnect}
            onLogout={handleLogout}
            onUpdate={handleUpdate}
            onSetExitNode={handleSetExitNode}
            onClearExitNode={handleClearExitNode}
            onSetTUNMode={handleSetTUNMode}
            onSetAdvertiseExitNode={handleSetAdvertiseExitNode}
            onAuthDialogClose={() => setAuthDialogProvider(null)}
          />
        ))}
      </div>

      {/* Empty State */}
      {providers.length === 0 && (
        <div className="rounded-lg border border-dashed border-slate-300 p-8 text-center dark:border-slate-600">
          <GlobeAltIcon className="mx-auto h-12 w-12 text-slate-400" />
          <p className="mt-2 text-sm text-slate-600 dark:text-slate-400">
            No VPN providers available
          </p>
        </div>
      )}
    </div>
  );
}
