import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { LuCopy, LuFileUp, LuPlus, LuTrash2 } from "react-icons/lu";

import { Button } from "@components/Button";
import { Checkbox } from "@components/Checkbox";
import { ConfirmDialog } from "@components/ConfirmDialog";
import { InputFieldWithLabel } from "@components/InputField";
import LoadingSpinner from "@components/LoadingSpinner";
import { NestedSettingsGroup } from "@components/NestedSettingsGroup";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SettingsSectionHeader } from "@components/SettingsSectionHeader";
import { TextAreaWithLabel } from "@components/TextArea";
import { useCopyToClipboard } from "@components/useCopyToClipBoard";
import { useCapability } from "@hooks/stores";
import { useJsonRpc } from "@hooks/useJsonRpc";
import api from "@/api";
import { isOnDevice } from "@/main";
import notifications from "@/notifications";
import { DEVICE_API } from "@/ui.config";
import { formatters } from "@/utils";
import { m } from "@localizations/messages.js";
import type { LocalDevice } from "@routes/devices.$id";

interface WireguardPeer {
  name?: string;
  public_key: string;
  preshared_key?: string;
  endpoint?: string;
  allowed_ips: string[];
  persistent_keepalive: number;
}

interface WireguardSettings {
  enabled: boolean;
  private_key?: string;
  addresses: string[];
  listen_port?: number;
  peers: WireguardPeer[];
}

interface WireguardPeerStatus {
  name?: string;
  publicKey: string;
  endpoint?: string;
  secondsSinceHandshake?: number;
  rxBytes: number;
  txBytes: number;
}

interface WireguardStatus {
  enabled: boolean;
  up: boolean;
  publicKey?: string;
  listenPort?: number;
  error?: string;
  peers: WireguardPeerStatus[];
}

interface ImportWarning {
  code: "dns_ignored" | "option_ignored" | "default_route_removed";
  detail: string;
}

interface ImportResult {
  config: WireguardSettings;
  warnings: ImportWarning[];
}

/* The device sends this in place of a stored key; sending it back keeps the key. */
const SECRET_MASK = "********";
/* WireGuard re-handshakes every two minutes on an active tunnel. */
const RECENT_HANDSHAKE_SEC = 180;
const STATUS_POLL_MS = 5000;
const DEFAULT_KEEPALIVE = 25;

/* Lists are edited as comma-separated text. */
interface PeerForm {
  name: string;
  publicKey: string;
  presharedKey: string;
  endpoint: string;
  allowedIPs: string;
  keepalive: string;
}

interface Form {
  enabled: boolean;
  /* "" (none), SECRET_MASK (the stored key) or a key from an imported file */
  privateKey: string;
  addresses: string;
  listenPort: string;
  peers: PeerForm[];
}

const emptyPeer: PeerForm = {
  name: "",
  publicKey: "",
  presharedKey: "",
  endpoint: "",
  allowedIPs: "",
  keepalive: String(DEFAULT_KEEPALIVE),
};

function splitList(text: string) {
  return text.split(/[\s,]+/).filter(Boolean);
}

function toForm(settings: WireguardSettings): Form {
  return {
    enabled: settings.enabled,
    privateKey: settings.private_key ?? "",
    addresses: (settings.addresses ?? []).join(", "),
    listenPort: settings.listen_port ? String(settings.listen_port) : "",
    peers: (settings.peers ?? []).map(peer => ({
      name: peer.name ?? "",
      publicKey: peer.public_key,
      presharedKey: peer.preshared_key ?? "",
      endpoint: peer.endpoint ?? "",
      allowedIPs: (peer.allowed_ips ?? []).join(", "),
      keepalive: String(peer.persistent_keepalive ?? 0),
    })),
  };
}

function fromForm(form: Form): WireguardSettings {
  return {
    enabled: form.enabled,
    private_key: form.privateKey,
    addresses: splitList(form.addresses),
    listen_port: Number(form.listenPort) || 0,
    peers: form.peers.map(peer => ({
      name: peer.name.trim(),
      public_key: peer.publicKey.trim(),
      preshared_key: peer.presharedKey.trim(),
      endpoint: peer.endpoint.trim(),
      allowed_ips: splitList(peer.allowedIPs),
      persistent_keepalive: Number(peer.keepalive) || 0,
    })),
  };
}

type SaveAction = "connect" | "reconnect" | "disconnect" | "save";

/* What saving does to the tunnel, from its saved state and the checkbox. */
function saveAction(wasEnabled: boolean, enabled: boolean): SaveAction {
  if (enabled) return wasEnabled ? "reconnect" : "connect";
  return wasEnabled ? "disconnect" : "save";
}

const saveButtonTexts: Record<SaveAction, () => string> = {
  connect: () => m.wireguard_save_connect_button(),
  reconnect: () => m.wireguard_save_reconnect_button(),
  disconnect: () => m.wireguard_save_disconnect_button(),
  save: () => m.wireguard_save_button(),
};

const savedMessages: Record<SaveAction, () => string> = {
  connect: () => m.wireguard_saved_connect(),
  reconnect: () => m.wireguard_saved_reconnect(),
  disconnect: () => m.wireguard_saved_disconnect(),
  save: () => m.wireguard_saved(),
};

function rpcErrorMessage(error: { message: string; data?: unknown }) {
  return typeof error.data === "string" ? error.data : error.message;
}

function importWarningText(warning: ImportWarning) {
  switch (warning.code) {
    case "dns_ignored":
      return m.wireguard_import_dns_ignored({ dns: warning.detail });
    case "default_route_removed":
      return m.wireguard_import_default_route_removed({
        range: warning.detail,
      });
    default:
      return m.wireguard_import_option_ignored({ option: warning.detail });
  }
}

export default function SettingsWireguardRoute() {
  const { send } = useJsonRpc();
  const hasLocalLoopback = useCapability("local_loopback");
  const { copy } = useCopyToClipboard();

  const [status, setStatus] = useState<WireguardStatus | null>(null);
  const [saved, setSaved] = useState<Form | null>(null);
  const [form, setForm] = useState<Form | null>(null);
  const [saving, setSaving] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [confirmRegenerate, setConfirmRegenerate] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [importText, setImportText] = useState("");
  const fileInput = useRef<HTMLInputElement>(null);
  const [importWarnings, setImportWarnings] = useState<ImportWarning[]>([]);
  const [noPassword, setNoPassword] = useState(false);
  const [loopbackOnly, setLoopbackOnly] = useState(false);

  const refreshStatus = useCallback(() => {
    send("getWireguardStatus", {}, resp => {
      if ("error" in resp) return;
      setStatus(resp.result as WireguardStatus);
    });
  }, [send]);

  /* keepForm leaves unsaved edits in place and only updates what is saved. */
  const loadSettings = useCallback(
    (keepForm = false) => {
      send("getWireguardSettings", {}, resp => {
        if ("error" in resp) {
          notifications.error(m.wireguard_load_failed({ error: rpcErrorMessage(resp.error) }));
          return;
        }
        const next = toForm(resp.result as WireguardSettings);
        setSaved(next);
        if (!keepForm) setForm(next);
      });
    },
    [send],
  );

  useEffect(() => {
    loadSettings();
    refreshStatus();
  }, [loadSettings, refreshStatus]);

  // Keep the handshake and traffic numbers current while the tunnel is on.
  useEffect(() => {
    if (!status?.enabled) return;
    const timer = setInterval(refreshStatus, STATUS_POLL_MS);
    return () => clearInterval(timer);
  }, [status?.enabled, refreshStatus]);

  useEffect(() => {
    if (!isOnDevice) return;
    api
      .GET(`${DEVICE_API}/device`)
      .then(res => res.json() as Promise<LocalDevice>)
      .then(device => setNoPassword(device.authMode === "noPassword"))
      .catch(() => setNoPassword(false));
  }, []);

  useEffect(() => {
    if (!hasLocalLoopback) return;
    send("getLocalLoopbackOnly", {}, resp => {
      if ("error" in resp) return;
      setLoopbackOnly(resp.result as boolean);
    });
  }, [send, hasLocalLoopback]);

  const dirty = useMemo(() => JSON.stringify(form) !== JSON.stringify(saved), [form, saved]);

  const updateForm = useCallback((changes: Partial<Form>) => {
    setForm(current => (current ? { ...current, ...changes } : current));
  }, []);

  const updatePeer = useCallback((index: number, changes: Partial<PeerForm>) => {
    setForm(current =>
      current
        ? {
            ...current,
            peers: current.peers.map((peer, i) => (i === index ? { ...peer, ...changes } : peer)),
          }
        : current,
    );
  }, []);

  const save = useCallback(() => {
    if (!form) return;
    const action = saveAction(saved?.enabled ?? false, form.enabled);
    setSaving(true);
    send("setWireguardSettings", { settings: fromForm(form) }, resp => {
      setSaving(false);
      if ("error" in resp) {
        notifications.error(m.wireguard_save_failed({ error: rpcErrorMessage(resp.error) }));
        // a failed apply still saved the settings; keep the form for fixing
        loadSettings(true);
        refreshStatus();
        return;
      }
      notifications.success(savedMessages[action]());
      setImportWarnings([]);
      loadSettings();
      refreshStatus();
    });
  }, [form, saved?.enabled, send, loadSettings, refreshStatus]);

  const generateKey = useCallback(() => {
    setConfirmRegenerate(false);
    setGenerating(true);
    send("generateWireguardKey", {}, resp => {
      setGenerating(false);
      if ("error" in resp) {
        notifications.error(
          m.wireguard_generate_key_failed({
            error: rpcErrorMessage(resp.error),
          }),
        );
        return;
      }
      // The new key is stored on the device; keep any other unsaved edits.
      setSaved(current => (current ? { ...current, privateKey: SECRET_MASK } : current));
      updateForm({ privateKey: SECRET_MASK });
      refreshStatus();
    });
  }, [send, updateForm, refreshStatus]);

  const importConf = useCallback(() => {
    send("parseWireguardConfig", { text: importText }, resp => {
      if ("error" in resp) {
        notifications.error(m.wireguard_import_failed({ error: rpcErrorMessage(resp.error) }));
        return;
      }
      const result = resp.result as ImportResult;
      const imported = toForm(result.config);
      // A file can hold only an [Interface] or only a [Peer] section; keep
      // what it leaves out.
      setForm(current => ({
        enabled: current?.enabled ?? false,
        privateKey: imported.privateKey || current?.privateKey || "",
        addresses: imported.addresses || current?.addresses || "",
        listenPort: imported.listenPort || current?.listenPort || "",
        peers: imported.peers.length > 0 ? imported.peers : (current?.peers ?? []),
      }));
      setImportWarnings(result.warnings ?? []);
      setImportText("");
      setImportOpen(false);
      notifications.success(m.wireguard_import_success());
    });
  }, [send, importText]);

  if (status === null || form === null) {
    return (
      <div className="space-y-4">
        <SettingsPageHeader title={m.wireguard_title()} description={m.wireguard_description()} />
        <div className="flex items-center justify-center py-8">
          <LoadingSpinner className="h-6 w-6 text-blue-500" />
        </div>
      </div>
    );
  }

  const saveButtonText = () =>
    saving ? m.saving() : saveButtonTexts[saveAction(saved?.enabled ?? false, form.enabled)]();

  const hasStoredKey = form.privateKey === SECRET_MASK;
  const hasImportedKey = form.privateKey !== "" && !hasStoredKey;
  const peerStatus = new Map(status.peers.map(peer => [peer.publicKey, peer]));

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={m.wireguard_title()}
        description={m.wireguard_description()}
        action={form.enabled ? <StatusBadge status={status} /> : undefined}
      />

      {form.enabled && noPassword && (
        <Notice tone="warning">{m.wireguard_warning_no_password()}</Notice>
      )}
      {form.enabled && loopbackOnly && (
        <Notice tone="warning">{m.wireguard_warning_loopback()}</Notice>
      )}
      {status.error && <Notice tone="error">{m.wireguard_error({ error: status.error })}</Notice>}

      <SettingsItem
        title={m.wireguard_enable_title()}
        description={m.wireguard_enable_description()}
      >
        <Checkbox
          checked={form.enabled}
          onChange={e => updateForm({ enabled: e.target.checked })}
        />
      </SettingsItem>

      {form.enabled && (
        <>
          <div className="flex items-start justify-between gap-x-2">
            <SettingsSectionHeader
              title={m.wireguard_interface_title()}
              description={m.wireguard_interface_description()}
            />
            <Button
              size="SM"
              theme="light"
              type="button"
              text={m.wireguard_import()}
              data-testid="wireguard-import"
              LeadingIcon={LuFileUp}
              onClick={() => setImportOpen(open => !open)}
            />
          </div>

          {importOpen && (
            <NestedSettingsGroup>
              <input
                ref={fileInput}
                type="file"
                accept=".conf,.txt,text/plain"
                className="hidden"
                onChange={e => {
                  const file = e.target.files?.[0];
                  e.target.value = "";
                  if (!file) return;
                  file
                    .text()
                    .then(setImportText)
                    .catch(() => notifications.error(m.wireguard_import_file_failed()));
                }}
              />
              <TextAreaWithLabel
                label={m.wireguard_import_label()}
                description={m.wireguard_import_description()}
                rows={8}
                className="font-mono text-xs"
                value={importText}
                onChange={e => setImportText(e.target.value)}
                spellCheck={false}
                autoComplete="off"
              />
              <div className="flex items-center gap-x-2">
                <Button
                  size="SM"
                  theme="primary"
                  type="button"
                  text={m.wireguard_import_apply()}
                  data-testid="wireguard-import-apply"
                  disabled={importText.trim() === ""}
                  onClick={importConf}
                />
                <Button
                  size="SM"
                  theme="light"
                  type="button"
                  text={m.wireguard_import_file()}
                  data-testid="wireguard-import-file"
                  onClick={() => fileInput.current?.click()}
                />
                <Button
                  size="SM"
                  theme="light"
                  type="button"
                  text={m.cancel()}
                  data-testid="wireguard-import-cancel"
                  onClick={() => {
                    setImportText("");
                    setImportOpen(false);
                  }}
                />
              </div>
            </NestedSettingsGroup>
          )}

          {importWarnings.length > 0 && (
            <Notice tone="info">
              <ul className="list-disc space-y-1 pl-4">
                {importWarnings.map((warning, i) => (
                  <li key={i}>{importWarningText(warning)}</li>
                ))}
              </ul>
            </Notice>
          )}

          <div className="space-y-2">
            <div className="flex items-center justify-between gap-x-2">
              <div>
                <div className="text-sm font-semibold">{m.wireguard_public_key_title()}</div>
                <div className="text-xs text-slate-600 dark:text-slate-400">
                  {m.wireguard_public_key_description()}
                </div>
              </div>
              <Button
                size="XS"
                theme="light"
                type="button"
                text={hasStoredKey ? m.wireguard_regenerate_key() : m.wireguard_generate_key()}
                data-testid="wireguard-generate-key"
                loading={generating}
                onClick={() =>
                  hasStoredKey || hasImportedKey ? setConfirmRegenerate(true) : generateKey()
                }
              />
            </div>
            {hasImportedKey ? (
              <p className="text-sm text-slate-600 dark:text-slate-400">
                {m.wireguard_public_key_imported()}
              </p>
            ) : hasStoredKey && status.publicKey ? (
              <div className="flex items-center gap-x-2">
                <code className="min-w-0 flex-1 truncate rounded-sm bg-slate-100 px-2 py-1 font-mono text-[13px] dark:bg-slate-800">
                  {status.publicKey}
                </code>
                <Button
                  size="XS"
                  theme="light"
                  type="button"
                  text={m.wireguard_copy()}
                  data-testid="wireguard-copy-public-key"
                  LeadingIcon={LuCopy}
                  onClick={async () => {
                    if (await copy(status.publicKey ?? "")) {
                      notifications.success(m.wireguard_copied());
                    }
                  }}
                />
              </div>
            ) : (
              <p className="text-sm text-slate-600 dark:text-slate-400">
                {m.wireguard_public_key_none()}
              </p>
            )}
          </div>

          <InputFieldWithLabel
            size="SM"
            label={m.wireguard_addresses_label()}
            description={m.wireguard_addresses_description()}
            placeholder="10.8.0.5/24"
            value={form.addresses}
            onChange={e => updateForm({ addresses: e.target.value })}
          />

          <InputFieldWithLabel
            size="SM"
            type="number"
            min={1}
            max={65535}
            label={m.wireguard_listen_port_label()}
            description={m.wireguard_listen_port_description()}
            value={form.listenPort}
            onChange={e => updateForm({ listenPort: e.target.value })}
          />

          <SettingsSectionHeader
            title={m.wireguard_peers_title()}
            description={m.wireguard_peers_description()}
          />
          {form.peers.map((peer, index) => (
            <PeerFields
              key={index}
              index={index}
              peer={peer}
              status={peerStatus.get(peer.publicKey.trim())}
              onChange={changes => updatePeer(index, changes)}
              onRemove={() => updateForm({ peers: form.peers.filter((_, i) => i !== index) })}
            />
          ))}
          <Button
            size="SM"
            theme="light"
            type="button"
            text={m.wireguard_peer_add()}
            data-testid="wireguard-add-peer"
            LeadingIcon={LuPlus}
            onClick={() => updateForm({ peers: [...form.peers, { ...emptyPeer }] })}
          />
        </>
      )}

      <div className="flex items-center gap-x-2 border-t border-slate-800/10 pt-4 dark:border-slate-300/20">
        <Button
          size="SM"
          theme="primary"
          type="button"
          text={saveButtonText()}
          data-testid="wireguard-save"
          loading={saving}
          onClick={save}
        />
        {dirty && (
          <Button
            size="SM"
            theme="light"
            type="button"
            text={m.wireguard_discard()}
            data-testid="wireguard-discard"
            disabled={saving}
            onClick={() => {
              setForm(saved);
              setImportWarnings([]);
            }}
          />
        )}
      </div>

      <ConfirmDialog
        open={confirmRegenerate}
        onClose={() => setConfirmRegenerate(false)}
        title={m.wireguard_regenerate_confirm_title()}
        description={m.wireguard_regenerate_confirm_description()}
        variant="warning"
        confirmText={m.wireguard_regenerate_key()}
        onConfirm={generateKey}
      />
    </div>
  );
}

function PeerFields({
  index,
  peer,
  status,
  onChange,
  onRemove,
}: {
  index: number;
  peer: PeerForm;
  status?: WireguardPeerStatus;
  onChange: (changes: Partial<PeerForm>) => void;
  onRemove: () => void;
}) {
  return (
    <NestedSettingsGroup>
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium">
          {peer.name.trim() || m.wireguard_peer_title({ number: index + 1 })}
        </span>
        <Button
          size="XS"
          theme="light"
          type="button"
          text={m.wireguard_peer_remove()}
          data-testid="wireguard-remove-peer"
          LeadingIcon={LuTrash2}
          onClick={onRemove}
        />
      </div>

      {status && <PeerStatusRows status={status} />}

      <InputFieldWithLabel
        size="SM"
        label={m.wireguard_peer_name_label()}
        placeholder={m.wireguard_peer_name_placeholder()}
        value={peer.name}
        onChange={e => onChange({ name: e.target.value })}
      />
      <InputFieldWithLabel
        size="SM"
        label={m.wireguard_peer_public_key_label()}
        className="font-mono"
        value={peer.publicKey}
        onChange={e => onChange({ publicKey: e.target.value })}
        spellCheck={false}
        autoComplete="off"
      />
      <InputFieldWithLabel
        size="SM"
        label={m.wireguard_peer_endpoint_label()}
        description={m.wireguard_peer_endpoint_description()}
        placeholder="vpn.example.com:51820"
        value={peer.endpoint}
        onChange={e => onChange({ endpoint: e.target.value })}
        spellCheck={false}
      />
      <InputFieldWithLabel
        size="SM"
        label={m.wireguard_peer_allowed_ips_label()}
        description={m.wireguard_peer_allowed_ips_description()}
        placeholder="10.8.0.0/24"
        value={peer.allowedIPs}
        onChange={e => onChange({ allowedIPs: e.target.value })}
        spellCheck={false}
      />
      <InputFieldWithLabel
        size="SM"
        type="number"
        min={0}
        max={65535}
        label={m.wireguard_peer_keepalive_label()}
        description={m.wireguard_peer_keepalive_description()}
        value={peer.keepalive}
        onChange={e => onChange({ keepalive: e.target.value })}
      />
      <InputFieldWithLabel
        size="SM"
        type="password"
        label={m.wireguard_peer_preshared_key_label()}
        description={m.wireguard_peer_preshared_key_description()}
        className="font-mono"
        value={peer.presharedKey}
        onChange={e => onChange({ presharedKey: e.target.value })}
        autoComplete="new-password"
      />
    </NestedSettingsGroup>
  );
}

function PeerStatusRows({ status }: { status: WireguardPeerStatus }) {
  const handshake =
    status.secondsSinceHandshake === undefined
      ? m.wireguard_peer_handshake_never()
      : (formatters.timeAgo(new Date(Date.now() - status.secondsSinceHandshake * 1000)) ?? "");
  const rows: [string, string][] = [[m.wireguard_peer_handshake(), handshake]];
  if (status.endpoint) rows.push([m.wireguard_peer_endpoint_label(), status.endpoint]);
  rows.push([
    m.wireguard_peer_transfer(),
    `${formatters.bytes(status.rxBytes)} / ${formatters.bytes(status.txBytes)}`,
  ]);

  return (
    <div className="space-y-1">
      {rows.map(([label, value]) => (
        <div key={label} className="flex justify-between gap-x-2">
          <span className="text-sm text-slate-600 dark:text-slate-400">{label}</span>
          <span className="truncate font-mono text-[13px] font-medium">{value}</span>
        </div>
      ))}
    </div>
  );
}

function Notice({
  tone,
  children,
}: {
  tone: "info" | "warning" | "error";
  children: React.ReactNode;
}) {
  const tones = {
    info: "border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-900/50 dark:bg-blue-900/20 dark:text-blue-300",
    warning:
      "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-300",
    error:
      "border-red-200 bg-red-50 text-red-800 dark:border-red-900/50 dark:bg-red-900/20 dark:text-red-300",
  };
  return <div className={`rounded-sm border px-3 py-2 text-sm ${tones[tone]}`}>{children}</div>;
}

/* Same look as the MQTT page's connection badge. */
function StatusBadge({ status }: { status: WireguardStatus }) {
  const connected = status.peers.some(
    peer =>
      peer.secondsSinceHandshake !== undefined && peer.secondsSinceHandshake < RECENT_HANDSHAKE_SEC,
  );
  let state: { dotClass: string; label: string };
  if (status.error) {
    state = { dotClass: "bg-red-500", label: m.wireguard_status_error() };
  } else if (!status.enabled) {
    state = { dotClass: "bg-slate-400", label: m.wireguard_status_off() };
  } else if (status.up && connected) {
    state = { dotClass: "bg-green-500", label: m.wireguard_status_connected() };
  } else {
    state = { dotClass: "bg-yellow-500 animate-pulse", label: m.wireguard_status_connecting() };
  }

  return (
    <div className="flex items-center gap-x-1.5 rounded-full bg-slate-100 px-2.5 py-1 dark:bg-slate-700/50">
      <span className={`inline-block h-1.5 w-1.5 shrink-0 rounded-full ${state.dotClass}`} />
      <span className="text-xs font-medium text-slate-600 dark:text-slate-300">{state.label}</span>
    </div>
  );
}
