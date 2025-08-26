import { KeyboardLayout, KeyCombo } from "../keyboardLayouts"

import { en_US } from "./en_US" // for fallback of keyDisplayMap, modifierDisplayMap, and virtualKeyboard

const name = "Deutsch";
const isoCode = "de-DE";

const keyAcute: KeyCombo = { key: "Equal" } // accent aigu (acute accent), mark ´ placed above the letter
const keyHat: KeyCombo = { key: "Backquote" } // accent circonflexe (accent hat), mark ^ placed above the letter
const keyGrave: KeyCombo = { key: "Equal", shift: true } // accent grave, mark ` placed above the letter

const chars = {
  a: { key: "KeyA" },
  "á": { key: "KeyA", accentKey: keyAcute },
  "â": { key: "KeyA", accentKey: keyHat },
  "à": { key: "KeyA", accentKey: keyGrave },
  A: { key: "KeyA", shift: true },
  "Á": { key: "KeyA", shift: true, accentKey: keyAcute },
  "Â": { key: "KeyA", shift: true, accentKey: keyHat },
  "À": { key: "KeyA", shift: true, accentKey: keyGrave },
  "☺": { key: "KeyA", altRight: true }, // white smiling face ☺
  b: { key: "KeyB" },
  B: { key: "KeyB", shift: true },
  "‹": { key: "KeyB", altRight: true }, // single left-pointing angle quotation mark, ‹
  c: { key: "KeyC" },
  C: { key: "KeyC", shift: true },
  "\u202f": { key: "KeyC", altRight: true }, // narrow no-break space
  d: { key: "KeyD" },
  D: { key: "KeyD", shift: true },
  "′": { key: "KeyD", altRight: true }, // prime, mark ′ placed above the letter
  e: { key: "KeyE" },
  "é": { key: "KeyE", accentKey: keyAcute },
  "ê": { key: "KeyE", accentKey: keyHat },
  "è": { key: "KeyE", accentKey: keyGrave },
  "€": { key: "KeyE", altRight: true },
  E: { key: "KeyE", shift: true },
  "É": { key: "KeyE", shift: true, accentKey: keyAcute },
  "Ê": { key: "KeyE", shift: true, accentKey: keyHat },
  "È": { key: "KeyE", shift: true, accentKey: keyGrave },
  f: { key: "KeyF" },
  F: { key: "KeyF", shift: true },
  "˟": { key: "KeyF", deadKey: true, altRight: true }, // modifier letter cross accent, ˟
  G: { key: "KeyG", shift: true },
  g: { key: "KeyG" },
  "ẞ": { key: "KeyG", altRight: true }, // capital sharp S, ẞ
  h: { key: "KeyH" },
  H: { key: "KeyH", shift: true },
  "ˍ": { key: "KeyH", deadKey: true, altRight: true }, // modifier letter low macron, ˍ
  i: { key: "KeyI" },
  "í": { key: "KeyI", accentKey: keyAcute },
  "î": { key: "KeyI", accentKey: keyHat },
  "ì": { key: "KeyI", accentKey: keyGrave },
  I: { key: "KeyI", shift: true },
  "Í": { key: "KeyI", shift: true, accentKey: keyAcute },
  "Î": { key: "KeyI", shift: true, accentKey: keyHat },
  "Ì": { key: "KeyI", shift: true, accentKey: keyGrave },
  "˜": { key: "KeyI", deadKey: true, altRight: true }, // tilde accent, mark ˜ placed above the letter
  j: { key: "KeyJ" },
  J: { key: "KeyJ", shift: true },
  "¸": { key: "KeyJ", deadKey: true, altRight: true }, // cedilla accent, mark ¸ placed below the letter
  k: { key: "KeyK" },
  K: { key: "KeyK", shift: true },
  l: { key: "KeyL" },
  L: { key: "KeyL", shift: true },
  "ˏ": { key: "KeyL", deadKey: true, altRight: true }, // modifier letter reversed comma, ˏ
  m: { key: "KeyM" },
  M: { key: "KeyM", shift: true },
  "µ": { key: "KeyM", altRight: true },
  n: { key: "KeyN" },
  N: { key: "KeyN", shift: true },
  "–": { key: "KeyN", altRight: true }, // en dash, –
  o: { key: "KeyO" },
  "ó": { key: "KeyO", accentKey: keyAcute },
  "ô": { key: "KeyO", accentKey: keyHat },
  "ò": { key: "KeyO", accentKey: keyGrave },
  O: { key: "KeyO", shift: true },
  "Ó": { key: "KeyO", shift: true, accentKey: keyAcute },
  "Ô": { key: "KeyO", shift: true, accentKey: keyHat },
  "Ò": { key: "KeyO", shift: true, accentKey: keyGrave },
  "˚": { key: "KeyO", deadKey: true, altRight: true }, // ring above, ˚
  p: { key: "KeyP" },
  P: { key: "KeyP", shift: true },
  "ˀ": { key: "KeyP", deadKey: true, altRight: true }, // modifier letter apostrophe, ʾ
  q: { key: "KeyQ" },
  Q: { key: "KeyQ", shift: true },
  "@": { key: "KeyQ", altRight: true },
  R: { key: "KeyR", shift: true },
  r: { key: "KeyR" },
  "˝": { key: "KeyR", deadKey: true, altRight: true }, // double acute accent, mark ˝ placed above the letter
  S: { key: "KeyS", shift: true },
  s: { key: "KeyS" },
  "″": { key: "KeyS", altRight: true }, // double prime, mark ″ placed above the letter
  T: { key: "KeyT", shift: true },
  t: { key: "KeyT" },
  "ˇ": { key: "KeyT", deadKey: true, altRight: true }, // caron/hacek accent, mark ˇ placed above the letter
  u: { key: "KeyU" },
  "ú": { key: "KeyU", accentKey: keyAcute },
  "û": { key: "KeyU", accentKey: keyHat },
  "ù": { key: "KeyU", accentKey: keyGrave },
  U: { key: "KeyU", shift: true },
  "Ú": { key: "KeyU", shift: true, accentKey: keyAcute },
  "Û": { key: "KeyU", shift: true, accentKey: keyHat },
  "Ù": { key: "KeyU", shift: true, accentKey: keyGrave },
  "˘": { key: "KeyU", deadKey: true, altRight: true }, // breve accent, ˘ placed above the letter
  v: { key: "KeyV" },
  V: { key: "KeyV", shift: true },
  "«": { key: "KeyV", altRight: true }, // left-pointing double angle quotation mark, «
  w: { key: "KeyW" },
  W: { key: "KeyW", shift: true },
  "¯": { key: "KeyW", deadKey: true, altRight: true }, // macron accent, mark ¯ placed above the letter
  x: { key: "KeyX" },
  X: { key: "KeyX", shift: true },
  "»": { key: "KeyX", altRight: true },
  // cross key between shift and y (aka OEM 102 key)
  y: { key: "KeyZ" },
  Y: { key: "KeyZ", shift: true },
  "›": { key: "KeyZ", altRight: true }, // single right-pointing angle quotation mark, ›
  z: { key: "KeyY" },
  Z: { key: "KeyY", shift: true },
  "¨": { key: "KeyY", deadKey: true, altRight: true }, // diaeresis accent, mark ¨ placed above the letter
  "°": { key: "Backquote", shift: true },
  "^": { key: "Backquote", deadKey: true },
  "|": { key: "Backquote", altRight: true },
  1: { key: "Digit1" },
  "!": { key: "Digit1", shift: true },
  "’": { key: "Digit1", altRight: true }, // single quote, mark ’ placed above the letter
  2: { key: "Digit2" },
  "\"": { key: "Digit2", shift: true },
  "²": { key: "Digit2", altRight: true },
  "<": { key: "Digit2", altRight: true }, // non-US < and >
  3: { key: "Digit3" },
  "§": { key: "Digit3", shift: true },
  "³": { key: "Digit3", altRight: true },
  ">": { key: "Digit3", altRight: true }, // non-US < and >
  4: { key: "Digit4" },
  "$": { key: "Digit4", shift: true },
  "—": { key: "Digit4", altRight: true }, // em dash, — 
  5: { key: "Digit5" },
  "%": { key: "Digit5", shift: true },
  "¡": { key: "Digit5", altRight: true }, // inverted exclamation mark, ¡
  6: { key: "Digit6" },
  "&": { key: "Digit6", shift: true },
  "¿": { key: "Digit6", altRight: true }, // inverted question mark, ¿
  7: { key: "Digit7" },
  "/": { key: "Digit7", shift: true },
  "{": { key: "Digit7", altRight: true },
  8: { key: "Digit8" },
  "(": { key: "Digit8", shift: true },
  "[": { key: "Digit8", altRight: true },
  9: { key: "Digit9" },
  ")": { key: "Digit9", shift: true },
  "]": { key: "Digit9", altRight: true },
  0: { key: "Digit0" },
  "=": { key: "Digit0", shift: true },
  "}": { key: "Digit0", altRight: true },
  "ß": { key: "Minus" },
  "?": { key: "Minus", shift: true },
  "\\": { key: "Minus", altRight: true },
  "´": { key: "Equal", deadKey: true }, // accent acute, mark ´ placed above the letter
  "`": { key: "Equal", shift: true, deadKey: true },  // accent grave, mark ` placed above the letter
  "˙": { key: "Equal", control: true, altRight: true, deadKey: true }, // acute accent, mark ˙ placed above the letter
  "ü": { key: "BracketLeft" },
  "Ü": { key: "BracketLeft", shift: true },
  Escape: { key: "BracketLeft", control: true },
  "ʼ": { key: "BracketLeft", altRight: true }, // modifier letter apostrophe, ʼ
  "+": { key: "BracketRight" },
  "*": { key: "BracketRight", shift: true },
  Control: { key: "BracketRight", control: true },
  "~": { key: "BracketRight", altRight: true },
  "ö": { key: "Semicolon" },
  "Ö": { key: "Semicolon", shift: true },
  "ˌ": { key: "Semicolon", deadkey: true, altRight: true }, // modifier letter low vertical line, ˌ
  "ä": { key: "Quote" },
  "Ä": { key: "Quote", shift: true },
  "˗": { key: "Quote", deadKey: true, altRight: true }, // modifier letter minus sign, ˗
  "#": { key: "Backslash" },
  "'": { key: "Backslash", shift: true },
  "−": { key: "Backslash", altRight: true }, // minus sign, −
  ",": { key: "Comma" },
  ";": { key: "Comma", shift: true },
  "\u2011": { key: "Comma", altRight: true }, // non-breaking hyphen, ‑
  ".": { key: "Period" },
  ":": { key: "Period", shift: true },
  "·": { key: "Period", altRight: true }, // middle dot, ·
  "-": { key: "Slash" },
  "_": { key: "Slash", shift: true },
  "\u00ad": { key: "Slash", altRight: true }, // soft hyphen, ­
  " ": { key: "Space" },
  "\n": { key: "Enter" },
  Enter: { key: "Enter" },
  Tab: { key: "Tab" },
} as Record<string, KeyCombo>;

export const keyDisplayMap: Record<string, string> = {
  ...en_US.keyDisplayMap,
  // now override the English keyDisplayMap with German specific keys

  // Combination keys
  CtrlAltDelete: "Strg + Alt + Entf",
  CtrlAltBackspace: "Strg + Alt + ←",

  // German action keys
  AltLeft: "Alt",
  AltRight: "AltGr",
  Backspace: "Rücktaste",
  "(Backspace)": "Rücktaste",
  CapsLock: "Feststelltaste",
  Clear: "Entf",
  ControlLeft: "Strg",
  ControlRight: "Strg",
  Delete: "Entf",
  End: "Ende",
  Enter: "Eingabe",
  Escape: "Esc",
  Home: "Pos1",
  Insert: "Einfg",
  Menu: "Menü",
  MetaLeft: "Meta",
  MetaRight: "Meta",
  PageDown: "Bild ↓",
  PageUp: "Bild ↑",
  ShiftLeft: "Umschalt",
  ShiftRight: "Umschalt",

  // German umlauts and ß
  BracketLeft: "ü",
  "(BracketLeft)": "Ü",
  Semicolon: "ö",
  "(Semicolon)": "Ö",
  Quote: "ä",
  "(Quote)": "Ä",
  Minus: "ß",
  "(Minus)": "?",
  Equal: "´",
  "(Equal)": "`",
  Backslash: "#",
  "(Backslash)": "'",

  // Shifted Numbers
  "(Digit2)": "\"",
  "(Digit3)": "§",
  "(Digit6)": "&",
  "(Digit7)": "/",
  "(Digit8)": "(",
  "(Digit9)": ")",
  "(Digit0)": "=",

  // Additional German symbols
  Backquote: "^",
  "(Backquote)": "°",
  Comma: ",",
  "(Comma)": ";",
  Period: ".",
  "(Period)": ":",
  Slash: "-",
  "(Slash)": "_",

  // Numpad
  NumpadDecimal: "Num ,",
  NumpadEnter: "Num Eingabe",
  NumpadInsert: "Einfg",
  NumpadDelete: "Entf",

  // Modals
  PrintScreen: "Druck",
  ScrollLock: "Rollen",
  "(Pause)": "Unterbr",
}

export const modifierDisplayMap: Record<string, string> = {
  ShiftLeft: "Umschalt (links)",
  ShiftRight: "Umschalt (rechts)",
  ControlLeft: "Strg (links)",
  ControlRight: "Strg (rechts)",
  AltLeft: "Alt",
  AltRight: "AltGr",
  MetaLeft: "Meta (links)",
  MetaRight: "Meta (rechts)",
  AltGr: "AltGr",
} as Record<string, string>;

export const virtualKeyboard = {
  main: {
    default: [
      "CtrlAltDelete AltMetaEscape CtrlAltBackspace",
      "Escape  F1 F2 F3 F4  F5 F6 F7 F8  F9 F10 F11 F12",
      "Backquote Digit1 Digit2 Digit3 Digit4 Digit5 Digit6 Digit7 Digit8 Digit9 Digit0 Minus Equal Backspace",
      "Tab KeyQ KeyW KeyE KeyR KeyT KeyY KeyU KeyI KeyO KeyP BracketLeft BracketRight",
      "CapsLock KeyA KeyS KeyD KeyF KeyG KeyH KeyJ KeyK KeyL Semicolon Quote Backslash Enter",
      "ShiftLeft KeyZ KeyX KeyC KeyV KeyB KeyN KeyM Comma Period Slash ShiftRight",
      "ControlLeft MetaLeft AltLeft Space AltGr MetaRight Menu ControlRight",
    ],
    shift: [
      "CtrlAltDelete AltMetaEscape CtrlAltBackspace",
      "Escape F1 F2 F3 F4 F5 F6 F7 F8 F9 F10 F11 F12",
      "(Backquote) (Digit1) (Digit2) (Digit3) (Digit4) (Digit5) (Digit6) (Digit7) (Digit8) (Digit9) (Digit0) (Minus) (Equal) (Backspace)",
      "Tab (KeyQ) (KeyW) (KeyE) (KeyR) (KeyT) (KeyY) (KeyU) (KeyI) (KeyO) (KeyP) (BracketLeft) (BracketRight) (Backslash)",
      "CapsLock (KeyA) (KeyS) (KeyD) (KeyF) (KeyG) (KeyH) (KeyJ) (KeyK) (KeyL) (Semicolon) (Quote) Enter",
      "ShiftLeft (KeyZ) (KeyX) (KeyC) (KeyV) (KeyB) (KeyN) (KeyM) (Comma) (Period) (Slash) ShiftRight",
      "ControlLeft MetaLeft AltLeft Space AltGr MetaRight Menu ControlRight",
    ]
  },
  control: {
    default: [
      "PrintScreen ScrollLock Pause",
      "Insert Home PageUp",
      "Delete End PageDown"
    ],
    shift: [
      "(PrintScreen) ScrollLock (Pause)",
      "Insert Home PageUp",
      "Delete End PageDown"
    ],
  },

  arrows: {
    default: [
      " ArrowUp ",
      "ArrowLeft ArrowDown ArrowRight"],
  },

  numpad: {
    numlocked: [
      "NumLock NumpadDivide NumpadMultiply NumpadSubtract",
      "Numpad7 Numpad8 Numpad9 NumpadAdd",
      "Numpad4 Numpad5 Numpad6",
      "Numpad1 Numpad2 Numpad3 NumpadEnter",
      "Numpad0 NumpadDecimal",
    ],
    default: [
      "NumLock NumpadDivide NumpadMultiply NumpadSubtract",
      "Home ArrowUp PageUp NumpadAdd",
      "ArrowLeft Clear ArrowRight",
      "End ArrowDown PageDown NumpadEnter",
      "NumpadInsert NumpadDelete",
    ],
  }
}

export const de_DE: KeyboardLayout = {
  isoCode: isoCode,
  name: name,
  chars: chars,
  keyDisplayMap: keyDisplayMap,
  modifierDisplayMap: modifierDisplayMap,
  virtualKeyboard: virtualKeyboard
};