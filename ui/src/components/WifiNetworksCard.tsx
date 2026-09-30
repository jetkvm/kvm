import { useCallback, useEffect, useRef, useState } from "react";
import { LuLock, LuPlus, LuTrash2 } from "react-icons/lu";
import { useRTCStore } from "@hooks/stores";
import { useJsonRpc } from "@hooks/useJsonRpc";
import { Button } from "@components/Button";
import Card from "@components/Card";
import { ConfirmDialog } from "@components/ConfirmDialog";
import { FieldError, InputFieldWithLabel } from "@components/InputField";
import { SettingsSectionHeader } from "@components/SettingsSectionHeader";
import WifiBandCard from "@components/WifiBandCard";
import notifications from "@/notifications";
import { cx } from "@/cva.config";
import { m } from "@localizations/messages";

interface SavedNetwork {
  ssid: string;
  connected: boolean;
  /* Saved with a password (older firmware omits it). */
  secure?: boolean;
}
interface WifiSettings {
  available: boolean;
  connected: boolean;
  applying: boolean;
  switching: boolean;
  static_ssid: string;
  connect_error: string;
  channel: number;
  rssi: number;
  max_networks: number;
  networks?: SavedNetwork[];
}
interface ScannedNetwork {
  ssid: string;
  rssi: number;
  secure: boolean;
  saved: boolean;
}
interface ScanResult {
  scanning: boolean;
  networks: ScannedNetwork[];
}

/*
 * One network being added, or a password change (`editing`). `manual` means
 * the SSID was typed ("Other network"), so the device may not see it now.
 */
interface NetworkForm {
  editing: boolean;
  manual: boolean;
  ssid: string;
  secure: boolean;
  password: string;
}
const emptyForm: NetworkForm = {
  editing: false,
  manual: false,
  ssid: "",
  secure: true,
  password: "",
};

function ssidError(ssid: string) {
  const bytes = new TextEncoder().encode(ssid).length;
  if (bytes === 0) return m.wifi_networks_ssid_required();
  return bytes > 32 ? m.wifi_networks_ssid_long() : null;
}

/* WPA takes 8-63 printable ASCII characters or a 64-digit hex key. A typed
 * network with an empty password is an open network. */
function passwordError(form: NetworkForm) {
  const { password } = form;
  if (password === "")
    return form.manual || !form.secure ? null : m.wifi_networks_password_required();
  if (!/^[\x20-\x7e]+$/.test(password)) return m.wifi_networks_password_characters();
  if (password.length < 8) return m.wifi_networks_password_short();
  if (password.length === 64 && /^[0-9a-fA-F]+$/.test(password)) return null;
  return password.length > 63 ? m.wifi_networks_password_long() : null;
}

function band(channel: number) {
  return channel > 14 ? "5 GHz" : "2.4 GHz";
}

/*
 * The dialogs keep the last value while they fade out, so their content does
 * not disappear before the panel does.
 */
function useLastValue<T>(value: T | null) {
  const last = useRef(value);
  if (value !== null) last.current = value;
  return last.current;
}

/* Enter confirms a dialog unless a button has focus; buttons handle Enter. */
function confirmOnEnter(action: () => void) {
  return (event: React.KeyboardEvent) => {
    if (event.key !== "Enter" || event.nativeEvent.isComposing) return;
    if ((event.target as HTMLElement).closest("button, a")) return;
    event.preventDefault();
    action();
  };
}

export default function WifiNetworksCard() {
  const { send } = useJsonRpc();
  const channel = useRTCStore(state => state.rpcDataChannel);
  const [settings, setSettings] = useState<WifiSettings | null>(null);
  const [scan, setScan] = useState<ScanResult>({ scanning: false, networks: [] });
  const [form, setForm] = useState<NetworkForm | null>(null);
  const [showErrors, setShowErrors] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [removing, setRemoving] = useState<SavedNetwork | null>(null);
  const [connecting, setConnecting] = useState<SavedNetwork | null>(null);
  const pendingConnect = useRef<string | null>(null);
  // While a switch is pending, poll quickly and close the session once the
  // device says it is leaving, not before.
  const [awaitingSwitch, setAwaitingSwitch] = useState(false);
  const sessionClosed = useRef(false);
  const scanTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Scan replies that arrive after the add dialog closed are ignored.
  const scanOpen = useRef(false);
  const shownForm = useLastValue(form);
  const shownRemoving = useLastValue(removing);
  const shownConnecting = useLastValue(connecting);

  const refresh = useCallback(
    () =>
      send("getWifiSettings", {}, response => {
        if (!("result" in response)) return;
        const value = response.result as WifiSettings;
        setSettings(value);
        // Report a switch to another network once the device has finished it.
        const ssid = pendingConnect.current;
        if (ssid !== null && value.switching && !sessionClosed.current) {
          sessionClosed.current = true;
          window.dispatchEvent(new Event("jetkvm-network-reconnect"));
        }
        if (ssid !== null && !value.applying) {
          pendingConnect.current = null;
          setAwaitingSwitch(false);
          if (value.connect_error)
            notifications.error(
              m.wifi_networks_connect_failed({ ssid, error: value.connect_error }),
            );
          else notifications.success(m.wifi_networks_connected({ ssid }));
        }
      }),
    [send],
  );

  useEffect(() => {
    if (channel?.readyState !== "open") return;
    refresh();
    const timer = setInterval(refresh, awaitingSwitch ? 1000 : 5000);
    return () => clearInterval(timer);
  }, [channel, refresh, awaitingSwitch]);

  // The device scans in the background and keeps the last results, so show
  // those at once and poll until a requested scan ends.
  const pollScan = useCallback(
    (refreshScan: boolean) => {
      if (scanTimer.current) clearTimeout(scanTimer.current);
      send("scanWifiNetworks", { refresh: refreshScan }, response => {
        if (!scanOpen.current || !("result" in response)) return;
        const result = response.result as ScanResult;
        setScan(result);
        if (result.scanning) scanTimer.current = setTimeout(() => pollScan(false), 1500);
      });
    },
    [send],
  );
  const adding = form !== null && !form.editing;
  useEffect(() => {
    if (!adding || channel?.readyState !== "open") return;
    scanOpen.current = true;
    pollScan(true);
    return () => {
      scanOpen.current = false;
      if (scanTimer.current) clearTimeout(scanTimer.current);
    };
  }, [adding, channel, pollScan]);

  if (!settings?.available) return null;
  const networks = settings.networks ?? [];
  const current = networks.find(network => network.connected)?.ssid ?? "";
  const full = networks.length >= settings.max_networks;
  const busy = settings.applying || channel?.readyState !== "open";
  // A new password for the current network is tested by using it; the
  // device keeps the old one if it fails.
  const editingCurrent = shownForm?.editing === true && shownForm.ssid === current;
  const update = (change: Partial<NetworkForm>) => {
    setFormError(null);
    setForm(value => (value ? { ...value, ...change } : value));
  };
  const openForm = (value: NetworkForm) => {
    setShowErrors(false);
    setFormError(null);
    setForm(value);
  };

  // The device first checks that the network is in range; it reports
  // `switching` just before it leaves, and refresh() then closes the session.
  const switchingNetwork = (ssid: string) => {
    pendingConnect.current = ssid;
    sessionClosed.current = false;
    setAwaitingSwitch(true);
    notifications.success(m.wifi_networks_connecting({ ssid }));
  };

  const save = () => {
    if (!form || pending) return;
    if (!form.manual && !form.editing && form.ssid === "") {
      setFormError(m.wifi_networks_pick());
      return;
    }
    if (ssidError(form.ssid) || passwordError(form)) {
      setShowErrors(true);
      return;
    }
    setPending(true);
    send(
      "saveWifiNetwork",
      { ssid: form.ssid, password: form.password, connect: editingCurrent },
      response => {
        setPending(false);
        if ("error" in response) {
          setFormError(response.error.message);
          return;
        }
        const ssid = form.ssid;
        setForm(null);
        // The device decides whether it tests the network now (for example a
        // new password for the network it is using).
        if ((response.result as { connecting?: boolean } | undefined)?.connecting) {
          switchingNetwork(ssid);
          return;
        }
        notifications.success(m.wifi_networks_saved({ ssid }));
        refresh();
      },
    );
  };

  const connect = () => {
    if (!connecting || pending) return;
    const { ssid } = connecting;
    setPending(true);
    send("connectWifiNetwork", { ssid }, response => {
      setPending(false);
      setConnecting(null);
      if ("error" in response) {
        notifications.error(response.error.message);
        return;
      }
      switchingNetwork(ssid);
    });
  };

  const remove = () => {
    if (!removing || pending) return;
    const network = removing;
    setPending(true);
    send("removeWifiNetwork", { ssid: network.ssid }, response => {
      setPending(false);
      setRemoving(null);
      if ("error" in response) {
        notifications.error(response.error.message);
        return;
      }
      notifications.success(m.wifi_networks_removed({ ssid: network.ssid }));
      if (network.connected) window.dispatchEvent(new Event("jetkvm-network-reconnect"));
      else refresh();
    });
  };

  const status = (network: SavedNetwork) => {
    const base = !network.connected
      ? m.wifi_networks_saved_badge()
      : !settings.connected || settings.channel === 0
        ? m.wifi_networks_connected_badge()
        : m.wifi_networks_connected_status({
            band: band(settings.channel),
            channel: settings.channel,
            rssi: settings.rssi,
          });
    // Static IPv4 settings belong to one network; the others use DHCP.
    return network.ssid === settings.static_ssid
      ? `${base} · ${m.wifi_networks_static_badge()}`
      : base;
  };

  const available = scan.networks.filter(network => !network.saved);
  const passwordShown =
    shownForm !== null &&
    (shownForm.editing || shownForm.manual || (shownForm.ssid !== "" && shownForm.secure));

  return (
    <div className="space-y-4" data-testid="wifi-networks-settings">
      <div className="flex items-center justify-between gap-x-8">
        <SettingsSectionHeader
          title={m.wifi_networks_title()}
          description={m.wifi_networks_description()}
        />
        <Button
          type="button"
          size="SM"
          theme="light"
          LeadingIcon={LuPlus}
          text={m.wifi_networks_add()}
          disabled={full || busy}
          onClick={() => openForm({ ...emptyForm })}
        />
      </div>
      <Card>
        <div className="w-full divide-y divide-slate-700/30 dark:divide-slate-600/30">
          {networks.map(network => (
            <div key={network.ssid} className="flex items-center justify-between gap-x-2 p-3">
              <div className="min-w-0 space-y-0.5">
                <p className="truncate text-sm leading-none font-semibold text-slate-900 dark:text-slate-100">
                  {network.ssid}
                </p>
                <p className="text-sm text-slate-600 dark:text-slate-400">{status(network)}</p>
              </div>
              <div className="flex shrink-0 items-center gap-x-2">
                {!network.connected && (
                  <Button
                    type="button"
                    size="XS"
                    theme="light"
                    text={m.wifi_networks_connect()}
                    disabled={busy}
                    onClick={() => setConnecting(network)}
                  />
                )}
                <Button
                  type="button"
                  size="XS"
                  theme="light"
                  text={m.wifi_networks_change_password()}
                  disabled={busy}
                  onClick={() =>
                    openForm({
                      ...emptyForm,
                      editing: true,
                      ssid: network.ssid,
                      // A network saved with a password needs one, unless the
                      // last scan shows its AP is open now; a saved open network
                      // is edited with an empty password.
                      secure:
                        (network.secure ?? true) &&
                        !scan.networks.some(seen => seen.ssid === network.ssid && !seen.secure),
                    })
                  }
                />
                <Button
                  type="button"
                  size="XS"
                  theme="danger"
                  LeadingIcon={LuTrash2}
                  aria-label={m.wifi_networks_remove()}
                  disabled={networks.length <= 1 || busy}
                  onClick={() => setRemoving(network)}
                />
              </div>
            </div>
          ))}
        </div>
      </Card>
      <WifiBandCard />
      <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />

      <div onKeyDown={confirmOnEnter(save)}>
        <ConfirmDialog
          open={form !== null}
          onClose={() => setForm(null)}
          title={
            shownForm?.editing
              ? m.wifi_networks_edit_title({ ssid: shownForm.ssid })
              : m.wifi_networks_add_title()
          }
          description={
            shownForm?.editing
              ? m.wifi_networks_edit_description()
              : m.wifi_networks_add_description()
          }
          confirmText={m.wifi_networks_save()}
          isConfirming={pending}
          onConfirm={save}
        >
          {shownForm && (
            <div className="mt-4 space-y-4">
              {!shownForm.editing && (
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between text-xs">
                    <span className="font-medium text-slate-700 dark:text-slate-300">
                      {m.wifi_networks_nearby()}
                    </span>
                    {scan.scanning ? (
                      <span className="text-slate-500">{m.wifi_networks_scanning()}</span>
                    ) : (
                      <button
                        type="button"
                        className="text-blue-700 hover:underline dark:text-blue-500"
                        onClick={() => pollScan(true)}
                      >
                        {m.wifi_networks_scan_again()}
                      </button>
                    )}
                  </div>
                  <Card>
                    <div className="max-h-52 w-full divide-y divide-slate-700/30 overflow-y-auto dark:divide-slate-600/30">
                      {available.map(network => (
                        <button
                          key={network.ssid}
                          type="button"
                          className={cx(
                            "flex w-full items-center justify-between gap-x-2 px-3 py-2 text-left text-sm",
                            !shownForm.manual && shownForm.ssid === network.ssid
                              ? "bg-blue-50 dark:bg-blue-900/30"
                              : "hover:bg-slate-50 dark:hover:bg-slate-800",
                          )}
                          onClick={() =>
                            update({
                              manual: false,
                              ssid: network.ssid,
                              secure: network.secure,
                              password: "",
                            })
                          }
                        >
                          <span className="truncate text-slate-900 dark:text-slate-100">
                            {network.ssid}
                          </span>
                          <span className="flex shrink-0 items-center gap-1.5 text-xs text-slate-500">
                            {network.secure && <LuLock className="size-3" />}
                            {network.rssi} dBm
                          </span>
                        </button>
                      ))}
                    </div>
                    <button
                      type="button"
                      className={cx(
                        "w-full border-t border-slate-700/30 px-3 py-2 text-left text-sm text-slate-900 dark:border-slate-600/30 dark:text-slate-100",
                        shownForm.manual
                          ? "bg-blue-50 dark:bg-blue-900/30"
                          : "hover:bg-slate-50 dark:hover:bg-slate-800",
                      )}
                      onClick={() => update({ manual: true, ssid: "", secure: true, password: "" })}
                    >
                      {m.wifi_networks_other()}
                    </button>
                  </Card>
                </div>
              )}
              {shownForm.manual && (
                <InputFieldWithLabel
                  size="SM"
                  id="wifi-network-ssid"
                  label={m.wifi_networks_ssid()}
                  description={m.wifi_networks_ssid_description()}
                  value={shownForm.ssid}
                  autoComplete="off"
                  spellCheck={false}
                  autoFocus
                  error={showErrors ? ssidError(shownForm.ssid) : null}
                  onChange={event => update({ ssid: event.target.value })}
                />
              )}
              {passwordShown && (
                <InputFieldWithLabel
                  size="SM"
                  type="password"
                  id="wifi-network-password"
                  label={m.wifi_networks_password()}
                  description={
                    shownForm.manual || !shownForm.secure
                      ? m.wifi_networks_password_optional()
                      : undefined
                  }
                  value={shownForm.password}
                  autoComplete="new-password"
                  autoFocus={shownForm.editing}
                  error={showErrors ? passwordError(shownForm) : null}
                  onChange={event => update({ password: event.target.value })}
                />
              )}
              {editingCurrent && (
                <p className="text-sm text-slate-600 dark:text-slate-300">
                  {m.wifi_networks_current_warning()}
                </p>
              )}
              {formError && <FieldError error={formError} />}
            </div>
          )}
        </ConfirmDialog>
      </div>

      <div onKeyDown={confirmOnEnter(connect)}>
        <ConfirmDialog
          open={connecting !== null}
          onClose={() => setConnecting(null)}
          title={m.wifi_networks_connect_title({ ssid: shownConnecting?.ssid ?? "" })}
          description={m.wifi_networks_connect_description({
            ssid: shownConnecting?.ssid ?? "",
            current,
          })}
          confirmText={m.wifi_networks_connect()}
          isConfirming={pending}
          onConfirm={connect}
        />
      </div>

      <div onKeyDown={confirmOnEnter(remove)}>
        <ConfirmDialog
          open={removing !== null}
          onClose={() => setRemoving(null)}
          variant="danger"
          title={m.wifi_networks_remove_title({ ssid: shownRemoving?.ssid ?? "" })}
          description={
            shownRemoving?.connected
              ? m.wifi_networks_remove_current({ ssid: shownRemoving.ssid })
              : m.wifi_networks_remove_description()
          }
          confirmText={m.wifi_networks_remove()}
          isConfirming={pending}
          onConfirm={remove}
        />
      </div>
    </div>
  );
}
