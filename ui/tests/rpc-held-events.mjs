// Regression for device events sent the moment the rpc channel opens, before
// the route's useJsonRpc effect has attached its listener.
// Run from ui/: node tests/rpc-held-events.mjs
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { rolldown } from "rolldown";
import { chromium } from "playwright";
const entry = fileURLToPath(new URL("./rpc-held-events-entry.ts", import.meta.url));
const bundle = await rolldown({
  input: entry,
  transform: { define: { "process.env.NODE_ENV": '"production"' } },
  plugins: [
    {
      name: "test-rpc-store",
      resolveId(source) {
        if (source === entry) return entry;
        if (source === "@hooks/stores") return "\0rpc-store";
      },
      load(id) {
        if (id === "\0rpc-store")
          return `export const useRTCStore = () => window.testStore;
            export const useFailsafeModeStore = () => ({ isFailsafeMode: false, reason: null });`;
        if (id === entry)
          return `
          import { createElement } from 'react';
          import { createRoot } from 'react-dom/client';
          import { flushSync } from 'react-dom';
          import { holdRpcEvents, useJsonRpc } from '../src/hooks/useJsonRpc';
          window.holdRpcEvents = holdRpcEvents;
          const root = createRoot(document.getElementById('root'));
          function Subscriber({ name, receiveHeldEvents }) {
            useJsonRpc(event => window.received.push(name + ":" + event.method), { receiveHeldEvents });
            return null;
          }
          window.render = subscribers => flushSync(() =>
            root.render(subscribers.map(s => createElement(Subscriber, { key: s.name, ...s }))));
        `;
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
  const received = await page.evaluate(() => {
    window.received = [];
    const channel = new EventTarget();
    channel.readyState = "open";
    channel.send = () => {};
    const event = method =>
      channel.dispatchEvent(
        new MessageEvent("message", {
          data: JSON.stringify({ jsonrpc: "2.0", method, params: {} }),
        }),
      );
    // The route creates the channel and holds its events.
    window.holdRpcEvents(channel);
    // The device sends its connect-time events before anything subscribes.
    event("localVersion");
    event("deviceCapabilities");
    window.testStore = { rpcDataChannel: channel };
    // A subscriber without receiveHeldEvents (like ATXPowerControl) does not
    // take them; the route's subscriber does, in order.
    window.render([{ name: "atx" }, { name: "route", receiveHeldEvents: true }]);
    // Later events reach every subscriber once; nothing is replayed again.
    event("usbState");
    window.render([{ name: "atx" }, { name: "route", receiveHeldEvents: true }]);
    return window.received;
  });
  assert.deepEqual(received, [
    "route:localVersion",
    "route:deviceCapabilities",
    "atx:usbState",
    "route:usbState",
  ]);
  console.log("rpc events sent before the route subscribes: OK");
} finally {
  await browser.close();
}
