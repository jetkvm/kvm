import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
import test from "node:test";
import ts from "typescript";

// Execute the real DOM callbacks AND useKeyboard. Only React and transport are
// stubbed. The receiver models USB rising edges; this is not a hardware test.
const video = fs.readFileSync(new URL("../src/components/WebRTCVideo.tsx", import.meta.url), "utf8");
const keyboard = fs.readFileSync(new URL("../src/hooks/useKeyboard.ts", import.meta.url), "utf8");
const mappings = fs.readFileSync(new URL("../src/keyboardMappings.ts", import.meta.url), "utf8");
const compile = source => ts.transpileModule(source, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
}).outputText;
const mappingContext = { exports: {} };
vm.runInNewContext(compile(mappings), mappingContext);
const { keys, hidKeyToModifierMask } = mappingContext.exports;

function fixture({ mac = true, remoteCaps = false, legacy = false } = {}) {
  let held = new Set();
  let caps = remoteCaps;
  let channel = { readyState: "open", generation: 0 };
  const reports = [];
  const timers = new Map();
  let timerId = 0;
  const state = { keys: [0, 0, 0, 0, 0, 0], modifier: 0 };
  const reactRefs = [];
  let refIndex = 0;
  let effectDeps;
  let listenerCleanup;
  let ocr = false;
  const react = {
    useCallback: fn => fn,
    useRef: value => reactRefs[refIndex++] ??= { current: value },
  };
  const receive = (next, kind) => {
    assert.equal(channel.readyState, "open", "no writes to a closed session");
    if (next.has(keys.CapsLock) && !held.has(keys.CapsLock)) caps = !caps;
    held = next;
    reports.push({ keys: [...held], kind, generation: channel.generation });
  };
  const reportKeypressEvent = (key, down) => {
    if (channel.readyState !== "open") return;
    const next = new Set(held);
    if (down) next.add(key);
    else next.delete(key);
    receive(next, "keypress");
  };
  const reportKeyboardEvent = (keyValues, modifier) => {
    if (channel.readyState !== "open") return;
    const next = new Set(keyValues.filter(Boolean));
    for (const [key, mask] of Object.entries(hidKeyToModifierMask)) {
      if (modifier & mask) next.add(Number(key));
    }
    receive(next, "report");
  };
  const transport = {
    reportKeypressEvent,
    reportKeyboardEvent,
    reportKeypressKeepAlive() {},
  };
  const store = {
    keysDownState: state,
    setKeysDownState(next) { Object.assign(state, next); },
    setKeyboardLedState() {},
    setPasteModeEnabled() {},
  };
  const modules = {
    react,
    "@/hooks/hidRpc": {},
    "@/hooks/stores": {
      useRTCStore: () => ({ rpcDataChannel: channel }),
      useHidStore: () => store,
      hidKeyBufferSize: 6,
      hidErrorRollOver: 1,
    },
    "@/hooks/useHidRpc": { useHidRpc: () => ({ ...transport, rpcHidReady: !legacy && channel.readyState === "open" }) },
    "@/hooks/useJsonRpc": { useJsonRpc: () => ({ send(method, params, callback) {
      assert.equal(method, "keyboardReport");
      reportKeyboardEvent([...params.keys], params.modifier);
      callback({ result: {} });
    } }) },
    "@/keyboardMappings": mappingContext.exports,
    "@/utils": { sleep: async () => {} },
  };
  const context = vm.createContext({
    exports: {},
    require(name) { assert.ok(name in modules, `unexpected module ${name}`); return modules[name]; },
    console: { debug() {}, warn() {}, error() {} },
    AbortController,
    document: new EventTarget(),
    window: new EventTarget(),
    setInterval(fn) { const id = ++timerId; timers.set(id, fn); return id; },
    clearInterval(id) { timers.delete(id); },
    setTimeout() { throw new Error("Caps Lock must not schedule a delayed release"); },
    clearTimeout() {},
  });
  vm.runInContext(compile(keyboard), context);
  function render() {
    refIndex = 0;
    const api = context.exports.default();
    Object.assign(context, {
      ...react, keys, useMemo: fn => fn(), isMac: () => mac, isWindows: () => false,
      useEffect(fn, deps) {
        if (!effectDeps || deps.some((dep, i) => dep !== effectDeps[i])) fn();
        effectDeps = deps;
      },
      peerConnection: channel,
      resetKeyboardState: api.resetKeyboardState,
      isOcrMode: ocr, isKeyboardLockActive: true,
      getAdjustedKeyCode: e => e.code,
      handleKeyPress: api.handleKeyPress,
    });
    // Execute production platform/ref declarations, retaining actual hook refs
    // across callback re-registration just as React does.
    const declarations = video.slice(video.indexOf("  // AltGr Fix"), video.indexOf("  useResizeObserver("));
    assert.ok(declarations.includes("isWindowsClient"));
    vm.runInContext(compile(declarations.replaceAll("const ", "var ")), context);
    const start = video.includes("  const resetKeyboardInput = useCallback(")
      ? video.indexOf("  const resetKeyboardInput = useCallback(")
      : video.includes("  const handlePhysicalCapsLock = useCallback(")
        ? video.indexOf("  const handlePhysicalCapsLock = useCallback(")
        : video.indexOf("  const keyDownHandler = useCallback(");
    const end = video.indexOf("  const videoKeyUpHandler = useCallback(", start);
    assert.ok(start >= 0 && end > start, "actual event handler extraction");
    vm.runInContext(compile(video.slice(start, end).replaceAll("const ", "var ")), context);
    // Install the real keyboard event effect, including abort cleanup. This
    // tests focus/blur/visibility wiring, not a hand-written lifecycle model.
    const setup = video.slice(video.indexOf("  // Setup Keyboard Events"), video.indexOf("  // Setup Video Event Listeners"));
    assert.ok(setup.includes("setupKeyboardEvents"));
    listenerCleanup?.();
    context.useEffect = fn => { listenerCleanup = fn(); };
    vm.runInContext(compile(setup), context);
    return api;
  }
  let api = render();
  return {
    reports, timers,
    get held() { return [...held]; },
    get caps() { return caps; },
    event(type, capsLock, extra = {}) {
      const e = {
        code: "CapsLock", key: "CapsLock", type: type === "down" ? "keydown" : "keyup", metaKey: false,
        repeat: false, timeStamp: 0, preventDefault() {},
        getModifierState: name => name === "CapsLock" && capsLock,
        ...extra,
      };
      return context[type === "down" ? "keyDownHandler" : "keyUpHandler"](e);
    },
    direct: (key, down) => api.handleKeyPress(key, down),
    reset: () => context.window.dispatchEvent(new Event("blur")),
    visibility: () => context.document.dispatchEvent(new Event("visibilitychange")),
    focus: () => context.document.dispatchEvent(new Event("focusin")),
    setOcr(value) { ocr = value; api = render(); },
    unmount() { listenerCleanup?.(); api.cleanup(); },
    rerender() { api = render(); },
    disconnect() { channel.readyState = "closed"; held.clear(); api.cleanup(); },
    reconnect() {
      channel = { readyState: "open", generation: channel.generation + 1 };
      api = render();
    },
  };
}

for (const remoteCaps of [false, true]) {
  for (const initialLocal of [false, true]) {
    for (const pattern of ["chromium", "paired", "down-only"]) {
      test(`${pattern}: remote=${remoteCaps}, initial local=${initialLocal}`, async () => {
        const f = fixture({ remoteCaps });
        let local = initialLocal;
        for (let i = 0; i < 4; i++) {
          local = !local;
          if (pattern === "paired") {
            await f.event("down", local);
            await f.event("up", local);
          } else await f.event(pattern === "down-only" || local ? "down" : "up", local);
          assert.equal(f.caps, i % 2 === 0 ? !remoteCaps : remoteCaps);
          assert.deepEqual(f.held, [], "Caps must be released after each logical press");
          assert.equal(f.timers.size, 0, "no leftover keepalive");
          assert.equal(f.reports.length, 2 * (i + 1));
        }
      });
    }
  }
}

test("non-Mac paired events remain passthrough", async () => {
  const f = fixture({ mac: false });
  for (const state of [true, false, true, false]) {
    await f.event("down", state);
    assert.deepEqual(f.held, [keys.CapsLock]);
    await f.event("up", state);
    assert.equal(f.caps, state);
    assert.deepEqual(f.held, []);
  }
});

test("repeat, duplicate edges and callback re-registration do not double-toggle", async () => {
  const f = fixture();
  await f.event("down", true);
  await f.event("down", true, { repeat: true });
  await f.event("down", true);
  f.rerender();
  await f.event("up", true);
  assert.equal(f.reports.length, 2);
  assert.equal(f.caps, true);
  assert.deepEqual(f.held, []);
});

for (const legacy of [false, true]) {
  test(`pulse preserves held letters and all modifier masks; legacy=${legacy}`, async () => {
    const f = fixture({ legacy });
    const other = [keys.KeyA, keys.ShiftLeft, keys.ControlLeft, keys.AltLeft, keys.MetaLeft];
    for (const key of other) await f.direct(key, true);
    const before = f.reports.length;
    await f.event("down", true, { metaKey: true });
    assert.equal(f.reports.length, before + 2);
    assert.deepEqual(new Set(f.held), new Set(other));
    assert.equal(f.timers.size, legacy ? 0 : 1);
    for (const key of other.toReversed()) await f.direct(key, false);
    assert.deepEqual(f.held, []);
    assert.equal(f.timers.size, 0);
  });
}

test("blur/visibility reset, disconnect and replacement have no late Caps writes", async () => {
  const f = fixture();
  await f.event("down", true);
  await f.reset(); // actual reset used by both blur and visibilitychange
  f.rerender();
  await f.event("up", true); // trailing half of an ordinary pair
  assert.equal(f.caps, true);
  const beforeDisconnect = f.reports.length;
  f.disconnect();
  assert.equal(f.timers.size, 0);
  await f.event("up", false); // not delivered; must not replay on reconnect
  assert.equal(f.reports.length, beforeDisconnect);
  f.reconnect();
  assert.equal(f.reports.length, beforeDisconnect, "connect must not synchronize Caps");
  await f.event("down", true);
  await f.event("up", true);
  assert.equal(f.caps, false);
  assert.deepEqual(f.held, []);
  assert.equal(f.timers.size, 0);
});

for (const before of [false, true]) {
  test(`first Caps after blur and an unseen external toggle is counted: last=${before}`, async () => {
    const f = fixture();
    await f.event(before ? "down" : "up", before);
    await f.reset();
    // Toggle to !before happened in another tab; the next toggle back to
    // before is a genuinely new press, with no intervening ordinary key.
    const count = f.reports.length;
    await f.event(before ? "down" : "up", before);
    assert.equal(f.reports.length, count + 2);
    assert.equal(f.caps, false);
    assert.deepEqual(f.held, []);
  });
}

for (const gap of ["visibility", "focus", "ocr", "replacement"]) {
  test(`${gap} invalidates observation without auto-toggle; paired release stays deduplicated`, async () => {
    const f = fixture();
    await f.event("down", true);
    const count = f.reports.length;
    if (gap === "ocr") { f.setOcr(true); f.setOcr(false); }
    else if (gap === "replacement") { f.disconnect(); f.reconnect(); }
    else f[gap]();
    // visibility retains the existing full reset; all other observation-only
    // events send no keyboard reports.
    assert.equal(f.reports.length, count + (gap === "visibility" ? 1 : 0));
    const afterGap = f.reports.length;
    await f.event("up", true);
    assert.equal(f.reports.length, afterGap, "no extra toggle on trailing release");
    await f.event("up", false);
    assert.equal(f.caps, false);
    assert.deepEqual(f.held, []);
    f.unmount();
    const beforeFocus = f.reports.length;
    f.focus();
    assert.equal(f.reports.length, beforeFocus, "unmounted listeners are removed");
  });
}

test("OCR bypasses both physical edges without sending a pulse", async () => {
  const f = fixture();
  f.setOcr(true);
  await f.event("down", true);
  await f.event("up", false);
  assert.equal(f.reports.length, 0);
  f.setOcr(false);
  await f.event("down", true);
  assert.equal(f.reports.length, 2);
});

test("ordinary events rebase local observation after a Caps change outside capture", async () => {
  const f = fixture();
  await f.event("down", true);
  await f.reset();
  // Caps turns off outside this document (no event delivered to the handler).
  // On return, ordinary input carries current modifier metadata, not a command
  // to synchronize the remote host.
  const beforeRebase = f.caps;
  await f.event("down", false, { code: "KeyA", key: "a" });
  await f.event("up", false, { code: "KeyA", key: "a" });
  assert.equal(f.caps, beforeRebase, "observation must not synchronize Caps");
  const count = f.reports.length;
  await f.event("down", true);
  await f.event("up", true);
  assert.equal(f.reports.length, count + 2, "new physical press must not be lost");
  assert.equal(f.caps, false);
  assert.deepEqual(f.held, []);
});

test("changed ordinary metadata invalidates an old paired-down across two gaps", async () => {
  const f = fixture();
  await f.event("down", true);
  f.reset();
  // First unseen change: Caps off elsewhere; ordinary input proves that the
  // old Caps down no longer describes the observed local state.
  await f.event("down", false, { code: "KeyA", key: "a" });
  await f.event("up", false, { code: "KeyA", key: "a" });
  f.reset();
  // Second unseen change: Caps on elsewhere. A new Chromium up disables it.
  const count = f.reports.length;
  await f.event("up", false);
  assert.equal(f.reports.length, count + 2, "new up-only press must not be mistaken for old release");
  assert.equal(f.caps, false);
  assert.deepEqual(f.held, []);
});

test("ordinary letters and Shift retain their existing semantics", async () => {
  const f = fixture();
  for (const key of ["ShiftLeft", "KeyA", "Digit1"]) await f.event("down", false, { code: key, key });
  assert.deepEqual(new Set(f.held), new Set([keys.ShiftLeft, keys.KeyA, keys.Digit1]));
  for (const key of ["Digit1", "KeyA", "ShiftLeft"]) await f.event("up", false, { code: key, key });
  assert.deepEqual(f.held, []);
  assert.equal(f.timers.size, 0);
});

test("virtual/macro-ready pulses bypass DOM normalization", async () => {
  const f = fixture();
  for (let i = 0; i < 4; i++) {
    await f.direct(keys.CapsLock, true);
    await f.direct(keys.CapsLock, false);
    assert.equal(f.caps, i % 2 === 0);
    assert.deepEqual(f.held, []);
  }
  assert.equal(f.reports.length, 8);
  assert.equal(f.timers.size, 0);
});
