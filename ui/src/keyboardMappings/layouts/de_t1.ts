import { charsUS, keysUS, modifiersUS } from "./us";

export const keysDE_T1 = {
  ...keysUS,
} as Record<string, number>;

export const charsDE_T1 = {
  ...charsUS,

  "y": { key: "KeyZ", shift: false },
  "Y": { key: "KeyZ", shift: true },
  "z": { key: "KeyY", shift: false },
  "Z": { key: "KeyY", shift: true },

  "ä": { key: "Quote", shift: false },
  "Ä": { key: "Quote", shift: true },
  "ö": { key: "Semicolon", shift: false },
  "Ö": { key: "Semicolon", shift: true },
  "ü": { key: "BracketLeft", shift: false },
  "Ü": { key: "BracketLeft", shift: true },
  "ß": { key: "Minus", shift: false },
  "?": { key: "Minus", shift: true },

  "§": { key: "Digit3", shift: true },
  "°": { key: "Backquote", shift: true },

  "@": { key: "KeyQ", shift: false, altRight: true },
  "\"": { key: "Digit2", shift: true },

  "#": { key: "Backslash", shift: false },
  "'": { key: "Backslash", shift: true },

  ".": { key: "Period", shift: false },
  ":": { key: "Period", shift: true },
  ",": { key: "Comma", shift: false },
  ";": { key: "Comma", shift: true },

  "-": { key: "Slash", shift: false },
  "_": { key: "Slash", shift: true },

  "*": { key: "BracketRight", shift: true },
  "+": { key: "BracketRight", shift: false },
  "=": { key: "Digit0", shift: true },
  "~": { key: "BracketRight", shift: false, altRight: true },
  "{": { key: "Digit7", shift: false, altRight: true },
  "}": { key: "Digit0", shift: false, altRight: true },
  "[": { key: "Digit8", shift: false, altRight: true },
  "]": { key: "Digit9", shift: false, altRight: true },

  "\\": { key: "Minus", shift: false, altRight: true },
  "|": { key: "IntlBackslash", shift: true, altRight: true },

  "<": { key: "IntlBackslash", shift: false },
  ">": { key: "IntlBackslash", shift: true },

  "^": {key: "Backquote", shift: false},

  "€": { key: "KeyE", shift: false, altRight: true },

  "²": {key: "Digit2", shift: false, altRight: true },
  "³": {key: "Digit3", shift: false, altRight: true },

  "μ": {key: "KeyM", shift: false, altRight: true },

} as Record<string, { key: string; shift: boolean; altLeft?: boolean; altRight?: boolean }>;

export const modifiersDE_T1 = {
  ...modifiersUS,
} as Record<string, number>;