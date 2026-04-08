/**
 * LayoutPreviewDialog — modal that shows a keyboard layout preview.
 *
 * Fetches the full KeyboardLayout by ID and renders it using the KLE
 * VirtualKeyboard component. Includes a "Use this layout" action button
 * that sets it as the active layout without closing the dialog.
 *
 * Used from:
 *   - Upload success flow (preview what was just uploaded)
 *   - Settings page preview button (preview any layout)
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { LuCheck, LuKeyboard } from "react-icons/lu";

import Modal from "@components/Modal";
import { Button } from "@components/Button";
import { VirtualKeyboard } from "@components/keyboard/VirtualKeyboard";
import type { KeyboardLayout } from "@components/keyboard/types/schema";
import { useSettingsStore } from "@hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

import "@components/keyboard/virtual-keyboard.css";

interface LayoutPreviewDialogProps {
  /** The layout ID to preview, or null to close */
  layoutId: string | null;
  onClose: () => void;
}

export function LayoutPreviewDialog({ layoutId, onClose }: LayoutPreviewDialogProps) {
  const { send } = useJsonRpc();
  const { keyboardLayout, setKeyboardLayout } = useSettingsStore();
  const [layout, setLayout] = useState<KeyboardLayout | null>(null);
  const [loading, setLoading] = useState(false);

  const isActive = keyboardLayout === layoutId;

  // Fetch layout data when layoutId changes
  useEffect(() => {
    if (!layoutId) {
      setLayout(null);
      return;
    }
    setLoading(true);
    void send("getKeyboardLayoutData", { id: layoutId }, (resp: JsonRpcResponse) => {
      setLoading(false);
      if ("error" in resp) {
        setLayout(null);
        return;
      }
      setLayout(resp.result as KeyboardLayout);
    });
  }, [layoutId, send]);

  const handleUseLayout = useCallback(() => {
    if (!layoutId) return;
    void send("setKeyboardLayout", { layout: layoutId }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          m.keyboard_layout_error({ error: resp.error.data || "" }),
        );
        return;
      }
      setKeyboardLayout(layoutId);
      notifications.success(
        m.keyboard_layout_success({ layout: layout?.name ?? layoutId }),
      );
    });
  }, [layoutId, layout, send, setKeyboardLayout]);

  // Press flash — briefly highlights the clicked key in the preview
  const [pressedKeys, setPressedKeys] = useState<Set<number>>(new Set());
  const timersRef = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map());

  const pressedScancodes = useMemo(() => pressedKeys, [pressedKeys]);

  const handlePreviewKeyPress = useCallback((scancode: number) => {
    if (scancode === 0) return;

    // Clear existing timer for this key if re-pressed quickly
    const existing = timersRef.current.get(scancode);
    if (existing) clearTimeout(existing);

    setPressedKeys(prev => new Set(prev).add(scancode));

    const timer = setTimeout(() => {
      setPressedKeys(prev => {
        const next = new Set(prev);
        next.delete(scancode);
        return next;
      });
      timersRef.current.delete(scancode);
    }, 200);

    timersRef.current.set(scancode, timer);
  }, []);

  // Clean up timers on close
  useEffect(() => {
    if (!layoutId) {
      for (const timer of timersRef.current.values()) clearTimeout(timer);
      timersRef.current.clear();
      setPressedKeys(new Set());
    }
  }, [layoutId]);

  return (
    <Modal open={layoutId !== null} onClose={onClose}>
      <div className="mx-auto max-w-7xl px-4">
        <div className="pointer-events-auto relative w-full overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
          {/* Header */}
          <div className="flex items-center justify-between border-b border-slate-200 px-6 py-4 dark:border-slate-700">
            <div className="flex items-center gap-3">
              <LuKeyboard className="h-5 w-5 text-slate-500 dark:text-slate-400" />
              <div>
                <h2 className="font-semibold text-slate-950 dark:text-white">
                  {layout?.name ?? layoutId ?? ""}
                </h2>
                {layout && (
                  <p className="text-xs text-slate-500 dark:text-slate-400">
                    {m.keyboard_layout_preview_keys({ count: layout.keys.length })}
                    {" · "}
                    {m.keyboard_layout_preview_chars({ count: Object.keys(layout.charMap).length })}
                    {layout.author && ` · ${layout.author}`}
                  </p>
                )}
              </div>
            </div>
            <div className="flex items-center gap-2">
              {isActive ? (
                <Button
                  size="SM"
                  theme="light"
                  text={m.keyboard_layout_preview_using()}
                  LeadingIcon={LuCheck}
                  data-testid="preview-layout-active"
                  disabled
                />
              ) : (
                <Button
                  size="SM"
                  theme="primary"
                  text={m.keyboard_layout_preview_use()}
                  data-testid="preview-use-layout"
                  onClick={handleUseLayout}
                />
              )}
              <Button
                size="SM"
                theme="blank"
                text={m.keyboard_layout_preview_close()}
                data-testid="preview-layout-close"
                onClick={onClose}
              />
            </div>
          </div>

          {/* Keyboard preview */}
          <div className="overflow-x-auto bg-slate-50 p-4 dark:bg-slate-800/50">
            {loading && (
              <div className="flex items-center justify-center py-12 text-sm text-slate-500">
                Loading...
              </div>
            )}
            {!loading && layout && (
              <div className="flex justify-center">
                <VirtualKeyboard
                  keyboard={layout}
                  isMetaActive={false}
                  onKeySend={handlePreviewKeyPress}
                  pressedScancodes={pressedScancodes}
                />
              </div>
            )}
            {!loading && !layout && layoutId && (
              <div className="flex items-center justify-center py-12 text-sm text-red-500">
                Failed to load layout
              </div>
            )}
          </div>
        </div>
      </div>
    </Modal>
  );
}
