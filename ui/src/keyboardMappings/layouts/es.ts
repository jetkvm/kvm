import { charsUS, keysUS, modifiersUS } from "./us";

export const keysES = {
  ...keysUS,
} as Record<string, number>;

export const charsES = {
  ...charsUS,

  "ñ": { key: "Semicolon", shift: false },
  "Ñ": { key: "Semicolon", shift: true },

  "º": { key: "Backquote", shift: false },
  "ª": { key: "Backquote", shift: true },

  "¡": { key: "Equals", shift: false},

  "¿": { key: "Slash", shift: false, altRight: true },
  "?": { key: "Slash", shift: true },

  "|": { key: "Digit1", shift: false, altRight: true },

  "@": { key: "Digit2", shift: false, altRight: true },
  "\"": { key: "Digit2", shift: true },

  "·": { key: "Digit3", shift: false, altRight: true },
  "#": { key: "Digit3", shift: true },

  "$": { key: "Digit4", shift: true },
  "€": { key: "Digit5", shift: false, altRight: true },

  "&": { key: "Digit6", shift: true },

  "/": { key: "Digit7", shift: true },
  "(": { key: "Digit8", shift: true },
  ")": { key: "Digit9", shift: true },
  "=": { key: "Digit0", shift: true },

  "'": { key: "Quote", shift: false },
  "?": { key: "Quote", shift: true },

  "-": { key: "Minus", shift: false },
  "_": { key: "Minus", shift: true },

  "`": { key: "IntlBackslash", shift: false },
  "^": { key: "IntlBackslash", shift: true },
  "[": { key: "IntlBackslash", shift: false, altRight: true },
  "{": { key: "IntlBackslash", shift: true, altRight: true },

  "+": { key: "Equal", shift: true },
  "]": { key: "Equal", shift: false, altRight: true },
  "}": { key: "Equal", shift: true, altRight: true },

  "<": { key: "Backslash", shift: false },
  ">": { key: "Backslash", shift: true },
  

  ",": { key: "Comma", shift: false },
  ";": { key: "Comma", shift: true },

  ".": { key: "Period", shift: false },
  ":": { key: "Period", shift: true },

} as Record<string, { key: string; shift: boolean; altLeft?: boolean; altRight?: boolean }>;

export const modifiersES = {
  ...modifiersUS,
} as Record<string, number>;