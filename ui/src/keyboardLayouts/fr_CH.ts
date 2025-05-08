import { KeyCombo } from "../keyboardLayouts"
import { chars as chars_de_CH } from "./de_CH"

export const name = "Français de Suisse";

export const chars = {
  ...chars_de_CH,
  "è": { key: "BracketLeft" },
  "ü": { key: "BracketLeft", shift: true },
  "é": { key: "Semicolon" },
  "ö": { key: "Semicolon", shift: true },
  "à": { key: "Quote" },
  "ä": { key: "Quote", shift: true },
} as Record<string, KeyCombo>;
