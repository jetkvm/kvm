import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChevronDownIcon } from "@heroicons/react/16/solid";
import { AnimatePresence, motion } from "framer-motion";
import { LuKeyboard } from "react-icons/lu";

import DetachIconRaw from "@/assets/detach-icon.svg";
import { cx } from "@/cva.config";
import { useHidStore, useSettingsStore, useUiStore } from "@hooks/stores";
import useKeyboard from "@hooks/useKeyboard";
import { Button, LinkButton } from "@components/Button";
import Card from "@components/Card";
import { useJsonRpc, JsonRpcResponse } from "@hooks/useJsonRpc";
import { VirtualKeyboard as KleKeyboard } from "@components/keyboard/VirtualKeyboard";
import { QuickActions } from "@components/QuickActions";
import type { KeyboardLayout } from "@components/keyboard/types/schema";
import { isModifierScancode, hidKeyToModifierMask } from "@/keyboardMappings";
import { m } from "@localizations/messages.js";

import "@components/keyboard/virtual-keyboard.css";

export const DetachIcon = ({ className }: { className?: string }) => {
  return <img src={DetachIconRaw} alt="Detach Icon" className={className} />;
};

// Reverse map: modifier bit → HID scancode (for decoding keysDownState.modifier)
const modifierBitToScancode: [number, number][] = Object.entries(hidKeyToModifierMask).map(
  ([sc, bit]) => [bit as number, Number(sc)],
);

type ResizeDirection =
  | "left"
  | "right"
  | "top"
  | "bottom"
  | "top-left"
  | "top-right"
  | "bottom-left"
  | "bottom-right";

const DETACHED_MIN_WIDTH = 600;
const DETACHED_MAX_WIDTH = 1800;

function KeyboardWrapper() {
  const keyboardRef = useRef<HTMLDivElement>(null);
  const { isAttachedVirtualKeyboardVisible, setAttachedVirtualKeyboardVisibility } = useUiStore();
  const { keysDownState, keyboardLedState, isVirtualKeyboardEnabled, setVirtualKeyboardEnabled } =
    useHidStore();
  const { handleKeyPress, executeMacro } = useKeyboard();
  const { keyboardLayout, modifierLatching } = useSettingsStore();
  const { send } = useJsonRpc();

  // KLE layout fetched from backend
  const [kleLayout, setKleLayout] = useState<KeyboardLayout | null>(null);

  // Dragging state for detached mode
  const [isDragging, setIsDragging] = useState(false);
  const [isResizing, setIsResizing] = useState(false);
  const [position, setPosition] = useState({ x: 0, y: 0 });
  const [newPosition, setNewPosition] = useState({ x: 0, y: 0 });
  const [detachedWidth, setDetachedWidth] = useState<number | null>(null);
  const resizeDirectionRef = useRef<ResizeDirection | null>(null);
  const resizeStartRef = useRef({
    x: 0,
    y: 0,
    width: 0,
    height: 0,
    pointerX: 0,
    pointerY: 0,
  });

  // Track which modifiers the virtual keyboard has latched.
  // This provides immediate visual feedback without waiting
  // for the HID round-trip via keysDownState.
  const [latchedModifiers, setLatchedModifiers] = useState<Set<number>>(new Set());
  const latchedModifiersRef = useRef(latchedModifiers);
  latchedModifiersRef.current = latchedModifiers;

  // ---------------------------------------------------------------------------
  // Fetch KLE layout from backend when keyboard layout changes
  // ---------------------------------------------------------------------------

  useEffect(() => {
    if (!keyboardLayout) return;

    void send("getKeyboardLayoutData", { id: keyboardLayout }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        setKleLayout(null);
        return;
      }
      setKleLayout(resp.result as KeyboardLayout);
    });
  }, [send, keyboardLayout]);

  // ---------------------------------------------------------------------------
  // Pressed scancodes — derived entirely from keysDownState (single source of truth)
  // ---------------------------------------------------------------------------

  const pressedScancodes = useMemo(() => {
    const set = new Set<number>();

    // Non-modifier keys from the HID key buffer
    if (keysDownState.keys) {
      for (const k of keysDownState.keys) {
        if (k !== 0) set.add(k);
      }
    }

    // Decode modifier byte into individual scancodes
    if (keysDownState.modifier) {
      for (const [bit, scancode] of modifierBitToScancode) {
        if (keysDownState.modifier & bit) {
          set.add(scancode);
        }
      }
    }

    // Merge optimistic latch state for immediate visual feedback
    for (const sc of latchedModifiers) {
      set.add(sc);
    }

    return set;
  }, [keysDownState, latchedModifiers]);

  // ---------------------------------------------------------------------------
  // Key send handler — bridges the KLE keyboard to the HID layer
  // ---------------------------------------------------------------------------

  const onKeySend = useCallback(
    (scancode: number) => {
      if (scancode === 0) return;

      if (isModifierScancode(scancode) && modifierLatching) {
        // Latch mode: click to toggle on/off.
        // Read from ref so this callback's identity stays stable across
        // latch toggles — prevents defeating React.memo on all keycaps.
        const current = latchedModifiersRef.current;
        const isLatched = current.has(scancode);
        const next = new Set(current);
        if (isLatched) {
          next.delete(scancode);
        } else {
          next.add(scancode);
        }
        setLatchedModifiers(next);
        void handleKeyPress(scancode, !isLatched);
      } else {
        // Regular key (or non-latching modifier): press then release
        void handleKeyPress(scancode, true);
        setTimeout(() => void handleKeyPress(scancode, false), 50);
      }
    },
    [handleKeyPress, modifierLatching],
  );

  // ---------------------------------------------------------------------------
  // Drag handling (for detached/floating mode)
  // ---------------------------------------------------------------------------

  const getClientPoint = (e: MouseEvent | TouchEvent) => {
    if (e instanceof TouchEvent) {
      if (e.touches.length === 0) return null;
      return { x: e.touches[0].clientX, y: e.touches[0].clientY };
    }
    return { x: e.clientX, y: e.clientY };
  };

  const startDrag = useCallback(
    (e: MouseEvent | TouchEvent) => {
      if (!keyboardRef.current) return;
      if (isResizing) return;
      const target = e.target as HTMLElement | null;
      if (target?.closest('[data-resize-handle="true"]')) return;
      if (e instanceof TouchEvent && e.touches.length > 1) return;
      setIsDragging(true);

      const point = getClientPoint(e);
      if (!point) return;

      const rect = keyboardRef.current.getBoundingClientRect();
      setPosition({
        x: point.x - rect.left,
        y: point.y - rect.top,
      });
    },
    [isResizing],
  );

  const startResize = useCallback(
    (direction: ResizeDirection, e: MouseEvent | TouchEvent) => {
      if (!keyboardRef.current) return;
      if (e instanceof TouchEvent && e.touches.length > 1) return;

      const point = getClientPoint(e);
      if (!point) return;

      const rect = keyboardRef.current.getBoundingClientRect();
      resizeDirectionRef.current = direction;
      resizeStartRef.current = {
        x: newPosition.x,
        y: newPosition.y,
        width: rect.width,
        height: rect.height,
        pointerX: point.x,
        pointerY: point.y,
      };
      setIsResizing(true);
      setIsDragging(false);
    },
    [newPosition.x, newPosition.y],
  );

  const onPointerMove = useCallback(
    (e: MouseEvent | TouchEvent) => {
      if (!keyboardRef.current) return;
      const point = getClientPoint(e);
      if (!point) return;

      if (isResizing && resizeDirectionRef.current) {
        const start = resizeStartRef.current;
        const dir = resizeDirectionRef.current;

        const dx = point.x - start.pointerX;
        const dy = point.y - start.pointerY;

        const hasLeft = dir.includes("left");
        const hasRight = dir.includes("right");
        const hasTop = dir.includes("top");
        const hasBottom = dir.includes("bottom");

        const aspect = start.height > 0 ? start.width / start.height : 1;
        const fromX = (hasLeft ? -dx : 0) + (hasRight ? dx : 0);
        const fromY = ((hasTop ? -dy : 0) + (hasBottom ? dy : 0)) * aspect;

        let deltaW = fromX;
        if ((hasLeft || hasRight) && (hasTop || hasBottom)) {
          deltaW = (fromX + fromY) / 2;
        } else if (hasTop || hasBottom) {
          deltaW = fromY;
        }

        const nextWidth = Math.max(
          DETACHED_MIN_WIDTH,
          Math.min(DETACHED_MAX_WIDTH, start.width + deltaW),
        );
        const scale = nextWidth / start.width;
        const nextHeight = start.height * scale;

        let nextX = start.x;
        let nextY = start.y;

        if (hasLeft) {
          nextX = start.x + (start.width - nextWidth);
        }
        if (hasTop) {
          nextY = start.y + (start.height - nextHeight);
        }

        const maxX = window.innerWidth - nextWidth;
        const maxY = window.innerHeight - nextHeight;

        setDetachedWidth(nextWidth);
        setNewPosition({
          x: Math.min(Math.max(nextX, 0), Math.max(maxX, 0)),
          y: Math.min(Math.max(nextY, 0), Math.max(maxY, 0)),
        });
        return;
      }

      if (isDragging) {
        const clientX = point.x;
        const clientY = point.y;

        const newX = clientX - position.x;
        const newY = clientY - position.y;

        const rect = keyboardRef.current.getBoundingClientRect();
        const maxX = window.innerWidth - rect.width;
        const maxY = window.innerHeight - rect.height;

        setNewPosition({
          x: Math.min(maxX, Math.max(0, newX)),
          y: Math.min(maxY, Math.max(0, newY)),
        });
      }
    },
    [isDragging, isResizing, position.x, position.y],
  );

  const endDrag = useCallback(() => {
    setIsDragging(false);
    setIsResizing(false);
    resizeDirectionRef.current = null;
  }, []);

  const onResizeHandleMouseDown = useCallback(
    (direction: ResizeDirection) => (e: React.MouseEvent<HTMLDivElement>) => {
      e.preventDefault();
      e.stopPropagation();
      startResize(direction, e.nativeEvent);
    },
    [startResize],
  );

  const onResizeHandleTouchStart = useCallback(
    (direction: ResizeDirection) => (e: React.TouchEvent<HTMLDivElement>) => {
      e.preventDefault();
      e.stopPropagation();
      startResize(direction, e.nativeEvent);
    },
    [startResize],
  );

  useEffect(() => {
    if (!isAttachedVirtualKeyboardVisible && keyboardRef.current && detachedWidth == null) {
      const rect = keyboardRef.current.getBoundingClientRect();
      if (rect.width > 0) {
        setDetachedWidth(Math.max(DETACHED_MIN_WIDTH, Math.min(DETACHED_MAX_WIDTH, rect.width)));
      }
    }
  }, [isAttachedVirtualKeyboardVisible, detachedWidth]);

  useEffect(() => {
    if (isAttachedVirtualKeyboardVisible) return;

    const handle = keyboardRef.current;
    if (handle) {
      handle.addEventListener("touchstart", startDrag);
      handle.addEventListener("mousedown", startDrag);
    }

    document.addEventListener("mouseup", endDrag);
    document.addEventListener("touchend", endDrag);

    document.addEventListener("mousemove", onPointerMove);
    document.addEventListener("touchmove", onPointerMove);

    return () => {
      if (handle) {
        handle.removeEventListener("touchstart", startDrag);
        handle.removeEventListener("mousedown", startDrag);
      }

      document.removeEventListener("mouseup", endDrag);
      document.removeEventListener("touchend", endDrag);

      document.removeEventListener("mousemove", onPointerMove);
      document.removeEventListener("touchmove", onPointerMove);
    };
  }, [isAttachedVirtualKeyboardVisible, endDrag, onPointerMove, startDrag]);

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <div
      className="transition-all duration-500 ease-in-out"
      style={{
        marginBottom: isVirtualKeyboardEnabled ? "0px" : `-${350}px`,
      }}
    >
      <AnimatePresence>
        {isVirtualKeyboardEnabled && (
          <motion.div
            initial={{ opacity: 0, y: "100%" }}
            animate={{ opacity: 1, y: "0%" }}
            exit={{ opacity: 0, y: "100%" }}
            transition={{
              duration: 0.5,
              ease: "easeInOut",
            }}
          >
            <div
              className={cx(
                !isAttachedVirtualKeyboardVisible
                  ? "fixed top-0 left-0 z-10 max-w-[1800px] min-w-[600px] select-none"
                  : "mx-auto max-w-[1200px] min-w-[600px]",
              )}
              ref={keyboardRef}
              style={{
                ...(!isAttachedVirtualKeyboardVisible
                  ? {
                      transform: `translate(${newPosition.x}px, ${newPosition.y}px)`,
                      width: detachedWidth ? `${detachedWidth}px` : undefined,
                    }
                  : {}),
              }}
            >
              <Card
                className={cx("overflow-hidden", {
                  "rounded-none": isAttachedVirtualKeyboardVisible,
                  "keyboard-detached": !isAttachedVirtualKeyboardVisible,
                })}
              >
                <div className="flex items-center justify-between border-b border-b-slate-800/30 bg-white px-2 py-3 dark:border-b-slate-300/20 dark:bg-slate-800">
                  <div className="flex items-center gap-x-2">
                    {isAttachedVirtualKeyboardVisible ? (
                      <Button
                        size="XS"
                        theme="light"
                        text={m.detach()}
                        data-testid="virtual-keyboard-detach"
                        onClick={() => setAttachedVirtualKeyboardVisibility(false)}
                      />
                    ) : (
                      <Button
                        size="XS"
                        theme="light"
                        text={m.attach()}
                        data-testid="virtual-keyboard-attach"
                        onClick={() => setAttachedVirtualKeyboardVisibility(true)}
                      />
                    )}
                    <h2 className="font-sans text-sm leading-none font-medium text-slate-700 select-none dark:text-slate-300">
                      {m.virtual_keyboard_header()}
                    </h2>
                  </div>
                  <div className="flex items-center gap-x-2">
                    <div className="hidden items-center gap-x-2 md:flex">
                      <QuickActions onExecuteMacro={executeMacro} />
                      <div className="h-5 w-px bg-slate-800/20 dark:bg-slate-200/20" />
                      <LinkButton
                        size="XS"
                        to="settings/keyboard"
                        theme="light"
                        text={kleLayout?.name ?? keyboardLayout ?? "Keyboard"}
                        LeadingIcon={LuKeyboard}
                      />
                      <div className="h-5 w-px bg-slate-800/20 dark:bg-slate-200/20" />
                    </div>

                    <Button
                      size="XS"
                      theme="light"
                      text={m.hide()}
                      data-testid="virtual-keyboard-hide"
                      LeadingIcon={ChevronDownIcon}
                      onClick={() => setVirtualKeyboardEnabled(false)}
                    />
                  </div>
                </div>

                <div className="bg-blue-50/80 dark:bg-slate-700">
                  {kleLayout ? (
                    <KleKeyboard
                      keyboard={kleLayout}
                      isMetaActive={false}
                      onKeySend={onKeySend}
                      pressedScancodes={pressedScancodes}
                      kanaLedOn={keyboardLedState.kana}
                      vkbClassName={[
                        keyboardLedState.caps_lock && "caps-lock-on",
                        keyboardLedState.num_lock && "num-lock-on",
                        keyboardLedState.scroll_lock && "scroll-lock-on",
                        keyboardLedState.kana && "kana-on",
                      ]
                        .filter(Boolean)
                        .join(" ")}
                    />
                  ) : (
                    <div className="flex items-center justify-center p-8 text-sm text-slate-500 dark:text-slate-400">
                      {m.virtual_keyboard_header()}
                    </div>
                  )}
                </div>
              </Card>

              {!isAttachedVirtualKeyboardVisible && (
                <>
                  <div
                    className="absolute top-0 bottom-0 -left-1 z-20 w-2 cursor-ew-resize"
                    data-resize-handle="true"
                    onMouseDown={onResizeHandleMouseDown("left")}
                    onTouchStart={onResizeHandleTouchStart("left")}
                  />
                  <div
                    className="absolute top-0 -right-1 bottom-0 z-20 w-2 cursor-ew-resize"
                    data-resize-handle="true"
                    onMouseDown={onResizeHandleMouseDown("right")}
                    onTouchStart={onResizeHandleTouchStart("right")}
                  />
                  <div
                    className="absolute -top-1 right-0 left-0 z-20 h-2 cursor-ns-resize"
                    data-resize-handle="true"
                    onMouseDown={onResizeHandleMouseDown("top")}
                    onTouchStart={onResizeHandleTouchStart("top")}
                  />
                  <div
                    className="absolute right-0 -bottom-1 left-0 z-20 h-2 cursor-ns-resize"
                    data-resize-handle="true"
                    onMouseDown={onResizeHandleMouseDown("bottom")}
                    onTouchStart={onResizeHandleTouchStart("bottom")}
                  />
                  <div
                    className="absolute -top-1 -left-1 z-30 h-3 w-3 cursor-nwse-resize"
                    data-resize-handle="true"
                    onMouseDown={onResizeHandleMouseDown("top-left")}
                    onTouchStart={onResizeHandleTouchStart("top-left")}
                  />
                  <div
                    className="absolute -top-1 -right-1 z-30 h-3 w-3 cursor-nesw-resize"
                    data-resize-handle="true"
                    onMouseDown={onResizeHandleMouseDown("top-right")}
                    onTouchStart={onResizeHandleTouchStart("top-right")}
                  />
                  <div
                    className="absolute -bottom-1 -left-1 z-30 h-3 w-3 cursor-nesw-resize"
                    data-resize-handle="true"
                    onMouseDown={onResizeHandleMouseDown("bottom-left")}
                    onTouchStart={onResizeHandleTouchStart("bottom-left")}
                  />
                  <div
                    className="absolute -right-1 -bottom-1 z-30 h-3 w-3 cursor-nwse-resize"
                    data-resize-handle="true"
                    onMouseDown={onResizeHandleMouseDown("bottom-right")}
                    onTouchStart={onResizeHandleTouchStart("bottom-right")}
                  />
                </>
              )}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

export default KeyboardWrapper;
