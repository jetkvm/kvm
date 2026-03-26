import { KeyboardLayout, KeyCombo } from "../keyboardLayouts";

import { en_US, chars as en_US_chars } from "./en_US";

const name = "Polski";
const isoCode = "pl-PL";

// Polish Programmer layout (kbdpl1): QWERTY + AltGr diacritics, no dead keys
const chars: Record<string, KeyCombo> = {
  ...en_US_chars,
  // lowercase diacritics (AltGr + letter)
  ą: { key: "KeyA", altRight: true },
  ć: { key: "KeyC", altRight: true },
  ę: { key: "KeyE", altRight: true },
  ł: { key: "KeyL", altRight: true },
  ń: { key: "KeyN", altRight: true },
  ó: { key: "KeyO", altRight: true },
  ś: { key: "KeyS", altRight: true },
  ż: { key: "KeyZ", altRight: true },
  ź: { key: "KeyX", altRight: true },
  // uppercase diacritics (Shift + AltGr + letter)
  Ą: { key: "KeyA", shift: true, altRight: true },
  Ć: { key: "KeyC", shift: true, altRight: true },
  Ę: { key: "KeyE", shift: true, altRight: true },
  Ł: { key: "KeyL", shift: true, altRight: true },
  Ń: { key: "KeyN", shift: true, altRight: true },
  Ó: { key: "KeyO", shift: true, altRight: true },
  Ś: { key: "KeyS", shift: true, altRight: true },
  Ż: { key: "KeyZ", shift: true, altRight: true },
  Ź: { key: "KeyX", shift: true, altRight: true },
};

export const pl_PL: KeyboardLayout = {
  isoCode: isoCode,
  name: name,
  chars: chars,
  keyDisplayMap: en_US.keyDisplayMap,
  modifierDisplayMap: en_US.modifierDisplayMap,
  virtualKeyboard: en_US.virtualKeyboard,
};
