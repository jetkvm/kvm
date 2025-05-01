import { chars as chars_en_US } from "@/keyboardLayouts/en_US"
import { chars as chars_de_CH } from "@/keyboardLayouts/de_CH"

export const layouts = {
  "en_US": "English (US)",
  "de_CH": "Swiss German"
} as Record<string, string>;

export const chars = {
  "en_US": chars_en_US,
  "de_CH": chars_de_CH,
} as Record<string, Record<string, { key: string | number; shift?: boolean, altRight?: boolean, space?: boolean, capsLock?: boolean, trema?: boolean }>>;
