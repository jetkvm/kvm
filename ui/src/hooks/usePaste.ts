import { useCallback } from "react";

import useKeyboard, { type MacroStep } from "@/hooks/useKeyboard";
import useKeyboardLayout from "@/hooks/useKeyboardLayout";

type Layout = ReturnType<typeof useKeyboardLayout>["selectedKeyboard"];

export interface PasteResult {
  via: "keystrokes" | "cancelled";
  // Characters the keyboard layout cannot produce.
  skipped: string[];
}

export const defaultKeystrokeDelay = 20;

// Converts text to key presses of the layout, the way the paste modal always
// has: accent key first for composed characters, a space after a dead key.
export function textToMacroSteps(
  text: string,
  layout: Layout,
  delay = defaultKeystrokeDelay,
): { steps: MacroStep[]; skipped: string[] } {
  const steps: MacroStep[] = [];
  const skipped = new Set<string>();
  for (const char of text) {
    const normalized = char.normalize("NFC");
    const keyprops = layout.chars[normalized];
    if (!keyprops?.key) {
      skipped.add(normalized);
      continue;
    }
    const { key, shift, altRight, deadKey, accentKey } = keyprops;
    if (accentKey) {
      const accentModifiers: string[] = [];
      if (accentKey.shift) accentModifiers.push("ShiftLeft");
      if (accentKey.altRight) accentModifiers.push("AltRight");
      steps.push({
        keys: [String(accentKey.key)],
        modifiers: accentModifiers.length > 0 ? accentModifiers : null,
        delay,
      });
    }
    const modifiers: string[] = [];
    if (shift) modifiers.push("ShiftLeft");
    if (altRight) modifiers.push("AltRight");
    steps.push({ keys: [String(key)], modifiers: modifiers.length > 0 ? modifiers : null, delay });
    if (deadKey) steps.push({ keys: ["Space"], modifiers: null, delay });
  }
  return { steps, skipped: [...skipped] };
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
