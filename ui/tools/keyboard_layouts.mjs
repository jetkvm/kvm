// Loads the keyboard layouts and HID key codes from src/ outside the browser
// and finds mistakes that the UI would silently ignore when typing text.
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const uiRoot = fileURLToPath(new URL("..", import.meta.url));

export async function loadKeyboardLayouts() {
  const server = await createServer({
    root: uiRoot,
    configFile: false,
    logLevel: "error",
    appType: "custom",
    resolve: { alias: { "@": `${uiRoot}src` } },
    server: { middlewareMode: true, hmr: false, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
  });
  try {
    const { keyboards } = await server.ssrLoadModule("/src/keyboardLayouts.ts");
    const { keys } = await server.ssrLoadModule("/src/keyboardMappings.ts");
    return { keyboards, keys };
  } finally {
    await server.close();
  }
}

// Text is typed one code point at a time, so entries named after a key, such
// as "Enter" or "PrintScreen", are never looked up.
export function isTypeable(char) {
  const normalized = char.normalize("NFC");
  return String.fromCodePoint(normalized.codePointAt(0) ?? 0) === normalized;
}

const infoFields = new Set(["key", "shift", "altRight"]);
const comboFields = new Set([...infoFields, "deadKey", "accentKey"]);

// Returns one message per problem: a duplicate layout id, a key name that has
// no HID code, or a misspelled flag such as "AltGr" instead of "altRight".
export function checkKeyboardLayouts(keyboards, keys) {
  const errors = [];
  const isoCodes = new Set();
  for (const layout of keyboards) {
    if (isoCodes.has(layout.isoCode)) {
      errors.push(`${layout.isoCode}: duplicate layout id`);
    }
    isoCodes.add(layout.isoCode);

    for (const [char, combo] of Object.entries(layout.chars)) {
      if (!isTypeable(char)) continue;
      const where = `${layout.isoCode}: ${JSON.stringify(char)}`;
      for (const [info, allowed] of [
        [combo, comboFields],
        [combo.accentKey, infoFields],
      ]) {
        for (const field of Object.keys(info ?? {})) {
          if (!allowed.has(field)) errors.push(`${where} has unknown field ${field}`);
        }
      }
      for (const info of [combo, combo.accentKey].filter(Boolean)) {
        if (!(String(info.key) in keys)) errors.push(`${where} uses unknown key ${info.key}`);
      }
    }
  }
  return errors;
}
