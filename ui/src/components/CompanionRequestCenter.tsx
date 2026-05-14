import { useCallback, useEffect, useRef, useState } from "react";
import { LuBell, LuX } from "react-icons/lu";

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

type PairRequestsResponse = {
  requests?: CompanionPairRequest[];
};

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
  const [otpById, setOtpById] = useState<Record<string, string>>({});
  const [companionUrl, setCompanionUrl] = useState("");
  const [initiatedOtp, setInitiatedOtp] = useState("");
  const setDisableVideoFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);

  const refresh = useCallback(async () => {
    try {
      const resp = await api.GET(`${DEVICE_API}/companion/pair/requests`);
      if (!resp.ok) return;
      const body = (await resp.json()) as PairRequestsResponse;
      setRequests(body.requests || []);
    } catch {
      // Ignore transient polling failures; the connection state UI covers outages.
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

  const initiate = useCallback(async () => {
    const url = companionUrl.trim();
    if (!url) {
      notifications.error("Enter the companion address.");
      return;
    }
    const resp = await api.POST(`${DEVICE_API}/companion/pair/initiate`, {
      companion_url: url,
    });
    if (!resp.ok) {
      notifications.error("Failed to notify companion.");
      return;
    }
    const body = (await resp.json()) as { otp?: string };
    setInitiatedOtp(body.otp || "");
    notifications.success("Pairing request sent.");
    void refresh();
  }, [companionUrl, refresh]);

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
          {compact && <span className="min-w-0 truncate">Requests</span>}
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
            "z-[80] w-80 p-px",
            compact ? "fixed top-3 right-3" : "absolute top-full right-0 mt-1",
          )}
        >
          <Card className="max-h-[calc(100dvh-24px)] overflow-auto p-3 shadow-2xl">
            <div className="mb-2 flex items-center justify-between gap-2">
              <div className="text-sm font-semibold text-slate-900 dark:text-white">
                Pairing requests
              </div>
              <button
                type="button"
                aria-label="Close"
                className="rounded-md p-1 text-slate-500 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
                onClick={closePanel}
              >
                <LuX className="h-4 w-4" />
              </button>
            </div>
            {requests.length === 0 ? (
              <div className="text-sm text-slate-500 dark:text-slate-400">No pending requests.</div>
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
            <div className="mt-3 border-t border-slate-800/10 pt-3 dark:border-slate-300/20">
              <div className="text-sm font-semibold text-slate-900 dark:text-white">Pair phone</div>
              <input
                className="mt-2 w-full rounded-md border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-900 outline-none focus:border-blue-600 dark:border-slate-600 dark:bg-slate-900 dark:text-white"
                placeholder="Phone IP, e.g. 192.168.8.120:8787"
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
                  Unpair
                </button>
                <button
                  type="button"
                  className="rounded-md bg-blue-700 px-2 py-1 text-sm font-medium text-white hover:bg-blue-800"
                  onClick={() => void initiate()}
                >
                  Send
                </button>
              </div>
            </div>
          </Card>
        </div>
      )}
    </div>
  );
}
