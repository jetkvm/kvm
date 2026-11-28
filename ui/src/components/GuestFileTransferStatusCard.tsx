import { useEffect } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { LuCheck, LuCircleX } from "react-icons/lu";

import { cx } from "@/cva.config";
import { formatters } from "@/utils";
import { m } from "@localizations/messages.js";
import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import LoadingSpinner from "@components/LoadingSpinner";
import { useGuestFileTransferStore } from "@hooks/useGuestFileDrop";

// How long a completed transfer stays on screen before it clears itself.
const doneVisibleMs = 2000;

// Progress of the files being sent to the remote host after a drop on the
// video, in the same status card the firmware update uses.
export default function GuestFileTransferStatusCard() {
  const { visible, transfers, cancel, hide } = useGuestFileTransferStore();
  const sending = cancel !== null;
  const failed = transfers.some(
    transfer => transfer.status === "failed" || transfer.status === "cancelled",
  );
  const sent = transfers.reduce((total, transfer) => total + transfer.sent, 0);
  const size = transfers.reduce((total, transfer) => total + transfer.size, 0);
  const percent = size === 0 ? 100 : Math.min(100, Math.round((sent / size) * 100));
  const subject =
    transfers.length === 1
      ? transfers[0].name
      : m.guest_file_transfer_count({ count: transfers.length });

  // A finished transfer clears itself; one that failed stays until it is
  // dismissed, so the reason does not disappear before it is read.
  useEffect(() => {
    if (!visible || sending || failed) return;
    const timer = window.setTimeout(hide, doneVisibleMs);
    return () => window.clearTimeout(timer);
  }, [visible, sending, failed, hide]);

  return (
    <AnimatePresence>
      {visible && (
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
                  {sending ? (
                    <LoadingSpinner className={cx("h-5 w-5", "shrink-0 text-blue-700")} />
                  ) : failed ? (
                    <LuCircleX className="h-5 w-5 shrink-0 text-red-600 dark:text-red-400" />
                  ) : (
                    <LuCheck className="h-5 w-5 shrink-0 text-emerald-600 dark:text-emerald-400" />
                  )}
                  <div className="min-w-0 grow space-y-1.5">
                    <div className="truncate text-sm leading-none font-semibold">
                      {sending ? m.guest_file_transfer_title() : m.guest_file_transfer_done()}
                    </div>
                    <div className="truncate text-sm leading-none">
                      {sending
                        ? `${subject} — ${formatters.bytes(sent)} / ${formatters.bytes(size)}`
                        : failed
                          ? m.guest_file_status_failed()
                          : m.guest_file_transfer_destination()}
                    </div>
                    <div className="h-1 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-slate-700">
                      <div
                        className={cx("h-full rounded-full transition-[width] duration-200", {
                          "bg-blue-600 dark:bg-blue-500": sending,
                          "bg-red-500": !sending && failed,
                          "bg-emerald-600 dark:bg-emerald-500": !sending && !failed,
                        })}
                        style={{ width: `${sending ? percent : 100}%` }}
                      />
                    </div>
                  </div>
                </div>
                <Button
                  size="SM"
                  className="pointer-events-auto"
                  theme="light"
                  text={sending ? m.cancel() : m.close()}
                  onClick={() => (sending ? cancel?.() : hide())}
                  data-testid="guest-file-transfer-dismiss"
                />
              </div>
            </GridCard>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
