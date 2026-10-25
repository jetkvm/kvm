#!/usr/bin/env node
// Generates keyboard layouts for macOS hosts from the layout data macOS ships,
// so text pasted to a Mac comes out right. The PC layouts in src/ do not fit a
// Mac: macOS puts most symbols on Option with its own assignments (@ is
// Option+L on a German Mac, not AltGr+Q).
//
// Runs on macOS only: tools/macos/dump_keylayout.swift reads each layout with
// UCKeyTranslate. The tables are generated for the ISO keyboard type, which is
// what a Mac with a European layout assigns to the JetKVM's USB keyboard; with
// it, the key codes for the key left of 1 (IntlBackslash) and next to the left
// Shift (Backquote) map as on an Apple ISO keyboard. U.S. English uses ANSI.
//
// usage: npm run keyboard:generate-macos [isoCode ...]
import { execFileSync } from "node:child_process";
import { writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

import { loadUiModules } from "./keyboard_layouts.mjs";

const uiRoot = fileURLToPath(new URL("..", import.meta.url));
const dumpScript = fileURLToPath(new URL("macos/dump_keylayout.swift", import.meta.url));

const ISO_KEYBOARD = 43;
const ANSI_KEYBOARD = 40;

// The PC layout each macOS layout replaces, and the input source macOS selects
// by default for that language. Japanese is missing: on a Mac it is an input
// method, not a keyboard layout.
const layouts = [
  { base: "cs_CZ", isoCode: "cs-CZ", source: "com.apple.keylayout.Czech" },
  { base: "da_DK", isoCode: "da-DK", source: "com.apple.keylayout.Danish" },
  { base: "de_CH", isoCode: "de-CH", source: "com.apple.keylayout.SwissGerman" },
  { base: "de_DE", isoCode: "de-DE", source: "com.apple.keylayout.German" },
  { base: "en_UK", isoCode: "en-UK", source: "com.apple.keylayout.British" },
  { base: "en_US", isoCode: "en-US", source: "com.apple.keylayout.US", ansi: true },
  { base: "es_ES", isoCode: "es-ES", source: "com.apple.keylayout.Spanish-ISO" },
  { base: "fr_BE", isoCode: "nl-BE", source: "com.apple.keylayout.Belgian" },
  { base: "fr_CH", isoCode: "fr-CH", source: "com.apple.keylayout.SwissFrench" },
  { base: "fr_FR", isoCode: "fr-FR", source: "com.apple.keylayout.French" },
  { base: "hu_HU", isoCode: "hu-HU", source: "com.apple.keylayout.Hungarian" },
  { base: "it_IT", isoCode: "it-IT", source: "com.apple.keylayout.Italian-Pro" },
  { base: "nb_NO", isoCode: "nb-NO", source: "com.apple.keylayout.Norwegian" },
  { base: "pl_PL", isoCode: "pl-PL", source: "com.apple.keylayout.PolishPro" },
  { base: "pt_PT", isoCode: "pt-PT", source: "com.apple.keylayout.Portuguese" },
  { base: "ru_RU", isoCode: "ru-RU", source: "com.apple.keylayout.Russian" },
  { base: "sl_SI", isoCode: "sl-SI", source: "com.apple.keylayout.Slovenian" },
  { base: "sv_SE", isoCode: "sv-SE", source: "com.apple.keylayout.Swedish-Pro" },
];

// HID key names and the macOS virtual key codes they arrive as.
const macKeyCodes = {
  KeyA: 0x00,
  KeyS: 0x01,
  KeyD: 0x02,
  KeyF: 0x03,
  KeyH: 0x04,
  KeyG: 0x05,
  KeyZ: 0x06,
  KeyX: 0x07,
  KeyC: 0x08,
  KeyV: 0x09,
  IntlBackslash: 0x0a,
  KeyB: 0x0b,
  KeyQ: 0x0c,
  KeyW: 0x0d,
  KeyE: 0x0e,
  KeyR: 0x0f,
  KeyY: 0x10,
  KeyT: 0x11,
  Digit1: 0x12,
  Digit2: 0x13,
  Digit3: 0x14,
  Digit4: 0x15,
  Digit6: 0x16,
  Digit5: 0x17,
  Equal: 0x18,
  Digit9: 0x19,
  Digit7: 0x1a,
  Minus: 0x1b,
  Digit8: 0x1c,
  Digit0: 0x1d,
  BracketRight: 0x1e,
  KeyO: 0x1f,
  KeyU: 0x20,
  BracketLeft: 0x21,
  KeyI: 0x22,
  KeyP: 0x23,
  KeyL: 0x25,
  KeyJ: 0x26,
  Quote: 0x27,
  KeyK: 0x28,
  Semicolon: 0x29,
  Backslash: 0x2a,
  Comma: 0x2b,
  Slash: 0x2c,
  KeyN: 0x2d,
  KeyM: 0x2e,
  Period: 0x2f,
  Backquote: 0x32,
};
const keyOrder = Object.keys(macKeyCodes);

// Modifier sets from simplest to hardest to type: none, Shift, Option, both.
const modifierOrder = ["", "S", "A", "SA"];
const modifierFlags = {
  "": {},
  S: { shift: true },
  A: { altRight: true },
  SA: { shift: true, altRight: true },
};

const accentNames = {
  "´": "keyAcute",
  "`": "keyGrave",
  "^": "keyHat",
  ˆ: "keyHat",
  "¨": "keyTrema",
  "~": "keyTilde",
  "˜": "keyTilde",
  ˇ: "keyCaron",
  "˘": "keyBreve",
  "˚": "keyRing",
  "°": "keyRing",
  "˙": "keyOverdot",
  "˛": "keyOgonek",
  "¸": "keyCedilla",
  "˝": "keyDoubleAcute",
};

function dumpLayout(source, keyboardType) {
  const keys = keyOrder.map(name => `${name}:${macKeyCodes[name]}`).join(",");
  const output = execFileSync("swift", [dumpScript, source, String(keyboardType), keys], {
    encoding: "utf-8",
    maxBuffer: 64 * 1024 * 1024,
  });
  return JSON.parse(output).keys;
}

// Space and Enter are added explicitly, so a key that also types a space
// (some layouts put one on Option) is left out.
function isPrintable(text) {
  return [...text].length === 1 && !/\p{C}/u.test(text) && text !== " ";
}

// Picks for every character the simplest way to type it: a key on its own,
// a dead key on its own (followed by Space), or a dead key and a base key.
function buildChars(entries) {
  const chars = new Map();
  const rank = new Map();
  const add = (char, combo, order) => {
    if (chars.has(char)) return;
    chars.set(char, combo);
    rank.set(char, order);
  };

  for (const [modIndex, mods] of modifierOrder.entries()) {
    for (const entry of entries) {
      if (entry.mods !== mods || entry.dead || !isPrintable(entry.out)) continue;
      add(entry.out, { key: entry.key, ...modifierFlags[mods] }, [
        keyOrder.indexOf(entry.key),
        modIndex,
        0,
      ]);
    }
  }

  const output = new Map(entries.map(e => [`${e.key}|${e.mods}`, e.out]));
  const accents = new Map();
  const deadKeys = entries
    .filter(e => e.dead)
    .sort((a, b) => modifierOrder.indexOf(a.mods) - modifierOrder.indexOf(b.mods));
  for (const entry of deadKeys) {
    const accent = entry.composed["Space|"];
    if (!isPrintable(accent) || accents.has(accent)) continue;
    const accentKey = { key: entry.key, ...modifierFlags[entry.mods] };
    accents.set(accent, accentKey);
    const order = [keyOrder.indexOf(entry.key), modifierOrder.indexOf(entry.mods), 0];
    add(accent, { ...accentKey, deadKey: true }, order);
    for (const [press, out] of Object.entries(entry.composed)) {
      const [key, mods] = press.split("|");
      if (key === "Space" || !isPrintable(out) || out === accent || out === output.get(press))
        continue;
      add(out, { key, ...modifierFlags[mods], accent }, [
        keyOrder.indexOf(key),
        modifierOrder.indexOf(mods),
        1,
      ]);
    }
  }

  const sorted = [...chars.keys()].sort((a, b) => {
    const [x, y] = [rank.get(a), rank.get(b)];
    return x[0] - y[0] || x[1] - y[1] || x[2] - y[2];
  });
  return { chars: sorted.map(c => [c, chars.get(c)]), accents };
}

// Key labels for the on-screen keyboard: "Key" without and "(Key)" with Shift.
function keyLabels(entries) {
  const labels = [];
  for (const entry of entries) {
    if (entry.mods !== "" && entry.mods !== "S") continue;
    const text = entry.dead ? entry.composed["Space|"] : entry.out;
    if (!isPrintable(text)) continue;
    labels.push([entry.mods === "S" ? `(${entry.key})` : entry.key, text]);
  }
  return labels;
}

function literal(text) {
  if (text.length === 1 && /[\p{C}\p{Z}\p{M}]/u.test(text) && text !== " ") {
    return `"\\u${text.charCodeAt(0).toString(16).padStart(4, "0")}"`;
  }
  return JSON.stringify(text);
}

// Names the dead key constants; two accents that look alike, such as ~ and ˜,
// get the code point appended to keep the names apart.
function accentNamer(accents) {
  const names = new Map();
  for (const accent of accents) {
    const hex = accent.codePointAt(0).toString(16).toUpperCase();
    let name = accentNames[accent] ?? `keyDead${hex}`;
    if ([...names.values()].includes(name)) name = `${name}${hex}`;
    names.set(accent, name);
  }
  return accent => names.get(accent);
}

function combo(c, accentName) {
  const parts = [`key: "${c.key}"`];
  if (c.shift) parts.push("shift: true");
  if (c.altRight) parts.push("altRight: true");
  if (c.deadKey) parts.push("deadKey: true");
  if (c.accent) parts.push(`accentKey: ${accentName(c.accent)}`);
  return `{ ${parts.join(", ")} }`;
}

function macName(name) {
  return name.endsWith(")") ? `${name.slice(0, -1)}, macOS)` : `${name} (macOS)`;
}

function render(layout, baseLayout, entries) {
  const { chars, accents } = buildChars(entries);
  const name = `${layout.base}_macos`;
  const lines = [
    "// Generated by tools/generate_macos_layouts.mjs from the macOS keyboard layout",
    `// ${layout.source}. Do not edit by hand; run npm run keyboard:generate-macos on a Mac.`,
    'import { KeyboardLayout, KeyCombo } from "../keyboardLayouts";',
    "",
    `import { ${layout.base} } from "./${layout.base}";`,
    "",
    `const name = ${JSON.stringify(macName(baseLayout.name))};`,
    `const isoCode = "${layout.isoCode}-macos";`,
    "",
  ];
  // Only dead keys that compose a character needed elsewhere get a constant.
  const used = new Set(chars.map(([, c]) => c.accent).filter(Boolean));
  const accentName = accentNamer([...accents.keys()].filter(a => used.has(a)));
  for (const [accent, key] of accents) {
    if (used.has(accent)) {
      lines.push(`const ${accentName(accent)}: KeyCombo = ${combo(key, accentName)}; // ${accent}`);
    }
  }
  lines.push("", "const chars = {");
  for (const [char, c] of chars) lines.push(`  ${literal(char)}: ${combo(c, accentName)},`);
  lines.push(
    '  " ": { key: "Space" },',
    '  "\\n": { key: "Enter" },',
    '  Enter: { key: "Enter" },',
    '  Tab: { key: "Tab" },',
    "} as Record<string, KeyCombo>;",
    "",
    "const keyDisplayMap: Record<string, string> = {",
    `  ...${layout.base}.keyDisplayMap,`,
  );
  for (const [key, text] of keyLabels(entries)) lines.push(`  ${literal(key)}: ${literal(text)},`);
  lines.push(
    '  AltLeft: "⌥ Option",',
    '  AltRight: "⌥ Option",',
    '  AltGr: "⌥ Option",',
    '  MetaLeft: "⌘ Command",',
    '  MetaRight: "⌘ Command",',
    '  ControlLeft: "⌃ Control",',
    '  ControlRight: "⌃ Control",',
    "};",
    "",
    "const modifierDisplayMap: Record<string, string> = {",
    `  ...${layout.base}.modifierDisplayMap,`,
    '  ControlLeft: "⌃ Control (left)",',
    '  ControlRight: "⌃ Control (right)",',
    '  AltLeft: "⌥ Option (left)",',
    '  AltRight: "⌥ Option (right)",',
    '  AltGr: "⌥ Option (right)",',
    '  MetaLeft: "⌘ Command (left)",',
    '  MetaRight: "⌘ Command (right)",',
    "};",
    "",
  );
  if (layout.ansi) {
    lines.push("const fixRow = (row: string) => row;");
  } else {
    lines.push(
      "// On an Apple ISO keyboard the key left of 1 sends IntlBackslash and the key",
      "// next to the left Shift sends Backquote.",
      "const fixRow = (row: string) =>",
      "  row",
      '    .replace(/^(\\(?)Backquote(\\)?) /, "$1IntlBackslash$2 ")',
      '    .replace(/^ShiftLeft (\\(?)KeyZ/, "ShiftLeft $1Backquote$2 $1KeyZ");',
    );
  }
  lines.push(
    'const macBottomRow = "ControlLeft AltLeft MetaLeft Space MetaRight AltRight";',
    "",
    "const virtualKeyboard = {",
    `  ...${layout.base}.virtualKeyboard,`,
    "  main: {",
    `    default: [...${layout.base}.virtualKeyboard.main.default.slice(0, -1).map(fixRow), macBottomRow],`,
    `    shift: [...${layout.base}.virtualKeyboard.main.shift.slice(0, -1).map(fixRow), macBottomRow],`,
    "  },",
    "};",
    "",
    `export const ${name}: KeyboardLayout = {`,
    "  isoCode: isoCode,",
    "  name: name,",
    "  chars: chars,",
    "  keyDisplayMap: keyDisplayMap,",
    "  modifierDisplayMap: modifierDisplayMap,",
    "  virtualKeyboard: virtualKeyboard,",
    "};",
    "",
  );
  return { file: `src/keyboardLayouts/${name}.ts`, text: lines.join("\n"), count: chars.length };
}

if (process.platform !== "darwin") {
  console.error("This script reads the macOS keyboard layouts and must run on a Mac.");
  process.exit(1);
}

const wanted = new Set(process.argv.slice(2));
const selected = layouts.filter(l => wanted.size === 0 || wanted.has(l.isoCode));
// Load only the PC layouts, not keyboardLayouts.ts, which also imports the
// generated files.
const baseModules = await loadUiModules(selected.map(l => `/src/keyboardLayouts/${l.base}.ts`));
const written = [];
for (const [i, layout] of selected.entries()) {
  const baseLayout = baseModules[i][layout.base];
  if (baseLayout?.isoCode !== layout.isoCode) {
    throw new Error(`src/keyboardLayouts/${layout.base}.ts does not export ${layout.isoCode}`);
  }
  const entries = dumpLayout(layout.source, layout.ansi ? ANSI_KEYBOARD : ISO_KEYBOARD);
  const { file, text, count } = render(layout, baseLayout, entries);
  await writeFile(`${uiRoot}${file}`, text);
  written.push(`${uiRoot}${file}`);
  console.log(`${layout.isoCode}-macos: ${count} characters from ${layout.source}`);
}
execFileSync(`${uiRoot}node_modules/.bin/oxfmt`, written, { stdio: "inherit" });
