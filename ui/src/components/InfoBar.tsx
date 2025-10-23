import { useMemo } from "react";

import {
  useHidStore,
  useMouseStore,
  useRTCStore,
  useSettingsStore,
  useVideoStore,
  VideoState
} from "@hooks/stores";
import { useHidRpc } from "@hooks/useHidRpc";
import { keys, modifiers } from "@/keyboardMappings";
import { cx } from "@/cva.config";
import { m } from "@localizations/messages.js";

export default function InfoBar() {
  const { keysDownState } = useHidStore();
  const { mouseX, mouseY, mouseMove } = useMouseStore();
  const { rpcHidStatus } = useHidRpc();

  const videoClientSize = useVideoStore(
    (state: VideoState) => `${Math.round(state.clientWidth)}x${Math.round(state.clientHeight)}`,
  );

  const videoSize = useVideoStore(
    (state: VideoState) => `${Math.round(state.width)}x${Math.round(state.height)}`,
  );

  const { debugMode, mouseMode, showPressedKeys } = useSettingsStore();
  const { isPasteInProgress } = useHidStore();
  const { keyboardLedState, usbState } = useHidStore();
  const { isTurnServerInUse } = useRTCStore();
  const { hdmiState } = useVideoStore();

  const displayKeys = useMemo(() => {
    if (!showPressedKeys) return "";

    const activeModifierMask = keysDownState.modifier || 0;
    const keysDown = keysDownState.keys || [];
    const modifierNames = Object.entries(modifiers)
      .filter(([_, mask]) => (activeModifierMask & mask) !== 0)
      .map(([name]) => name);
    const keyNames = Object.entries(keys)
      .filter(([_, value]) => keysDown.includes(value))
      .map(([name]) => name);

    return [...modifierNames, ...keyNames].join(", ");
  }, [keysDownState, showPressedKeys]);

  return (
    <div className="bg-white border-t border-t-slate-800/30 text-slate-800 dark:border-t-slate-300/20 dark:bg-slate-900 dark:text-slate-300">
      <div className="flex flex-wrap items-stretch justify-between gap-1">
        <div className="flex items-center">
          <div className="flex flex-wrap items-center pl-2 gap-x-4">
            {debugMode ? (
              <div className="flex">
                <span className="text-xs font-semibold">{m.info_resolution()}</span>{" "}
                <span className="text-xs">{videoSize}</span>
              </div>
            ) : null}

            {debugMode ? (
              <div className="flex">
                <span className="text-xs font-semibold">{m.info_video_size()}</span>
                <span className="text-xs">{videoClientSize}</span>
              </div>
            ) : null}

            {(debugMode && mouseMode == "absolute") ? (
              <div className="flex w-[118px] items-center gap-x-1">
                <span className="text-xs font-semibold">{m.info_pointer()}</span>
                <span className="text-xs">{mouseX},{mouseY}</span>
              </div>
            ) : null}

            {(debugMode && mouseMode == "relative") ? (
              <div className="flex w-[118px] items-center gap-x-1">
                <span className="text-xs font-semibold">{m.info_last_move()}</span>
                <span className="text-xs">
                  {mouseMove ? `${mouseMove.x},${mouseMove.y} ${mouseMove.buttons ? `(${mouseMove.buttons})` : ""}` : "N/A"}
                </span>
              </div>
            ) : null}

            {debugMode && (
              <div className="flex w-[156px] items-center gap-x-1">
                <span className="text-xs font-semibold">{m.info_usb_state()}</span>
                <span className="text-xs">{usbState}</span>
              </div>
            )}

            {debugMode && (
              <div className="flex w-[156px] items-center gap-x-1">
                <span className="text-xs font-semibold">{m.info_hdmi_state()}</span>
                <span className="text-xs">{hdmiState}</span>
              </div>
            )}

            {debugMode && (
              <div className="flex w-[156px] items-center gap-x-1">
                <span className="text-xs font-semibold">{m.info_hidrpc_state()}</span>
                <span className="text-xs">{rpcHidStatus}</span>
              </div>
            )}

            {isPasteInProgress && (
              <div className="flex w-[156px] items-center gap-x-1">
                <span className="text-xs font-semibold">{m.info_paste_mode()}</span>
                <span className="text-xs">{m.info_paste_enabled()}</span>
              </div>
            )}

            {showPressedKeys && (
              <div className="flex items-center gap-x-1">
                <span className="text-xs font-semibold">{m.info_keys()}</span>
                <h2 className="text-xs">{displayKeys}</h2>
              </div>
            )}
          </div>
        </div>

        <div className="flex items-center divide-x first:divide-l divide-slate-800/20 dark:divide-slate-300/20">
          {isTurnServerInUse && (
            <div className="shrink-0 p-1 px-1.5 text-xs text-black dark:text-white">
              {m.info_relayed_by_cloudflare()}
            </div>
          )}

          <div
            className={cx(
              "shrink-0 p-1 px-1.5 text-xs",
              keyboardLedState.caps_lock
                ? "text-black dark:text-white"
                : "text-slate-800/20 dark:text-slate-300/20",
            )}
          >
            {m.info_caps_lock()}
          </div>

          <div
            className={cx(
              "shrink-0 p-1 px-1.5 text-xs",
              keyboardLedState.num_lock
                ? "text-black dark:text-white"
                : "text-slate-800/20 dark:text-slate-300/20",
            )}
          >
            {m.info_num_lock()}
          </div>

          <div
            className={cx(
              "shrink-0 p-1 px-1.5 text-xs",
              keyboardLedState.scroll_lock
                ? "text-black dark:text-white"
                : "text-slate-800/20 dark:text-slate-300/20",
            )}
          >
            {m.info_scroll_lock()}
          </div>

          {keyboardLedState.compose ? (
            <div className="shrink-0 p-1 px-1.5 text-xs">{m.info_compose()}</div>
          ) : null}

          {keyboardLedState.kana ? (
            <div className="shrink-0 p-1 px-1.5 text-xs">{m.info_kana()}</div>
          ) : null}

          {keyboardLedState.shift ? (
            <div className="shrink-0 p-1 px-1.5 text-xs">{m.info_shift()}</div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
