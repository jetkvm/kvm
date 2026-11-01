// The device's keyboard layout is fetched once per rpc channel, only once the
// channel is open, and a paste waits for it; a missing answer is reported as
// not loaded, never treated as the layout.
// Run from ui/: node tests/keyboard-layout-loader.mjs
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { rolldown } from "rolldown";

const entry = fileURLToPath(
  new URL("../src/keyboardLayoutLoader.ts", import.meta.url),
);
const bundle = await rolldown({ input: entry });
const { output } = await bundle.generate({ format: "esm" });
await bundle.close();
const { loadKeyboardLayout } = await import(
  `data:text/javascript;base64,${Buffer.from(output[0].code).toString("base64")}`
);

class FakeChannel extends EventTarget {
  readyState = "connecting";
  open() {
    this.readyState = "open";
    this.dispatchEvent(new Event("open"));
  }
  close() {
    this.readyState = "closed";
    this.dispatchEvent(new Event("close"));
  }
}
const tick = () => new Promise((r) => setTimeout(r, 0));
const requests = [];
const send = (method, params, callback) =>
  void requests.push({ method, params, callback });
const layouts = [];
const setLayout = (layout) => layouts.push(layout);

// Nothing is asked before the channel opens, and the caller waits.
const a = new FakeChannel();
let settled = null;
const first = loadKeyboardLayout(a, send, setLayout).then(
  (loaded) => (settled = loaded),
);
await tick();
assert.equal(requests.length, 0);
assert.equal(settled, null);
a.open();
assert.equal(requests.length, 1);
assert.equal(requests[0].method, "getKeyboardLayout");
requests[0].callback({ jsonrpc: "2.0", id: 1, result: "de-DE" });
await first;
assert.equal(settled, true);
assert.deepEqual(layouts, ["de-DE"]);

// The same channel is asked once.
assert.equal(await loadKeyboardLayout(a, send, setLayout), true);
assert.equal(requests.length, 1);

// Another connection is asked again. An error reply is reported as not
// loaded, leaves the layout alone, and the next caller retries.
const b = new FakeChannel();
b.readyState = "open";
const failed = loadKeyboardLayout(b, send, setLayout);
assert.equal(requests.length, 2);
requests[1].callback({
  jsonrpc: "2.0",
  id: 2,
  error: { code: -32000, message: "no" },
});
assert.equal(await failed, false);
assert.deepEqual(layouts, ["de-DE"]);
void loadKeyboardLayout(b, send, setLayout);
assert.equal(requests.length, 3);

// A channel that closes before it opens releases the caller, not loaded,
// without a request.
const c = new FakeChannel();
const closed = loadKeyboardLayout(c, send, setLayout);
c.close();
assert.equal(await closed, false);
assert.equal(requests.length, 3);

// A reply that never comes releases the caller after the timeout, not
// loaded, and the next caller asks again.
const d = new FakeChannel();
d.readyState = "open";
assert.equal(await loadKeyboardLayout(d, send, setLayout, 20), false);
assert.equal(requests.length, 4);
void loadKeyboardLayout(d, send, setLayout, 20);
assert.equal(requests.length, 5);

// Without a channel there is nothing to wait for, and nothing loaded.
assert.equal(await loadKeyboardLayout(null, send, setLayout), false);
assert.equal(requests.length, 5);

console.log(
  "Keyboard layout loader: waits for open, once per channel, reports failures: OK",
);
