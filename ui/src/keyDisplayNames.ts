/**
 * Display name mappings for keyboard modifiers and keys.
 *
 * Modifier names are universal (not layout-specific).
 * Key display names are synthesized from the active KLE layout at runtime
 * via buildKeyDisplayMap(), so the macro UI shows the correct legends
 * for the selected target keyboard.
 */

import { keys } from "@/keyboardMappings";
import type { KeyboardLayout } from "@components/keyboard/types/schema";

export const modifierDisplayMap: Record<string, string> = {
  ControlLeft: "Left Ctrl",
  ControlRight: "Right Ctrl",
  ShiftLeft: "Left Shift",
  ShiftRight: "Right Shift",
  AltLeft: "Left Alt",
  AltRight: "Right Alt",
  MetaLeft: "Left Meta",
  MetaRight: "Right Meta",
  AltGr: "AltGr",
};

/**
 * Builds a key code → display name map from a KLE layout.
 *
 * Maps KeyboardEvent.code values (e.g. "KeyA", "ArrowUp") to the legend
 * shown on that key in the target layout. For example, on German QWERTZ
 * the "KeyZ" code maps to scancode 0x1C (Y position), whose legend is "y".
 *
 * Falls back to the code name itself for unmapped keys.
 */
export function buildKeyDisplayMap(layout: KeyboardLayout | null): Record<string, string> {
  const displayMap: Record<string, string> = {};

  if (layout) {
    // Build scancode → legend lookup from the KLE layout
    const scancodeToLegend = new Map<number, string>();
    for (const key of layout.keys) {
      if (key.scancode === 0) continue;
      // Prefer the normal (unshifted) legend; skip multi-word labels like "Num Lock"
      const legend = key.legends.normal ?? key.legends.shift;
      if (legend && !scancodeToLegend.has(key.scancode)) {
        scancodeToLegend.set(key.scancode, legend);
      }
    }

    // Map each KeyboardEvent.code → its HID scancode → the KLE legend
    for (const [code, scancode] of Object.entries(keys)) {
      const legend = scancodeToLegend.get(scancode);
      displayMap[code] = legend ?? code;
    }
  } else {
    // No layout loaded — just use the code names
    for (const code of Object.keys(keys)) {
      displayMap[code] = code;
    }
  }

  return displayMap;
}
