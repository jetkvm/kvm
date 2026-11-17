// Regression for RPCs whose data channel closes before the response arrives.
// Run from ui/: node tests/jsonrpc-channel-close.mjs
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
const require = createRequire(new URL("../package.json", import.meta.url));
const ts = require("typescript");

const state = { channel: null, backoffs: [] };
globalThis.rpcCloseTest = state;
const source = readFileSync(new URL("../src/utils/jsonrpc.ts", import.meta.url), "utf8")
  .replace(
    'import { useRTCStore } from "@/hooks/stores";',
    "const useRTCStore = { getState: () => ({ rpcDataChannel: globalThis.rpcCloseTest.channel }) };",
  )
  .replace(
    'import { sleep } from "@/utils";',
    "const sleep = async ms => { globalThis.rpcCloseTest.backoffs.push(ms); };",
  );
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 },
});
const rpc = await import(
  `data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`
);

class Channel extends EventTarget {
  readyState = "open";
  listeners = new Set();
  sent = [];
  onSend = () => {};
  addEventListener(type, listener, options) {
    this.listeners.add(listener);
    super.addEventListener(type, listener, options);
  }
  removeEventListener(type, listener, options) {
    this.listeners.delete(listener);
    super.removeEventListener(type, listener, options);
  }
  send(data) {
    const request = JSON.parse(data);
    this.sent.push(request);
    this.onSend(request);
  }
  close() {
    this.readyState = "closed";
    this.dispatchEvent(new Event("close"));
  }
  reply(request, result) {
    this.dispatchEvent(
      new MessageEvent("message", {
        data: JSON.stringify({ jsonrpc: "2.0", id: request.id, result }),
      }),
    );
  }
}

// A call with retries fails over to the replacement channel after one
// backoff, not after the 5 s attempt timeout. The old channel's late reply
// and an unrelated reply on the new channel are ignored.
const old = new Channel();
const replacement = new Channel();
state.channel = old;
old.onSend = () => {
  state.channel = replacement;
  old.close();
};
replacement.onSend = request => {
  assert.equal(old.listeners.size, 0);
  old.reply(old.sent[0], { appVersion: "stale", systemVersion: "stale" });
  replacement.reply({ id: "unrelated" }, {});
  replacement.reply(request, { appVersion: "current", systemVersion: "current" });
};
const started = Date.now();
assert.deepEqual(await rpc.getLocalVersion({ maxAttempts: 3 }), {
  appVersion: "current",
  systemVersion: "current",
});
assert.equal(old.sent.length, 1);
assert.equal(replacement.sent.length, 1);
assert.equal(replacement.listeners.size, 0);
assert.deepEqual(state.backoffs, [500]);
assert(Date.now() - started < 1000, "failover must not wait for the attempt timeout");

// With the default single attempt, a closed channel rejects at once and the
// request is not sent again.
const single = new Channel();
state.channel = single;
state.backoffs = [];
single.onSend = () => single.close();
await assert.rejects(rpc.callJsonRpc({ method: "setVideoSleepMode" }), /channel closed/);
assert.equal(single.sent.length, 1);
assert.equal(single.listeners.size, 0);
assert.deepEqual(state.backoffs, []);

// A send failure releases the handlers instead of waiting for the timeout.
const throwing = new Channel();
state.channel = throwing;
throwing.onSend = () => {
  throw new Error("send failed");
};
await assert.rejects(rpc.callJsonRpc({ method: "getLocalVersion" }), /send failed/);
assert.equal(throwing.listeners.size, 0);

// The channel can close between waitForRtcReady and the send.
const raced = new Channel();
state.channel = raced;
queueMicrotask(() => raced.close());
await assert.rejects(rpc.callJsonRpc({ method: "getLocalVersion" }), /channel closed/);
assert.equal(raced.sent.length, 0);
assert.equal(raced.listeners.size, 0);

// A device that does not answer still times out, and the handlers are released.
const silent = new Channel();
state.channel = silent;
await assert.rejects(
  rpc.callJsonRpc({ method: "getLocalVersion", attemptTimeoutMs: 10 }),
  /aborted/,
);
assert.equal(silent.listeners.size, 0);
console.log("JSON-RPC close, retry, send failure, race and timeout: PASS");
