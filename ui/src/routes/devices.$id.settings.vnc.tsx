import { useCallback, useEffect, useState } from "react";

import { useJsonRpc } from "@hooks/useJsonRpc";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SettingsSectionHeader } from "@components/SettingsSectionHeader";
import { SettingsItem } from "@components/SettingsItem";
import { NestedSettingsGroup } from "@components/NestedSettingsGroup";
import InputField from "@components/InputField";
import { Checkbox } from "@components/Checkbox";
import { Button } from "@components/Button";
import LoadingSpinner from "@components/LoadingSpinner";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

interface VNCConfig {
  enabled: boolean;
  port: number;
  password: string;
}

const DEFAULT_PORT = 5900;
const PASSWORD_SENTINEL = "********";

export default function SettingsVncRoute() {
  const { send } = useJsonRpc();

  const [settings, setSettings] = useState<VNCConfig>({
    enabled: false,
    port: DEFAULT_PORT,
    password: "",
  });
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    send("getVNCConfig", {}, resp => {
      if ("error" in resp) {
        setLoadError(resp.error.message || m.unknown_error());
        setLoading(false);
        return;
      }
      setSettings(resp.result as VNCConfig);
      setLoading(false);
    });
  }, [send]);

  const validate = useCallback((): boolean => {
    const errs: Record<string, string> = {};
    if (settings.port <= 0 || settings.port > 65535) {
      errs.port = "Port must be between 1 and 65535";
    }
    setFieldErrors(errs);
    return Object.keys(errs).length === 0;
  }, [settings]);

  const handleSave = useCallback(() => {
    if (!validate()) return;

    setSaving(true);
    send("setVNCConfig", { config: settings }, resp => {
      setSaving(false);
      if ("error" in resp) {
        notifications.error(m.vnc_saved_error({ error: resp.error.message || m.unknown_error() }));
        return;
      }
      notifications.success(m.vnc_saved_success());
      // Refresh the masked password if a value was just set.
      if (settings.password && settings.password !== PASSWORD_SENTINEL) {
        setSettings(prev => ({ ...prev, password: PASSWORD_SENTINEL }));
      }
    });
  }, [send, settings, validate]);

  const updateField = <K extends keyof VNCConfig>(field: K, value: VNCConfig[K]) => {
    setSettings(prev => ({ ...prev, [field]: value }));
  };

  if (loading) {
    return (
      <div className="space-y-4">
        <SettingsPageHeader title={m.settings_vnc()} description={m.vnc_description()} />
        <div className="flex items-center justify-center py-8">
          <LoadingSpinner className="h-6 w-6 text-blue-500" />
        </div>
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="space-y-4">
        <SettingsPageHeader title={m.settings_vnc()} description={m.vnc_description()} />
        <div className="rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-400">
          {m.vnc_load_error({ error: loadError })}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <SettingsPageHeader title={m.settings_vnc()} description={m.vnc_description()} />

      <div className="space-y-4">
        <SettingsItem title={m.vnc_enable_title()} description={m.vnc_enable_description()}>
          <Checkbox
            checked={settings.enabled}
            onChange={e => updateField("enabled", e.target.checked)}
          />
        </SettingsItem>

        {settings.enabled && (
          <>
            <SettingsItem title={m.vnc_port_label()} description={m.vnc_port_description()}>
              <InputField
                size="SM"
                type="number"
                placeholder={DEFAULT_PORT.toString()}
                value={settings.port.toString()}
                error={fieldErrors.port}
                onChange={e => {
                  updateField("port", parseInt(e.target.value) || DEFAULT_PORT);
                  if (fieldErrors.port) setFieldErrors(prev => ({ ...prev, port: "" }));
                }}
              />
            </SettingsItem>

            <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />
            <SettingsSectionHeader
              title={m.vnc_section_security()}
              description={m.vnc_section_security_description()}
            />
            <NestedSettingsGroup>
              <SettingsItem
                title={m.vnc_password_label()}
                description={m.vnc_password_description()}
              >
                <InputField
                  size="SM"
                  type="password"
                  placeholder={m.vnc_password_placeholder()}
                  value={settings.password}
                  onChange={e => updateField("password", e.target.value)}
                />
              </SettingsItem>
            </NestedSettingsGroup>

            <div className="rounded-md border border-blue-200 bg-blue-50 p-3 text-xs text-blue-700 dark:border-blue-800 dark:bg-blue-900/20 dark:text-blue-300">
              {m.vnc_required_client_help()}
            </div>
          </>
        )}

        {/* Save button is rendered unconditionally so users can also
            persist the disabled state (unchecking the toggle). */}
        <div className="flex items-center gap-x-2 pt-2">
          <Button
            size="SM"
            theme="primary"
            text={saving ? m.saving() : m.vnc_save_button()}
            loading={saving}
            onClick={handleSave}
          />
        </div>
      </div>
    </div>
  );
}
