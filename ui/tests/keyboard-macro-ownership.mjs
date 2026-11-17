import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { rolldown } from "rolldown";
import { chromium } from "playwright";

const entry = fileURLToPath(new URL("./keyboard-macro-entry.ts", import.meta.url));
const bundle = await rolldown({
  input: entry,
  transform: { define: { "process.env.NODE_ENV": '"production"' } },
  plugins: [
    {
      name: "keyboard-test-transport",
      resolveId(source) {
        if (source === entry) return entry;
        if (source === "@/hooks/useHidRpc") return "\0hid-transport";
        if (source === "@/hooks/stores" || source === "./stores") return "\0stores";
        if (source === "@/hooks/useJsonRpc") return "\0jsonrpc";
        if (source === "@/keyboardMappings") return "\0mappings";
        if (source === "@/utils") return "\0utils";
        if (source === "@/hooks/hidRpc")
          return fileURLToPath(new URL("../src/hooks/hidRpc.ts", import.meta.url));
      },
      load(id) {
        if (id === "\0stores")
          return `
        export const useRTCStore = () => window.store;
        export const useHidStore = () => ({ setPasteModeEnabled() {} });
        export const hidKeyBufferSize = 6;
        export const hidErrorRollOver = 1;
      `;
        if (id === "\0jsonrpc") return "export const useJsonRpc = () => ({send() {}});";
        if (id === "\0mappings")
          return "export const keys = {KeyA:4,KeyB:5}, modifiers = {}, hidKeyToModifierMask = {};";
        if (id === "\0utils")
          return "export const sleep = ms => new Promise(r => setTimeout(r, ms));";
        if (id === "\0hid-transport")
          return `
        import {useEffect} from 'react';
        export function useHidRpc(listener) {
          useEffect(() => { window.listeners.add(listener); return () => window.listeners.delete(listener); }, [listener]);
          return { rpcHidReady: true,
            reportKeyboardMacroEvent: steps => window.sent.push(steps),
            cancelOngoingKeyboardMacro: () => window.cancels++,
          };
        }
      `;
        if (id === entry)
          return `
        import {createElement} from 'react';
        import {createRoot} from 'react-dom/client';
        import {flushSync} from 'react-dom';
        import useKeyboard from '../src/hooks/useKeyboard';
        import {KeyboardMacroStateMessage} from '../src/hooks/hidRpc';
        const root = createRoot(document.getElementById('root'));
        function Hook({id}) { window.apis[id] = useKeyboard(); return null; }
        window.renderHooks = generation => flushSync(() => root.render([
          createElement(Hook, {id:0, key:'popover'+generation}),
          createElement(Hook, {id:1, key:'toolbar'}),
        ]));
        window.emitState = async state => {
          for (const listener of [...window.listeners]) {
            listener(new KeyboardMacroStateMessage(state, true));
            await Promise.resolve();
          }
        };
      `;
      },
    },
  ],
});
const { output } = await bundle.generate({ format: "iife" });
await bundle.close();
const browser = await chromium.launch({ executablePath: process.env.CHROME_BIN, headless: true });
try {
  const page = await browser.newPage();
  await page.setContent('<div id="root"></div>');
  await page.addScriptTag({ content: output[0].code });
  const result = await page.evaluate(async () => {
    class Channel extends EventTarget {
      readyState = "open";
      close() {
        this.readyState = "closed";
        this.dispatchEvent(new Event("close"));
      }
    }
    window.listeners = new Set();
    window.apis = [];
    window.sent = [];
    window.cancels = 0;
    window.store = { rpcHidChannel: new Channel() };
    window.renderHooks(0);
    const steps = (key, count) =>
      Array.from({ length: count }, () => ({ keys: [key], modifiers: [], delay: 1 }));
    const tick = () => new Promise(r => setTimeout(r, 0));
    const first = window.apis[0].executeMacro(steps("KeyA", 130));
    await tick();
    window.renderHooks(1);
    await window.apis[0].cancelExecuteMacro();
    const beforeStart = window.cancels;
    await window.emitState(true);
    const afterStart = window.cancels;
    await window.emitState(false);
    await first;
    const afterRemount = window.sent.length;

    const old = window.apis[0].executeMacro(steps("KeyA", 130));
    await tick();
    await window.emitState(true);
    const replacement = window.apis[1].executeMacro(steps("KeyB", 2));
    await tick();
    const beforeStop = window.sent.length;
    await window.emitState(false);
    await tick();
    const afterStop = window.sent.length;
    await window.emitState(true);
    await window.emitState(false);
    await Promise.all([old, replacement]);
    return {
      beforeStart,
      afterStart,
      afterRemount,
      beforeStop,
      afterStop,
      chunks: window.sent.map(chunk => chunk.length),
      keys: window.sent.map(chunk => chunk[0].keys[0]),
      cancels: window.cancels,
    };
  });
  assert.deepEqual(result, {
    beforeStart: 0,
    afterStart: 1,
    afterRemount: 1,
    beforeStop: 2,
    afterStop: 3,
    chunks: [128, 128, 4],
    keys: [4, 4, 5],
    cancels: 2,
  });
  await page.clock.install();
  await page.evaluate(async () => {
    window.timeoutRun = window.apis[0]
      .executeMacro([{ keys: ["KeyA"], modifiers: [], delay: 1 }])
      .catch(error => error.message);
    await Promise.resolve();
    await window.emitState(true);
    window.queuedRun = window.apis[1].executeMacro([{ keys: ["KeyB"], modifiers: [], delay: 1 }]);
  });
  await page.clock.runFor(2500);
  const timedOut = await page.evaluate(async () => ({
    error: await window.timeoutRun,
    queued: await window.queuedRun,
    channel: window.store.rpcHidChannel.readyState,
    chunks: window.sent.length,
  }));
  assert.equal(timedOut.channel, "closed");
  assert.equal(timedOut.chunks, 4);
  assert.match(timedOut.error, /Timed out/);
  console.log("Macro ownership across remount, replacement, early cancel, and timeout: OK");
} finally {
  await browser.close();
}
