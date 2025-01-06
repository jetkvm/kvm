export const keys = {
  AltLeft: 0xe2,
  AltRight: 0xe6,
  ArrowDown: 0x51,
  ArrowLeft: 0x50,
  ArrowRight: 0x4f,
  ArrowUp: 0x52,
  Backquote: 0x35,
  Backslash: 0x31,
  Backspace: 0x2a,
  BracketLeft: 0x2f,
  BracketRight: 0x30,
  CapsLock: 0x39,
  Comma: 0x36,
  ContextMenu: 0,
  Delete: 0x4c,
  Digit0: 0x27,
  Digit1: 0x1e,
  Digit2: 0x1f,
  Digit3: 0x20,
  Digit4: 0x21,
  Digit5: 0x22,
  Digit6: 0x23,
  Digit7: 0x24,
  Digit8: 0x25,
  Digit9: 0x26,
  End: 0x4d,
  Enter: 0x28,
  Equal: 0x2e,
  Escape: 0x29,
  F1: 0x3a,
  F2: 0x3b,
  F3: 0x3c,
  F4: 0x3d,
  F5: 0x3e,
  F6: 0x3f,
  F7: 0x40,
  F8: 0x41,
  F9: 0x42,
  F10: 0x43,
  F11: 0x44,
  F12: 0x45,
  F13: 0x68,
  Home: 0x4a,
  Insert: 0x49,
  IntlBackslash: 0x31,
  KeyA: 0x04,
  KeyB: 0x05,
  KeyC: 0x06,
  KeyD: 0x07,
  KeyE: 0x08,
  KeyF: 0x09,
  KeyG: 0x0a,
  KeyH: 0x0b,
  KeyI: 0x0c,
  KeyJ: 0x0d,
  KeyK: 0x0e,
  KeyL: 0x0f,
  KeyM: 0x10,
  KeyN: 0x11,
  KeyO: 0x12,
  KeyP: 0x13,
  KeyQ: 0x14,
  KeyR: 0x15,
  KeyS: 0x16,
  KeyT: 0x17,
  KeyU: 0x18,
  KeyV: 0x19,
  KeyW: 0x1a,
  KeyX: 0x1b,
  KeyY: 0x1c,
  KeyZ: 0x1d,
  KeypadExclamation: 0xcf,
  Minus: 0x2d,
  NumLock: 0x53,
  Numpad0: 0x62,
  Numpad1: 0x59,
  Numpad2: 0x5a,
  Numpad3: 0x5b,
  Numpad4: 0x5c,
  Numpad5: 0x5d,
  Numpad6: 0x5e,
  Numpad7: 0x5f,
  Numpad8: 0x60,
  Numpad9: 0x61,
  NumpadAdd: 0x57,
  NumpadDivide: 0x54,
  NumpadEnter: 0x58,
  NumpadMultiply: 0x55,
  NumpadSubtract: 0x56,
  NumpadDecimal: 0x63,
  PageDown: 0x4e,
  PageUp: 0x4b,
  Period: 0x37,
  Quote: 0x34,
  Semicolon: 0x33,
  Slash: 0x38,
  Space: 0x2c,
  Tab: 0x2b,
} as Record<string, number>;

export const chars = {
  A: { key: "KeyA", shift: true },
  B: { key: "KeyB", shift: true },
  C: { key: "KeyC", shift: true },
  D: { key: "KeyD", shift: true },
  E: { key: "KeyE", shift: true },
  F: { key: "KeyF", shift: true },
  G: { key: "KeyG", shift: true },
  H: { key: "KeyH", shift: true },
  I: { key: "KeyI", shift: true },
  J: { key: "KeyJ", shift: true },
  K: { key: "KeyK", shift: true },
  L: { key: "KeyL", shift: true },
  M: { key: "KeyM", shift: true },
  N: { key: "KeyN", shift: true },
  O: { key: "KeyO", shift: true },
  P: { key: "KeyP", shift: true },
  Q: { key: "KeyQ", shift: true },
  R: { key: "KeyR", shift: true },
  S: { key: "KeyS", shift: true },
  T: { key: "KeyT", shift: true },
  U: { key: "KeyU", shift: true },
  V: { key: "KeyV", shift: true },
  W: { key: "KeyW", shift: true },
  X: { key: "KeyX", shift: true },
  Y: { key: "KeyY", shift: true },
  Z: { key: "KeyZ", shift: true },
  a: { key: "KeyA", shift: false },
  b: { key: "KeyB", shift: false },
  c: { key: "KeyC", shift: false },
  d: { key: "KeyD", shift: false },
  e: { key: "KeyE", shift: false },
  f: { key: "KeyF", shift: false },
  g: { key: "KeyG", shift: false },
  h: { key: "KeyH", shift: false },
  i: { key: "KeyI", shift: false },
  j: { key: "KeyJ", shift: false },
  k: { key: "KeyK", shift: false },
  l: { key: "KeyL", shift: false },
  m: { key: "KeyM", shift: false },
  n: { key: "KeyN", shift: false },
  o: { key: "KeyO", shift: false },
  p: { key: "KeyP", shift: false },
  q: { key: "KeyQ", shift: false },
  r: { key: "KeyR", shift: false },
  s: { key: "KeyS", shift: false },
  t: { key: "KeyT", shift: false },
  u: { key: "KeyU", shift: false },
  v: { key: "KeyV", shift: false },
  w: { key: "KeyW", shift: false },
  x: { key: "KeyX", shift: false },
  y: { key: "KeyY", shift: false },
  z: { key: "KeyZ", shift: false },
  1: { key: "Digit1", shift: false },
  "!": { key: "Digit1", shift: true },
  2: { key: "Digit2", shift: false },
  "@": { key: "Digit2", shift: true },
  3: { key: "Digit3", shift: false },
  "#": { key: "Digit3", shift: true },
  4: { key: "Digit4", shift: false },
  $: { key: "Digit4", shift: true },
  "%": { key: "Digit5", shift: true },
  5: { key: "Digit5", shift: false },
  "^": { key: "Digit6", shift: true },
  6: { key: "Digit6", shift: false },
  "&": { key: "Digit7", shift: true },
  7: { key: "Digit7", shift: false },
  "*": { key: "Digit8", shift: true },
  8: { key: "Digit8", shift: false },
  "(": { key: "Digit9", shift: true },
  9: { key: "Digit9", shift: false },
  ")": { key: "Digit0", shift: true },
  0: { key: "Digit0", shift: false },
  "-": { key: "Minus", shift: false },
  _: { key: "Minus", shift: true },
  "=": { key: "Equal", shift: false },
  "+": { key: "Equal", shift: true },
  "'": { key: "Quote", shift: false },
  '"': { key: "Quote", shift: true },
  ",": { key: "Comma", shift: false },
  "<": { key: "Comma", shift: true },
  "/": { key: "Slash", shift: false },
  "?": { key: "Slash", shift: true },
  ".": { key: "Period", shift: false },
  ">": { key: "Period", shift: true },
  ";": { key: "Semicolon", shift: false },
  ":": { key: "Semicolon", shift: true },
  "[": { key: "BracketLeft", shift: false },
  "{": { key: "BracketLeft", shift: true },
  "]": { key: "BracketRight", shift: false },
  "}": { key: "BracketRight", shift: true },
  "\\": { key: "Backslash", shift: false },
  "|": { key: "Backslash", shift: true },
  "`": { key: "Backquote", shift: false },
  "~": { key: "Backquote", shift: true },
  "§": { key: "IntlBackslash", shift: false },
  "±": { key: "IntlBackslash", shift: true },
  " ": { key: "Space", shift: false },
  "\n": { key: "Enter", shift: false },
  Enter: { key: "Enter", shift: false },
  Tab: { key: "Tab", shift: false },
} as Record<string, { key: string | number; shift: boolean }>;

export const modifiers = {
  ControlLeft: 0x01,
  ControlRight: 0x10,
  ShiftLeft: 0x02,
  ShiftRight: 0x20,
  AltLeft: 0x04,
  AltRight: 0x40,
  MetaLeft: 0x08,
  MetaRight: 0x80,
} as Record<string, number>;
