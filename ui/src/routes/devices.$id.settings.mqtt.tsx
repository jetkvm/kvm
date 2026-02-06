import { useCallback, useEffect, useState } from "react";
import { useJsonRpc } from "@hooks/useJsonRpc";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SettingsItem } from "@components/SettingsItem";
import InputField from "@components/InputField";
import { Checkbox } from "@components/Checkbox";
import { Button } from "@components/Button";
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

interface MQTTStatus {
  connected: boolean;
}

export default function SettingsMqttRoute() {
  const { send } = useJsonRpc();

  const [settings, setSettings] = useState<MQTTSettings>({
    enabled: false,
    broker: "",
    port: 1883,
    username: "",
    password: "",
    base_topic: "jetkvm",
    use_tls: false,
    tls_insecure: false,
    enable_ha_discovery: true,
    enable_actions: true,
    debounce_ms: 500,
  });

  const [status, setStatus] = useState<MQTTStatus>({ connected: false });
  const [saving, setSaving] = useState(false);

  // Fetch current settings
  useEffect(() => {
    send("getMqttSettings", {}, resp => {
      if ("error" in resp) return;
      setSettings(resp.result as MQTTSettings);
    });
    send("getMqttStatus", {}, resp => {
      if ("error" in resp) return;
      setStatus(resp.result as MQTTStatus);
    });
  }, [send]);

  // Poll connection status
  useEffect(() => {
    const interval = setInterval(() => {
      send("getMqttStatus", {}, resp => {
        if ("error" in resp) return;
        setStatus(resp.result as MQTTStatus);
      });
    }, 5000);
    return () => clearInterval(interval);
  }, [send]);

  const handleSave = useCallback(() => {
    setSaving(true);
    send("setMqttSettings", { settings }, resp => {
      setSaving(false);
      if ("error" in resp) {
        notifications.error(m.mqtt_saved_error({ error: resp.error.message || "Unknown error" }));
        return;
      }
      notifications.success(m.mqtt_saved_success());
      // Refresh status after save
      setTimeout(() => {
        send("getMqttStatus", {}, statusResp => {
          if ("error" in statusResp) return;
          setStatus(statusResp.result as MQTTStatus);
        });
      }, 2000);
    });
  }, [send, settings]);

  const updateField = <K extends keyof MQTTSettings>(field: K, value: MQTTSettings[K]) => {
    setSettings(prev => ({ ...prev, [field]: value }));
  };

  return (
    <div className="space-y-4">
      <SettingsPageHeader title={m.settings_mqtt()} description={m.mqtt_description()} />

      <div className="space-y-4">
        <SettingsItem title={m.mqtt_enable_title()} description={m.mqtt_enable_description()}>
          <Checkbox
            checked={settings.enabled}
            onChange={e => updateField("enabled", e.target.checked)}
          />
        </SettingsItem>

        {settings.enabled && (
          <>
            <div className="flex items-center gap-2">
              <span
                className={`inline-block h-2 w-2 rounded-full ${
                  status.connected ? "bg-green-500" : "bg-red-500"
                }`}
              />
              <span className="text-xs text-slate-600 dark:text-slate-400">
                {status.connected ? m.mqtt_status_connected() : m.mqtt_status_disconnected()}
              </span>
            </div>

            <SettingsItem title={m.mqtt_broker_label()} description={m.mqtt_broker_description()}>
              <InputField
                size="SM"
                placeholder="192.168.1.2"
                value={settings.broker}
                onChange={e => updateField("broker", e.target.value)}
              />
            </SettingsItem>

            <SettingsItem title={m.mqtt_port_label()} description={m.mqtt_port_description()}>
              <InputField
                size="SM"
                type="number"
                placeholder="1883"
                value={settings.port.toString()}
                onChange={e => updateField("port", parseInt(e.target.value) || 1883)}
              />
            </SettingsItem>

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

            <SettingsItem
              title={m.mqtt_base_topic_label()}
              description={m.mqtt_base_topic_description()}
            >
              <InputField
                size="SM"
                placeholder="jetkvm"
                value={settings.base_topic}
                onChange={e => updateField("base_topic", e.target.value)}
              />
            </SettingsItem>

            <SettingsItem title={m.mqtt_use_tls_title()} description={m.mqtt_use_tls_description()}>
              <Checkbox
                checked={settings.use_tls}
                onChange={e => updateField("use_tls", e.target.checked)}
              />
            </SettingsItem>

            {settings.use_tls && (
              <SettingsItem
                title={m.mqtt_tls_insecure_title()}
                description={m.mqtt_tls_insecure_description()}
              >
                <Checkbox
                  checked={settings.tls_insecure}
                  onChange={e => updateField("tls_insecure", e.target.checked)}
                />
              </SettingsItem>
            )}

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
              title={m.mqtt_enable_actions_title()}
              description={m.mqtt_enable_actions_description()}
            >
              <Checkbox
                checked={settings.enable_actions}
                onChange={e => updateField("enable_actions", e.target.checked)}
              />
            </SettingsItem>

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
          </>
        )}

        <div>
          <Button
            size="SM"
            theme="primary"
            text={saving ? m.saving() : m.mqtt_save_button()}
            loading={saving}
            onClick={handleSave}
          />
        </div>
      </div>
    </div>
  );
}
