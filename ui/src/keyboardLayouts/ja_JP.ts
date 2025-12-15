import { KeyboardLayout, KeyCombo } from "../keyboardLayouts";

import { en_US } from "./en_US";

const name = "Japanese";
const isoCode = "ja-JP";

// NOTE:
// This layout is primarily implemented with primarily targets Windows/Linux in mind on common JIS 106/109 keyboards.
// Across Windows, Linux, and macOS, there are small but important differences in:
//  - how backslash ("\\") vs yen ("¥") are produced / interpreted, and
//  - how Japanese IME mode switching keys behave (e.g. Henkan/Muhenkan/KatakanaHiragana).
//
// For Windows/Linux friendliness, we intentionally map both "\\" and "¥" to the Yen key,
// since many environments/applications render the Yen key as a backslash.
//
// TODO:
// If macOS-specific behavior is required, consider adding a dedicated macOS JIS layout
// (e.g. ja_JP_mac) and adjust mappings (often mapping "\\" to Backslash instead of Yen),
// plus any IME-key semantics differences as needed.

export const chars = {
  ...en_US.chars,
  '"': { key: "Digit2", shift: true },
  "&": { key: "Digit6", shift: true },
  "'": { key: "Digit7", shift: true },
  "(": { key: "Digit8", shift: true },
  ")": { key: "Digit9", shift: true },
  "=": { key: "Minus", shift: true },
  "^": { key: "Equal" },
  "~": { key: "Equal", shift: true },
  "\\": { key: "Yen" },
  "¥": { key: "Yen" },
  "|": { key: "Yen", shift: true },
  "@": { key: "BracketLeft" },
  "`": { key: "BracketLeft", shift: true },
  "[": { key: "BracketRight" },
  "{": { key: "BracketRight", shift: true },
  ";": { key: "Semicolon" },
  "+": { key: "Semicolon", shift: true },
  ":": { key: "Quote" },
  "*": { key: "Quote", shift: true },
  "]": { key: "Backslash" },
  "}": { key: "Backslash", shift: true },
  _: { key: "KeyRO", shift: true },
} as Record<string, KeyCombo>;

// NOTE:
// We intentionally avoid providing Hiragana glyph labels on keycaps in the UI.
// Only about 5.1% of users typed with Kana input as of 2015; thus Kana legends are
// generally omitted to reduce visual clutter while keeping IME-related keys functional
// (Henkan/Muhenkan/KatakanaHiragana) for users who need them.
// Source: https://ja.wikipedia.org/wiki/%E3%81%8B%E3%81%AA%E5%85%A5%E5%8A%9B#%E3%81%8B%E3%81%AA%E5%85%A5%E5%8A%9B%E3%81%AE%E5%88%A9%E7%94%A8%E7%8A%B6%E6%B3%81
export const keyDisplayMap: Record<string, string> = {
  ...en_US.keyDisplayMap,
  "(Digit2)": '"',
  "(Digit6)": "&",
  "(Digit7)": "'",
  "(Digit8)": "(",
  "(Digit9)": ")",
  "(Minus)": "=",
  Equal: "^",
  "(Equal)": "~",
  Yen: "¥",
  "(Yen)": "|",
  KeyRO: "\\",
  "(KeyRO)": "_",
  Henkan: "変換",
  Muhenkan: "無変換",
  KatakanaHiragana: "ひらがな",
  Backquote: "半角/全角",
  "(KatakanaHiragana)": "ローマ字",
  BracketLeft: "@",
  "(BracketLeft)": "`",
  BracketRight: "[",
  "(BracketRight)": "{",
  Semicolon: ";",
  "(Semicolon)": "+",
  Quote: ":",
  "(Quote)": "*",
  Backslash: "]",
  "(Backslash)": "}",
  ContextMenu: "Menu",

  // UI-only notes:
  // - Keep a placeholder label for shifted Digit0 to avoid a "missing" keycap in the UI.
  // - Use "⏎" to hint at the tall, JIS/ISO-style L-shaped Enter key in the UI,
  //   while internally representing it with two virtual buttons.
  "(Digit0)": " ",
  "(Enter)": "⏎",
};

export const virtualKeyboard = {
  ...en_US.virtualKeyboard,
  main: {
    default: [
      "CtrlAltDelete AltMetaEscape CtrlAltBackspace",
      "Escape F1 F2 F3 F4 F5 F6 F7 F8 F9 F10 F11 F12",
      "Backquote Digit1 Digit2 Digit3 Digit4 Digit5 Digit6 Digit7 Digit8 Digit9 Digit0 Minus Equal Yen Backspace",
      "Tab KeyQ KeyW KeyE KeyR KeyT KeyY KeyU KeyI KeyO KeyP BracketLeft BracketRight Enter",
      "CapsLock KeyA KeyS KeyD KeyF KeyG KeyH KeyJ KeyK KeyL Semicolon Quote Backslash (Enter)",
      "ShiftLeft KeyZ KeyX KeyC KeyV KeyB KeyN KeyM Comma Period Slash KeyRO ShiftRight",
      "ControlLeft MetaLeft AltLeft Muhenkan Space Henkan KatakanaHiragana AltRight MetaRight ContextMenu ControlRight",
    ],
    shift: [
      "CtrlAltDelete AltMetaEscape CtrlAltBackspace",
      "Escape F1 F2 F3 F4 F5 F6 F7 F8 F9 F10 F11 F12",
      "Backquote (Digit1) (Digit2) (Digit3) (Digit4) (Digit5) (Digit6) (Digit7) (Digit8) (Digit9) (Digit0) (Minus) (Equal) (Yen) (Backspace)",
      "Tab (KeyQ) (KeyW) (KeyE) (KeyR) (KeyT) (KeyY) (KeyU) (KeyI) (KeyO) (KeyP) (BracketLeft) (BracketRight) Enter",
      "CapsLock (KeyA) (KeyS) (KeyD) (KeyF) (KeyG) (KeyH) (KeyJ) (KeyK) (KeyL) (Semicolon) (Quote) (Backslash) (Enter)",
      "ShiftLeft (KeyZ) (KeyX) (KeyC) (KeyV) (KeyB) (KeyN) (KeyM) (Comma) (Period) (Slash) (KeyRO) ShiftRight",
      "ControlLeft MetaLeft AltLeft Muhenkan Space Henkan (KatakanaHiragana) AltRight MetaRight ContextMenu ControlRight",
    ],
  },
};

export const ja_JP: KeyboardLayout = {
  isoCode,
  name,
  chars,
  keyDisplayMap,
  modifierDisplayMap: en_US.modifierDisplayMap,
  virtualKeyboard,
};
