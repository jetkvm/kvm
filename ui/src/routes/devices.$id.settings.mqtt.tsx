import { useCallback, useEffect, useState } from "react";

import { useJsonRpc } from "@hooks/useJsonRpc";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SettingsSectionHeader } from "@components/SettingsSectionHeader";
import { SettingsItem } from "@components/SettingsItem";
import { NestedSettingsGroup } from "@components/NestedSettingsGroup";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import InputField from "@components/InputField";
import { Checkbox } from "@components/Checkbox";
import { Button } from "@components/Button";
import LoadingSpinner from "@components/LoadingSpinner";
import notifications from "@/notifications";
import { m } from "@localizations/messages";

interface MQTTSettings {
  enabled: boolean;
  broker: string;
  port: number;
  username: string;
  password: string;
  base_topic: string;
  use_tls: boolean;
  tls_insecure: boolean;
  enable_ha_discovery: boolean;
  enable_actions: boolean;
  debounce_ms: number;
}

type ConnectionState = "disconnected" | "connecting" | "connected";
type SavePhase = "idle" | "testing" | "saving";

const DEFAULT_PORT = 1883;
const DEFAULT_TLS_PORT = 8883;
const DEFAULT_BASE_TOPIC = "jetkvm";

const mqttErrorPatterns: [RegExp, () => string][] = [
  [/not auth/i, () => m.mqtt_error_auth()],
  [/bad user name or password/i, () => m.mqtt_error_auth()],
  [/connection refused/i, () => m.mqtt_error_refused()],
  [/i\/o timeout|deadline exceeded/i, () => m.mqtt_error_timeout()],
  [/no such host/i, () => m.mqtt_error_host()],
  [/connection reset/i, () => m.mqtt_error_reset()],
  [/certificate|tls|x509/i, () => m.mqtt_error_tls()],
];

function friendlyMqttError(raw: string): string {
  const lower = raw.toLowerCase();
  for (const [pattern, message] of mqttErrorPatterns) {
    if (pattern.test(lower)) return message();
  }
  return raw;
}

function defaultPortForTLS(useTLS: boolean) {
  return useTLS ? DEFAULT_TLS_PORT : DEFAULT_PORT;
}

function isDefaultPort(port: number) {
  return port === DEFAULT_PORT || port === DEFAULT_TLS_PORT;
}

export default function SettingsMqttRoute() {
  const { send } = useJsonRpc();

  const [settings, setSettings] = useState<MQTTSettings>({
    enabled: false,
    broker: "",
    port: DEFAULT_PORT,
    username: "",
    password: "",
    base_topic: "jetkvm",
    use_tls: false,
    tls_insecure: false,
    enable_ha_discovery: true,
    enable_actions: true,
    debounce_ms: 500,
  });

  const [connectionState, setConnectionState] = useState<ConnectionState>("disconnected");
  const [savePhase, setSavePhase] = useState<SavePhase>("idle");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [activeExtension, setActiveExtension] = useState<string>("");
  const [portMode, setPortMode] = useState<"default" | "custom">("default");
  const [topicMode, setTopicMode] = useState<"default" | "custom">("default");
  const [testing, setTesting] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    let settled = 0;
    const settle = () => {
      settled++;
      if (settled >= 3) setLoading(false);
    };

    send("getMqttSettings", {}, resp => {
      if ("error" in resp) {
        setLoadError(resp.error.message || m.unknown_error());
        settle();
        return;
      }
      const result = resp.result as MQTTSettings;
      setSettings(result);
      setPortMode(isDefaultPort(result.port) ? "default" : "custom");
      setTopicMode(result.base_topic === DEFAULT_BASE_TOPIC ? "default" : "custom");
      settle();
    });

    send("getMqttStatus", {}, resp => {
      if ("error" in resp) {
        settle();
        return;
      }
      const result = resp.result as { connected: boolean };
      setConnectionState(result.connected ? "connected" : "disconnected");
      settle();
    });

    send("getActiveExtension", {}, resp => {
      if ("error" in resp) {
        settle();
        return;
      }
      setActiveExtension(resp.result as string);
      settle();
    });
  }, [send]);

  // Poll connection status
  useEffect(() => {
    if (!settings.enabled) return;

    const interval = setInterval(() => {
      send("getMqttStatus", {}, resp => {
        if ("error" in resp) return;
        const result = resp.result as { connected: boolean };
        setConnectionState(result.connected ? "connected" : "disconnected");
      });
    }, 3000);
    return () => clearInterval(interval);
  }, [send, settings.enabled]);

  const validate = useCallback((): boolean => {
    const errs: Record<string, string> = {};
    if (settings.enabled && !settings.broker.trim()) {
      errs.broker = m.mqtt_error_broker_required();
    }
    setFieldErrors(errs);
    return Object.keys(errs).length === 0;
  }, [settings]);

  const handleSave = useCallback(() => {
    if (!validate()) return;

    if (!settings.enabled) {
      setSavePhase("saving");
      send("setMqttSettings", { settings }, resp => {
        setSavePhase("idle");
        if ("error" in resp) {
          notifications.error(
            m.mqtt_saved_error({ error: resp.error.message || m.unknown_error() }),
          );
          return;
        }
        notifications.success(m.mqtt_saved_disconnect());
        setConnectionState("disconnected");
      });
      return;
    }

    // Test connection first, then save
    setSavePhase("testing");
    send("testMqttConnection", { settings }, testResp => {
      if ("error" in testResp) {
        setSavePhase("idle");
        notifications.error(friendlyMqttError(testResp.error.message || m.unknown_error()));
        return;
      }
      const testResult = testResp.result as { success: boolean; error?: string };
      if (!testResult.success) {
        setSavePhase("idle");
        notifications.error(friendlyMqttError(testResult.error || m.unknown_error()));
        return;
      }

      // Test passed — save and reconnect
      setSavePhase("saving");
      setConnectionState("connecting");
      send("setMqttSettings", { settings }, saveResp => {
        setSavePhase("idle");
        if ("error" in saveResp) {
          notifications.error(
            m.mqtt_saved_error({ error: saveResp.error.message || m.unknown_error() }),
          );
          setConnectionState("disconnected");
          return;
        }
        notifications.success(m.mqtt_saved_success());
      });
    });
  }, [send, settings, validate]);

  const handleTestConnection = useCallback(() => {
    if (!validate()) return;

    setTesting(true);
    send("testMqttConnection", { settings }, resp => {
      setTesting(false);
      if ("error" in resp) {
        notifications.error(friendlyMqttError(resp.error.message || m.unknown_error()));
        return;
      }
      const result = resp.result as { success: boolean; error?: string };
      if (result.success) {
        notifications.success(m.mqtt_test_success());
      } else {
        notifications.error(friendlyMqttError(result.error || m.unknown_error()));
      }
    });
  }, [send, settings, validate]);

  const updateField = <K extends keyof MQTTSettings>(field: K, value: MQTTSettings[K]) => {
    setSettings(prev => {
      const next = { ...prev, [field]: value };

      if (field === "use_tls" && portMode === "default") {
        next.port = defaultPortForTLS(value as boolean);
      }

      return next;
    });
  };

  const handlePortModeChange = (mode: string) => {
    const newMode = mode as "default" | "custom";
    setPortMode(newMode);
    if (newMode === "default") {
      updateField("port", defaultPortForTLS(settings.use_tls));
    }
  };

  const handleTopicModeChange = (mode: string) => {
    const newMode = mode as "default" | "custom";
    setTopicMode(newMode);
    if (newMode === "default") {
      updateField("base_topic", DEFAULT_BASE_TOPIC);
    }
  };

  const hasATXExtension = activeExtension === "atx-power";

  const saveButtonText = () => {
    if (savePhase === "testing") return m.mqtt_testing();
    if (savePhase === "saving") return m.saving();
    if (settings.enabled === false) return m.mqtt_save_disconnect_button();
    return m.mqtt_save_button();
  };

  if (loading) {
    return (
      <div className="space-y-4">
        <SettingsPageHeader title={m.settings_mqtt()} description={m.mqtt_description()} />
        <div className="flex items-center justify-center py-8">
          <LoadingSpinner className="h-6 w-6 text-blue-500" />
        </div>
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="space-y-4">
        <SettingsPageHeader title={m.settings_mqtt()} description={m.mqtt_description()} />
        <div className="rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-400">
          {m.mqtt_load_error({ error: loadError })}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={m.settings_mqtt()}
        description={m.mqtt_description()}
        action={settings.enabled ? <ConnectionStatusBadge state={connectionState} /> : undefined}
      />

      <div className="space-y-4">
        <SettingsItem title={m.mqtt_enable_title()} description={m.mqtt_enable_description()}>
          <Checkbox
            checked={settings.enabled}
            onChange={e => updateField("enabled", e.target.checked)}
          />
        </SettingsItem>

        {settings.enabled && (
          <>
            <SettingsItem title={m.mqtt_broker_label()} description={m.mqtt_broker_description()}>
              <InputField
                size="SM"
                placeholder="192.168.1.2"
                value={settings.broker}
                error={fieldErrors.broker}
                onChange={e => {
                  updateField("broker", e.target.value);
                  if (fieldErrors.broker) setFieldErrors(prev => ({ ...prev, broker: "" }));
                }}
              />
            </SettingsItem>

            <SettingsItem title={m.mqtt_port_label()} description={m.mqtt_port_description()}>
              <SelectMenuBasic
                size="SM"
                label=""
                value={portMode}
                options={[
                  { value: "default", label: m.mqtt_port_auto() },
                  { value: "custom", label: m.mqtt_port_custom() },
                ]}
                onChange={e => handlePortModeChange(e.target.value)}
              />
            </SettingsItem>

            {portMode === "custom" && (
              <NestedSettingsGroup>
                <InputField
                  size="SM"
                  type="number"
                  placeholder={defaultPortForTLS(settings.use_tls).toString()}
                  value={settings.port.toString()}
                  onChange={e => updateField("port", parseInt(e.target.value) || DEFAULT_PORT)}
                />
              </NestedSettingsGroup>
            )}

            <SettingsItem title={m.mqtt_use_tls_title()} description={m.mqtt_use_tls_description()}>
              <Checkbox
                checked={settings.use_tls}
                onChange={e => updateField("use_tls", e.target.checked)}
              />
            </SettingsItem>

            {settings.use_tls && (
              <NestedSettingsGroup>
                <SettingsItem
                  title={m.mqtt_tls_insecure_title()}
                  description={m.mqtt_tls_insecure_description()}
                >
                  <Checkbox
                    checked={settings.tls_insecure}
                    onChange={e => updateField("tls_insecure", e.target.checked)}
                  />
                </SettingsItem>
              </NestedSettingsGroup>
            )}

            <SettingsItem
              title={m.mqtt_remote_control_title()}
              description={m.mqtt_remote_control_description()}
            >
              <Checkbox
                checked={settings.enable_actions}
                onChange={e => updateField("enable_actions", e.target.checked)}
              />
            </SettingsItem>

            {/* --- Authentication --- */}
            <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />
            <SettingsSectionHeader
              title={m.mqtt_section_auth()}
              description={m.mqtt_section_auth_description()}
            />
            <NestedSettingsGroup>
              <SettingsItem
                title={m.mqtt_username_label()}
                description={m.mqtt_username_description()}
              >
                <InputField
                  size="SM"
                  placeholder="username"
                  value={settings.username}
                  onChange={e => updateField("username", e.target.value)}
                />
              </SettingsItem>

              <SettingsItem
                title={m.mqtt_password_label()}
                description={m.mqtt_password_description()}
              >
                <InputField
                  size="SM"
                  type="password"
                  placeholder="password"
                  value={settings.password}
                  onChange={e => updateField("password", e.target.value)}
                />
              </SettingsItem>
            </NestedSettingsGroup>

            {/* --- Home Assistant --- */}
            <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />
            <SettingsSectionHeader
              title={m.mqtt_section_homeassistant()}
              description={m.mqtt_section_homeassistant_description()}
            />
            <NestedSettingsGroup>
              <SettingsItem
                title={m.mqtt_ha_discovery_title()}
                description={m.mqtt_ha_discovery_description()}
              >
                <Checkbox
                  checked={settings.enable_ha_discovery}
                  onChange={e => updateField("enable_ha_discovery", e.target.checked)}
                />
              </SettingsItem>

              <SettingsItem
                title={m.mqtt_base_topic_label()}
                description={m.mqtt_base_topic_description()}
              >
                <SelectMenuBasic
                  size="SM"
                  label=""
                  value={topicMode}
                  options={[
                    { value: "default", label: m.mqtt_topic_default() },
                    { value: "custom", label: m.mqtt_port_custom() },
                  ]}
                  onChange={e => handleTopicModeChange(e.target.value)}
                />
              </SettingsItem>

              {topicMode === "custom" && (
                <NestedSettingsGroup>
                  <InputField
                    size="SM"
                    placeholder={DEFAULT_BASE_TOPIC}
                    value={settings.base_topic}
                    onChange={e => updateField("base_topic", e.target.value)}
                  />
                </NestedSettingsGroup>
              )}
            </NestedSettingsGroup>

            {/* --- Advanced (only when ATX extension is active) --- */}
            {hasATXExtension && (
              <>
                <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />
                <SettingsSectionHeader
                  title={m.mqtt_section_advanced()}
                  description={m.mqtt_section_advanced_description()}
                />
                <NestedSettingsGroup>
                  <SettingsItem
                    title={m.mqtt_debounce_title()}
                    description={m.mqtt_debounce_description()}
                  >
                    <InputField
                      size="SM"
                      type="number"
                      placeholder="500"
                      value={settings.debounce_ms.toString()}
                      onChange={e => updateField("debounce_ms", parseInt(e.target.value) || 0)}
                    />
                  </SettingsItem>
                </NestedSettingsGroup>
              </>
            )}

            {/* Actions */}
            <div className="flex items-center gap-x-2 pt-2">
              <Button
                size="SM"
                theme="primary"
                text={saveButtonText()}
                loading={savePhase !== "idle"}
                onClick={handleSave}
              />
              <Button
                size="SM"
                theme="light"
                text={testing ? m.mqtt_testing() : m.mqtt_test_button()}
                loading={testing}
                onClick={handleTestConnection}
              />
            </div>
          </>
        ) || (
          <>
            <div className="flex items-center gap-x-2 pt-2">
              <Button
                size="SM"
                theme="primary"
                text={saveButtonText()}
                loading={savePhase !== "idle"}
                onClick={handleSave}
              />
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function ConnectionStatusBadge({ state }: { state: ConnectionState }) {
  const config = {
    connected: {
      dotClass: "bg-green-500",
      label: m.mqtt_status_connected(),
    },
    connecting: {
      dotClass: "bg-yellow-500 animate-pulse",
      label: m.mqtt_status_connecting(),
    },
    disconnected: {
      dotClass: "bg-red-500",
      label: m.mqtt_status_disconnected(),
    },
  }[state];

  return (
    <div className="flex items-center gap-x-1.5 rounded-full bg-slate-100 px-2.5 py-1 dark:bg-slate-700/50">
      <span className={`inline-block h-1.5 w-1.5 shrink-0 rounded-full ${config.dotClass}`} />
      <span className="text-xs font-medium text-slate-600 dark:text-slate-300">{config.label}</span>
    </div>
  );
}
