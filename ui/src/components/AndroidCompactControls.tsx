import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ComponentType } from "react";
import {
  LuCable,
  LuCommand,
  LuHardDrive,
  LuKeyboard,
  LuLogOut,
  LuMaximize,
  LuMenu,
  LuMessageSquare,
  LuPower,
  LuSettings,
  LuTerminal,
  LuX,
} from "react-icons/lu";

import { cx } from "@/cva.config";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import {
  useHidStore,
  useSettingsStore,
  useUiStore,
} from "@hooks/stores";
import PasteModal from "@components/popovers/PasteModal";
import MountPopopover from "@components/popovers/MountPopover";
import WakeOnLanModal from "@components/popovers/WakeOnLan/Index";
import ExtensionPopover from "@components/popovers/ExtensionPopover";
import { DEVICE_API } from "@/ui.config";
import api from "@/api";
import { m } from "@localizations/messages.js";

type Panel = "actions" | "paste" | "media" | "wol" | "extension";

type Position = {
  x: number;
  y: number;
};

const STORAGE_KEY = "androidCompactControlPosition";
const BUTTON_SIZE = 44;
const EDGE_PADDING = 8;
const PANEL_WIDTH = 280;

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max);

const getDefaultPosition = (): Position => ({
  x: Math.max(EDGE_PADDING, window.innerWidth - BUTTON_SIZE - EDGE_PADDING),
  y: Math.max(EDGE_PADDING, Math.round(window.innerHeight * 0.18)),
});

const getStoredPosition = (): Position => {
  if (typeof window === "undefined") return { x: EDGE_PADDING, y: EDGE_PADDING };

  try {
    const parsed = JSON.parse(window.localStorage.getItem(STORAGE_KEY) || "null") as Position | null;
    if (parsed && Number.isFinite(parsed.x) && Number.isFinite(parsed.y)) return parsed;
  } catch {
    window.localStorage.removeItem(STORAGE_KEY);
  }

  return getDefaultPosition();
};

const clampPosition = (position: Position): Position => {
  if (typeof window === "undefined") return position;

  return {
    x: clamp(position.x, EDGE_PADDING, window.innerWidth - BUTTON_SIZE - EDGE_PADDING),
    y: clamp(position.y, EDGE_PADDING, window.innerHeight - BUTTON_SIZE - EDGE_PADDING),
  };
};

function ActionButton({
  icon: Icon,
  label,
  onClick,
  active,
}: {
  icon: ComponentType<{ className?: string }>;
  label: string;
  onClick: () => void;
  active?: boolean;
}) {
  return (
    <button
      type="button"
      className={cx(
        "flex h-10 w-full items-center gap-3 rounded-md px-3 text-left text-sm text-white",
        "hover:bg-white/15 active:bg-white/25",
        active && "bg-white/15",
      )}
      onClick={onClick}
    >
      <Icon className="h-4 w-4 shrink-0" />
      <span className="min-w-0 truncate">{label}</span>
    </button>
  );
}

export default function AndroidCompactControls({
  requestFullscreen,
}: {
  requestFullscreen: () => Promise<void>;
}) {
  const { navigateTo } = useDeviceUiNavigation();
  const { isVirtualKeyboardEnabled, setVirtualKeyboardEnabled } = useHidStore();
  const {
    setDisableVideoFocusTrap,
    terminalType,
    setTerminalType,
    isOcrMode,
    setOcrMode,
  } = useUiStore();
  const { developerMode } = useSettingsStore();

  const [position, setPosition] = useState<Position>(() => clampPosition(getStoredPosition()));
  const [open, setOpen] = useState(false);
  const [panel, setPanel] = useState<Panel>("actions");
  const dragRef = useRef<{
    pointerId: number;
    offsetX: number;
    offsetY: number;
    startX: number;
    startY: number;
    moved: boolean;
  } | null>(null);

  const persistPosition = useCallback((next: Position) => {
    const clamped = clampPosition(next);
    setPosition(clamped);
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(clamped));
  }, []);

  useEffect(() => {
    const onResize = () => persistPosition(position);
    window.addEventListener("resize", onResize);
    window.addEventListener("orientationchange", onResize);
    return () => {
      window.removeEventListener("resize", onResize);
      window.removeEventListener("orientationchange", onResize);
    };
  }, [persistPosition, position]);

  const panelStyle = useMemo(() => {
    const left = clamp(
      position.x + BUTTON_SIZE - PANEL_WIDTH,
      EDGE_PADDING,
      window.innerWidth - PANEL_WIDTH - EDGE_PADDING,
    );
    const top = clamp(
      position.y + BUTTON_SIZE + 8,
      EDGE_PADDING,
      window.innerHeight - EDGE_PADDING - 360,
    );

    return { left, top, width: PANEL_WIDTH };
  }, [position]);

  const openPanel = useCallback(
    (nextPanel: Panel) => {
      setPanel(nextPanel);
      setOpen(true);
      setDisableVideoFocusTrap(true);
    },
    [setDisableVideoFocusTrap],
  );

  const closePanel = useCallback(() => {
    setOpen(false);
    setPanel("actions");
    window.setTimeout(() => setDisableVideoFocusTrap(false), 0);
  }, [setDisableVideoFocusTrap]);

  const logout = useCallback(async () => {
    const res = await api.POST(`${DEVICE_API}/auth/logout`);
    if (!res.ok) return;

    closePanel();
    window.location.href = "/login-local";
  }, [closePanel]);

  return (
    <>
      <button
        type="button"
        aria-label="JetKVM controls"
        className={cx(
          "fixed z-50 flex h-11 w-11 touch-none items-center justify-center rounded-full",
          "border border-white/20 bg-slate-950/55 text-white shadow-lg backdrop-blur",
          "active:bg-slate-950/75",
        )}
        style={{ left: position.x, top: position.y }}
        onPointerDown={e => {
          e.preventDefault();
          e.currentTarget.setPointerCapture(e.pointerId);
          dragRef.current = {
            pointerId: e.pointerId,
            offsetX: e.clientX - position.x,
            offsetY: e.clientY - position.y,
            startX: e.clientX,
            startY: e.clientY,
            moved: false,
          };
        }}
        onPointerMove={e => {
          const drag = dragRef.current;
          if (!drag || drag.pointerId !== e.pointerId) return;

          const moved = Math.hypot(e.clientX - drag.startX, e.clientY - drag.startY) > 4;
          drag.moved = drag.moved || moved;
          persistPosition({ x: e.clientX - drag.offsetX, y: e.clientY - drag.offsetY });
        }}
        onPointerUp={e => {
          const drag = dragRef.current;
          if (!drag || drag.pointerId !== e.pointerId) return;

          dragRef.current = null;
          e.currentTarget.releasePointerCapture(e.pointerId);
          if (!drag.moved) {
            setOpen(current => {
              const next = !current;
              if (next) {
                setPanel("actions");
                setDisableVideoFocusTrap(true);
              } else {
                window.setTimeout(() => setDisableVideoFocusTrap(false), 0);
              }
              return next;
            });
          }
        }}
        onPointerCancel={() => {
          dragRef.current = null;
        }}
      >
        {open ? <LuX className="h-5 w-5" /> : <LuMenu className="h-5 w-5" />}
      </button>

      {open && (
        <div
          className={cx(
            "fixed z-50 max-h-[calc(100dvh-16px)] overflow-auto rounded-md",
            "border border-white/15 bg-slate-950/80 p-2 text-white shadow-xl backdrop-blur",
          )}
          style={panelStyle}
          onPointerDown={e => e.stopPropagation()}
        >
          {panel !== "actions" && (
            <div className="mb-2 flex items-center justify-between border-b border-white/10 pb-2">
              <button
                type="button"
                className="rounded-md px-2 py-1 text-sm hover:bg-white/15 active:bg-white/25"
                onClick={() => setPanel("actions")}
              >
                Back
              </button>
              <button
                type="button"
                className="rounded-md p-1.5 hover:bg-white/15 active:bg-white/25"
                onClick={closePanel}
              >
                <LuX className="h-4 w-4" />
              </button>
            </div>
          )}

          {panel === "actions" ? (
            <div className="grid gap-1">
              {developerMode && (
                <ActionButton
                  icon={LuTerminal}
                  label={m.kvm_terminal()}
                  active={terminalType === "kvm"}
                  onClick={() => setTerminalType(terminalType === "kvm" ? "none" : "kvm")}
                />
              )}
              <ActionButton
                icon={LuMessageSquare}
                label={m.paste_text()}
                active={isOcrMode}
                onClick={() => openPanel("paste")}
              />
              <ActionButton
                icon={LuCommand}
                label={m.action_bar_copy_text()}
                active={isOcrMode}
                onClick={() => {
                  setOcrMode(!isOcrMode);
                  setOpen(false);
                }}
              />
              <ActionButton
                icon={LuHardDrive}
                label={m.action_bar_virtual_media()}
                onClick={() => openPanel("media")}
              />
              <ActionButton
                icon={LuPower}
                label={m.action_bar_wake_on_lan()}
                onClick={() => openPanel("wol")}
              />
              <ActionButton
                icon={LuCable}
                label={m.action_bar_extension()}
                onClick={() => openPanel("extension")}
              />
              <ActionButton
                icon={LuKeyboard}
                label={m.action_bar_virtual_keyboard()}
                active={isVirtualKeyboardEnabled}
                onClick={() => setVirtualKeyboardEnabled(!isVirtualKeyboardEnabled)}
              />
              <ActionButton
                icon={LuMaximize}
                label={m.action_bar_fullscreen()}
                onClick={() => void requestFullscreen()}
              />
              <ActionButton
                icon={LuSettings}
                label={m.action_bar_settings()}
                onClick={() => {
                  closePanel();
                  navigateTo("/settings");
                }}
              />
              <ActionButton
                icon={LuLogOut}
                label={m.log_out()}
                onClick={() => void logout()}
              />
            </div>
          ) : panel === "paste" ? (
            <PasteModal />
          ) : panel === "media" ? (
            <MountPopopover />
          ) : panel === "wol" ? (
            <WakeOnLanModal />
          ) : (
            <ExtensionPopover />
          )}
        </div>
      )}
    </>
  );
}
