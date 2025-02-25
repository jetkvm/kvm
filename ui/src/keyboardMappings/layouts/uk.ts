import { charsUS, keysUS, modifiersUS } from "./us";

export const keysUK = {
    ...keysUS,
} as Record<string, number>;

export const charsUK = {
    ...charsUS,
    "`": { key: "Backquote", shift: false },
    "~": { key: "Backslash", shift: true },
    "\\": { key: "IntlBacklash", shift: false },
    "|": { key: "IntlBacklash", shift: true },
    "#": { key: "Backslash", shift: false },
    "£": { key: "Digit3", shift: true }, 
    "@": { key: "Quote", shift: true }, 
    "\"": { key: "Digit2", shift: true }, 
    "¬": { key: "Backquote", shift: true }, 
    "¦": { key: "Backquote", shift: false, altRight: true },
    "€": { key: "Digit4", shift: false, altRight: true },
} as Record<string, { key: string; shift: boolean; altLeft?: boolean; altRight?: boolean; }>;
  
export const modifiersUK = {
    ...modifiersUS,
} as Record<string, number>;