import { Fragment, useCallback, useEffect, useState } from "react";
import { CloseButton, Popover, PopoverButton, PopoverPanel } from "@headlessui/react";
import { LuHardDrive, LuPower, LuRotateCcw, LuTriangleAlert } from "react-icons/lu";

import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";

import { Button } from "@components/Button";
import Modal from "@components/Modal";
import { ConfirmDialog } from "@components/ConfirmDialog";
import { cx } from "@/cva.config";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

interface ATXState {
  power: boolean;
  hdd: boolean;
}

export default function ATXStateIndicator({
  setDisableVideoFocusTrap,
}: {
  setDisableVideoFocusTrap: (disable: boolean) => void;
}) {
  const [isATXExtensionActive, setIsATXExtensionActive] = useState(false);
  const [atxState, setAtxState] = useState<ATXState | null>(null);

  // Confirmation dialog state
  const [showPowerConfirm, setShowPowerConfirm] = useState(false);
  const [showResetConfirm, setShowResetConfirm] = useState(false);
  const [isActionInProgress, setIsActionInProgress] = useState(false);

  const { send } = useJsonRpc(function onRequest(req) {
    if (req.method === "atxState") {
      setAtxState(req.params as ATXState);
    }
  });

  // Check if ATX extension is active
  useEffect(() => {
    send("getActiveExtension", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const extensionId = resp.result as string;
      setIsATXExtensionActive(extensionId === "atx-power");
    });
  }, [send]);

  // Fetch ATX state when extension is active
  useEffect(() => {
    if (!isATXExtensionActive) {
      return;
    }

    send("getATXState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.atx_power_control_get_state_error({
            error: resp.error.data || m.unknown_error(),
          }),
        );
        return;
      }
      setAtxState(resp.result as ATXState);
    });

    // Reset state when extension becomes inactive
    return () => {
      setAtxState(null);
    };
  }, [isATXExtensionActive, send]);

  const handlePowerAction = useCallback(
    (action: "power-short" | "power-long" | "reset") => {
      setIsActionInProgress(true);
      send("setATXPowerAction", { action }, (resp: JsonRpcResponse) => {
        setIsActionInProgress(false);
        if ("error" in resp) {
          const actionName =
            action === "reset"
              ? m.atx_power_control_reset_button()
              : action === "power-long"
                ? m.atx_power_control_long_power_button()
                : m.atx_power_control_short_power_button();
          notifications.error(
            m.atx_power_control_send_action_error({
              action: actionName,
              error: resp.error.data || m.unknown_error(),
            }),
          );
          return;
        }
        // Close confirmation dialogs
        setShowPowerConfirm(false);
        setShowResetConfirm(false);
      });
    },
    [send],
  );

  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.stopPropagation();
      setShowPowerConfirm(false);
    }
  };

  // Don't render if ATX extension is not active
  if (!isATXExtensionActive) {
    return null;
  }

  return (
    <>
      <Popover>
        <PopoverButton as={Fragment}>
          <button
            className={cx(
              "flex items-center gap-x-1.5 rounded-md px-2 py-1.5",
              "border border-slate-200 dark:border-slate-700",
              "bg-white dark:bg-slate-800",
              "hover:bg-slate-50 dark:hover:bg-slate-700/50",
              "transition-colors duration-150",
            )}
            onClick={() => setDisableVideoFocusTrap(true)}
          >
            {/* Power LED */}
            <LuPower
              strokeWidth={3}
              className={cx(
                "h-4 w-4 transition-colors",
                atxState?.power ? "text-green-500" : "text-slate-300 dark:text-slate-600",
              )}
            />
            {/* HDD LED */}
            <LuHardDrive
              strokeWidth={3}
              className={cx(
                "h-4 w-4 transition-colors",
                atxState?.hdd ? "text-blue-400" : "text-slate-300 dark:text-slate-600",
              )}
            />
          </button>
        </PopoverButton>
        <PopoverPanel
          anchor="bottom end"
          transition
          className={cx(
            "z-10 w-[200px]",
            "flex origin-top flex-col transition duration-200 ease-out",
            "data-closed:translate-y-2 data-closed:opacity-0",
          )}
        >
          <div className="mt-2 rounded-lg border border-slate-200 bg-white p-3 shadow-lg dark:border-slate-700 dark:bg-slate-800">
            <div className="space-y-3">
              <div className="text-xs font-medium text-slate-500 dark:text-slate-400">
                {m.atx_indicator_title()}
              </div>
              <div className="flex gap-x-2">
                <Button
                  size="SM"
                  theme="light"
                  LeadingIcon={LuPower}
                  text={m.atx_power_control_power_button()}
                  onClick={() => setShowPowerConfirm(true)}
                />
                <Button
                  size="SM"
                  theme="light"
                  LeadingIcon={LuRotateCcw}
                  text={m.atx_power_control_reset_button()}
                  onClick={() => setShowResetConfirm(true)}
                />
              </div>
              <div className="flex items-center gap-x-3 text-xs text-slate-500 dark:text-slate-400">
                <span className="flex items-center gap-x-1">
                  <LuPower
                    strokeWidth={3}
                    className={cx("h-3 w-3", atxState?.power ? "text-green-500" : "text-slate-300")}
                  />
                  {m.atx_power_control_power_led()}
                </span>
                <span className="flex items-center gap-x-1">
                  <LuHardDrive
                    strokeWidth={3}
                    className={cx("h-3 w-3", atxState?.hdd ? "text-blue-400" : "text-slate-300")}
                  />
                  {m.atx_power_control_hdd_led()}
                </span>
              </div>
            </div>
          </div>
        </PopoverPanel>
      </Popover>

      {/* Power Confirmation Dialog - Custom with two action buttons */}
      <div onKeyDown={handleKeyDown}>
        <Modal open={showPowerConfirm} onClose={() => setShowPowerConfirm(false)}>
          <div className="mx-auto max-w-md px-4 transition-all duration-300 ease-in-out">
            <div className="pointer-events-auto relative w-full overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm transition-all dark:border-slate-800 dark:bg-slate-900">
              <div className="p-6">
                <div className="flex items-start gap-3.5">
                  <LuTriangleAlert
                    aria-hidden="true"
                    className="mt-[2px] size-[18px] shrink-0 text-amber-600 dark:text-amber-400"
                  />
                  <div className="min-w-0 flex-1 space-y-2">
                    <h2 className="font-semibold text-slate-950 dark:text-white">
                      {m.atx_indicator_power_confirm_title()}
                    </h2>
                    <div className="text-sm text-slate-700 dark:text-slate-300">
                      {m.atx_indicator_power_confirm_description()}
                    </div>
                  </div>
                </div>

                <div className="mt-6 flex justify-end gap-2">
                  <CloseButton as={Button} size="SM" theme="blank" text={m.cancel()} />
                  <Button
                    size="SM"
                    type="button"
                    theme="primary"
                    text={
                      isActionInProgress
                        ? `${m.atx_power_control_short_power_button()}...`
                        : m.atx_power_control_short_power_button()
                    }
                    onClick={() => handlePowerAction("power-short")}
                    disabled={isActionInProgress}
                  />
                  <Button
                    size="SM"
                    type="button"
                    theme="danger"
                    text={
                      isActionInProgress
                        ? `${m.atx_indicator_force_power_off()}...`
                        : m.atx_indicator_force_power_off()
                    }
                    onClick={() => handlePowerAction("power-long")}
                    disabled={isActionInProgress}
                  />
                </div>
              </div>
            </div>
          </div>
        </Modal>
      </div>

      {/* Reset Confirmation Dialog */}
      <ConfirmDialog
        open={showResetConfirm}
        onClose={() => setShowResetConfirm(false)}
        title={m.atx_indicator_reset_confirm_title()}
        description={m.atx_indicator_reset_confirm_description()}
        variant="danger"
        confirmText={m.atx_power_control_reset_button()}
        cancelText={m.cancel()}
        onConfirm={() => handlePowerAction("reset")}
        isConfirming={isActionInProgress}
      />
    </>
  );
}
