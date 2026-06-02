import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { LuBell, LuRefreshCw, LuX } from "react-icons/lu";

import api from "@/api";
import { DEVICE_API } from "@/ui.config";
import notifications from "@/notifications";
import { cx } from "@/cva.config";
import Card from "@components/Card";
import { useUiStore } from "@hooks/stores";

type CompanionPairRequest = {
  request_id: string;
  remote_addr: string;
  user_agent?: string;
  created_at?: number;
};

type CompanionStatus = {
  companion_id: string;
  remote_addr?: string;
  remote_hostname?: string;
  has_report?: boolean;
  last_seen_unix_milli?: number;
  notification_permission_granted?: boolean;
  display_over_apps_permission_granted?: boolean;
  battery_unrestricted_granted?: boolean;
  paired_jetkvm_urls?: string[];
  visible_ips?: string[];
  jetkvm_usb_identity?: string;
  target_type?: string;
  preferred_mouse_mode?: string;
  display_width?: number;
  display_height?: number;
  evidence?: string[];
  peripherals?: Record<string, boolean>;
  pending_actions?: string[];
};

type VisibleIP = {
  ip: string;
  hostname?: string;
  source?: string;
  interface?: string;
};

type PairRequestsResponse = {
  requests?: CompanionPairRequest[];
};

type CompanionStatusResponse = {
  companions?: CompanionStatus[];
  visible_ips?: VisibleIP[];
};

const permissionLabels: Record<string, string> = {
  notifications: "Notifications",
  overlay: "Display over apps",
  battery: "Unrestricted battery",
};

const permissionDescriptors = [
  {
    key: "notifications",
    granted: (companion: CompanionStatus) => !!companion.notification_permission_granted,
  },
  {
    key: "overlay",
    granted: (companion: CompanionStatus) => !!companion.display_over_apps_permission_granted,
  },
  {
    key: "battery",
    granted: (companion: CompanionStatus) => !!companion.battery_unrestricted_granted,
  },
] as const;

export default function CompanionRequestCenter({
  compact = false,
  forceOpen,
  hideTrigger = false,
  onOpen,
  onClose,
}: {
  compact?: boolean;
  forceOpen?: boolean;
  hideTrigger?: boolean;
  onOpen?: () => void;
  onClose?: () => void;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [requests, setRequests] = useState<CompanionPairRequest[]>([]);
  const [companions, setCompanions] = useState<CompanionStatus[]>([]);
  const [visibleIps, setVisibleIps] = useState<VisibleIP[]>([]);
  const [otpById, setOtpById] = useState<Record<string, string>>({});
  const [companionUrl, setCompanionUrl] = useState("");
  const [initiatedOtp, setInitiatedOtp] = useState("");
  const setDisableVideoFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);

  const refresh = useCallback(async () => {
    try {
      const [requestsResp, statusResp] = await Promise.all([
        api.GET(`${DEVICE_API}/companion/pair/requests`),
        api.GET(`${DEVICE_API}/companion/status`),
      ]);
      if (requestsResp.ok) {
        const body = (await requestsResp.json()) as PairRequestsResponse;
        setRequests(body.requests || []);
      }
      if (statusResp.ok) {
        const body = (await statusResp.json()) as CompanionStatusResponse;
        setCompanions(body.companions || []);
        setVisibleIps(body.visible_ips || []);
      }
    } catch {
      // Transient failures are expected while the backend is rebooting.
    }
  }, []);

  useEffect(() => {
    void refresh();
    const id = window.setInterval(() => void refresh(), 3000);
    return () => window.clearInterval(id);
  }, [refresh]);

  useEffect(() => {
    if (!(forceOpen ?? open)) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!panelRef.current?.contains(event.target as Node)) {
        onClose?.();
        setOpen(false);
      }
    };
    window.addEventListener("pointerdown", onPointerDown);
    return () => window.removeEventListener("pointerdown", onPointerDown);
  }, [forceOpen, onClose, open]);

  const approve = useCallback(
    async (request: CompanionPairRequest) => {
      const otp = (otpById[request.request_id] || "").trim();
      if (!/^\d{6}$/.test(otp)) {
        notifications.error("Enter the 6 digit companion code.");
        return;
      }
      const resp = await api.POST(`${DEVICE_API}/companion/pair/${request.request_id}/approve`, {
        otp,
      });
      if (!resp.ok) {
        notifications.error("Companion pairing failed.");
        return;
      }
      notifications.success("Companion paired.");
      setOtpById(current => {
        const next = { ...current };
        delete next[request.request_id];
        return next;
      });
      void refresh();
    },
    [otpById, refresh],
  );

  const reject = useCallback(
    async (request: CompanionPairRequest) => {
      await api.POST(`${DEVICE_API}/companion/pair/${request.request_id}/reject`, {});
      notifications.success("Companion pairing rejected.");
      void refresh();
    },
    [refresh],
  );

  const initiate = useCallback(
    async (rawUrl?: string) => {
      const url = (rawUrl ?? companionUrl).trim();
      if (!url) {
        notifications.error("Enter the companion address.");
        return;
      }
      const resp = await api.POST(`${DEVICE_API}/companion/pair/initiate`, {
        companion_url: url,
      });
      if (!resp.ok) {
        notifications.error("Failed to notify companion over HTTPS.");
        return;
      }
      const body = (await resp.json()) as { otp?: string };
      setInitiatedOtp(body.otp || "");
      notifications.success("Pairing request sent.");
      void refresh();
    },
    [companionUrl, refresh],
  );

  const unpair = useCallback(async () => {
    const resp = await api.POST(`${DEVICE_API}/companion/unpair-admin`, {});
    if (!resp.ok) {
      notifications.error("Failed to unpair companion.");
      return;
    }
    notifications.success("Companion unpaired.");
    setInitiatedOtp("");
    void refresh();
  }, [refresh]);

  const unpairCompanion = useCallback(
    async (companionID: string) => {
      const resp = await api.POST(`${DEVICE_API}/companion/${companionID}/unpair-admin`, {});
      if (!resp.ok) {
        notifications.error("Failed to unpair companion.");
        return;
      }
      notifications.success("Companion unpaired.");
      void refresh();
    },
    [refresh],
  );

  const requestPermission = useCallback(
    async (companionID: string, permission: string) => {
      const resp = await api.POST(`${DEVICE_API}/companion/${companionID}/request-permission`, {
        permission,
      });
      if (!resp.ok) {
        notifications.error("Failed to queue permission request.");
        return;
      }
      notifications.success(`${permissionLabels[permission]} request queued.`);
      void refresh();
    },
    [refresh],
  );

  const pairedHosts = useMemo(() => {
    const hosts = new Set<string>();
    for (const companion of companions) {
      const remoteHost = companion.remote_addr?.split(":")[0];
      if (remoteHost) hosts.add(remoteHost);
      for (const url of companion.paired_jetkvm_urls || []) {
        try {
          hosts.add(new URL(url).hostname);
        } catch {
          // Ignore malformed companion-reported URLs.
        }
      }
    }
    return hosts;
  }, [companions]);

  const candidateIps = useMemo(() => {
    const seen = new Set<string>();
    const candidates: VisibleIP[] = [];
    for (const entry of visibleIps) {
      if (!entry.ip || seen.has(entry.ip) || pairedHosts.has(entry.ip)) continue;
      seen.add(entry.ip);
      candidates.push(entry);
    }
    for (const companion of companions) {
      for (const ip of companion.visible_ips || []) {
        if (!ip || seen.has(ip) || pairedHosts.has(ip)) continue;
        seen.add(ip);
        candidates.push({ ip, source: "companion" });
      }
    }
    return candidates.sort((a, b) => candidateIPSortKey(a).localeCompare(candidateIPSortKey(b)));
  }, [companions, pairedHosts, visibleIps]);

  const count = requests.length;
  const isOpen = forceOpen ?? open;

  useEffect(() => {
    if (!isOpen) return;
    setDisableVideoFocusTrap(true);
    return () => setDisableVideoFocusTrap(false);
  }, [isOpen, setDisableVideoFocusTrap]);

  const openPanel = useCallback(() => {
    onOpen?.();
    setOpen(true);
  }, [onOpen]);
  const closePanel = useCallback(() => {
    onClose?.();
    setOpen(false);
  }, [onClose]);

  return (
    <div className={compact ? "w-full" : "relative h-full"}>
      {!hideTrigger && (
        <button
          type="button"
          className={cx(
            compact
              ? "flex min-h-11 w-full items-center gap-3 rounded-md px-3 text-left text-sm text-white hover:bg-white/15 active:bg-white/25"
              : "relative flex h-full items-center rounded-md border border-slate-800/20 bg-white px-2 py-1.5 text-slate-700 dark:border-slate-600 dark:bg-slate-800 dark:text-white",
          )}
          onClick={openPanel}
        >
          <LuBell className="h-5 w-5 shrink-0" />
          {compact && <span className="min-w-0 truncate">Companion</span>}
          {count > 0 && (
            <span className="ml-2 flex h-5 min-w-5 items-center justify-center rounded-full bg-blue-600 px-1.5 text-xs font-semibold text-white">
              {count}
            </span>
          )}
        </button>
      )}

      {isOpen && (
        <div
          ref={panelRef}
          className={cx(
            "z-[80] w-[28rem] max-w-[calc(100vw-24px)] p-px",
            compact ? "fixed top-3 right-3" : "absolute top-full right-0 mt-1",
          )}
        >
          <Card className="max-h-[calc(100dvh-24px)] overflow-auto p-3 shadow-2xl">
            <div className="mb-2 flex items-center justify-between gap-2">
              <div className="text-sm font-semibold text-slate-900 dark:text-white">
                Android companion
              </div>
              <div className="flex gap-1">
                <button
                  type="button"
                  aria-label="Refresh"
                  className="rounded-md p-1 text-slate-500 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
                  onClick={() => void refresh()}
                >
                  <LuRefreshCw className="h-4 w-4" />
                </button>
                <button
                  type="button"
                  aria-label="Close"
                  className="rounded-md p-1 text-slate-500 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
                  onClick={closePanel}
                >
                  <LuX className="h-4 w-4" />
                </button>
              </div>
            </div>

            <Section title="Paired companions">
              {companions.length === 0 ? (
                <Muted>No paired companions.</Muted>
              ) : (
                <div className="space-y-2">
                  {companions.map(companion => (
                    <div
                      key={companion.companion_id}
                      className="rounded-md border border-slate-800/10 p-2 dark:border-slate-300/20"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0">
                          <div className="text-sm font-medium break-all text-slate-900 dark:text-white">
                            {companion.remote_hostname ||
                              companion.remote_addr ||
                              companion.companion_id}
                          </div>
                          {companion.remote_hostname && companion.remote_addr && (
                            <div className="text-xs break-all text-slate-500 dark:text-slate-400">
                              {companion.remote_addr}
                            </div>
                          )}
                          {!companion.has_report && (
                            <div className="text-xs font-medium text-amber-600 dark:text-amber-300">
                              Waiting for signed status report
                            </div>
                          )}
                        </div>
                        <button
                          type="button"
                          className="rounded-md px-2 py-1 text-xs text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
                          onClick={() => void unpairCompanion(companion.companion_id)}
                        >
                          Unpair
                        </button>
                      </div>
                      <div className="mt-1 grid grid-cols-1 gap-1 text-xs text-slate-600 dark:text-slate-300">
                        {companion.has_report ? (
                          <>
                            <StatusRow
                              label="Notifications"
                              ok={!!companion.notification_permission_granted}
                            />
                            <StatusRow
                              label="Display over apps"
                              ok={!!companion.display_over_apps_permission_granted}
                            />
                            <StatusRow
                              label="Unrestricted battery"
                              ok={!!companion.battery_unrestricted_granted}
                            />
                          </>
                        ) : (
                          <Detail label="Permissions" value="Unknown" />
                        )}
                        <Detail
                          label="Identity"
                          value={companion.jetkvm_usb_identity || "Unknown"}
                        />
                        <Detail
                          label="Display"
                          value={
                            companion.display_width && companion.display_height
                              ? `${companion.display_width}x${companion.display_height}`
                              : "Unknown"
                          }
                        />
                        <Detail
                          label="Peripherals"
                          value={
                            Object.keys(companion.peripherals || {})
                              .filter(key => companion.peripherals?.[key])
                              .join(", ") || "None"
                          }
                        />
                        <Detail
                          label="Visible IPs"
                          value={(companion.visible_ips || []).join(", ") || "None"}
                        />
                      </div>
                      {companion.has_report && (
                        <div className="mt-2 flex flex-wrap gap-2">
                          {permissionDescriptors
                            .filter(permission => !permission.granted(companion))
                            .map(permission => (
                              <button
                                key={permission.key}
                                type="button"
                                className="rounded-md bg-blue-700 px-2 py-1 text-xs font-medium text-white hover:bg-blue-800"
                                onClick={() =>
                                  void requestPermission(companion.companion_id, permission.key)
                                }
                              >
                                Request {permissionLabels[permission.key]}
                              </button>
                            ))}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </Section>

            <Section title="Visible LAN/VPN IPs">
              {candidateIps.length === 0 ? (
                <Muted>No unpaired IPs visible.</Muted>
              ) : (
                <div className="space-y-1">
                  {candidateIps.map(entry => (
                    <div
                      key={`${entry.source || "ip"}-${entry.ip}`}
                      className="flex items-center gap-2 text-sm"
                    >
                      <div className="min-w-0 flex-1">
                        <div className="break-all text-slate-900 dark:text-white">
                          {entry.hostname || entry.ip}
                        </div>
                        {entry.hostname && (
                          <div className="text-xs break-all text-slate-500 dark:text-slate-400">
                            {entry.ip}
                          </div>
                        )}
                        <div className="text-xs text-slate-500 dark:text-slate-400">
                          {[entry.source, entry.interface].filter(Boolean).join(" / ") || "visible"}
                        </div>
                      </div>
                      <button
                        type="button"
                        className="rounded-md bg-blue-700 px-2 py-1 text-xs font-medium text-white hover:bg-blue-800"
                        onClick={() => void initiate(`https://${entry.ip}:8787`)}
                      >
                        Pair
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </Section>

            <Section title="Pairing requests">
              {requests.length === 0 ? (
                <Muted>No pending requests.</Muted>
              ) : (
                <div className="space-y-3">
                  {requests.map(request => (
                    <div
                      key={request.request_id}
                      className="rounded-md border border-slate-800/10 p-2 dark:border-slate-300/20"
                    >
                      <div className="text-sm font-medium text-slate-900 dark:text-white">
                        Companion from {request.remote_addr}
                      </div>
                      <input
                        className="mt-2 w-full rounded-md border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-900 outline-none focus:border-blue-600 dark:border-slate-600 dark:bg-slate-900 dark:text-white"
                        inputMode="numeric"
                        maxLength={6}
                        placeholder="6 digit code"
                        value={otpById[request.request_id] || ""}
                        onChange={event =>
                          setOtpById(current => ({
                            ...current,
                            [request.request_id]: event.target.value.replace(/\D/g, "").slice(0, 6),
                          }))
                        }
                      />
                      <div className="mt-2 flex justify-end gap-2">
                        <button
                          type="button"
                          className="rounded-md px-2 py-1 text-sm text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
                          onClick={() => void reject(request)}
                        >
                          Reject
                        </button>
                        <button
                          type="button"
                          className="rounded-md bg-blue-700 px-2 py-1 text-sm font-medium text-white hover:bg-blue-800"
                          onClick={() => void approve(request)}
                        >
                          Pair
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </Section>

            <Section title="Pair companion">
              <input
                className="w-full rounded-md border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-900 outline-none focus:border-blue-600 dark:border-slate-600 dark:bg-slate-900 dark:text-white"
                placeholder="https://<phone-ip>:8787"
                value={companionUrl}
                onChange={event => setCompanionUrl(event.target.value)}
              />
              {initiatedOtp && (
                <div className="mt-2 text-sm text-slate-600 dark:text-slate-300">
                  Code shown to phone: <span className="font-semibold">{initiatedOtp}</span>
                </div>
              )}
              <div className="mt-2 flex justify-end gap-2">
                <button
                  type="button"
                  className="rounded-md px-2 py-1 text-sm text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
                  onClick={() => void unpair()}
                >
                  Unpair all
                </button>
                <button
                  type="button"
                  className="rounded-md bg-blue-700 px-2 py-1 text-sm font-medium text-white hover:bg-blue-800"
                  onClick={() => void initiate()}
                >
                  Send
                </button>
              </div>
            </Section>
          </Card>
        </div>
      )}
    </div>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="mt-3 border-t border-slate-800/10 pt-3 first:mt-0 first:border-t-0 first:pt-0 dark:border-slate-300/20">
      <div className="mb-2 text-sm font-semibold text-slate-900 dark:text-white">{title}</div>
      {children}
    </div>
  );
}

function Muted({ children }: { children: ReactNode }) {
  return <div className="text-sm text-slate-500 dark:text-slate-400">{children}</div>;
}

function StatusRow({ label, ok }: { label: string; ok: boolean }) {
  return (
    <div className="flex items-center justify-between gap-2">
      <span>{label}</span>
      <span className={ok ? "font-medium text-green-600" : "font-medium text-red-600"}>
        {ok ? "Granted" : "Missing"}
      </span>
    </div>
  );
}

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex gap-2">
      <span className="shrink-0 text-slate-500 dark:text-slate-400">{label}</span>
      <span className="min-w-0 break-all text-slate-700 dark:text-slate-200">{value}</span>
    </div>
  );
}

function candidateIPSortKey(entry: VisibleIP) {
  return `${entry.hostname || ""} ${entry.ip || ""} ${entry.source || ""} ${entry.interface || ""}`.toLowerCase();
}
