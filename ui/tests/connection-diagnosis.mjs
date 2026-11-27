import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
const require = createRequire(new URL("../package.json", import.meta.url));
const ts = require("typescript");

const source = readFileSync(
  new URL("../src/diagnosis/diagnose.ts", import.meta.url),
  "utf8",
).replace(
  'import { m } from "@localizations/messages.js";',
  "const m = new Proxy({}, { get: (_, key) => () => key });",
);
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 },
});
const { diagnose, WAIT_LIMIT_MS } = await import(
  `data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`
);

const healthy = {
  mode: "local",
  browser: "chrome",
  linux: false,
  addressIsIp: true,
  webrtcSupported: true,
  h264Supported: true,
  computerOnline: true,
  socketOpen: true,
  httpReachable: true,
  cloudOnline: null,
  answerState: "accepted",
  reachedThroughOtherAddress: null,
  selfTestConnected: null,
  selfTestH264: null,
  iceWasConnected: true,
  peerConnected: true,
  hdmiState: "ready",
  usbState: "configured",
  edidIsDefault: true,
  receivingBytes: true,
  decodingFrames: true,
  codec: "H.264",
  videoPlaying: true,
  playError: null,
  msWaiting: 0,
};
const run = facts => diagnose({ ...healthy, ...facts });
const at = diagnosis => `${diagnosis.status}:${diagnosis.question ?? ""}`;

assert.equal(at(run({})), "ok:");

const brokenEverywhere = {
  socketOpen: false,
  iceWasConnected: false,
  peerConnected: false,
  hdmiState: "no_signal",
  receivingBytes: false,
};
assert.equal(at(run(brokenEverywhere)), "waiting:3", "the earliest open question is shown first");
assert.equal(at(run({ ...brokenEverywhere, msWaiting: WAIT_LIMIT_MS - 1 })), "waiting:3");
assert.equal(at(run({ ...brokenEverywhere, msWaiting: WAIT_LIMIT_MS })), "failed:3");
assert.equal(at(run({ ...brokenEverywhere, webrtcSupported: false })), "failed:1");
assert.equal(at(run({ ...brokenEverywhere, h264Supported: false })), "failed:2");

assert.equal(
  at(run({ socketOpen: false, computerOnline: false })),
  "failed:3",
  "offline fails at once",
);
assert.equal(
  at(run({ iceWasConnected: false, peerConnected: false, answerState: "rejected" })),
  "failed:4",
  "a rejected answer fails at once",
);
assert.equal(at(run({ iceWasConnected: false, peerConnected: false })), "waiting:4");

const noSignal = { hdmiState: "no_signal", msWaiting: WAIT_LIMIT_MS };
assert.equal(run({ ...noSignal, edidIsDefault: false }).steps[0].action, "restoreEdid");
assert.ok(!run(noSignal).steps.some(step => step.action === "restoreEdid"));
assert.equal(run({ ...noSignal, usbState: "suspended" }).steps[0].action, "wake");

console.log("connection diagnosis: ok");
