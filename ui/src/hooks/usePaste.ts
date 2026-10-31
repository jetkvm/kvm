import { useCallback } from "react";

import useKeyboard from "@/hooks/useKeyboard";
import useKeyboardLayout from "@/hooks/useKeyboardLayout";
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
  const { selectedKeyboard } = useKeyboardLayout();

  const doPasteText = useCallback(
    async (text: string, options?: { delay?: number }): Promise<PasteResult> => {
      const { steps, skipped } = textToMacroSteps(text, selectedKeyboard, options?.delay);
      if (steps.length > 0) await executeMacro(steps);
      return { via: "keystrokes", skipped };
    },
    [selectedKeyboard, executeMacro],
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
