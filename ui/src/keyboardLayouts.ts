import { chars as chars_en_US } from "@/keyboardLayouts/en_US"
import { chars as chars_fr_FR } from "@/keyboardLayouts/fr_FR"
import { chars as chars_de_DE } from "@/keyboardLayouts/de_DE"
import { chars as chars_fr_CH } from "@/keyboardLayouts/fr_CH"
import { chars as chars_de_CH } from "@/keyboardLayouts/de_CH"

type KeyInfo = { key: string | number; shift?: boolean, altRight?: boolean }
export type KeyCombo = KeyInfo & { deadKey?: boolean, accentKey?: KeyInfo }

export const layouts = {
  "en_US": "English (US)",
  "fr_FR": "French",
  "de_DE": "German",
  "fr_CH": "Swiss French",
  "de_CH": "Swiss German"
} as Record<string, string>;

export const chars = {
  "en_US": chars_en_US,
  "fr_FR": chars_fr_FR,
  "de_DE": chars_de_DE,
  "fr_CH": chars_fr_CH,
  "de_CH": chars_de_CH,
} as Record<string, Record <string, KeyCombo>>
