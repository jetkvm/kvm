// Regression for held-button reconnects and reports sent before the handshake.
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { rolldown } from "rolldown";
import { chromium } from "playwright";
const entry = fileURLToPath(new URL("./hid-button-entry.ts", import.meta.url));
const bundle = await rolldown({
  input: entry, transform: { define: { "process.env.NODE_ENV": '"production"' } },
  plugins: [{
    name: "test-hid-store",
    resolveId(source) {
      if (source === entry) return entry;
      if (source === "@hooks/stores" || source === "./stores") return "\0hid-store";
    },
    load(id) {
      if (id === "\0hid-store") return "export const useRTCStore = () => window.testStore; export const hidKeyBufferSize = 6;";
      if (id === entry) return `
        import { createElement } from 'react';
        import { createRoot } from 'react-dom/client';
        import { flushSync } from 'react-dom';
        import { useHidRpc } from '../src/hooks/useHidRpc';
        const root = createRoot(document.getElementById('root'));
        function Harness() { window.hid = useHidRpc(); return null; }
        window.renderHid = () => flushSync(() => root.render(createElement(Harness)));
      `;
    },
  }],
});
const { output } = await bundle.generate({ format: "iife" });
await bundle.close();
const browser = await chromium.launch({
  executablePath: process.env.CHROME_BIN,
  headless: true, args: ["--no-sandbox"],
});
try {
  const page = await browser.newPage();
  await page.setContent('<div id="root"></div>');
  await page.addScriptTag({ content: output[0].code });
  const sent = await page.evaluate(() => {
    const reports = [];
    const channel = label => ({
      label, readyState: "open", addEventListener() {}, removeEventListener() {},
      send(data) { reports.push({ channel: label, bytes: Array.from(new Uint8Array(data)) }); },
    });
    window.testStore = {
      rpcHidChannel: channel("reliable-1"), rpcHidUnreliableChannel: channel("movement-1"),
      rpcHidProtocolVersion: 1, hidRpcDisabled: false, setRpcHidProtocolVersion() {},
    };
    window.renderHid();
    window.hid.reportAbsMouseEvent(100, 100, 1);
    window.hid.reportAbsMouseEvent(101, 101, 1);
    window.testStore.rpcHidChannel = channel("reliable-2");
    window.testStore.rpcHidUnreliableChannel = channel("movement-2");
    window.testStore.rpcHidProtocolVersion = null;
    window.renderHid();
    window.hid.reportAbsMouseEvent(102, 102, 1);
    window.testStore.rpcHidProtocolVersion = 1;
    window.renderHid();
    window.hid.reportAbsMouseEvent(103, 103, 1);
    window.hid.reportAbsMouseEvent(104, 104, 1);
    window.hid.reportAbsMouseEvent(105, 105, 0);
    window.testStore.rpcHidProtocolVersion = null;
    window.renderHid();
    window.hid.reportAbsMouseEvent(106, 106, 0);
    window.testStore.rpcHidProtocolVersion = 1;
    window.renderHid();
    window.hid.reportAbsMouseEvent(107, 107, 0);
    return reports;
  });
  assert.deepEqual(sent.map(report => report.channel), [
    "reliable-1", "movement-1", "reliable-2", "movement-2", "reliable-2", "reliable-2",
  ]);
  assert.deepEqual(sent.map(report => report.bytes[9]), [1, 1, 1, 1, 0, 0]);
  console.log("HID button authority across reconnect and handshake: OK");
} finally { await browser.close(); }
