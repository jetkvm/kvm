import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ComponentType, PointerEvent as ReactPointerEvent } from "react";
import { MdOutlineContentPasteGo } from "react-icons/md";
import {
  LuCable,
  LuBell,
  LuCommand,
  LuHardDrive,
  LuKeyboard,
  LuMenu,
  LuMonitorUp,
  LuLogOut,
  LuPower,
  LuSettings,
  LuSignal,
  LuTerminal,
  LuX,
} from "react-icons/lu";

import { cx } from "@/cva.config";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import {
  useHidStore,
  useMountMediaStore,
  useSettingsStore,
  useUiStore,
  useUserStore,
} from "@hooks/stores";
import useKeyboardLayout from "@hooks/useKeyboardLayout";
import ExtensionPopover from "@components/popovers/ExtensionPopover";
import MountPopopover from "@components/popovers/MountPopover";
import PasteModal from "@components/popovers/PasteModal";
import WakeOnLanModal from "@components/popovers/WakeOnLan/Index";
import CompanionRequestCenter from "@components/CompanionRequestCenter";
import { DEVICE_API } from "@/ui.config";
import api from "@/api";
import useKeyboard, { type MacroStep } from "@hooks/useKeyboard";
import { m } from "@localizations/messages.js";

type Panel = "root" | "paste" | "media" | "wol" | "extension";

type Position = {
  x: number;
  y: number;
};

declare global {
  interface Window {
    JetKVMAndroid?: {
      showInputMethod?: () => void;
    };
  }
}

const STORAGE_KEY = "androidCompactControlPosition";
const BUTTON_SIZE = 50;
const EDGE_PADDING = 10;
const PANEL_WIDTH = 320;
const PANEL_MAX_HEIGHT_MARGIN = 20;

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max);

const getDefaultPosition = (): Position => {
  if (typeof window === "undefined") return { x: EDGE_PADDING, y: EDGE_PADDING };

  return {
    x: Math.max(EDGE_PADDING, window.innerWidth - BUTTON_SIZE - EDGE_PADDING),
    y: Math.max(EDGE_PADDING, Math.round(window.innerHeight * 0.16)),
  };
};

const clampPosition = (position: Position): Position => {
  if (typeof window === "undefined") return position;

  return {
    x: clamp(position.x, EDGE_PADDING, window.innerWidth - BUTTON_SIZE - EDGE_PADDING),
    y: clamp(position.y, EDGE_PADDING, window.innerHeight - BUTTON_SIZE - EDGE_PADDING),
  };
};

const getStoredPosition = (): Position => {
  if (typeof window === "undefined") return { x: EDGE_PADDING, y: EDGE_PADDING };

  try {
    const parsed = JSON.parse(
      window.localStorage.getItem(STORAGE_KEY) || "null",
    ) as Position | null;
    if (parsed && Number.isFinite(parsed.x) && Number.isFinite(parsed.y)) {
      return clampPosition(parsed);
    }
  } catch {
    window.localStorage.removeItem(STORAGE_KEY);
  }

  return getDefaultPosition();
};

function ActionButton({
  active,
  icon: Icon,
  label,
  onClick,
}: {
  active?: boolean;
  icon: ComponentType<{ className?: string }>;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={cx(
        "flex min-h-11 w-full items-center gap-3 rounded-md px-3 text-left text-sm text-white",
        "hover:bg-white/15 active:bg-white/25",
        active && "bg-white/15",
      )}
      onClick={onClick}
    >
      <Icon className="h-5 w-5 shrink-0" />
      <span className="min-w-0 truncate">{label}</span>
    </button>
  );
}

export default function AndroidCompactControls() {
  const { navigateTo } = useDeviceUiNavigation();
  const { isVirtualKeyboardEnabled, setVirtualKeyboardEnabled } = useHidStore();
  const { remoteVirtualMediaState } = useMountMediaStore();
  const { executeMacro } = useKeyboard();
  const { selectedKeyboard } = useKeyboardLayout();
  const setUser = useUserStore(state => state.setUser);
  const {
    isOcrMode,
    setDisableVideoFocusTrap,
    setOcrMode,
    setTerminalType,
    terminalType,
    toggleSidebarView,
  } = useUiStore();
  const { developerMode } = useSettingsStore();

  const [position, setPosition] = useState<Position>(() => getStoredPosition());
  const [open, setOpen] = useState(false);
  const [requestCenterOpen, setRequestCenterOpen] = useState(false);
  const [panel, setPanel] = useState<Panel>("root");
  const buttonRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{
    moved: boolean;
    offsetX: number;
    offsetY: number;
    pointerId: number;
    startX: number;
    startY: number;
  } | null>(null);

  const persistPosition = useCallback((next: Position) => {
    const clamped = clampPosition(next);
    setPosition(clamped);
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(clamped));
  }, []);

  const closePanel = useCallback(() => {
    setOpen(false);
    setPanel("root");
    window.setTimeout(() => setDisableVideoFocusTrap(false), 0);
  }, [setDisableVideoFocusTrap]);

  const openRootPanel = useCallback(() => {
    setPanel("root");
    setOpen(true);
    setDisableVideoFocusTrap(true);
  }, [setDisableVideoFocusTrap]);

  const openChildPanel = useCallback(
    (nextPanel: Exclude<Panel, "root">) => {
      setPanel(nextPanel);
      setOpen(true);
      setDisableVideoFocusTrap(true);
    },
    [setDisableVideoFocusTrap],
  );

  const executeTextInput = useCallback(
    async (text: string) => {
      const macroSteps: MacroStep[] = [];

      for (const char of text) {
        const normalizedChar = char.normalize("NFC");
        const keyprops = selectedKeyboard.chars[normalizedChar];
        if (!keyprops?.key) continue;

        if (keyprops.accentKey) {
          const accentModifiers: string[] = [];
          if (keyprops.accentKey.shift) accentModifiers.push("ShiftLeft");
          if (keyprops.accentKey.altRight) accentModifiers.push("AltRight");

          macroSteps.push({
            keys: [String(keyprops.accentKey.key)],
            modifiers: accentModifiers.length > 0 ? accentModifiers : null,
            delay: 20,
          });
        }

        const modifiers: string[] = [];
        if (keyprops.shift) modifiers.push("ShiftLeft");
        if (keyprops.altRight) modifiers.push("AltRight");

        macroSteps.push({
          keys: [String(keyprops.key)],
          modifiers: modifiers.length > 0 ? modifiers : null,
          delay: 20,
        });

        if (keyprops.deadKey) {
          macroSteps.push({ keys: ["Space"], modifiers: null, delay: 20 });
        }
      }

      if (macroSteps.length > 0) await executeMacro(macroSteps);
    },
    [executeMacro, selectedKeyboard],
  );

  useEffect(() => {
    const onAndroidImeText = (event: Event) => {
      const customEvent = event as CustomEvent<{ text?: string }>;
      const text = customEvent.detail?.text;
      if (!text) return;

      void executeTextInput(text);
    };

    window.addEventListener("jetkvm-android-ime-text", onAndroidImeText);
    return () => window.removeEventListener("jetkvm-android-ime-text", onAndroidImeText);
  }, [executeTextInput]);

  useEffect(() => {
    const onResize = () => persistPosition(position);
    window.addEventListener("orientationchange", onResize);
    window.addEventListener("resize", onResize);
    return () => {
      window.removeEventListener("orientationchange", onResize);
      window.removeEventListener("resize", onResize);
    };
  }, [persistPosition, position]);

  useEffect(() => {
    if (!open) return;

    const onPointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (buttonRef.current?.contains(target)) return;
      if (panelRef.current?.contains(target)) return;

      event.preventDefault();
      event.stopPropagation();
      closePanel();
    };

    document.addEventListener("pointerdown", onPointerDown, true);
    return () => document.removeEventListener("pointerdown", onPointerDown, true);
  }, [closePanel, open]);

  const panelStyle = useMemo(() => {
    if (typeof window === "undefined")
      return { left: position.x, top: position.y, width: PANEL_WIDTH };

    const left = clamp(
      position.x + BUTTON_SIZE - PANEL_WIDTH,
      EDGE_PADDING,
      window.innerWidth - PANEL_WIDTH - EDGE_PADDING,
    );
    const top = clamp(
      position.y + BUTTON_SIZE + 8,
      EDGE_PADDING,
      window.innerHeight - PANEL_MAX_HEIGHT_MARGIN - EDGE_PADDING,
    );

    return { left, top, width: PANEL_WIDTH };
  }, [position]);

  const startDrag = (event: ReactPointerEvent<HTMLButtonElement>) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    dragRef.current = {
      moved: false,
      offsetX: event.clientX - position.x,
      offsetY: event.clientY - position.y,
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
    };
  };

  const moveDrag = (event: ReactPointerEvent<HTMLButtonElement>) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;

    const moved = Math.hypot(event.clientX - drag.startX, event.clientY - drag.startY) > 4;
    drag.moved = drag.moved || moved;
    persistPosition({
      x: event.clientX - drag.offsetX,
      y: event.clientY - drag.offsetY,
    });
  };

  const finishDrag = (event: ReactPointerEvent<HTMLButtonElement>) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;

    dragRef.current = null;
    event.currentTarget.releasePointerCapture(event.pointerId);
    if (drag.moved) return;

    if (open) {
      closePanel();
    } else {
      openRootPanel();
    }
  };

  const toggleDisplay = useCallback(() => {
    void executeMacro([{ keys: ["Power"], modifiers: null, delay: 80 }]);
    closePanel();
  }, [closePanel, executeMacro]);

  const logout = useCallback(async () => {
    const res = await api.POST(`${DEVICE_API}/auth/logout`);
    if (!res.ok) return;

    setUser(null);
    window.location.assign("/");
  }, [setUser]);

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        aria-label={open ? m.close() : "Android controller controls"}
        className={cx(
          "fixed z-50 flex touch-none items-center justify-center rounded-full",
          "border border-white/25 bg-slate-950/30 text-white shadow-xl backdrop-blur",
          "active:bg-slate-950/40",
        )}
        style={{
          height: BUTTON_SIZE,
          left: position.x,
          top: position.y,
          width: BUTTON_SIZE,
        }}
        onPointerCancel={() => {
          dragRef.current = null;
        }}
        onPointerDown={startDrag}
        onPointerMove={moveDrag}
        onPointerUp={finishDrag}
      >
        {open ? <LuX className="h-7 w-7" /> : <LuMenu className="h-7 w-7" />}
      </button>

      {open && (
        <div
          ref={panelRef}
          className={cx(
            "fixed z-50 max-h-[calc(100dvh-20px)] overflow-auto rounded-md",
            "border border-white/20 bg-slate-950/82 p-2 text-white shadow-2xl backdrop-blur",
          )}
          style={panelStyle}
          onPointerDown={event => event.stopPropagation()}
        >
          <div className="mb-2 flex items-center justify-between border-b border-white/10 pb-2">
            {panel === "root" ? (
              <span className="px-2 text-sm font-medium">Controls</span>
            ) : (
              <button
                type="button"
                className="rounded-md px-2 py-1 text-sm hover:bg-white/15 active:bg-white/25"
                onClick={openRootPanel}
              >
                Back
              </button>
            )}
            <button
              type="button"
              aria-label={m.close()}
              className="rounded-md p-2 hover:bg-white/15 active:bg-white/25"
              onClick={closePanel}
            >
              <LuX className="h-5 w-5" />
            </button>
          </div>

          {panel === "root" ? (
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
                icon={MdOutlineContentPasteGo}
                label={m.paste_text()}
                onClick={() => openChildPanel("paste")}
              />
              <ActionButton
                icon={LuCommand}
                label={m.action_bar_copy_text()}
                active={isOcrMode}
                onClick={() => {
                  setOcrMode(!isOcrMode);
                  closePanel();
                }}
              />
              <ActionButton
                icon={LuMonitorUp}
                label="Toggle display on/off"
                onClick={toggleDisplay}
              />
              <ActionButton
                icon={LuHardDrive}
                label={m.action_bar_virtual_media()}
                active={!!remoteVirtualMediaState}
                onClick={() => openChildPanel("media")}
              />
              <ActionButton
                icon={LuPower}
                label={m.action_bar_wake_on_lan()}
                onClick={() => openChildPanel("wol")}
              />
              <ActionButton
                icon={LuCable}
                label={m.action_bar_extension()}
                onClick={() => openChildPanel("extension")}
              />
              <ActionButton
                icon={LuKeyboard}
                label={m.action_bar_virtual_keyboard()}
                active={isVirtualKeyboardEnabled}
                onClick={() => {
                  if (window.JetKVMAndroid?.showInputMethod) {
                    window.JetKVMAndroid.showInputMethod();
                    closePanel();
                    return;
                  }

                  setVirtualKeyboardEnabled(!isVirtualKeyboardEnabled);
                }}
              />
              <ActionButton
                icon={LuSignal}
                label={m.action_bar_connection_stats()}
                onClick={() => {
                  toggleSidebarView("connection-stats");
                  closePanel();
                }}
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
                icon={LuBell}
                label="Requests"
                onClick={() => {
                  closePanel();
                  setRequestCenterOpen(true);
                }}
              />
              <ActionButton icon={LuLogOut} label="Log out" onClick={() => void logout()} />
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
      <CompanionRequestCenter
        compact
        forceOpen={requestCenterOpen}
        hideTrigger
        onClose={() => setRequestCenterOpen(false)}
      />
    </>
  );
}
