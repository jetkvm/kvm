#!/usr/bin/env node
// Fails when a keyboard layout in src/ refers to a key that does not exist or
// carries a misspelled flag. Both make pasted text come out wrong without any
// error in the UI.
import { checkKeyboardLayouts, loadKeyboardLayouts } from "./keyboard_layouts.mjs";

const { keyboards, keys } = await loadKeyboardLayouts();
const errors = checkKeyboardLayouts(keyboards, keys);
if (errors.length > 0) {
  console.error(errors.join("\n"));
  process.exit(1);
}
console.log(`${keyboards.length} keyboard layouts are valid`);
