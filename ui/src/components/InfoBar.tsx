import { useEffect } from "react";

import { cx } from "@/cva.config";
import {
  useHidStore,
  useMouseStore,
  useRTCStore,
  useSettingsStore,
  useVideoStore,
} from "@/hooks/stores";
import { keys, modifiers } from "@/keyboardMappings";

export default function InfoBar() {
  const activeKeys = useHidStore(state => state.activeKeys);
  const activeModifiers = useHidStore(state => state.activeModifiers);
  const mouseX = useMouseStore(state => state.mouseX);
  const mouseY = useMouseStore(state => state.mouseY);
  const mouseMove = useMouseStore(state => state.mouseMove);

  const videoClientSize = useVideoStore(
    state => `${Math.round(state.clientWidth)}x${Math.round(state.clientHeight)}`,
  );

  const videoSize = useVideoStore(
    state => `${Math.round(state.width)}x${Math.round(state.height)}`,
  );

  const rpcDataChannel = useRTCStore(state => state.rpcDataChannel);

  const settings = useSettingsStore();

  useEffect(() => {
    if (!rpcDataChannel) return;
    rpcDataChannel.onclose = () => console.log("rpcDataChannel has closed");
    rpcDataChannel.onerror = e =>
      console.log(`Error on DataChannel '${rpcDataChannel.label}': ${e}`);
  }, [rpcDataChannel]);

  const keyboardLedState = useHidStore(state => state.keyboardLedState);
  const keyboardLedStateSyncAvailable = useHidStore(state => state.keyboardLedStateSyncAvailable);
  const keyboardLedSync = useSettingsStore(state => state.keyboardLedSync);

  const isTurnServerInUse = useRTCStore(state => state.isTurnServerInUse);

  const usbState = useHidStore(state => state.usbState);
  const hdmiState = useVideoStore(state => state.hdmiState);

  return (
    <div className="bg-white border-t border-t-slate-800/30 text-slate-800 dark:border-t-slate-300/20 dark:bg-slate-900 dark:text-slate-300">
      <div className="flex flex-wrap items-stretch justify-between gap-1">
        <div className="flex items-center">
          <div className="flex flex-wrap items-center pl-2 gap-x-4">
            {settings.debugMode ? (
              <div className="flex">
                <span className="text-xs font-semibold">Resolution:</span>{" "}
                <span className="text-xs">{videoSize}</span>
              </div>
            ) : null}

            {settings.debugMode ? (
              <div className="flex">
                <span className="text-xs font-semibold">Video Size: </span>
                <span className="text-xs">{videoClientSize}</span>
              </div>
            ) : null}

            {(settings.debugMode && settings.mouseMode == "absolute") ? (
              <div className="flex w-[118px] items-center gap-x-1">
                <span className="text-xs font-semibold">Pointer:</span>
                <span className="text-xs">
                  {mouseX},{mouseY}
                </span>
              </div>
            ) : null}

            {(settings.debugMode && settings.mouseMode == "relative") ? (
              <div className="flex w-[118px] items-center gap-x-1">
                <span className="text-xs font-semibold">Last Move:</span>
                <span className="text-xs">
                  {mouseMove ?
                    `${mouseMove.x},${mouseMove.y} ${mouseMove.buttons ? `(${mouseMove.buttons})` : ""}` :
                    "N/A"}
                </span>
              </div>
            ) : null}

            {settings.debugMode && (
              <div className="flex w-[156px] items-center gap-x-1">
                <span className="text-xs font-semibold">USB State:</span>
                <span className="text-xs">{usbState}</span>
              </div>
            )}
            {settings.debugMode && (
              <div className="flex w-[156px] items-center gap-x-1">
                <span className="text-xs font-semibold">HDMI State:</span>
                <span className="text-xs">{hdmiState}</span>
              </div>
            )}

            <div className="flex items-center gap-x-1">
              <span className="text-xs font-semibold">Keys:</span>
              <h2 className="text-xs">
                {[
                  ...activeKeys.map(
                    x => Object.entries(keys).filter(y => y[1] === x)[0][0],
                  ),
                  activeModifiers.map(
                    x => Object.entries(modifiers).filter(y => y[1] === x)[0][0],
                  ),
                ].join(", ")}
              </h2>
            </div>
          </div>
        </div>
        <div className="flex items-center divide-x first:divide-l divide-slate-800/20 dark:divide-slate-300/20">
          {isTurnServerInUse && (
            <div className="shrink-0 p-1 px-1.5 text-xs text-black dark:text-white">
              Relayed by Cloudflare
            </div>
          )}

          {keyboardLedStateSyncAvailable ? (
            <div
              className={cx(
                "shrink-0 p-1 px-1.5 text-xs",
                keyboardLedSync !== "browser"
                  ? "text-black dark:text-white"
                  : "text-slate-800/20 dark:text-slate-300/20",
              )}
              title={"Your keyboard LED state is managed by" + (keyboardLedSync === "browser" ? " the browser" : " the host")}
            >
              {keyboardLedSync === "browser" ? "Browser" : "Host"}
            </div>
          ) : null}
          <div
            className={cx(
              "shrink-0 p-1 px-1.5 text-xs",
              keyboardLedState?.caps_lock
                ? "text-black dark:text-white"
                : "text-slate-800/20 dark:text-slate-300/20",
            )}
          >
            Caps Lock
          </div>
          <div
            className={cx(
              "shrink-0 p-1 px-1.5 text-xs",
              keyboardLedState?.num_lock
                ? "text-black dark:text-white"
                : "text-slate-800/20 dark:text-slate-300/20",
            )}
          >
            Num Lock
          </div>
          <div
            className={cx(
              "shrink-0 p-1 px-1.5 text-xs",
              keyboardLedState?.scroll_lock
                ? "text-black dark:text-white"
                : "text-slate-800/20 dark:text-slate-300/20",
            )}
          >
            Scroll Lock
          </div>
          {keyboardLedState?.compose ? (
            <div className="shrink-0 p-1 px-1.5 text-xs">
              Compose
            </div>
          ) : null}
          {keyboardLedState?.kana ? (
            <div className="shrink-0 p-1 px-1.5 text-xs">
              Kana
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
