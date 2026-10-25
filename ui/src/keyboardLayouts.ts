export interface KeyStroke {
  modifier: number;
  keys: number[];
}
export interface KeyInfo {
  key: string | number;
  shift?: boolean;
  altRight?: boolean;
}
export interface KeyCombo extends KeyInfo {
  deadKey?: boolean;
  accentKey?: KeyInfo;
}
export interface KeyboardLayout {
  isoCode: string;
  name: string;
  chars: Record<string, KeyCombo>;
  modifierDisplayMap: Record<string, string>;
  keyDisplayMap: Record<string, string>;
  virtualKeyboard: {
    main: { default: string[]; shift: string[] };
    control?: { default: string[]; shift?: string[] };
    arrows?: { default: string[] };
  };
}

// To add a new layout, create a file like the above and add it to the list.
// Layouts describe the host's PC keyboard layout as Windows and Linux use it.
// A layout for another host system adds a variant subtag to the id and the
// system to the name and file: de-DE-macos, "Deutsch (macOS)", de_DE_macos.ts.
import { cs_CZ } from "@/keyboardLayouts/cs_CZ";
import { cs_CZ_macos } from "@/keyboardLayouts/cs_CZ_macos";
import { da_DK } from "@/keyboardLayouts/da_DK";
import { da_DK_macos } from "@/keyboardLayouts/da_DK_macos";
import { de_CH } from "@/keyboardLayouts/de_CH";
import { de_CH_macos } from "@/keyboardLayouts/de_CH_macos";
import { de_DE } from "@/keyboardLayouts/de_DE";
import { de_DE_macos } from "@/keyboardLayouts/de_DE_macos";
import { en_US } from "@/keyboardLayouts/en_US";
import { en_US_macos } from "@/keyboardLayouts/en_US_macos";
import { en_UK } from "@/keyboardLayouts/en_UK";
import { en_UK_macos } from "@/keyboardLayouts/en_UK_macos";
import { es_ES } from "@/keyboardLayouts/es_ES";
import { es_ES_macos } from "@/keyboardLayouts/es_ES_macos";
import { fr_BE } from "@/keyboardLayouts/fr_BE";
import { fr_BE_macos } from "@/keyboardLayouts/fr_BE_macos";
import { fr_CH } from "@/keyboardLayouts/fr_CH";
import { fr_CH_macos } from "@/keyboardLayouts/fr_CH_macos";
import { fr_FR } from "@/keyboardLayouts/fr_FR";
import { fr_FR_macos } from "@/keyboardLayouts/fr_FR_macos";
import { hu_HU } from "@/keyboardLayouts/hu_HU";
import { hu_HU_macos } from "@/keyboardLayouts/hu_HU_macos";
import { it_IT } from "@/keyboardLayouts/it_IT";
import { it_IT_macos } from "@/keyboardLayouts/it_IT_macos";
import { ja_JP } from "@/keyboardLayouts/ja_JP";
import { nb_NO } from "@/keyboardLayouts/nb_NO";
import { nb_NO_macos } from "@/keyboardLayouts/nb_NO_macos";
import { pl_PL } from "@/keyboardLayouts/pl_PL";
import { pl_PL_macos } from "@/keyboardLayouts/pl_PL_macos";
import { pt_PT } from "@/keyboardLayouts/pt_PT";
import { pt_PT_macos } from "@/keyboardLayouts/pt_PT_macos";
import { sv_SE } from "@/keyboardLayouts/sv_SE";
import { sv_SE_macos } from "@/keyboardLayouts/sv_SE_macos";
import { sl_SI } from "@/keyboardLayouts/sl_SI";
import { sl_SI_macos } from "@/keyboardLayouts/sl_SI_macos";
import { ru_RU } from "@/keyboardLayouts/ru_RU";
import { ru_RU_macos } from "@/keyboardLayouts/ru_RU_macos";

export const keyboards: KeyboardLayout[] = [
  cs_CZ,
  cs_CZ_macos,
  da_DK,
  da_DK_macos,
  de_CH,
  de_CH_macos,
  de_DE,
  de_DE_macos,
  en_UK,
  en_UK_macos,
  en_US,
  en_US_macos,
  es_ES,
  es_ES_macos,
  fr_BE,
  fr_BE_macos,
  fr_CH,
  fr_CH_macos,
  fr_FR,
  fr_FR_macos,
  hu_HU,
  hu_HU_macos,
  it_IT,
  it_IT_macos,
  ja_JP,
  nb_NO,
  nb_NO_macos,
  pl_PL,
  pl_PL_macos,
  pt_PT,
  pt_PT_macos,
  sv_SE,
  sv_SE_macos,
  sl_SI,
  sl_SI_macos,
  ru_RU,
  ru_RU_macos,
];
