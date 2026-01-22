import { useCallback, useEffect, useState } from "react";

import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { m } from "@localizations/messages.js";

import notifications from "../notifications";

interface RDPStateResult {
  enabled: boolean;
  running: boolean;
  port: number;
  connectionCount: number;
  tlsEnabled: boolean;
  maxConnections: number;
  videoEnabled: boolean;
  audioEnabled: boolean;
  micEnabled: boolean;
  cameraEnabled: boolean;
  username: string;
  domain: string;
}

const RDP_DEFAULTS = {
  port: 3389,
  maxConnections: 3,
} as const;

export default function SettingsRDPRoute() {
  const { send } = useJsonRpc();

  const [enabled, setEnabled] = useState<boolean>(false);
  const [running, setRunning] = useState<boolean>(false);
  const [port, setPort] = useState<number>(RDP_DEFAULTS.port);
  const [connectionCount, setConnectionCount] = useState<number>(0);
  const [tlsEnabled, setTlsEnabled] = useState<boolean>(true);
  const [maxConnections, setMaxConnections] = useState<number>(RDP_DEFAULTS.maxConnections);
  const [videoEnabled, setVideoEnabled] = useState<boolean>(true);
  const [audioEnabled, setAudioEnabled] = useState<boolean>(true);
  const [micEnabled, setMicEnabled] = useState<boolean>(true);
  const [cameraEnabled, setCameraEnabled] = useState<boolean>(false);
  const [username, setUsername] = useState<string>("");
  const [domain, setDomain] = useState<string>("");
  const [isLoading, setIsLoading] = useState(true);
  const [isEditingUsername, setIsEditingUsername] = useState(false);
  const [isEditingDomain, setIsEditingDomain] = useState(false);

  const loadRDPState = useCallback(() => {
    send("getRDPState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.rdp_settings_failed_load({ error: String(resp.error.data || m.unknown_error()) }),
        );
        setIsLoading(false);
        return;
      }
      const state = resp.result as RDPStateResult;
      setEnabled(state.enabled);
      setRunning(state.running);
      setPort(state.port);
      setConnectionCount(state.connectionCount);
      setTlsEnabled(state.tlsEnabled);
      setMaxConnections(state.maxConnections || RDP_DEFAULTS.maxConnections);
      setVideoEnabled(state.videoEnabled ?? true);
      setAudioEnabled(state.audioEnabled ?? true);
      setMicEnabled(state.micEnabled ?? true);
      setCameraEnabled(state.cameraEnabled ?? false);
      // Only update username/domain if user is not actively editing
      if (!isEditingUsername) {
        setUsername(state.username ?? "");
      }
      if (!isEditingDomain) {
        setDomain(state.domain ?? "");
      }
      setIsLoading(false);
    });
  }, [send, isEditingUsername, isEditingDomain]);

  useEffect(() => {
    loadRDPState();
    const interval = setInterval(loadRDPState, 5000);
    return () => clearInterval(interval);
  }, [loadRDPState]);

  const handleToggleEnabled = () => {
    const newEnabled = !enabled;
    send("setRDPEnabled", { enabled: newEnabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.rdp_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setEnabled(newEnabled);
      notifications.success(newEnabled ? m.rdp_settings_enabled() : m.rdp_settings_disabled());
      // Reload state to get running status
      setTimeout(loadRDPState, 500);
    });
  };

  const handlePortChange = (newPort: number) => {
    send("setRDPPort", { port: newPort }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.rdp_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setPort(newPort);
      notifications.success(m.rdp_settings_port_changed());
    });
  };

  const handleTlsToggle = () => {
    const newTlsEnabled = !tlsEnabled;
    send("setRDPTLS", { enabled: newTlsEnabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.rdp_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setTlsEnabled(newTlsEnabled);
      notifications.success(m.rdp_settings_tls_changed());
    });
  };

  const handleMaxConnectionsChange = (newMax: number) => {
    send("setRDPMaxConnections", { max: newMax }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.rdp_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setMaxConnections(newMax);
      notifications.success(m.rdp_settings_max_connections_changed());
    });
  };

  const handleVideoToggle = () => {
    const newVideoEnabled = !videoEnabled;
    send("setRDPVideoEnabled", { enabled: newVideoEnabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.rdp_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setVideoEnabled(newVideoEnabled);
      notifications.success(m.rdp_settings_video_changed());
    });
  };

  const handleAudioToggle = () => {
    const newAudioEnabled = !audioEnabled;
    send("setRDPAudioEnabled", { enabled: newAudioEnabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.rdp_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setAudioEnabled(newAudioEnabled);
      notifications.success(m.rdp_settings_audio_changed());
    });
  };

  const handleMicToggle = () => {
    const newMicEnabled = !micEnabled;
    send("setRDPMicEnabled", { enabled: newMicEnabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.rdp_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setMicEnabled(newMicEnabled);
      notifications.success(m.rdp_settings_mic_changed());
    });
  };

  const handleCameraToggle = () => {
    const newCameraEnabled = !cameraEnabled;
    send("setRDPCameraEnabled", { enabled: newCameraEnabled }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.rdp_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setCameraEnabled(newCameraEnabled);
      notifications.success(m.rdp_settings_camera_changed());
    });
  };

  const handleUsernameChange = (newUsername: string) => {
    send("setRDPUsername", { username: newUsername }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.rdp_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setUsername(newUsername);
      notifications.success(m.rdp_settings_username_changed());
    });
  };

  const handleDomainChange = (newDomain: string) => {
    send("setRDPDomain", { domain: newDomain }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.rdp_settings_failed_save({ error: String(resp.error.data || m.unknown_error()) }),
        );
        return;
      }
      setDomain(newDomain);
      notifications.success(m.rdp_settings_domain_changed());
    });
  };

  if (isLoading) {
    return (
      <div className="space-y-4">
        <SettingsPageHeader
          title={m.rdp_settings_title()}
          description={m.rdp_settings_description()}
        />
        <div className="flex items-center justify-center py-8">
          <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-blue-600"></div>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={m.rdp_settings_title()}
        description={m.rdp_settings_description()}
      />

      <div className="space-y-4">
        <SettingsItem
          title={m.rdp_settings_enable_title()}
          description={m.rdp_settings_enable_description()}
        >
          <label className="relative inline-flex cursor-pointer items-center">
            <input
              type="checkbox"
              checked={enabled}
              onChange={handleToggleEnabled}
              className="peer sr-only"
            />
            <div className="peer h-6 w-11 rounded-full bg-slate-200 peer-checked:bg-blue-600 peer-focus:ring-4 peer-focus:ring-blue-300 peer-focus:outline-none after:absolute after:top-[2px] after:left-[2px] after:h-5 after:w-5 after:rounded-full after:border after:border-slate-300 after:bg-white after:transition-all after:content-[''] peer-checked:after:translate-x-full peer-checked:after:border-white dark:border-slate-600 dark:bg-slate-700 dark:peer-focus:ring-blue-800"></div>
          </label>
        </SettingsItem>

        {enabled && (
          <>
            <div className="rounded-md bg-slate-50 p-4 dark:bg-slate-700">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm font-medium text-slate-900 dark:text-white">
                    {m.rdp_settings_status_title()}
                  </p>
                  <p className="text-sm text-slate-500 dark:text-slate-400">
                    {running
                      ? m.rdp_settings_status_running({ port })
                      : m.rdp_settings_status_stopped()}
                  </p>
                </div>
                <div className="text-right">
                  <span
                    className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${
                      running
                        ? "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200"
                        : "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200"
                    }`}
                  >
                    {running ? m.rdp_settings_running() : m.rdp_settings_stopped()}
                  </span>
                  {connectionCount > 0 && (
                    <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
                      {m.rdp_settings_connections({ count: connectionCount })}
                    </p>
                  )}
                </div>
              </div>
            </div>

            <SettingsItem
              title={m.rdp_settings_port_title()}
              description={m.rdp_settings_port_description()}
            >
              <SelectMenuBasic
                size="SM"
                value={String(port)}
                options={[
                  {
                    value: "3389",
                    label: `3389${port === RDP_DEFAULTS.port ? m.rdp_settings_default_suffix() : ""}`,
                  },
                  { value: "3390", label: "3390" },
                  { value: "3391", label: "3391" },
                  { value: "3392", label: "3392" },
                  { value: "3393", label: "3393" },
                ]}
                onChange={e => handlePortChange(parseInt(e.target.value))}
              />
            </SettingsItem>

            <SettingsItem
              title={m.rdp_settings_max_connections_title()}
              description={m.rdp_settings_max_connections_description()}
            >
              <SelectMenuBasic
                size="SM"
                value={String(maxConnections)}
                options={[
                  { value: "1", label: "1" },
                  { value: "2", label: "2" },
                  {
                    value: "3",
                    label: `3${maxConnections === RDP_DEFAULTS.maxConnections ? m.rdp_settings_default_suffix() : ""}`,
                  },
                  { value: "5", label: "5" },
                  { value: "10", label: "10" },
                ]}
                onChange={e => handleMaxConnectionsChange(parseInt(e.target.value))}
              />
            </SettingsItem>

            <SettingsItem
              title={m.rdp_settings_tls_title()}
              description={m.rdp_settings_tls_description()}
            >
              <label className="relative inline-flex cursor-pointer items-center">
                <input
                  type="checkbox"
                  checked={tlsEnabled}
                  onChange={handleTlsToggle}
                  className="peer sr-only"
                />
                <div className="peer h-6 w-11 rounded-full bg-slate-200 peer-checked:bg-blue-600 peer-focus:ring-4 peer-focus:ring-blue-300 peer-focus:outline-none after:absolute after:top-[2px] after:left-[2px] after:h-5 after:w-5 after:rounded-full after:border after:border-slate-300 after:bg-white after:transition-all after:content-[''] peer-checked:after:translate-x-full peer-checked:after:border-white dark:border-slate-600 dark:bg-slate-700 dark:peer-focus:ring-blue-800"></div>
              </label>
            </SettingsItem>

            <SettingsItem
              title={m.rdp_settings_username_title()}
              description={m.rdp_settings_username_description()}
            >
              <input
                type="text"
                value={username}
                onChange={e => setUsername(e.target.value)}
                onFocus={() => setIsEditingUsername(true)}
                onBlur={e => {
                  setIsEditingUsername(false);
                  handleUsernameChange(e.target.value);
                }}
                placeholder={m.rdp_settings_username_placeholder()}
                className="w-40 rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm text-slate-900 placeholder-slate-400 focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none dark:border-slate-600 dark:bg-slate-800 dark:text-white dark:placeholder-slate-500 dark:focus:border-blue-400"
              />
            </SettingsItem>

            <SettingsItem
              title={m.rdp_settings_domain_title()}
              description={m.rdp_settings_domain_description()}
            >
              <input
                type="text"
                value={domain}
                onChange={e => setDomain(e.target.value)}
                onFocus={() => setIsEditingDomain(true)}
                onBlur={e => {
                  setIsEditingDomain(false);
                  handleDomainChange(e.target.value);
                }}
                placeholder={m.rdp_settings_domain_placeholder()}
                className="w-40 rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm text-slate-900 placeholder-slate-400 focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none dark:border-slate-600 dark:bg-slate-800 dark:text-white dark:placeholder-slate-500 dark:focus:border-blue-400"
              />
            </SettingsItem>

            <div className="border-t border-slate-200 pt-4 dark:border-slate-700">
              <h3 className="mb-3 text-sm font-medium text-slate-900 dark:text-white">
                {m.rdp_settings_features_title()}
              </h3>

              <div className="space-y-4">
                <SettingsItem
                  title={m.rdp_settings_video_title()}
                  description={m.rdp_settings_video_description()}
                >
                  <label className="relative inline-flex cursor-pointer items-center">
                    <input
                      type="checkbox"
                      checked={videoEnabled}
                      onChange={handleVideoToggle}
                      className="peer sr-only"
                    />
                    <div className="peer h-6 w-11 rounded-full bg-slate-200 peer-checked:bg-blue-600 peer-focus:ring-4 peer-focus:ring-blue-300 peer-focus:outline-none after:absolute after:top-[2px] after:left-[2px] after:h-5 after:w-5 after:rounded-full after:border after:border-slate-300 after:bg-white after:transition-all after:content-[''] peer-checked:after:translate-x-full peer-checked:after:border-white dark:border-slate-600 dark:bg-slate-700 dark:peer-focus:ring-blue-800"></div>
                  </label>
                </SettingsItem>

                <SettingsItem
                  title={m.rdp_settings_audio_title()}
                  description={m.rdp_settings_audio_description()}
                >
                  <label className="relative inline-flex cursor-pointer items-center">
                    <input
                      type="checkbox"
                      checked={audioEnabled}
                      onChange={handleAudioToggle}
                      className="peer sr-only"
                    />
                    <div className="peer h-6 w-11 rounded-full bg-slate-200 peer-checked:bg-blue-600 peer-focus:ring-4 peer-focus:ring-blue-300 peer-focus:outline-none after:absolute after:top-[2px] after:left-[2px] after:h-5 after:w-5 after:rounded-full after:border after:border-slate-300 after:bg-white after:transition-all after:content-[''] peer-checked:after:translate-x-full peer-checked:after:border-white dark:border-slate-600 dark:bg-slate-700 dark:peer-focus:ring-blue-800"></div>
                  </label>
                </SettingsItem>

                <SettingsItem
                  title={m.rdp_settings_mic_title()}
                  description={m.rdp_settings_mic_description()}
                >
                  <label className="relative inline-flex cursor-pointer items-center">
                    <input
                      type="checkbox"
                      checked={micEnabled}
                      onChange={handleMicToggle}
                      className="peer sr-only"
                    />
                    <div className="peer h-6 w-11 rounded-full bg-slate-200 peer-checked:bg-blue-600 peer-focus:ring-4 peer-focus:ring-blue-300 peer-focus:outline-none after:absolute after:top-[2px] after:left-[2px] after:h-5 after:w-5 after:rounded-full after:border after:border-slate-300 after:bg-white after:transition-all after:content-[''] peer-checked:after:translate-x-full peer-checked:after:border-white dark:border-slate-600 dark:bg-slate-700 dark:peer-focus:ring-blue-800"></div>
                  </label>
                </SettingsItem>

                <SettingsItem
                  title={m.rdp_settings_camera_title()}
                  description={m.rdp_settings_camera_description()}
                >
                  <label className="relative inline-flex cursor-pointer items-center">
                    <input
                      type="checkbox"
                      checked={cameraEnabled}
                      onChange={handleCameraToggle}
                      className="peer sr-only"
                    />
                    <div className="peer h-6 w-11 rounded-full bg-slate-200 peer-checked:bg-blue-600 peer-focus:ring-4 peer-focus:ring-blue-300 peer-focus:outline-none after:absolute after:top-[2px] after:left-[2px] after:h-5 after:w-5 after:rounded-full after:border after:border-slate-300 after:bg-white after:transition-all after:content-[''] peer-checked:after:translate-x-full peer-checked:after:border-white dark:border-slate-600 dark:bg-slate-700 dark:peer-focus:ring-blue-800"></div>
                  </label>
                </SettingsItem>
              </div>
            </div>
          </>
        )}
      </div>

      <div className="rounded-md bg-blue-50 p-4 dark:bg-blue-900/20">
        <p className="text-sm text-blue-700 dark:text-blue-300">{m.rdp_settings_client_note()}</p>
      </div>
    </div>
  );
}
