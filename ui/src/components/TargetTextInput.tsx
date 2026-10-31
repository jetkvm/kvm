import { useCallback, useRef } from "react";

import { useUiStore } from "@hooks/stores";
import usePaste from "@hooks/usePaste";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

export const targetTextInputId = "targetTextInput";

// A soft keyboard would pop up on touch devices, so the input mode is "none"
// there. Elsewhere it stays default: "none" also switches off the OS text
// services for the control on Windows, which dictation tools insert through.
const coarsePointer =
  typeof window !== "undefined" && window.matchMedia?.("(pointer: coarse)").matches;

// Hidden textarea that holds the keyboard focus while the video is used.
// Dictation tools, IMEs and accessibility clients need an editable control to
// insert into; ordinary typing never reaches it because the video's key
// handler consumes every key first. Whatever lands in it is pasted into the
// target. The device page's focus trap keeps the focus on it.
export default function TargetTextInput() {
  const { pasteText } = usePaste();
  const isOcrMode = useUiStore(state => state.isOcrMode);
  const pending = useRef({ text: "", timer: 0 });
  const lastValue = useRef("");
  const clearTimer = useRef(0);

  // Insertions can arrive in pieces; collect briefly and paste once.
  const take = useCallback(
    (el: HTMLTextAreaElement) => {
      const value = el.value;
      // Only what was added since the last take; a tool that replaces the
      // whole value counts as fresh text.
      const text = value.startsWith(lastValue.current)
        ? value.slice(lastValue.current.length)
        : value;
      lastValue.current = value;
      // The text stays in the field for a moment: a dictation tool may read
      // it back to confirm the paste landed before it restores the clipboard.
      window.clearTimeout(clearTimer.current);
      clearTimer.current = window.setTimeout(() => {
        el.value = "";
        lastValue.current = "";
      }, 3000);
      if (!text || isOcrMode) return;
      pending.current.text += text;
      window.clearTimeout(pending.current.timer);
      pending.current.timer = window.setTimeout(() => {
        const batch = pending.current.text;
        pending.current.text = "";
        void pasteText(batch)
          .then(result => {
            if (result.skipped.length > 0)
              notifications.error(m.paste_skipped_chars({ chars: result.skipped.join(" ") }));
          })
          .catch((error: unknown) =>
            notifications.error(m.paste_modal_failed_paste({ error: String(error) })),
          );
      }, 30);
    },
    [pasteText, isOcrMode],
  );

  return (
    <textarea
      id={targetTextInputId}
      data-text-input-shim
      tabIndex={-1}
      aria-label={m.video_text_input_label()}
      autoComplete="off"
      autoCapitalize="off"
      autoCorrect="off"
      spellCheck={false}
      inputMode={coarsePointer ? "none" : undefined}
      onInput={e => {
        // Composition text is taken when the IME commits it.
        if ((e.nativeEvent as InputEvent).isComposing) return;
        take(e.currentTarget);
      }}
      onCompositionEnd={e => take(e.currentTarget)}
      className="pointer-events-none absolute top-0 left-0 h-8 w-48 resize-none overflow-hidden opacity-0"
    />
  );
}
