import { chars as chars_fr_BE, name as name_fr_BE } from "@/keyboardLayouts/fr_BE"
import { chars as chars_cs_CZ, name as name_cs_CZ } from "@/keyboardLayouts/cs_CZ"
import { chars as chars_en_UK, name as name_en_UK } from "@/keyboardLayouts/en_UK"
import { chars as chars_en_US, name as name_en_US } from "@/keyboardLayouts/en_US"
import { chars as chars_fr_FR, name as name_fr_FR } from "@/keyboardLayouts/fr_FR"
import { chars as chars_de_DE, name as name_de_DE } from "@/keyboardLayouts/de_DE"
import { chars as chars_it_IT, name as name_it_IT } from "@/keyboardLayouts/it_IT"
import { chars as chars_nb_NO, name as name_nb_NO } from "@/keyboardLayouts/nb_NO"
import { chars as chars_es_ES, name as name_es_ES } from "@/keyboardLayouts/es_ES"
import { chars as chars_sv_SE, name as name_sv_SE } from "@/keyboardLayouts/sv_SE"
import { chars as chars_fr_CH, name as name_fr_CH } from "@/keyboardLayouts/fr_CH"
import { chars as chars_de_CH, name as name_de_CH } from "@/keyboardLayouts/de_CH"

interface KeyInfo { key: string | number; shift?: boolean, altRight?: boolean }
export type KeyCombo = KeyInfo & { deadKey?: boolean, accentKey?: KeyInfo }

export const layouts: Record<string, string> = {
  be_FR: name_fr_BE,
  cs_CZ: name_cs_CZ,
  en_UK: name_en_UK,
  en_US: name_en_US,
  fr_FR: name_fr_FR,
  de_DE: name_de_DE,
  it_IT: name_it_IT,
  nb_NO: name_nb_NO,
  es_ES: name_es_ES,
  sv_SE: name_sv_SE,
  fr_CH: name_fr_CH,
  de_CH: name_de_CH,
}

export const chars: Record<string, Record<string, KeyCombo>> = {
  be_FR: chars_fr_BE,
  cs_CZ: chars_cs_CZ,
  en_UK: chars_en_UK,
  en_US: chars_en_US,
  fr_FR: chars_fr_FR,
  de_DE: chars_de_DE,
  it_IT: chars_it_IT,
  nb_NO: chars_nb_NO,
  es_ES: chars_es_ES,
  sv_SE: chars_sv_SE,
  fr_CH: chars_fr_CH,
  de_CH: chars_de_CH,
};
