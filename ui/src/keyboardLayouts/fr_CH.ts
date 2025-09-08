import { KeyboardLayout, KeyCombo } from "../keyboardLayouts"

import { de_CH } from "./de_CH"

const name = "Français de Suisse";
const isoCode = "fr-CH";

const chars = {
  ...de_CH.chars,
  "è": { key: "BracketLeft" },
  "ü": { key: "BracketLeft", shift: true },
  "é": { key: "Semicolon" },
  "ö": { key: "Semicolon", shift: true },
  "à": { key: "Quote" },
  "ä": { key: "Quote", shift: true },
} as Record<string, KeyCombo>;

const keyDisplayMap = {
  ...de_CH.keyDisplayMap,
  "BracketLeft": "è",
  "BracketLeftShift": "ü",
  "Semicolon": "é",
  "SemicolonShift": "ö",
  "Quote": "à",
  "QuoteShift": "ä",
} as Record<string, string>;

export const fr_CH: KeyboardLayout = {
  isoCode: isoCode,
  name: name,
  chars: chars,
  keyDisplayMap: keyDisplayMap, 
  // TODO need to localize these maps and layouts
  modifierDisplayMap: de_CH.modifierDisplayMap,
  virtualKeyboard: de_CH.virtualKeyboard
};
