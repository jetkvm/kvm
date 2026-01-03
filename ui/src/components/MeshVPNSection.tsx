import { useCallback, useEffect, useState } from "react";
import { GlobeAltIcon } from "@heroicons/react/24/outline";

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
import { NestedSettingsGroup } from "@components/NestedSettingsGroup";
import LoadingSpinner from "@components/LoadingSpinner";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";
import { Checkbox } from "@components/Checkbox";

interface MeshVPNAuthDialogProps {
  authUrl: string;
  onClose: () => void;
}

function MeshVPNAuthDialog({ authUrl, onClose }: MeshVPNAuthDialogProps) {
  const copyToClipboard = () => {
    navigator.clipboard.writeText(authUrl);
    notifications.success("URL copied to clipboard");
  };

  // Auto-open the auth URL in a new tab
  useEffect(() => {
    if (authUrl) {
      window.open(authUrl, "_blank");
    }
  }, [authUrl]);

  return (
    <GridCard>
      <div className="space-y-4 p-4">
        <div className="flex items-start gap-x-3">
          <GlobeAltIcon className="mt-0.5 h-6 w-6 shrink-0 text-blue-600 dark:text-blue-500" />
          <div className="space-y-2">
            <h3 className="text-base font-bold text-slate-900 dark:text-white">
              {m.meshvpn_auth_dialog_title()}
            </h3>
            <p className="text-sm text-slate-700 dark:text-slate-300">
              {m.meshvpn_auth_dialog_description()}
            </p>
            <div className="rounded bg-slate-100 p-2 font-mono text-xs break-all text-slate-800 dark:bg-slate-700 dark:text-slate-200">
              {authUrl}
            </div>
            <div className="flex items-center gap-x-2 pt-2">
              <Button
                size="SM"
                theme="primary"
                text={m.meshvpn_auth_dialog_copy()}
                onClick={copyToClipboard}
              />
              <Button
                size="SM"
                theme="light"
                text={m.meshvpn_auth_dialog_open()}
                onClick={() => window.open(authUrl, "_blank")}
              />
              <Button size="SM" theme="light" text={m.cancel()} onClick={onClose} />
            </div>
          </div>
        </div>
      </div>
    </GridCard>
  );
}

function MeshVPNStatusCard({ status }: { status: MeshVPNProviderStatus }) {
  const getStateLabel = (state: string) => {
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
  };

  const getStateColor = (state: string) => {
    switch (state) {
      case "connected":
        return "text-green-600 dark:text-green-400";
      case "connecting":
      case "needs_auth":
        return "text-yellow-600 dark:text-yellow-400";
      case "error":
        return "text-red-600 dark:text-red-400";
      default:
        return "text-slate-600 dark:text-slate-400";
    }
  };

  return (
    <GridCard>
      <div className="space-y-3 p-4">
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium text-slate-700 dark:text-slate-300">Status</span>
          <span className={`text-sm font-semibold ${getStateColor(status.state)}`}>
            {getStateLabel(status.state)}
          </span>
        </div>
        {status.ip && (
          <div className="flex items-center justify-between">
            <span className="text-sm text-slate-600 dark:text-slate-400">
              {m.meshvpn_ip_address()}
            </span>
            <span className="font-mono text-sm text-slate-800 dark:text-slate-200">
              {status.ip}
            </span>
          </div>
        )}
        {status.hostname && (
          <div className="flex items-center justify-between">
            <span className="text-sm text-slate-600 dark:text-slate-400">
              {m.meshvpn_hostname()}
            </span>
            <span className="font-mono text-sm text-slate-800 dark:text-slate-200">
              {status.hostname}
            </span>
          </div>
        )}
        {status.version && (
          <div className="flex items-center justify-between">
            <span className="text-sm text-slate-600 dark:text-slate-400">
              {m.meshvpn_version()}
            </span>
            <span className="text-sm text-slate-800 dark:text-slate-200">{status.version}</span>
          </div>
        )}
        {status.errorMessage && (
          <div className="rounded bg-red-50 p-2 text-sm text-red-600 dark:bg-red-900/20 dark:text-red-400">
            {status.errorMessage}
          </div>
        )}
      </div>
    </GridCard>
  );
}

export function MeshVPNSection() {
  const {
    providers,
    status,
    exitNodes,
    installProgress,
    updateProgress,
    isAuthDialogOpen,
    versionInfo,
    setProviders,
    setStatus,
    setConfig,
    setExitNodes,
    setInstallProgress,
    setUpdateProgress,
    setAuthDialogOpen,
    setVersionInfo,
  } = useMeshVPNStore();

  // Handle RPC events from the server
  const handleRpcEvent = useCallback(
    (req: JsonRpcRequest) => {
      if (req.method === "meshVPNState") {
        const newStatus = req.params as MeshVPNProviderStatus;
        setStatus(newStatus);
        if (newStatus.authUrl && newStatus.state === "needs_auth") {
          setAuthDialogOpen(true);
        }
      } else if (req.method === "meshVPNInstallProgress") {
        const { progress } = req.params as { provider: string; progress: number };
        setInstallProgress(progress);
      } else if (req.method === "meshVPNUpdateProgress") {
        const { progress } = req.params as { provider: string; progress: number };
        setUpdateProgress(progress);
      }
    },
    [setStatus, setAuthDialogOpen, setInstallProgress, setUpdateProgress],
  );

  const { send } = useJsonRpc(handleRpcEvent);

  const [selectedProvider, setSelectedProvider] = useState<string>("");
  const [controlServer, setControlServer] = useState("");
  const [authKey, setAuthKey] = useState("");
  const [selectedExitNode, setSelectedExitNode] = useState("");
  const [allowLanAccess, setAllowLanAccess] = useState(false);
  const [advertiseExitNode, setAdvertiseExitNode] = useState(false);
  const [tunMode, setTunMode] = useState<"userspace" | "kernel">("userspace");
  const [actionLoading, setActionLoading] = useState(false);

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

  // Fetch status for the selected provider
  const fetchStatus = useCallback(() => {
    if (!selectedProvider) {
      setStatus(null);
      return;
    }
    send(
      "getMeshVPNStatus",
      { params: { provider: selectedProvider } },
      (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          console.error("Failed to get VPN status:", resp.error);
          return;
        }
        const newStatus = resp.result as MeshVPNProviderStatus;
        setStatus(newStatus);

        // If we have an auth URL and status is needs_auth, open the dialog
        if (newStatus.authUrl && newStatus.state === "needs_auth") {
          setAuthDialogOpen(true);
        }
      },
    );
  }, [send, setStatus, setAuthDialogOpen, selectedProvider]);

  // Fetch config
  const fetchConfig = useCallback(() => {
    send("getMeshVPNConfig", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to get VPN config:", resp.error);
        return;
      }
      const cfg = resp.result as MeshVPNConfig;
      setConfig(cfg);
      if (cfg.activeProvider) {
        setSelectedProvider(cfg.activeProvider);
      }
      if (cfg.tailscale) {
        setControlServer(cfg.tailscale.controlServer || "");
        setAuthKey(cfg.tailscale.authKey || "");
        setSelectedExitNode(cfg.tailscale.exitNode || "");
        setAllowLanAccess(cfg.tailscale.exitNodeAllowLanAccess || false);
        setAdvertiseExitNode(cfg.tailscale.advertiseExitNode || false);
        setTunMode(cfg.tailscale.tunMode || "userspace");
      }
    });
  }, [send, setConfig]);

  // Fetch exit nodes
  const fetchExitNodes = useCallback(() => {
    send("meshVPNGetExitNodes", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        // It's okay if this fails when not connected
        return;
      }
      setExitNodes(resp.result as MeshVPNExitNode[]);
    });
  }, [send, setExitNodes]);

  // Fetch version info
  const fetchVersionInfo = useCallback(() => {
    if (!selectedProvider) {
      setVersionInfo(null);
      return;
    }
    send(
      "meshVPNGetVersionInfo",
      { params: { provider: selectedProvider } },
      (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          // It's okay if this fails - provider might not support version info
          return;
        }
        setVersionInfo(resp.result as MeshVPNVersionInfo);
      },
    );
  }, [send, setVersionInfo, selectedProvider]);

  // Initial load
  useEffect(() => {
    fetchProviders();
    fetchConfig();
  }, [fetchProviders, fetchConfig]);

  // Fetch status when provider changes
  useEffect(() => {
    fetchStatus();
  }, [fetchStatus]);

  // Fetch exit nodes when connected
  useEffect(() => {
    if (status?.state === "connected") {
      fetchExitNodes();
    }
  }, [status?.state, fetchExitNodes]);

  // Fetch version info when installed
  useEffect(() => {
    if (status?.installed) {
      fetchVersionInfo();
    }
  }, [status?.installed, fetchVersionInfo]);

  const handleProviderChange = (value: string) => {
    setSelectedProvider(value);
  };

  const handleInstall = () => {
    if (!selectedProvider) return;
    setActionLoading(true);
    setInstallProgress(0);

    send("meshVPNInstall", { provider: selectedProvider }, (resp: JsonRpcResponse) => {
      setActionLoading(false);
      setInstallProgress(null);
      if ("error" in resp) {
        notifications.error(m.meshvpn_install_error({ error: String(resp.error.message) }));
        return;
      }
      fetchStatus();
      fetchProviders();
    });
  };

  const handleUninstall = () => {
    if (!selectedProvider) return;
    if (!window.confirm(m.meshvpn_uninstall_confirm_description())) return;

    setActionLoading(true);
    send("meshVPNUninstall", { provider: selectedProvider }, (resp: JsonRpcResponse) => {
      setActionLoading(false);
      if ("error" in resp) {
        notifications.error(m.meshvpn_uninstall_error({ error: String(resp.error.message) }));
        return;
      }
      fetchStatus();
      fetchProviders();
    });
  };

  const handleConnect = () => {
    setActionLoading(true);
    send(
      "meshVPNConnect",
      {
        params: {
          provider: selectedProvider,
          controlServer: controlServer || undefined,
          authKey: authKey || undefined,
        },
      },
      (resp: JsonRpcResponse) => {
        setActionLoading(false);
        if ("error" in resp) {
          notifications.error(m.meshvpn_connect_error({ error: String(resp.error.message) }));
          return;
        }
        const result = resp.result as { success: boolean; authUrl?: string };
        if (result.authUrl && status) {
          setStatus({ ...status, authUrl: result.authUrl, state: "needs_auth" });
          setAuthDialogOpen(true);
        }
        fetchStatus();
      },
    );
  };

  const handleDisconnect = () => {
    setActionLoading(true);
    send("meshVPNDisconnect", {}, (resp: JsonRpcResponse) => {
      setActionLoading(false);
      if ("error" in resp) {
        notifications.error(m.meshvpn_disconnect_error({ error: String(resp.error.message) }));
        return;
      }
      setAuthDialogOpen(false);
      fetchStatus();
      fetchProviders();
    });
  };

  const handleLogout = () => {
    if (!window.confirm(m.meshvpn_logout_confirm_description())) return;

    setActionLoading(true);
    send("meshVPNLogout", {}, (resp: JsonRpcResponse) => {
      setActionLoading(false);
      if ("error" in resp) {
        notifications.error(m.meshvpn_logout_error({ error: String(resp.error.message) }));
        return;
      }
      setAuthDialogOpen(false);
      fetchStatus();
      fetchProviders();
    });
  };

  const handleUpdate = () => {
    if (!selectedProvider) return;
    if (!versionInfo?.updateAvailable) return;

    setActionLoading(true);
    setUpdateProgress(0);

    send("meshVPNUpdate", { params: { provider: selectedProvider } }, (resp: JsonRpcResponse) => {
      setActionLoading(false);
      setUpdateProgress(null);
      if ("error" in resp) {
        notifications.error(m.meshvpn_update_error({ error: String(resp.error.message) }));
        return;
      }
      notifications.success(m.meshvpn_update_success());
      fetchStatus();
      fetchVersionInfo();
    });
  };

  const handleSetExitNode = (hostname: string) => {
    setSelectedExitNode(hostname);

    const handleResponse = (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.meshvpn_exit_node_set_error({ error: String(resp.error.message) }));
        return;
      }
      fetchStatus();
    };

    if (!hostname) {
      send("meshVPNClearExitNode", {}, handleResponse);
    } else {
      send(
        "meshVPNSetExitNode",
        { params: { hostname, allowLan: allowLanAccess } },
        handleResponse,
      );
    }
  };

  const handleAllowLanChange = (checked: boolean) => {
    setAllowLanAccess(checked);
    if (selectedExitNode) {
      send(
        "meshVPNSetExitNode",
        { params: { hostname: selectedExitNode, allowLan: checked } },
        (resp: JsonRpcResponse) => {
          if ("error" in resp) {
            notifications.error(
              m.meshvpn_exit_node_set_error({ error: String(resp.error.message) }),
            );
          }
        },
      );
    }
  };

  const handleTunModeChange = (mode: "userspace" | "kernel") => {
    setTunMode(mode);
    setActionLoading(true);
    send(
      "meshVPNSetTUNMode",
      { params: { provider: selectedProvider, mode } },
      (resp: JsonRpcResponse) => {
        setActionLoading(false);
        if ("error" in resp) {
          notifications.error(m.meshvpn_tun_mode_error({ error: String(resp.error.message) }));
          fetchConfig();
          return;
        }
        notifications.success(m.meshvpn_tun_mode_updated());
        fetchStatus();
      },
    );
  };

  const handleAdvertiseExitNodeChange = (checked: boolean) => {
    setAdvertiseExitNode(checked);
    send(
      "meshVPNSetAdvertiseExitNode",
      { params: { provider: selectedProvider, advertise: checked } },
      (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            m.meshvpn_advertise_exit_node_error({ error: String(resp.error.message) }),
          );
          fetchConfig();
          return;
        }
        notifications.success(
          checked
            ? m.meshvpn_advertise_exit_node_enabled()
            : m.meshvpn_advertise_exit_node_disabled(),
        );
      },
    );
  };

  const selectedProviderInfo = providers.find(p => p.name === selectedProvider);
  // Use status.installed (always fresh after actions) with fallback to provider info
  const isInstalled = status?.installed ?? selectedProviderInfo?.installed ?? false;
  const isConnected = status?.state === "connected";
  const isConnecting = status?.state === "connecting";

  return (
    <div className="space-y-4">
      <SettingsSectionHeader title={m.meshvpn_title()} description={m.meshvpn_description()} />

      {/* Auth Dialog */}
      {isAuthDialogOpen && status?.authUrl && (
        <MeshVPNAuthDialog authUrl={status.authUrl} onClose={() => setAuthDialogOpen(false)} />
      )}

      {/* Provider Selection */}
      <SettingsItem
        title={m.meshvpn_provider_title()}
        description={m.meshvpn_provider_description()}
      >
        <SelectMenuBasic
          size="SM"
          value={selectedProvider}
          onChange={e => handleProviderChange(e.target.value)}
          disabled={actionLoading || isConnected || isConnecting}
          options={[
            { value: "", label: m.meshvpn_provider_none() },
            ...providers.map(p => ({
              value: p.name,
              label: p.displayName,
            })),
          ]}
        />
      </SettingsItem>

      {/* Status Card */}
      {selectedProvider && status && <MeshVPNStatusCard status={status} />}

      {/* Install Progress */}
      {installProgress !== null && (
        <div className="rounded bg-blue-50 p-3 dark:bg-blue-900/20">
          <div className="flex items-center gap-x-2">
            <LoadingSpinner className="h-4 w-4 text-blue-500" />
            <span className="text-sm text-blue-700 dark:text-blue-300">
              {m.meshvpn_install_progress({
                progress: Math.round(installProgress * 100).toString(),
              })}
            </span>
          </div>
        </div>
      )}

      {/* Update Progress */}
      {updateProgress !== null && (
        <div className="rounded bg-blue-50 p-3 dark:bg-blue-900/20">
          <div className="flex items-center gap-x-2">
            <LoadingSpinner className="h-4 w-4 text-blue-500" />
            <span className="text-sm text-blue-700 dark:text-blue-300">
              {m.meshvpn_update_progress({
                progress: Math.round(updateProgress * 100).toString(),
              })}
            </span>
          </div>
        </div>
      )}

      {/* Version Info with Update Available */}
      {isInstalled && versionInfo && versionInfo.latestVersion && (
        <div
          className={`rounded p-3 ${versionInfo.updateAvailable ? "bg-amber-50 dark:bg-amber-900/20" : "bg-slate-50 dark:bg-slate-800"}`}
        >
          <div className="flex items-center justify-between">
            <div className="space-y-1">
              <div className="text-sm text-slate-600 dark:text-slate-400">
                {m.meshvpn_current_version()}:{" "}
                <span className="font-mono">
                  {versionInfo.currentVersion || status?.version || "?"}
                </span>
              </div>
              <div className="text-sm text-slate-600 dark:text-slate-400">
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

      {/* Configuration for Tailscale */}
      {selectedProvider === "tailscale" && !isConnected && isInstalled && (
        <NestedSettingsGroup>
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
            placeholder=""
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
        </NestedSettingsGroup>
      )}

      {/* Exit Node Selection - only when connected and provider supports it */}
      {isConnected && selectedProviderInfo?.supportsExitNodes && exitNodes.length > 0 && (
        <NestedSettingsGroup>
          <SettingsItem
            title={m.meshvpn_exit_node_title()}
            description={m.meshvpn_exit_node_description()}
          >
            <SelectMenuBasic
              size="SM"
              value={selectedExitNode}
              onChange={e => handleSetExitNode(e.target.value)}
              options={[
                { value: "", label: m.meshvpn_exit_node_none() },
                ...exitNodes
                  .filter(n => n.online)
                  .map(n => ({
                    value: n.hostName,
                    label: n.name || n.hostName,
                  })),
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
        </NestedSettingsGroup>
      )}

      {/* Advertise as Exit Node - only when connected and provider supports exit nodes */}
      {isConnected && selectedProviderInfo?.supportsExitNodes && (
        <NestedSettingsGroup>
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
        </NestedSettingsGroup>
      )}

      {/* Action Buttons */}
      {selectedProvider && (
        <div className="flex flex-wrap items-center gap-2">
          {!isInstalled && (
            <Button
              size="SM"
              theme="primary"
              text={m.meshvpn_install_button()}
              onClick={handleInstall}
              disabled={actionLoading}
            />
          )}
          {isInstalled && !isConnected && !isConnecting && !isAuthDialogOpen && (
            <Button
              size="SM"
              theme="primary"
              text={m.meshvpn_connect_button()}
              onClick={handleConnect}
              disabled={actionLoading}
            />
          )}
          {(isConnected || isConnecting) && (
            <Button
              size="SM"
              theme="light"
              text={m.meshvpn_disconnect_button()}
              onClick={handleDisconnect}
              disabled={actionLoading}
            />
          )}
          {isInstalled && isConnected && (
            <Button
              size="SM"
              theme="light"
              text={m.meshvpn_logout_button()}
              onClick={handleLogout}
              disabled={actionLoading}
            />
          )}
          {isInstalled && !isConnected && !isConnecting && (
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
      )}
    </div>
  );
}
