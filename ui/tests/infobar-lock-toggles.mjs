import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { rolldown } from "rolldown";
import { chromium } from "playwright";

const src = fileURLToPath(new URL("../src/", import.meta.url));
const entry = fileURLToPath(new URL("./infobar-lock-toggles-entry.ts", import.meta.url));
const bundle = await rolldown({
  input: entry,
  transform: { define: { "process.env.NODE_ENV": '"production"' } },
  plugins: [
    {
      name: "infobar-test-env",
      resolveId(source) {
        if (source === entry) return entry;
        if (["@hooks/stores", "@/hooks/stores", "./stores"].includes(source)) return "\0stores";
        if (source === "@/utils") return "\0utils";
        if (source === "@hooks/useHidRpc" || source === "@/hooks/useHidRpc") return "\0hidrpc";
        if (source === "@/hooks/useJsonRpc") return "\0jsonrpc";
        if (source === "@localizations/messages.js") return "\0messages";
        if (source === "@hooks/useKeyboard") return `${src}hooks/useKeyboard.ts`;
        if (source.startsWith("@/")) {
          const path = `${src}${source.slice(2)}`;
          return path.endsWith(".ts") ? path : `${path}.ts`;
        }
      },
      load(id) {
        if (id === "\0stores")
          return `
        const pick = (state, selector) => (selector ? selector(state) : state);
        export const useHidStore = selector => pick(window.hid, selector);
        export const useRTCStore = selector => pick(window.rtc, selector);
        export const useMouseStore = selector => pick({}, selector);
        export const useSettingsStore = selector => pick({ debugMode: false, showPressedKeys: false }, selector);
        export const useVideoStore = selector => pick({ width: 0, height: 0, clientWidth: 0, clientHeight: 0 }, selector);
        export const hidKeyBufferSize = 6;
        export const hidErrorRollOver = 1;
      `;
        if (id === "\0utils")
          return "export const sleep = ms => new Promise(r => setTimeout(r, ms));";
        if (id === "\0hidrpc")
          return `
        export function useHidRpc() {
          return {
            rpcHidStatus: "ready",
            rpcHidReady: window.rtc.hidReady,
            reportKeypressEvent: (key, press) => window.sent.push([key, press]),
            reportKeypressKeepAlive() {},
            reportKeyboardEvent() {},
            reportKeyboardMacroEvent() {},
            cancelOngoingKeyboardMacro() {},
          };
        }
      `;
        if (id === "\0jsonrpc")
          return `export const useJsonRpc = () => ({ send: (method, params) => window.sent.push([method, JSON.parse(JSON.stringify(params))]) });`;
        if (id === "\0messages")
          return `export const m = new Proxy({}, { get: (_, name) => () => ({ info_caps_lock: "Caps Lock", info_num_lock: "Num Lock", info_scroll_lock: "Scroll Lock" })[name] ?? String(name) });`;
        if (id === entry)
          return `
        import { createElement } from "react";
        import { createRoot } from "react-dom/client";
        import { flushSync } from "react-dom";
        import InfoBar from "../src/components/InfoBar";
        let root = null;
        window.mount = () => {
          root = createRoot(document.getElementById("root"));
          flushSync(() => root.render(createElement(InfoBar)));
        };
        window.unmount = () => flushSync(() => root.unmount());
      `;
      },
    },
  ],
});
const { output } = await bundle.generate({ format: "iife" });
await bundle.close();

const leds = {
  caps_lock: false,
  num_lock: false,
  scroll_lock: false,
  compose: false,
  kana: false,
  shift: false,
};
const sessions = {
  hidRpc: { hidReady: true, channel: "open", usbState: "configured" },
  jsonRpcOnly: { hidReady: false, channel: "open", usbState: "configured" },
  noSession: { hidReady: false, channel: null, usbState: "configured" },
  usbOff: { hidReady: true, channel: "open", usbState: "not attached" },
};
const locks = [
  ["Caps Lock", 0x39],
  ["Num Lock", 0x53],
  ["Scroll Lock", 0x47],
];

const browser = await chromium.launch({
  executablePath: process.env.CHROME_BIN,
  headless: true,
});
try {
  const page = await browser.newPage();
  page.on("pageerror", error => console.error(error));
  await page.clock.install();
  await page.setContent('<input id="video"><div id="root"></div>');
  await page.addScriptTag({ content: output[0].code });

  async function setup(session) {
    await page.evaluate(
      ({ session, leds }) => {
        window.sent = [];
        window.hid = {
          keyboardLedState: leds,
          keysDownState: { modifier: 0, keys: [0, 0, 0, 0, 0, 0] },
          usbState: session.usbState,
          isPasteInProgress: false,
          setKeysDownState() {},
          setKeyboardLedState() {},
          setPasteModeEnabled() {},
        };
        window.rtc = {
          hidReady: session.hidReady,
          rpcDataChannel: session.channel ? { readyState: session.channel } : null,
          rpcHidChannel: null,
          isTurnServerInUse: false,
          peerConnection: null,
          peerConnectionState: "connected",
        };
        window.mount();
        document.getElementById("video").focus();
      },
      { session: sessions[session], leds },
    );
  }

  for (const [name, code] of locks) {
    await setup("hidRpc");
    const toggle = page.getByRole("button", { name });
    await toggle.click();
    await page.clock.runFor(500);
    const after = await page.evaluate(() => ({
      sent: window.sent,
      focused: document.activeElement.id,
    }));
    assert.deepEqual(
      after.sent,
      [
        [code, true],
        [code, false],
      ],
      `${name}: one press and one release`,
    );
    assert.equal(after.focused, "video", `${name}: click keeps focus on the video`);
    assert.equal(await toggle.getAttribute("aria-pressed"), "false", `${name}: no optimistic flip`);
    await page.evaluate(() => window.unmount());
  }

  await setup("hidRpc");
  await page.getByRole("button", { name: "Caps Lock" }).click();
  await page.evaluate(() => window.unmount());
  await page.clock.runFor(500);
  assert.deepEqual(
    await page.evaluate(() => window.sent),
    [
      [0x39, true],
      [0x39, false],
    ],
    "release is sent after the info bar unmounts",
  );

  await setup("jsonRpcOnly");
  await page.getByRole("button", { name: "Num Lock" }).click();
  await page.clock.runFor(500);
  assert.deepEqual(
    (await page.evaluate(() => window.sent)).map(([, params]) => params),
    [
      { keys: [0x53, 0, 0, 0, 0, 0], modifier: 0 },
      { keys: [0, 0, 0, 0, 0, 0], modifier: 0 },
    ],
    "JSON-RPC fallback sends one press and one release",
  );
  await page.evaluate(() => window.unmount());

  for (const session of ["noSession", "usbOff"]) {
    await setup(session);
    for (const [name] of locks) {
      const toggle = page.getByRole("button", { name });
      assert.equal(await toggle.isDisabled(), true, `${session}: ${name} is disabled`);
      await toggle.click({ force: true });
    }
    await page.clock.runFor(500);
    assert.deepEqual(await page.evaluate(() => window.sent), [], `${session}: nothing is sent`);
    await page.evaluate(() => window.unmount());
  }

  console.log(
    "Info bar lock toggles: press and release, unmount, fallback, no session, USB off: OK",
  );
} finally {
  await browser.close();
}
