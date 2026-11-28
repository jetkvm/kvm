import { useCallback, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { LuCheck, LuCircleArrowUp, LuCircleX } from "react-icons/lu";

import { formatters } from "@/utils";
import { m } from "@localizations/messages.js";
import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import LoadingSpinner from "@components/LoadingSpinner";
import OsServicesInstallerButton from "@components/OsServicesInstallerButton";
import { GuestUpdateState, useVideoStore } from "@hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { useOsServicesInstaller } from "@hooks/useOsServicesInstaller";
import notifications from "@/notifications";

// A dismissed offer stays dismissed for its version in this browser.
const dismissedStorageKey = "guestUpdateDismissed";

function readDismissed(): string | null {
  try {
    return window.localStorage.getItem(dismissedStorageKey);
  } catch {
    return null;
  }
}

function writeDismissed(key: string) {
  try {
    window.localStorage.setItem(dismissedStorageKey, key);
  } catch {
    // Without storage the offer returns on the next page load.
  }
}

// What the card shows and the key that dismisses it.
function cardKey(update: GuestUpdateState | null, connected: boolean): string | null {
  if (!update) return null;
  switch (update.phase) {
    case "downloading":
    case "sending":
    case "installing":
      return update.phase;
    case "done":
    case "failed":
      return `${update.phase}:${update.version}:${update.error ?? ""}`;
  }
  return connected && update.available && !update.automatic ? `offer:${update.version}` : null;
}

// Starts the update of the connected OS Services sender.
export function useGuestUpdateStart() {
  const { send } = useJsonRpc();
  return useCallback(() => {
    void send("guestUpdateStart", {}, (response: JsonRpcResponse) => {
      if ("error" in response) {
        notifications.error(
          m.guest_update_start_failed({
            error: response.error.data || m.unknown_error(),
          }),
        );
      }
    });
  }, [send]);
}

// The update of the OS Services sender to the newest release the KVM
// firmware installs: the offer when the sender is older and auto-update is
// off, then the progress and the result, in the same status card the
// firmware update uses.
export default function GuestUpdateStatusCard() {
  const { guestUpdate: update, osServices } = useVideoStore();
  const start = useGuestUpdateStart();
  const installer = useOsServicesInstaller();
  const [dismissed, setDismissed] = useState(readDismissed);
  const key = cardKey(update, osServices.connected);
  const visible = key !== null && key !== dismissed;
  const busy =
    update?.phase === "downloading" ||
    update?.phase === "sending" ||
    update?.phase === "installing";
  const dismiss = () => {
    if (!key) return;
    setDismissed(key);
    writeDismissed(key);
  };

  return (
    <AnimatePresence>
      {visible && update && (
        <motion.div
          className="pointer-events-none fixed inset-0 top-16 z-10 mx-auto flex h-full w-full max-w-xl translate-y-8 items-start justify-center"
          initial={{ opacity: 0, y: -20 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: -20 }}
          transition={{ duration: 0.3, ease: "easeInOut" }}
        >
          <div className="w-full opacity-100 transition-all duration-300 ease-in-out select-none">
            <GridCard cardClassName="shadow-xl!">
              <div className="flex items-center justify-between gap-x-3 px-2.5 py-2.5 text-black dark:text-white">
                <div className="flex min-w-0 grow items-center gap-x-3">
                  <GuestUpdateIcon update={update} />
                  <div className="min-w-0 grow space-y-1.5">
                    <div className="truncate text-sm leading-none font-semibold">
                      {guestUpdateTitle(update)}
                    </div>
                    <div className="text-sm leading-snug">{guestUpdateDescription(update)}</div>
                    {busy && <GuestUpdateProgress update={update} />}
                  </div>
                </div>
                {!busy && (
                  <div className="pointer-events-auto flex shrink-0 gap-x-2">
                    {!update.phase && update.installable && (
                      <Button
                        size="SM"
                        theme="primary"
                        text={m.guest_update_install()}
                        onClick={start}
                        data-testid="guest-update-install"
                      />
                    )}
                    {!update.phase && !update.installable && (
                      <OsServicesInstallerButton
                        installer={installer}
                        version={update.version}
                        theme="primary"
                        testId="guest-update-mount-installer"
                        menuTestId="guest-update-installer-menu"
                      />
                    )}
                    {update.phase === "failed" && update.installable && update.available && (
                      <Button
                        size="SM"
                        theme="primary"
                        text={m.retry()}
                        onClick={start}
                        data-testid="guest-update-retry"
                      />
                    )}
                    <Button
                      size="SM"
                      theme="light"
                      text={update.phase ? m.close() : m.guest_update_later()}
                      onClick={dismiss}
                      data-testid="guest-update-dismiss"
                    />
                  </div>
                )}
              </div>
            </GridCard>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

function GuestUpdateIcon({ update }: { update: GuestUpdateState }) {
  switch (update.phase) {
    case "downloading":
    case "sending":
    case "installing":
      return <LoadingSpinner className="h-5 w-5 shrink-0 text-blue-700" />;
    case "done":
      return <LuCheck className="h-5 w-5 shrink-0 text-emerald-600 dark:text-emerald-400" />;
    case "failed":
      return <LuCircleX className="h-5 w-5 shrink-0 text-red-600 dark:text-red-400" />;
  }
  return <LuCircleArrowUp className="h-5 w-5 shrink-0 text-blue-700 dark:text-blue-500" />;
}

function guestUpdateTitle(update: GuestUpdateState) {
  const version = update.version;
  switch (update.phase) {
    case "downloading":
    case "sending":
      return m.guest_update_updating_title({ version });
    case "installing":
      return m.guest_update_installing_title({ version });
    case "done":
      return m.guest_update_done_title({ version });
    case "failed":
      return m.guest_update_failed_title();
  }
  return m.guest_update_available_title({ version });
}

export function guestUpdateDescription(update: GuestUpdateState) {
  switch (update.phase) {
    case "downloading":
      return m.guest_update_downloading_progress({
        sent: formatters.bytes(update.sent ?? 0),
        size: formatters.bytes(update.size ?? 0),
      });
    case "sending":
      return m.guest_update_sending_progress({
        sent: formatters.bytes(update.sent ?? 0),
        size: formatters.bytes(update.size ?? 0),
      });
    case "installing":
      return m.guest_update_installing_description();
    case "done":
      return m.guest_update_done_description();
    case "failed":
      // The sender and the KVM report errors in lower case.
      return update.error
        ? update.error[0].toUpperCase() + update.error.slice(1)
        : m.unknown_error();
  }
  return update.installable
    ? m.guest_update_available_description()
    : m.guest_update_available_manual();
}

function GuestUpdateProgress({ update }: { update: GuestUpdateState }) {
  const size = update.size ?? 0;
  const percent =
    update.phase === "installing" || size === 0
      ? 100
      : Math.min(100, Math.round(((update.sent ?? 0) / size) * 100));
  return (
    <div className="h-1 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-slate-700">
      <div
        className="h-full rounded-full bg-blue-600 transition-[width] duration-200 dark:bg-blue-500"
        style={{ width: `${percent}%` }}
      />
    </div>
  );
}
