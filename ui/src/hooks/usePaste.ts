import { useCallback, useEffect } from "react";

import useKeyboard from "@/hooks/useKeyboard";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { keyboardForLayout } from "@/hooks/useKeyboardLayout";
import { useRTCStore, useSettingsStore } from "@/hooks/stores";
import { loadKeyboardLayout } from "@/keyboardLayoutLoader";
import { textToMacroSteps } from "@/keyboardPaste";

export interface PasteResult {
  via: "keystrokes" | "cancelled";
  // Characters the keyboard layout cannot produce.
  skipped: string[];
}

// Pastes run one after another: a second keystroke macro cancels the one
// still typing, which truncated text that arrived in pieces. Cancelling
// bumps the generation so pastes queued before it are skipped.
let pasteQueue: Promise<unknown> = Promise.resolve();
let pasteGeneration = 0;

// One paste path for the paste modal, the clipboard shortcuts and text
// inserted by dictation tools: the text is typed with the selected layout.
export default function usePaste() {
  const { executeMacro, cancelExecuteMacro } = useKeyboard();
  const { send } = useJsonRpc();
  const rpcDataChannel = useRTCStore((state) => state.rpcDataChannel);
  const { setKeyboardLayout } = useSettingsStore();

  // Only the keyboard settings and the paste modal fetched the device's
  // layout, so a fresh browser typed its first dictation or shortcut paste
  // with the "en-US" default. The layout is fetched once per connection, the
  // first paste waits for it, and nothing is typed with a layout the device
  // did not confirm.
  useEffect(() => {
    void loadKeyboardLayout(rpcDataChannel, send, setKeyboardLayout);
  }, [rpcDataChannel, send, setKeyboardLayout]);

  const doPasteText = useCallback(
    async (
      text: string,
      options?: { delay?: number },
    ): Promise<PasteResult> => {
      const loaded = await loadKeyboardLayout(
        rpcDataChannel,
        send,
        setKeyboardLayout,
      );
      if (!loaded)
        throw new Error("the device's keyboard layout could not be read");
      const keyboard = keyboardForLayout(
        useSettingsStore.getState().keyboardLayout,
      );
      const { steps, skipped } = textToMacroSteps(
        text,
        keyboard,
        options?.delay,
      );
      if (steps.length > 0) await executeMacro(steps);
      return { via: "keystrokes", skipped };
    },
    [rpcDataChannel, send, setKeyboardLayout, executeMacro],
  );

  const pasteText = useCallback(
    (text: string, options?: { delay?: number }): Promise<PasteResult> => {
      const generation = pasteGeneration;
      const run = pasteQueue.then(() =>
        generation === pasteGeneration
          ? doPasteText(text, options)
          : { via: "cancelled" as const, skipped: [] },
      );
      pasteQueue = run.catch(() => undefined);
      return run;
    },
    [doPasteText],
  );

  // Stops the paste being typed and drops the ones queued behind it.
  const cancelPaste = useCallback(async () => {
    pasteGeneration++;
    await cancelExecuteMacro();
  }, [cancelExecuteMacro]);

  return { pasteText, cancelPaste };
}
