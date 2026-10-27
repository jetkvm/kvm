import type { MacroStep } from "@hooks/useKeyboard";
import type { KeyboardLayout } from "@/keyboardLayouts";

export const defaultKeystrokeDelay = 20;

// Converts text to key presses of the layout, the way the paste modal always
// has: accent key first for composed characters, a space after a dead key.
export function textToMacroSteps(
  text: string,
  layout: KeyboardLayout,
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

// A physical key always carries a code. A key event without one was injected
// by software with a virtual key only, which is how dictation tools press
// Ctrl+V after putting the text on the clipboard.
export function isInjectedKey(e: KeyboardEvent) {
  return e.code === "" || e.code === "Unidentified";
}
export function isVKey(e: KeyboardEvent) {
  return e.code === "KeyV" || (isInjectedKey(e) && e.key.toLowerCase() === "v");
}

// Ctrl+V and Ctrl+Shift+V (Cmd on a Mac) each paste the client clipboard
// when their switch is on; otherwise the combination reaches the target.
export function isClientPasteShortcut(e: KeyboardEvent, ctrlV: boolean, ctrlShiftV: boolean) {
  if (!isVKey(e) || !(e.ctrlKey || e.metaKey) || e.altKey) return false;
  return e.shiftKey ? ctrlShiftV : ctrlV;
}

// A plain Ctrl+V injected without a key code, which is how dictation tools
// paste, is handed to the browser's paste so the text reaches the target. A
// person's plain Ctrl+V has a key code and reaches the target as keys.
export function isInjectedPasteChord(e: KeyboardEvent) {
  return isInjectedKey(e) && isVKey(e) && e.ctrlKey && !e.shiftKey && !e.altKey && !e.metaKey;
}

// Simulated typing (Windows VK_PACKET, some soft keyboards) carries no key
// code, only the character.
export function isTypedCharacter(e: KeyboardEvent) {
  if ((e.code !== "" && e.code !== "Unidentified") || e.ctrlKey || e.metaKey || e.altKey)
    return false;
  if (e.key === "Enter") return true;
  // One code point: a BMP character or a surrogate pair. Named keys such as
  // "Process" or "Dead" are longer.
  const first = e.key.charCodeAt(0);
  return e.key.length === 1 || (e.key.length === 2 && first >= 0xd800 && first <= 0xdbff);
}
