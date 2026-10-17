import { useEffect, useRef, useState } from "react";
import { useRTCStore } from "@hooks/stores";
import { useJsonRpc } from "@hooks/useJsonRpc";
import { Button } from "@components/Button";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsItem } from "@components/SettingsItem";
import notifications from "@/notifications";
import { m } from "@localizations/messages";

type WifiBand = "auto" | "2.4ghz" | "5ghz";
interface WifiSettings {
  available: boolean;
  applying: boolean;
  apply_failed: boolean;
  band: WifiBand;
}

export default function WifiBandCard() {
  const { send } = useJsonRpc();
  const channel = useRTCStore(state => state.rpcDataChannel);
  const [settings, setSettings] = useState<WifiSettings | null>(null);
  const [band, setBand] = useState<WifiBand>("auto");
  const [saving, setSaving] = useState(false);
  const edited = useRef(false);
  // Report a failed change only when this page applied it; the device keeps
  // apply_failed until the next change.
  const applied = useRef(false);
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (saveTimer.current) clearTimeout(saveTimer.current);
    },
    [],
  );
  useEffect(() => {
    if (channel?.readyState !== "open") return;
    let active = true;
    const refresh = () =>
      send("getWifiSettings", {}, response => {
        if (!active || !("result" in response)) return;
        const value = response.result as WifiSettings;
        setSettings(value);
        if (!edited.current) setBand(value.band);
        if (applied.current && !value.applying) {
          applied.current = false;
          if (value.apply_failed) notifications.error(m.wifi_band_failed());
        }
      });
    refresh();
    const timer = setInterval(refresh, 5000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [channel, send]);

  if (!settings?.available) return null;
  const save = () => {
    setSaving(true);
    saveTimer.current = setTimeout(() => {
      setSaving(false);
      notifications.error(m.wifi_band_save_timeout());
    }, 10000);
    send("setWifiBand", { band }, response => {
      if (saveTimer.current) clearTimeout(saveTimer.current);
      setSaving(false);
      if ("error" in response) {
        notifications.error(response.error.message);
        return;
      }
      edited.current = false;
      applied.current = true;
      setSettings(current => (current ? { ...current, band, applying: true } : current));
      notifications.success(m.wifi_band_saved());
      // Complete the WebSocket close handshake while Wi-Fi is still up,
      // before the device's delayed restart; otherwise TCP stays half-open.
      window.dispatchEvent(new Event("jetkvm-network-reconnect"));
    });
  };

  const changed = band !== settings.band;
  return (
    <div className="flex items-center justify-between gap-x-8" data-testid="wifi-band-settings">
      <SettingsItem title={m.wifi_band_title()} description={m.wifi_band_description()} />
      <div className="flex shrink-0 items-center gap-x-2">
        {changed && (
          <Button
            type="button"
            size="SM"
            theme="primary"
            text={m.wifi_band_apply()}
            loading={saving}
            onClick={save}
            disabled={saving || settings.applying || channel?.readyState !== "open"}
          />
        )}
        <SelectMenuBasic
          id="wifi-band"
          size="SM"
          aria-label={m.wifi_band_label()}
          value={band}
          disabled={saving || settings.applying}
          options={[
            { value: "auto", label: m.wifi_band_auto() },
            { value: "2.4ghz", label: m.wifi_band_2g() },
            { value: "5ghz", label: m.wifi_band_5g() },
          ]}
          onChange={event => {
            edited.current = true;
            setBand(event.target.value as WifiBand);
          }}
        />
      </div>
    </div>
  );
}
