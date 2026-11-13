// Regressions for the OS Services clipboard gestures: a reply that never
// arrives (the device drops a reply it cannot send) fails the gesture after
// the device's own deadline and the next gesture runs; text whose request
// the device would refuse is not sent.
// Run from ui/: node tests/guest-clipboard.mjs
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
const require = createRequire(new URL("../package.json", import.meta.url));
const ts = require("typescript");

// Loads a source file with each import taken from deps instead.
const deps = (globalThis.guestClipboardDeps = {});
async function load(path) {
  const source = readFileSync(new URL(path, import.meta.url), "utf8").replace(
    /^import (\w+)?,? ?(\{[^}]*\})? from "([^"]+)";$/gm,
    (_, name, names, from) =>
      `const ${(names ?? "{}").replace("{", `{ ${name ? `default: ${name},` : ""}`)} = globalThis.guestClipboardDeps["${from}"];`,
  );
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 },
  });
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);
}

class Channel extends EventTarget {
  readyState = "open";
  sent = [];
  answer = null;
  send(data) {
    const request = JSON.parse(data);
    this.sent.push(request);
    if (this.answer) {
      const reply = { jsonrpc: "2.0", id: request.id, result: this.answer };
      this.dispatchEvent(new MessageEvent("message", { data: JSON.stringify(reply) }));
    }
  }
}

const state = { channel: null, errors: [] };
const rtc = () => ({ rpcDataChannel: state.channel });
deps["react"] = { useCallback: fn => fn, useEffect: fn => void fn() };
deps["@hooks/stores"] = deps["@/hooks/stores"] = {
  useRTCStore: Object.assign(rtc, { getState: rtc }),
  useFailsafeModeStore: () => ({ isFailsafeMode: false, reason: null }),
  useCapability: () => true,
  useUiStore: () => ({ setOcrMode: () => {} }),
  useVideoStore: () => ({
    guestClipboard: { read: true, write: true, os: "linux" },
    setGuestClipboard: () => {},
    width: 1920,
    height: 1080,
  }),
};
deps["@/utils"] = { isMac: () => false, sleep: () => new Promise(setImmediate) };
deps["@hooks/useKeyboard"] = {
  default: () => ({ executeMacro: async () => {}, resetKeyboardState: async () => {} }),
};
deps["@components/useCopyToClipBoard"] = { useCopyToClipboard: () => ({ copy: async () => true }) };
deps["@/notifications"] = {
  default: { error: message => state.errors.push(message), success: () => {} },
};
deps["@localizations/messages.js"] = {
  m: new Proxy({}, { get: (_, key) => params => `${String(key)} ${JSON.stringify(params ?? {})}` }),
};
deps["@/utils/jsonrpc"] = await load("../src/utils/jsonrpc.ts");
deps["@hooks/useJsonRpc"] = await load("../src/hooks/useJsonRpc.ts");
const { default: useGuestClipboard } = await load("../src/hooks/useGuestClipboard.ts");

// Timers run at once, and the delays asked for are kept.
const realSetTimeout = globalThis.setTimeout;
const delays = [];
globalThis.setTimeout = (fn, ms) => {
  delays.push(ms);
  return realSetTimeout(fn, 0);
};
const settled = gesture =>
  Promise.race([gesture, new Promise(resolve => realSetTimeout(() => resolve("pending"), 1000))]);

// The device never answers: both gestures fail with an error, one after the
// other, and neither gives up before the device could still answer (4.5 s a
// request, one request queued ahead of it). The hook renders again with each
// channel, as in React.
state.channel = new Channel();
assert.equal(await settled(useGuestClipboard().pasteViaGuest("hello")), "failed");
assert.equal(await settled(useGuestClipboard().copyFromGuest()), undefined);
assert.equal(state.channel.sent.length, 2);
assert.equal(state.errors.length, 2);
assert(Math.min(...delays) >= 9000, `gave up after ${Math.min(...delays)} ms`);

// An answering device still gets the paste through.
state.channel.answer = { status: "ok" };
assert.equal(await settled(useGuestClipboard().pasteViaGuest("hello")), "pasted");

// Without a connection the gesture fails at once, not after a reconnect.
state.channel = null;
assert.equal(await settled(useGuestClipboard().pasteViaGuest("hello")), "failed");

// Text within the 65,528-byte clipboard limit whose JSON request would pass
// the device's 160 KiB (U+0001 escapes to six bytes) fails with the size
// error, unsent. 64 KB of CRLF line ends still goes: JSON doubles them, the
// most it grows ordinary text.
state.channel = new Channel();
state.channel.answer = { status: "ok" };
state.errors = [];
const controls = "\u0001".repeat(28000);
assert(JSON.stringify({ text: controls }).length > 160 * 1024);
assert.equal(await settled(useGuestClipboard().pasteViaGuest(controls)), "failed");
assert.equal(state.channel.sent.length, 0);
assert.match(state.errors.join(), /^clipboard_too_large/);
assert.equal(await settled(useGuestClipboard().pasteViaGuest("\r\n".repeat(32764))), "pasted");
console.log("Guest clipboard: PASS");
