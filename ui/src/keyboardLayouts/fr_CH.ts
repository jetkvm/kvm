import { KeyboardLayout, KeyCombo } from "../keyboardLayouts"

import { de_CH } from "./de_CH"

const name = "Français de Suisse";

const chars = {
  ...de_CH.chars,
  "è": { key: "BracketLeft" },
  "ü": { key: "BracketLeft", shift: true },
  "é": { key: "Semicolon" },
  "ö": { key: "Semicolon", shift: true },
  "à": { key: "Quote" },
  "ä": { key: "Quote", shift: true },
} as Record<string, KeyCombo>;

export const fr_CH: KeyboardLayout = {
  isoCode: "fr-CH",
  name: name,
  chars: chars
};
