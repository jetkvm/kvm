export interface KeyStroke { modifier: number; keys: number[]; }
export interface KeyInfo { key: string | number; shift?: boolean, altRight?: boolean }
export interface KeyCombo extends KeyInfo { deadKey?: boolean, accentKey?: KeyInfo }
export interface KeyboardLayout {
  isoCode: string;
  name: string;
  chars: Record<string, KeyCombo>;
  modifierDisplayMap: Record<string, string>;
  keyDisplayMap: Record<string, string>;
  virtualKeyboard: {
    main: { default: string[], shift: string[] },
    control?: { default: string[], shift?: string[] },
    arrows?: { default: string[] }
  };
}

// To add a new layout, create a file like the above and add it to the list
import { cs_CZ } from "@/keyboardLayouts/cs_CZ"
import { de_CH } from "@/keyboardLayouts/de_CH"
import { de_DE } from "@/keyboardLayouts/de_DE"
import { en_US } from "@/keyboardLayouts/en_US"
import { en_UK } from "@/keyboardLayouts/en_UK"
import { es_ES } from "@/keyboardLayouts/es_ES"
import { fr_BE } from "@/keyboardLayouts/fr_BE"
import { fr_CH } from "@/keyboardLayouts/fr_CH"
import { fr_FR } from "@/keyboardLayouts/fr_FR"
import { it_IT } from "@/keyboardLayouts/it_IT"
import { nb_NO } from "@/keyboardLayouts/nb_NO"
import { sv_SE } from "@/keyboardLayouts/sv_SE"

export const keyboards: KeyboardLayout[] = [ cs_CZ, de_CH, de_DE, en_UK, en_US, es_ES, fr_BE, fr_CH, fr_FR, it_IT, nb_NO, sv_SE ];
