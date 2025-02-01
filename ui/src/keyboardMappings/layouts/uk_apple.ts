import { charsUS, keysUS, modifiersUS } from "./us";

// Extend US Keys with UK Apple-specific changes
export const keysUKApple = {
    ...keysUS,
} as Record<string, number>;
  
// Extend US Chars with UK Apple-specific changes
export const charsUKApple = {
    ...charsUS,
    "`": { key: "Backquote", shift: false },
    "~": { key: "Backquote", shift: true },
    "\\" : { key: "Backslash", shift: false },
    "|": { key: "Backslash", shift: true },
    "#": { key: "Digit3", shift: false, altLeft: true },
    "£": { key: "Digit3", shift: true },
    "@": { key: "Digit2", shift: true },
    "\"": { key: "Quote", shift: true },
} as Record<string, { key: string | number; shift: boolean; altLeft?: boolean; altRight?: boolean; }>;
  
// Modifiers are typically the same between UK and US layouts
export const modifiersUKApple = {
    ...modifiersUS,
};