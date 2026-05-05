/**
 * Keyboard macro execution e2e tests.
 *
 * The basic "macro plays back the right keys" path is covered in
 * ra-all.spec.ts. This file covers the harder edge cases:
 *  - per-step `delay` actually pauses execution (not just the post-step gap)
 *  - the MACRO_RESET step (all-zero keys + modifier) releases anything held
 *  - cancelExecuteMacro stops further keys from arriving and releases held keys
 *
 * Run with:
 *   JETKVM_URL=http://<kvm-ip> JETKVM_REMOTE_HOST=<host-ip> \
 *     npx playwright test keyboard-macros --project=keyboard-paste
 */
import { test, expect, type Page } from "@playwright/test";
import { callJsonRpc, getDeviceHost, goToSession, restartAppViaSSH, sshExec } from "../helpers";
import { createRemoteAgent, KEY, type KeyboardEvent as RAKeyboardEvent } from "./remote-agent";

const HID_KEY_BUFFER_SIZE = 6;
const HID_A = 0x04;
const HID_B = 0x05;
const HID_C = 0x06;
const HID_MOD_LSHIFT = 0x02;

interface MacroStep {
  keys: number[];
  modifier: number;
  delay: number;
}

const MACRO_RESET: MacroStep = {
  keys: new Array(HID_KEY_BUFFER_SIZE).fill(0),
  modifier: 0,
  delay: 0,
};

function step(scancode: number, modifier: number, delay: number): MacroStep {
  return { keys: [scancode], modifier, delay };
}

const agent = createRemoteAgent();

async function ensureNoPasswordViaAPI() {
  const host = getDeviceHost();
  const status = await fetch(`http://${host}/device/status`).then(
    r => r.json() as Promise<{ isSetup: boolean }>,
  );
  if (!status.isSetup) {
    const res = await fetch(`http://${host}/device/setup`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ localAuthMode: "noPassword" }),
    });
    if (!res.ok) throw new Error(`Setup POST failed: ${res.status}`);
    return;
  }
  const probe = await fetch(`http://${host}/device`);
  if (probe.status === 401) {
    await sshExec("rm -f /userdata/kvm_config.json && sync");
    await restartAppViaSSH();
    const res = await fetch(`http://${host}/device/setup`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ localAuthMode: "noPassword" }),
    });
    if (!res.ok) throw new Error(`Setup POST after reset failed: ${res.status}`);
  }
}

async function runMacro(page: Page, steps: MacroStep[]): Promise<void> {
  await page.evaluate(s => window.__kvmTestHooks!.executeHidMacro(s), steps);
}

/** Fire-and-forget: run the macro without awaiting completion. */
async function runMacroAsync(page: Page, steps: MacroStep[]): Promise<void> {
  await page.evaluate(s => {
    void window.__kvmTestHooks!.executeHidMacro(s);
  }, steps);
}

async function cancelMacro(page: Page): Promise<void> {
  await page.evaluate(() => window.__kvmTestHooks!.cancelExecuteMacro());
}

async function waitForKeyPressCount(
  predicate: (ev: RAKeyboardEvent) => boolean,
  minCount: number,
  timeoutMs: number,
): Promise<RAKeyboardEvent[]> {
  const deadline = Date.now() + timeoutMs;
  let last: RAKeyboardEvent[] = [];
  while (Date.now() < deadline) {
    const events = await agent!.getKeyboardEvents();
    last = events.filter(ev => ev.type === "key_press" && predicate(ev));
    if (last.length >= minCount) return last;
    await new Promise(r => setTimeout(r, 50));
  }
  throw new Error(
    `Timed out waiting for ${minCount} matching key_press events; got ${last.length}`,
  );
}

test.describe.configure({ mode: "serial" });

let sharedPage: Page;

test.beforeAll(async ({ browser }) => {
  test.setTimeout(60_000);
  sharedPage = await browser.newPage();
  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);
  await goToSession(sharedPage);
  await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 30000);
});

test.afterAll(async () => {
  // Force a clean keyboard state in case a test left a key held.
  await cancelMacro(sharedPage).catch(() => {});
  await sharedPage?.close();
});

test.beforeEach(async () => {
  await agent!.clearKeyboardEvents();
});

test.describe("macro: timing", () => {
  test("per-step delay actually paces key events on the host", async () => {
    // Three keys with a 400ms delay after each. The interval between
    // host-side timestamps for key_press events should reflect the delay
    // (allowing for HID/USB jitter — assert lower bound only).
    const steps: MacroStep[] = [
      step(HID_A, 0, 400),
      { ...MACRO_RESET, delay: 400 },
      step(HID_B, 0, 400),
      { ...MACRO_RESET, delay: 400 },
      step(HID_C, 0, 400),
      { ...MACRO_RESET, delay: 0 },
    ];
    const t0 = Date.now();
    await runMacro(sharedPage, steps);
    const elapsed = Date.now() - t0;

    // Three keys × ~400ms each, but allow generous slack for HID round-trip.
    expect(elapsed, "macro should not complete instantly").toBeGreaterThan(800);

    const events = await agent!.getKeyboardEvents();
    const presses = events.filter(
      ev =>
        ev.type === "key_press" && (ev.code === KEY.A || ev.code === KEY.B || ev.code === KEY.C),
    );
    expect(presses.length).toBe(3);
    expect(presses.map(ev => ev.code)).toEqual([KEY.A, KEY.B, KEY.C]);

    // Each consecutive press should be ≥ ~250ms apart (400ms delay − margin).
    for (let i = 1; i < presses.length; i++) {
      const gap = presses[i].time_ms - presses[i - 1].time_ms;
      expect(gap, `gap between press ${i - 1} and ${i}`).toBeGreaterThan(250);
    }
  });
});

test.describe("macro: MACRO_RESET", () => {
  test("explicit reset between keys releases the modifier", async () => {
    // Press Shift+A, reset, then press B without modifier. The host should
    // see LSHIFT release before B press — i.e. B is lowercase (no shift held).
    const steps: MacroStep[] = [
      step(HID_A, HID_MOD_LSHIFT, 50),
      { ...MACRO_RESET, delay: 50 },
      step(HID_B, 0, 50),
      { ...MACRO_RESET, delay: 0 },
    ];
    await runMacro(sharedPage, steps);

    const events = await agent!.getKeyboardEvents();
    const interesting = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT || ev.code === KEY.A || ev.code === KEY.B,
    );
    // Required ordering: shift down, A down, A up, shift up, then B down (no shift).
    const shiftRelease = interesting.find(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    const bPress = interesting.find(ev => ev.code === KEY.B && ev.type === "key_press");
    expect(shiftRelease, "shift must be released after first step").toBeDefined();
    expect(bPress, "B must be pressed").toBeDefined();
    expect(
      bPress!.time_ms,
      "B press happens after shift release (reset cleared modifier)",
    ).toBeGreaterThanOrEqual(shiftRelease!.time_ms);
  });
});

test.describe("macro: cancel", () => {
  test("cancelExecuteMacro stops further keys from arriving", async () => {
    // 8 keys with 300ms between them = ~2.4s total. Cancel after ~600ms.
    // Expect at most a few keys delivered, not all 8.
    const steps: MacroStep[] = [];
    for (let i = 0; i < 8; i++) {
      steps.push(step(HID_A, 0, 300));
      steps.push({ ...MACRO_RESET, delay: 300 });
    }

    await runMacroAsync(sharedPage, steps);
    // Wait until the host sees at least 1 press to confirm the macro started.
    await waitForKeyPressCount(ev => ev.code === KEY.A, 1, 3000);
    await new Promise(r => setTimeout(r, 600));

    const beforeCancel = (await agent!.getKeyboardEvents()).filter(
      ev => ev.type === "key_press" && ev.code === KEY.A,
    ).length;
    await cancelMacro(sharedPage);

    // Give the device a moment to flush any in-flight HID report.
    await new Promise(r => setTimeout(r, 1500));

    const afterCancel = (await agent!.getKeyboardEvents()).filter(
      ev => ev.type === "key_press" && ev.code === KEY.A,
    ).length;
    expect(beforeCancel, "macro must have started").toBeGreaterThan(0);
    expect(beforeCancel, "macro must not have completed before cancel").toBeLessThan(8);
    // At most one extra in-flight key may slip through after cancel.
    expect(afterCancel - beforeCancel).toBeLessThanOrEqual(1);
    expect(afterCancel, "cancel must prevent the full sequence").toBeLessThan(8);
  });

  test("cancel mid-hold releases the held modifier on the host", async () => {
    // First step holds shift+A for a long time. Cancel before the macro's
    // own reset runs. The host must see LSHIFT release — otherwise shift
    // would be stuck pressed.
    const steps: MacroStep[] = [
      { keys: [HID_A], modifier: HID_MOD_LSHIFT, delay: 5000 },
      { ...MACRO_RESET, delay: 0 },
    ];
    await runMacroAsync(sharedPage, steps);
    await waitForKeyPressCount(ev => ev.code === KEY.LEFT_SHIFT, 1, 3000);
    await new Promise(r => setTimeout(r, 200));
    await cancelMacro(sharedPage);

    const deadline = Date.now() + 3000;
    let sawShiftRelease = false;
    while (Date.now() < deadline && !sawShiftRelease) {
      const events = await agent!.getKeyboardEvents();
      sawShiftRelease = events.some(ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release");
      if (!sawShiftRelease) await new Promise(r => setTimeout(r, 100));
    }
    expect(sawShiftRelease, "host should see shift released after cancel").toBe(true);
  });
});

test.describe("macro: getKeyboardLayouts smoke", () => {
  // Quick sanity check that the data path the paste/macro tests rely on
  // (JSON-RPC layout fetch) works for at least one built-in.
  test("getKeyboardLayouts returns at least one layout", async () => {
    const layouts = (await callJsonRpc(sharedPage, "getKeyboardLayouts")) as Array<{
      id: string;
      name: string;
    }>;
    expect(Array.isArray(layouts)).toBe(true);
    expect(layouts.length).toBeGreaterThan(0);
    expect(
      layouts.find(l => l.id === "en-US"),
      "en-US must be a built-in",
    ).toBeDefined();
  });
});
