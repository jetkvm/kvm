/**
 * Converts a text string into macro steps using the KLE layout's charMap.
 *
 * Each character is looked up in the charMap to get its HID scancode + modifier byte,
 * then reverse-mapped to the key/modifier names used by the macro system.
 *
 * Returns { steps, invalidChars } — steps ready to append to a macro,
 * and any characters that couldn't be mapped.
 */

import { keys, modifiers } from "@/keyboardMappings";
import type { KeyboardLayout, HIDCombo } from "@components/keyboard/types/schema";
import type { KeySequenceStep } from "@hooks/stores";

const DEFAULT_DELAY = 50;

// Build reverse lookup: scancode → key name (first match wins)
export const scancodeToKeyName = new Map<number, string>();
for (const [name, scancode] of Object.entries(keys)) {
  // Skip modifier key names (they go in the modifiers array, not keys)
  if (name.startsWith("Control") || name.startsWith("Shift") ||
      name.startsWith("Alt") || name.startsWith("Meta")) {
    continue;
  }
  if (!scancodeToKeyName.has(scancode)) {
    scancodeToKeyName.set(scancode, name);
  }
}

// Modifier bit → modifier name
const modBitToName: [number, string][] = [
  [modifiers.ShiftLeft, "ShiftLeft"],
  [modifiers.ShiftRight, "ShiftRight"],
  [modifiers.ControlLeft, "ControlLeft"],
  [modifiers.ControlRight, "ControlRight"],
  [modifiers.AltLeft, "AltLeft"],
  [modifiers.AltRight, "AltRight"],
  [modifiers.MetaLeft, "MetaLeft"],
  [modifiers.MetaRight, "MetaRight"],
];

function comboToStep(combo: HIDCombo): KeySequenceStep | null {
  const keyName = scancodeToKeyName.get(combo.s);
  if (!keyName) return null;

  const mods: string[] = [];
  for (const [bit, name] of modBitToName) {
    if (combo.m & bit) {
      mods.push(name);
    }
  }

  return { keys: [keyName], modifiers: mods, delay: DEFAULT_DELAY };
}

export interface TextToMacroResult {
  steps: KeySequenceStep[];
  invalidChars: string[];
}

export function textToMacroSteps(
  text: string,
  layout: KeyboardLayout,
): TextToMacroResult {
  const steps: KeySequenceStep[] = [];
  const invalidChars: string[] = [];

  for (const char of text) {
    const normalizedChar = char.normalize("NFC");
    const combo = layout.charMap[normalizedChar];

    if (!combo || combo.s === 0) {
      if (!invalidChars.includes(normalizedChar)) {
        invalidChars.push(normalizedChar);
      }
      continue;
    }

    // Dead key prefix: add the prefix keystroke first
    if (combo.p) {
      const prefixStep = comboToStep(combo.p);
      if (prefixStep) {
        steps.push(prefixStep);
      }
    }

    const step = comboToStep(combo);
    if (step) {
      steps.push(step);
    }
  }

  return { steps, invalidChars };
}
