// A paste is typed with the device's keyboard layout, and not at all while
// that layout could not be read: a host with a German layout must never get
// US-mapped keystrokes. Runs the real usePaste hook in a browser.
// Run from ui/: node tests/paste-layout-gate.mjs
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { rolldown } from "rolldown";
import { chromium } from "playwright";

const src = (p) => fileURLToPath(new URL(`../src/${p}`, import.meta.url));
const entry = "\0entry";
const mocked = ["@/hooks/useKeyboard", "@/hooks/useJsonRpc", "@/hooks/stores"];
const bundle = await rolldown({
  input: entry,
  transform: { define: { "process.env.NODE_ENV": '"production"' } },
  plugins: [
    {
      name: "paste-gate-test",
      resolveId(source, importer) {
        if (source === entry) return entry;
        if (mocked.includes(source)) return `\0mock:${source}`;
        if (
          source === "../keyboardLayouts" &&
          importer?.includes("keyboardLayouts/")
        )
          return src("keyboardLayouts.ts");
        if (source.startsWith("@/")) return src(`${source.slice(2)}.ts`);
      },
      load(id) {
        if (id === "\0mock:@/hooks/useKeyboard")
          return `export default () => ({
            executeMacro: steps => { window.macros.push(steps); return Promise.resolve(); },
            cancelExecuteMacro: () => Promise.resolve(),
          });`;
        if (id === "\0mock:@/hooks/useJsonRpc")
          return `export const useJsonRpc = () => ({
            send: (method, params, callback) => { window.sends.push({ method, callback }); },
          });`;
        if (id === "\0mock:@/hooks/stores")
          return `
            const settings = { keyboardLayout: "en-US", setKeyboardLayout: l => (settings.keyboardLayout = l) };
            export const useSettingsStore = sel => (sel ? sel(settings) : settings);
            useSettingsStore.getState = () => settings;
            export const useRTCStore = sel => (sel ? sel(window.rtc) : window.rtc);
            useRTCStore.getState = () => window.rtc;`;
        if (id === entry)
          return `
            import { createElement } from "react";
            import { createRoot } from "react-dom/client";
            import { flushSync } from "react-dom";
            import usePaste from "@/hooks/usePaste";
            function Pasting() { window.paste = usePaste(); return null; }
            window.mount = () => flushSync(() =>
              createRoot(document.getElementById("root")).render(createElement(Pasting)));`;
      },
    },
  ],
});
const { output } = await bundle.generate({ format: "iife" });
await bundle.close();

const browser = await chromium.launch({
  executablePath: process.env.CHROME_BIN,
  headless: true,
  args: ["--no-sandbox"],
});
try {
  const page = await browser.newPage();
  await page.setContent('<div id="root"></div>');
  await page.addScriptTag({ content: output[0].code });
  const result = await page.evaluate(async () => {
    const tick = () => new Promise((r) => setTimeout(r, 0));
    const until = async (check) => {
      for (let i = 0; i < 100 && !check(); i++) await tick();
    };
    window.macros = [];
    window.sends = [];
    const channel = new EventTarget();
    channel.readyState = "open";
    window.rtc = { rpcDataChannel: channel };
    window.mount();
    // Mounting asks for the layout once.
    await until(() => window.sends.length === 1);
    // The device cannot answer: the paste, which joined that request rather
    // than asking again, fails and types nothing.
    const first = window.paste.pasteText("zy");
    await tick();
    const askedBeforeFailure = window.sends.length;
    window.sends[0].callback({
      jsonrpc: "2.0",
      id: 1,
      error: { code: -32000, message: "no" },
    });
    let failure = null;
    await first.catch((e) => (failure = String(e)));
    const macrosAfterFailure = window.macros.length;
    // The next paste asks again; with the answer it types in that layout.
    const second = window.paste.pasteText("zy");
    await until(() => window.sends.length === 2);
    window.sends[1].callback({ jsonrpc: "2.0", id: 2, result: "de-DE" });
    const typed = await second;
    return {
      asked: window.sends.map((s) => s.method),
      askedBeforeFailure,
      failure,
      macrosAfterFailure,
      typed,
      keys: window.macros.flatMap((steps) => steps.map((step) => step.keys[0])),
    };
  });
  assert.deepEqual(result.asked, ["getKeyboardLayout", "getKeyboardLayout"]);
  assert.equal(result.askedBeforeFailure, 1);
  assert.match(result.failure ?? "", /keyboard layout/);
  assert.equal(result.macrosAfterFailure, 0);
  assert.equal(result.typed.via, "keystrokes");
  assert.deepEqual(result.keys, ["KeyY", "KeyZ"]);
  console.log(
    "Paste layout gate: a failed layout read types nothing, a German layout types German: OK",
  );
} finally {
  await browser.close();
}
