import { test, expect, type Page } from "@playwright/test";
import {
  HID_KEY,
  callJsonRpc,
  getKeysDownState,
  pauseKeepAlive,
  sendKeypress,
  tapKey,
  waitForWebRTCReady,
  ensureRpcReady,
  getLedState,
  waitForLedState,
} from "../helpers";
import { waitForKeyboardReady, KEY, type KeyboardEvent as RAKeyboardEvent } from "./remote-agent";
import { ALL_SCAN_KEYS, agent, registerSharedSession } from "./shared";

test.describe.configure({ mode: "serial" });

let sharedPage: Page;
registerSharedSession(page => (sharedPage = page));

test.describe("Remote Host Agent: keyboard", () => {
  // ═══════════════════════════════════════════
  // KEYBOARD: TOGGLE KEYS + LED ROUND-TRIP
  // ═══════════════════════════════════════════

  test("keyboard: toggle keys with LED round-trip", async () => {
    test.setTimeout(30_000);

    const initialState = await getLedState(sharedPage);
    expect(initialState).not.toBeNull();

    // EDID restore may still be in-flight; retry until full HID stack (including LED reports) stabilizes.
    // Re-read caps_lock each iteration so a failed-try + successful-undo doesn't leave us
    // permanently toggling in the wrong direction.
    const deadline = Date.now() + 15000;
    let capsToggled = false;
    let capsBeforeToggle = initialState!.caps_lock;
    while (Date.now() < deadline) {
      capsBeforeToggle = (await getLedState(sharedPage))!.caps_lock;
      await agent!.clearKeyboardEvents();
      try {
        await agent!.expectKeyPress(
          KEY.CAPS_LOCK,
          async () => {
            await tapKey(sharedPage, HID_KEY.CAPS_LOCK);
          },
          3000,
        );
        await waitForLedState(sharedPage, "caps_lock", !capsBeforeToggle, 2000);
        capsToggled = true;
        break;
      } catch {
        await tapKey(sharedPage, HID_KEY.CAPS_LOCK);
        await new Promise(r => setTimeout(r, 500));
      }
    }
    expect(capsToggled, "CAPS_LOCK LED should toggle").toBe(true);
    expect((await getLedState(sharedPage))!.caps_lock).toBe(!capsBeforeToggle);

    // Restore CAPS_LOCK
    await agent!.expectKeyPress(
      KEY.CAPS_LOCK,
      async () => {
        await tapKey(sharedPage, HID_KEY.CAPS_LOCK);
      },
      5000,
    );
    await waitForLedState(sharedPage, "caps_lock", capsBeforeToggle);

    // NUM_LOCK: same round-trip verification
    const initialNum = initialState!.num_lock;

    const numEvents = await agent!.expectKeyPress(
      KEY.NUM_LOCK,
      async () => {
        await tapKey(sharedPage, HID_KEY.NUM_LOCK);
      },
      5000,
    );
    expect(numEvents.length).toBeGreaterThan(0);
    await waitForLedState(sharedPage, "num_lock", !initialNum);
    expect((await getLedState(sharedPage))!.num_lock).toBe(!initialNum);

    await agent!.expectKeyPress(
      KEY.NUM_LOCK,
      async () => {
        await tapKey(sharedPage, HID_KEY.NUM_LOCK);
      },
      5000,
    );
    await waitForLedState(sharedPage, "num_lock", initialNum);

    // SPACE: verify received (no LED, just key delivery)
    const spaceEvents = await agent!.expectKeyPress(
      KEY.SPACE,
      async () => {
        await tapKey(sharedPage, HID_KEY.SPACE);
      },
      5000,
    );
    expect(spaceEvents.length).toBeGreaterThan(0);
  });

  // ═══════════════════════════════════════════
  // KEYBOARD: SCANS + PRESS/RELEASE + MODIFIERS
  // ═══════════════════════════════════════════

  test("keyboard: key scans, press/release, and modifiers", async () => {
    // Batch all 48 key scans in a single evaluate (eliminates per-key round-trip overhead)
    await agent!.clearKeyboardEvents();

    await sharedPage.evaluate(
      async (keys: number[]) => {
        const hooks = window.__kvmTestHooks;
        if (!hooks) throw new Error("Test hooks not available");
        for (const hid of keys) {
          hooks.sendKeypress(hid, true);
          hooks.sendKeypress(hid, false);
          await new Promise(r => setTimeout(r, 10));
        }
      },
      ALL_SCAN_KEYS.map(k => k.hid),
    );

    const scanDeadline = Date.now() + 5000;
    let failed: string[] = [];
    while (Date.now() < scanDeadline) {
      const events = await agent!.getKeyboardEvents();
      const pressedCodes = new Set(events.filter(ev => ev.type === "key_press").map(ev => ev.code));
      failed = ALL_SCAN_KEYS.filter(k => !pressedCodes.has(k.linux)).map(k => k.label);
      if (failed.length === 0) break;
      await new Promise(r => setTimeout(r, 50));
    }
    expect(failed, `Keys not received: ${failed.join(", ")}`).toHaveLength(0);

    // Press/release timing: verify release comes after press
    await agent!.clearKeyboardEvents();
    await sendKeypress(sharedPage, HID_KEY.SPACE, true);
    await new Promise(r => setTimeout(r, 10));
    await sendKeypress(sharedPage, HID_KEY.SPACE, false);
    await new Promise(r => setTimeout(r, 50));

    const prEvents = await agent!.getKeyboardEvents();
    const presses = prEvents.filter(ev => ev.code === KEY.SPACE && ev.type === "key_press");
    const releases = prEvents.filter(ev => ev.code === KEY.SPACE && ev.type === "key_release");
    expect(presses.length).toBeGreaterThanOrEqual(1);
    expect(releases.length).toBeGreaterThanOrEqual(1);
    expect(releases[0].time_ms).toBeGreaterThan(presses[0].time_ms);

    // Modifier combo: verify C key arrives
    await agent!.clearKeyboardEvents();
    await sendKeypress(sharedPage, 0x06, true);
    await new Promise(r => setTimeout(r, 10));
    await sendKeypress(sharedPage, 0x06, false);
    await new Promise(r => setTimeout(r, 50));

    const cEvents = await agent!.getKeyboardEvents();
    const cPresses = cEvents.filter(ev => ev.code === KEY.C && ev.type === "key_press");
    expect(cPresses.length).toBeGreaterThanOrEqual(1);
  });

  // ═══════════════════════════════════════════
  // KEYBOARD: MODIFIER AUTO-RELEASE
  // ═══════════════════════════════════════════

  test("keyboard: modifiers do not participate in per-key auto-release (10s lone hold)", async () => {
    test.setTimeout(60_000);

    const modifiers = [
      { hid: 0xe0, linux: KEY.LEFT_CTRL, label: "LeftCtrl", maskBit: 0x01 },
      { hid: 0xe1, linux: KEY.LEFT_SHIFT, label: "LeftShift", maskBit: 0x02 },
      { hid: 0xe2, linux: KEY.LEFT_ALT, label: "LeftAlt", maskBit: 0x04 },
    ];

    for (const { hid, linux, label, maskBit } of modifiers) {
      await agent!.clearKeyboardEvents();

      await sendKeypress(sharedPage, hid, true);

      const SAMPLES = 20;
      const SAMPLE_INTERVAL = 500;
      for (let i = 0; i < SAMPLES; i++) {
        await new Promise(r => setTimeout(r, SAMPLE_INTERVAL));

        const state = await getKeysDownState(sharedPage);
        expect(
          state?.modifier ?? 0,
          `${label} bit should be set on sample ${i + 1}/${SAMPLES} (t=${(i + 1) * SAMPLE_INTERVAL}ms)`,
        ).toBe(maskBit);

        const events = await agent!.getKeyboardEvents();
        const releases = events.filter(ev => ev.code === linux && ev.type === "key_release");
        expect(releases.length, `${label} must not auto-release (sample ${i + 1}/${SAMPLES})`).toBe(
          0,
        );
      }

      const releaseStart = Date.now();
      await sendKeypress(sharedPage, hid, false);
      await new Promise(r => setTimeout(r, 200));

      const finalEvents = await agent!.getKeyboardEvents();
      const presses = finalEvents.filter(ev => ev.code === linux && ev.type === "key_press");
      const releases = finalEvents.filter(ev => ev.code === linux && ev.type === "key_release");

      expect(presses.length, `${label} should have exactly 1 press`).toBe(1);
      expect(releases.length, `${label} should have exactly 1 release`).toBe(1);

      const releaseLatency = releases[0].time_ms - presses[0].time_ms;
      expect(
        releaseLatency,
        `${label} release should occur after the full 10s hold`,
      ).toBeGreaterThan(SAMPLES * SAMPLE_INTERVAL - 1000);
      expect(Date.now() - releaseStart).toBeLessThan(2000);
    }
  });

  test("keyboard: modifier does not auto-release without browser keepalives", async () => {
    await agent!.clearKeyboardEvents();

    try {
      await callJsonRpc(sharedPage, "keypressReport", { key: 0xe1, press: true });

      await expect
        .poll(
          async () => {
            const s = (await callJsonRpc(sharedPage, "getKeyDownState")) as {
              modifier: number;
              keys: number[];
            };
            return s.modifier === 0x02 && s.keys.every((k: number) => k === 0);
          },
          {
            message: "LeftShift should be held after direct keypressReport",
            timeout: 5000,
            intervals: [100, 200, 500],
          },
        )
        .toBe(true);

      await expect
        .poll(
          async () => {
            const events = await agent!.getKeyboardEvents();
            return events.some(ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_press");
          },
          {
            message: "Host should see LeftShift press",
            timeout: 5000,
            intervals: [100, 200, 500],
          },
        )
        .toBe(true);

      await new Promise(r => setTimeout(r, 300));

      const state = (await callJsonRpc(sharedPage, "getKeyDownState")) as {
        modifier: number;
        keys: number[];
      };
      expect(state.modifier, "LeftShift should still be held without keepalives").toBe(0x02);

      const events = await agent!.getKeyboardEvents();
      const releases = events.filter(ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release");
      expect(releases.length, "LeftShift must not auto-release without keepalives").toBe(0);
    } finally {
      await callJsonRpc(sharedPage, "keypressReport", { key: 0xe1, press: false }).catch(() => {});
    }
  });

  // ═══════════════════════════════════════════
  // KEYBOARD: KEEPALIVE & AUTO-RELEASE
  // ═══════════════════════════════════════════

  test("keepalive: held key survives beyond 100ms with keepalives", async () => {
    // The device-side auto-release timer fires at 100ms (DefaultAutoReleaseDuration).
    // The browser sends keepalives every 50ms, each extending the timer by up to 100ms
    // (baseExtension = expectedRate + maxLateness = 50ms + 50ms).
    // A key held for 300ms via the browser must NOT auto-release prematurely.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0x04, true);
    await new Promise(r => setTimeout(r, 300));
    await sendKeypress(sharedPage, 0x04, false);
    await new Promise(r => setTimeout(r, 150));

    const events = await agent!.getKeyboardEvents();
    const presses = events.filter(ev => ev.code === KEY.A && ev.type === "key_press");
    const releases = events.filter(ev => ev.code === KEY.A && ev.type === "key_release");

    expect(presses.length, "Key A should have been pressed").toBeGreaterThanOrEqual(1);
    expect(releases.length, "Key A should have exactly one release").toBe(1);

    const holdDuration = releases[0].time_ms - presses[0].time_ms;
    expect(
      holdDuration,
      "Key should be held for at least 250ms (keepalives kept it alive)",
    ).toBeGreaterThanOrEqual(250);
  });

  test("keepalive: auto-release fires at ~100ms without keepalives", async () => {
    // Bypass the browser keepalive by using keypressReport RPC directly.
    // The device schedules auto-release at 100ms (DefaultAutoReleaseDuration).
    // After 300ms the key must have been auto-released.
    await agent!.clearKeyboardEvents();

    await callJsonRpc(sharedPage, "keypressReport", { key: 0x04, press: true });
    await new Promise(r => setTimeout(r, 300));

    const events = await agent!.getKeyboardEvents();
    const presses = events.filter(ev => ev.code === KEY.A && ev.type === "key_press");
    const releases = events.filter(ev => ev.code === KEY.A && ev.type === "key_release");

    expect(presses.length, "Key A should have been pressed").toBeGreaterThanOrEqual(1);
    expect(releases.length, "Key A should have auto-released").toBeGreaterThanOrEqual(1);

    const holdDuration = releases[0].time_ms - presses[0].time_ms;
    // Auto-release fires at 100ms. Allow some slack for scheduling jitter.
    expect(holdDuration, "Auto-release should fire near 100ms").toBeLessThan(200);
  });

  test("keepalive: key auto-releases after window blur, no stuck keys on re-focus", async () => {
    // When the browser loses focus, resetKeyboardState fires: cancels keepalives
    // and sends a zero-key report. The device then auto-releases any held keys.
    // On re-focus, no phantom presses should appear.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0x04, true);
    await new Promise(r => setTimeout(r, 100));

    // Blur triggers resetKeyboardState — cancels keepalives, sends zero-key report
    await sharedPage.evaluate(() => window.dispatchEvent(new Event("blur")));

    // Wait for device-side auto-release (100ms timer + network margin)
    await new Promise(r => setTimeout(r, 400));

    const events = await agent!.getKeyboardEvents();
    const releases = events.filter(ev => ev.code === KEY.A && ev.type === "key_release");
    expect(releases.length, "Key A should auto-release after blur").toBeGreaterThanOrEqual(1);

    // Re-focus and verify no phantom key events
    await agent!.clearKeyboardEvents();
    await sharedPage.evaluate(() => window.dispatchEvent(new Event("focus")));
    await new Promise(r => setTimeout(r, 200));

    const focusEvents = await agent!.getKeyboardEvents();
    const stuckPresses = focusEvents.filter(ev => ev.type === "key_press");
    expect(stuckPresses.length, "No stuck keys after re-focus").toBe(0);
  });

  test("keepalive: window blur clears lone modifier", async () => {
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe1, true);
    await expect
      .poll(
        async () => {
          const state = await getKeysDownState(sharedPage);
          return state?.modifier === 0x02;
        },
        {
          message: "LeftShift should be held before blur",
          timeout: 5000,
          intervals: [100, 200, 500],
        },
      )
      .toBe(true);

    await sharedPage.evaluate(() => {
      const target = globalThis as typeof globalThis & {
        dispatchEvent: (event: Event) => boolean;
      };
      target.dispatchEvent(new Event("blur"));
    });

    await expect
      .poll(
        async () => {
          const state = await getKeysDownState(sharedPage);
          const events = await agent!.getKeyboardEvents();
          return (
            state?.modifier === 0 &&
            state.keys.every((k: number) => k === 0) &&
            events.some(ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release")
          );
        },
        {
          message: "Window blur should clear LeftShift",
          timeout: 5000,
          intervals: [100, 200, 500],
        },
      )
      .toBe(true);
  });

  test("keepalive: arrow key held for 500ms is not prematurely released", async () => {
    // Regression: arrow keys would release intermittently during hold when browser
    // setInterval jitter exceeded the old tolerance window.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0x4f, true); // HID Right Arrow
    await new Promise(r => setTimeout(r, 500));
    await sendKeypress(sharedPage, 0x4f, false);
    await new Promise(r => setTimeout(r, 150));

    const events = await agent!.getKeyboardEvents();
    const presses = events.filter(ev => ev.code === KEY.RIGHT && ev.type === "key_press");
    const releases = events.filter(ev => ev.code === KEY.RIGHT && ev.type === "key_release");

    expect(presses.length, "Right arrow should have been pressed").toBeGreaterThanOrEqual(1);
    expect(releases.length, "Right arrow should have exactly one release").toBe(1);

    const holdDuration = releases[0].time_ms - presses[0].time_ms;
    expect(holdDuration, "Right arrow should be held for at least 400ms").toBeGreaterThanOrEqual(
      400,
    );
  });

  test("keepalive: modifier + key combo does not auto-release modifier", async () => {
    // Hold Shift, hold A, release A, release Shift.
    // Shift must not auto-release independently while A is held.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe1, true); // HID ShiftLeft
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0x04, true); // HID A
    await new Promise(r => setTimeout(r, 200));
    await sendKeypress(sharedPage, 0x04, false);
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0xe1, false);
    await new Promise(r => setTimeout(r, 150));

    const events = await agent!.getKeyboardEvents();
    const shiftReleases = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    const aReleases = events.filter(ev => ev.code === KEY.A && ev.type === "key_release");

    expect(shiftReleases.length, "Shift should have exactly one release").toBe(1);
    expect(aReleases.length, "A should have exactly one release").toBe(1);

    // Shift must be released after A
    expect(shiftReleases[0].time_ms).toBeGreaterThan(aReleases[0].time_ms);
  });

  test("keepalive: modifier held while tapping multiple keys (Shift+10 chars over 10s)", async () => {
    // Reproduces https://github.com/jetkvm/kvm/issues/1386
    // Hold Shift and tap 10 letter keys with ~1s inter-key delays (~10s total).
    // The modifier must remain held throughout all letter presses.
    test.setTimeout(30_000);
    await agent!.clearKeyboardEvents();

    // HID codes for A-J (0x04-0x0D)
    const letters = [0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d];

    await sendKeypress(sharedPage, 0xe1, true); // Shift down
    await new Promise(r => setTimeout(r, 500));

    for (const hid of letters) {
      await sendKeypress(sharedPage, hid, true);
      await new Promise(r => setTimeout(r, 50));
      await sendKeypress(sharedPage, hid, false);
      await new Promise(r => setTimeout(r, 950)); // ~1s between keys
    }

    await sendKeypress(sharedPage, 0xe1, false); // Shift up
    await new Promise(r => setTimeout(r, 300));

    const events = await agent!.getKeyboardEvents();

    const shiftPresses = events.filter(ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_press");
    const shiftReleases = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    expect(shiftPresses.length, "Shift should have exactly 1 press").toBe(1);
    expect(
      shiftReleases.length,
      "Shift should have exactly 1 release (no premature auto-release)",
    ).toBe(1);

    // All 10 letter keys should have been pressed
    const expectedLinux: number[] = [
      KEY.A,
      KEY.B,
      KEY.C,
      KEY.D,
      KEY.E,
      KEY.F,
      KEY.G,
      KEY.H,
      KEY.I,
      KEY.J,
    ];
    for (const code of expectedLinux) {
      const presses = events.filter(ev => ev.code === code && ev.type === "key_press");
      expect(presses.length, `Key ${code} should have been pressed`).toBeGreaterThanOrEqual(1);
    }

    // Shift release must come after the last letter press
    const lastLetterPress = Math.max(
      ...events
        .filter(ev => expectedLinux.includes(ev.code) && ev.type === "key_press")
        .map(ev => ev.time_ms),
    );
    expect(
      shiftReleases[0].time_ms,
      "Shift release must come after all letter key presses",
    ).toBeGreaterThan(lastLetterPress);
  });

  test("keepalive: modifier held with key-repeat simulation", async () => {
    // Simulates browser key-repeat: Shift held, then rapid repeated A presses
    // without releases (mimicking how browsers fire keydown with repeat=true).
    // Key-repeat must not starve the keepalive and cause modifier auto-release.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe1, true); // Shift down
    await new Promise(r => setTimeout(r, 50));

    // Simulate key-repeat: 20 rapid A presses at ~30ms (no releases between)
    await sharedPage.evaluate(async () => {
      const hooks = window.__kvmTestHooks;
      if (!hooks) throw new Error("Test hooks not available");
      for (let i = 0; i < 20; i++) {
        hooks.sendKeypress(0x04, true); // repeated A press
        await new Promise(r => setTimeout(r, 30));
      }
    });

    await sendKeypress(sharedPage, 0x04, false);
    await new Promise(r => setTimeout(r, 200));
    await sendKeypress(sharedPage, 0xe1, false);
    await new Promise(r => setTimeout(r, 200));

    const events = await agent!.getKeyboardEvents();
    const shiftReleases = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    expect(shiftReleases.length, "Shift should have exactly 1 release").toBe(1);
  });

  test("keepalive: modifier held across rapid tap burst", async () => {
    // Hold Shift, tap 20 keys at ~50ms spacing. Shift must survive throughout.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe1, true);
    await new Promise(r => setTimeout(r, 50));

    await sharedPage.evaluate(async () => {
      const hooks = window.__kvmTestHooks;
      if (!hooks) throw new Error("Test hooks not available");
      for (let i = 0; i < 20; i++) {
        const hid = 0x04 + (i % 10); // cycle A-J
        hooks.sendKeypress(hid, true);
        await new Promise(r => setTimeout(r, 20));
        hooks.sendKeypress(hid, false);
        await new Promise(r => setTimeout(r, 30));
      }
    });

    await sendKeypress(sharedPage, 0xe1, false);
    await new Promise(r => setTimeout(r, 200));

    const events = await agent!.getKeyboardEvents();
    const shiftReleases = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    expect(shiftReleases.length, "Shift should have exactly 1 release").toBe(1);
  });

  test("keepalive: multiple simultaneous modifiers + key", async () => {
    // Hold Ctrl+Shift, tap A, release both. Neither modifier should auto-release.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe0, true); // Ctrl
    await sendKeypress(sharedPage, 0xe1, true); // Shift
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0x04, true); // A
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0x04, false);
    await new Promise(r => setTimeout(r, 200));
    await sendKeypress(sharedPage, 0xe1, false);
    await sendKeypress(sharedPage, 0xe0, false);
    await new Promise(r => setTimeout(r, 200));

    const events = await agent!.getKeyboardEvents();
    for (const [code, label] of [
      [KEY.LEFT_CTRL, "Ctrl"],
      [KEY.LEFT_SHIFT, "Shift"],
    ] as const) {
      const releases = events.filter(ev => ev.code === code && ev.type === "key_release");
      expect(releases.length, `${label} should have exactly 1 release`).toBe(1);
    }
    const aPresses = events.filter(ev => ev.code === KEY.A && ev.type === "key_press");
    expect(aPresses.length, "A should have been pressed").toBeGreaterThanOrEqual(1);
  });

  test("keepalive: modifier released before non-modifier (reversed release order)", async () => {
    // Shift down, A down, Shift up (while A still held), A up.
    // Tests that releasing the modifier first doesn't corrupt the key buffer.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe1, true); // Shift
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0x04, true); // A
    await new Promise(r => setTimeout(r, 100));
    await sendKeypress(sharedPage, 0xe1, false); // Shift up first
    await new Promise(r => setTimeout(r, 100));
    await sendKeypress(sharedPage, 0x04, false); // A up
    await new Promise(r => setTimeout(r, 200));

    const events = await agent!.getKeyboardEvents();
    const shiftRelease = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    const aRelease = events.filter(ev => ev.code === KEY.A && ev.type === "key_release");

    expect(shiftRelease.length, "Shift should have exactly 1 release").toBe(1);
    expect(aRelease.length, "A should have exactly 1 release").toBe(1);
    // Shift released before A
    expect(shiftRelease[0].time_ms).toBeLessThan(aRelease[0].time_ms);
  });

  test("keepalive: AltGr (AltRight) held while tapping key", async () => {
    // Hold AltRight (AltGr on international layouts), tap A.
    // AltRight must not auto-release prematurely.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe6, true); // AltRight
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0x04, true); // A
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0x04, false);
    await new Promise(r => setTimeout(r, 200));
    await sendKeypress(sharedPage, 0xe6, false);
    await new Promise(r => setTimeout(r, 200));

    const events = await agent!.getKeyboardEvents();
    const altReleases = events.filter(ev => ev.code === KEY.RIGHT_ALT && ev.type === "key_release");
    expect(altReleases.length, "AltRight should have exactly 1 release").toBe(1);
  });

  test("keepalive: multiple simultaneous keys held for 300ms", async () => {
    // Hold A + B + C simultaneously, wait 300ms, release all.
    // Each key should get exactly 1 release — tests per-key timer independence.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0x04, true); // A
    await sendKeypress(sharedPage, 0x05, true); // B
    await sendKeypress(sharedPage, 0x06, true); // C
    await new Promise(r => setTimeout(r, 300));
    await sendKeypress(sharedPage, 0x04, false);
    await sendKeypress(sharedPage, 0x05, false);
    await sendKeypress(sharedPage, 0x06, false);
    await new Promise(r => setTimeout(r, 150));

    const events = await agent!.getKeyboardEvents();
    for (const [code, label] of [
      [KEY.A, "A"],
      [KEY.B, "B"],
      [KEY.C, "C"],
    ] as const) {
      const presses = events.filter(ev => ev.code === code && ev.type === "key_press");
      const releases = events.filter(ev => ev.code === code && ev.type === "key_release");
      expect(presses.length, `${label} should have at least 1 press`).toBeGreaterThanOrEqual(1);
      expect(releases.length, `${label} should have exactly 1 release`).toBe(1);
    }
  });

  test("keepalive: rapid press/release cycles produce no phantom releases", async () => {
    // Tap a key 20 times fast (~30ms apart). Should get exactly 20 press + 20 release.
    const TAP_COUNT = 20;
    await agent!.clearKeyboardEvents();

    await sharedPage.evaluate(async (count: number) => {
      const hooks = window.__kvmTestHooks;
      if (!hooks) throw new Error("Test hooks not available");
      for (let i = 0; i < count; i++) {
        hooks.sendKeypress(0x04, true);
        await new Promise(r => setTimeout(r, 10));
        hooks.sendKeypress(0x04, false);
        await new Promise(r => setTimeout(r, 20));
      }
    }, TAP_COUNT);

    await new Promise(r => setTimeout(r, 500));

    const events = await agent!.getKeyboardEvents();
    const presses = events.filter(ev => ev.code === KEY.A && ev.type === "key_press");
    const releases = events.filter(ev => ev.code === KEY.A && ev.type === "key_release");

    expect(presses.length, `Should have ${TAP_COUNT} presses`).toBe(TAP_COUNT);
    expect(releases.length, `Should have ${TAP_COUNT} releases (no phantom releases)`).toBe(
      TAP_COUNT,
    );
  });

  test("keepalive: long hold (2s) stays held with keepalives", async () => {
    // Real-world scenario: holding Backspace to delete a line, or holding an arrow
    // key to scroll through code. The key must stay held for the full 2s.
    // Keepalives arrive every 50ms, each extending the 100ms auto-release timer.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0x2a, true); // HID Backspace
    await new Promise(r => setTimeout(r, 2000));
    await sendKeypress(sharedPage, 0x2a, false);
    await new Promise(r => setTimeout(r, 150));

    const events = await agent!.getKeyboardEvents();
    const presses = events.filter(ev => ev.code === KEY.BACKSPACE && ev.type === "key_press");
    const releases = events.filter(ev => ev.code === KEY.BACKSPACE && ev.type === "key_release");

    expect(presses.length, "Backspace should have been pressed").toBeGreaterThanOrEqual(1);
    expect(releases.length, "Backspace should have exactly one release").toBe(1);

    const holdDuration = releases[0].time_ms - presses[0].time_ms;
    expect(holdDuration, "Backspace should be held for at least 1800ms").toBeGreaterThanOrEqual(
      1800,
    );
  });

  // ═══════════════════════════════════════════
  // KEYBOARD: ISSUE #1428 REGRESSION TESTS
  // ═══════════════════════════════════════════

  test("regression #1428: modifier survives induced keepalive gap mid-chord", async () => {
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe1, true); // Shift down
    await new Promise(r => setTimeout(r, 80));

    // Exceeds the per-key auto-release deadline but stays below the session
    // replacement/ICE cleanup paths.
    await pauseKeepAlive(sharedPage, 250);

    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0x04, true); // A down
    await new Promise(r => setTimeout(r, 30));
    await sendKeypress(sharedPage, 0x04, false); // A up

    await new Promise(r => setTimeout(r, 250));

    const events = await agent!.getKeyboardEvents();
    const shiftPressesPreRelease = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_press",
    );
    const shiftReleasesPreRelease = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    expect(shiftPressesPreRelease.length, "Shift pressed exactly once").toBe(1);
    expect(
      shiftReleasesPreRelease.length,
      "Shift must NOT have been auto-released during the keepalive gap (#1428)",
    ).toBe(0);

    const aPresses = events.filter(ev => ev.code === KEY.A && ev.type === "key_press");
    expect(aPresses.length, "A should have been pressed inside the gap").toBeGreaterThanOrEqual(1);
    expect(aPresses[0].time_ms).toBeGreaterThan(shiftPressesPreRelease[0].time_ms);

    await sendKeypress(sharedPage, 0xe1, false);
    await new Promise(r => setTimeout(r, 200));

    const finalEvents = await agent!.getKeyboardEvents();
    const shiftReleases = finalEvents.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    expect(shiftReleases.length, "Shift should have exactly 1 release (the explicit one)").toBe(1);
  });

  test("regression #1428: auto-released key under modifier does not poison next chord", async () => {
    test.setTimeout(15_000);
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe1, true); // Shift down
    await new Promise(r => setTimeout(r, 80));
    await sendKeypress(sharedPage, 0x04, true); // A down
    await new Promise(r => setTimeout(r, 80));
    await pauseKeepAlive(sharedPage, 5000);
    await new Promise(r => setTimeout(r, 300));

    await expect
      .poll(
        async () => {
          const state = await getKeysDownState(sharedPage);
          return state?.modifier === 0x02 && state.keys.every((k: number) => k === 0);
        },
        {
          message: "A should auto-release while LeftShift remains held",
          timeout: 5000,
          intervals: [100, 200, 500],
        },
      )
      .toBe(true);

    await new Promise(r => setTimeout(r, 3000));

    await agent!.clearKeyboardEvents();
    const bPressStart = Date.now();
    await sendKeypress(sharedPage, 0x05, true); // B down

    for (const t of [50, 150, 300, 450]) {
      await new Promise(r => setTimeout(r, Math.max(0, t - (Date.now() - bPressStart))));
      const state = await getKeysDownState(sharedPage);
      expect(
        state?.modifier === 0x02 && state.keys.includes(0x05),
        `B should still be held with LeftShift at t+${t}ms`,
      ).toBe(true);
    }

    await sendKeypress(sharedPage, 0x05, false);
    await sendKeypress(sharedPage, 0x04, false);
    await sendKeypress(sharedPage, 0xe1, false);
    await new Promise(r => setTimeout(r, 200));

    const events = await agent!.getKeyboardEvents();
    const bPresses = events.filter(ev => ev.code === KEY.B && ev.type === "key_press");
    const bReleases = events.filter(ev => ev.code === KEY.B && ev.type === "key_release");
    const shiftReleases = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );

    expect(bPresses.length, "B pressed exactly once").toBe(1);
    expect(bReleases.length, "B should have exactly one release").toBe(1);
    expect(shiftReleases.length, "LeftShift should release only explicitly").toBe(1);

    const holdDuration = bReleases[0].time_ms - bPresses[0].time_ms;
    expect(
      holdDuration,
      "B should not auto-release at ~100ms under LeftShift",
    ).toBeGreaterThanOrEqual(400);
  });

  test("regression #1428: lone-modifier hold does not poison next hold's auto-release", async () => {
    test.setTimeout(15_000);
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe1, true);
    await new Promise(r => setTimeout(r, 1000));
    await sendKeypress(sharedPage, 0xe1, false);

    await new Promise(r => setTimeout(r, 3000));

    await agent!.clearKeyboardEvents();
    const aPressStart = Date.now();
    await sendKeypress(sharedPage, 0x04, true);

    for (const t of [50, 150, 300, 450]) {
      await new Promise(r => setTimeout(r, t - (Date.now() - aPressStart)));
      const state = await getKeysDownState(sharedPage);
      expect(
        state?.keys?.includes(0x04) ?? false,
        `'a' should still be held in keysDownState at t+${t}ms (cross-hold reset working)`,
      ).toBe(true);
    }

    await sendKeypress(sharedPage, 0x04, false);
    await new Promise(r => setTimeout(r, 200));

    const events = await agent!.getKeyboardEvents();
    const aPresses = events.filter(ev => ev.code === KEY.A && ev.type === "key_press");
    const aReleases = events.filter(ev => ev.code === KEY.A && ev.type === "key_release");
    expect(aPresses.length, "A pressed exactly once").toBe(1);
    expect(aReleases.length, "A should have exactly one release (no premature auto-release)").toBe(
      1,
    );

    const holdDuration = aReleases[0].time_ms - aPresses[0].time_ms;
    expect(
      holdDuration,
      "A should be held for ~500ms — premature release at ~100ms means cross-hold reset is broken",
    ).toBeGreaterThanOrEqual(400);
  });

  test("regression #1428: auto-released key does not poison next hold's auto-release", async () => {
    test.setTimeout(15_000);
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0x04, true);
    await new Promise(r => setTimeout(r, 80));
    await pauseKeepAlive(sharedPage, 5000);
    await new Promise(r => setTimeout(r, 300));

    await expect
      .poll(
        async () => {
          const state = await getKeysDownState(sharedPage);
          return state?.modifier === 0 && state.keys.every((k: number) => k === 0);
        },
        {
          message: "A should auto-release to an empty device state",
          timeout: 5000,
          intervals: [100, 200, 500],
        },
      )
      .toBe(true);

    await new Promise(r => setTimeout(r, 3000));

    await agent!.clearKeyboardEvents();
    const bPressStart = Date.now();
    await sendKeypress(sharedPage, 0x05, true);

    // Keepalive timing must reset when auto-release empties the keyboard state.
    for (const t of [50, 150, 300, 450]) {
      await new Promise(r => setTimeout(r, Math.max(0, t - (Date.now() - bPressStart))));
      const state = await getKeysDownState(sharedPage);
      expect(
        state?.keys?.includes(0x05) ?? false,
        `B should still be held in keysDownState at t+${t}ms after auto-release reset`,
      ).toBe(true);
    }

    await sendKeypress(sharedPage, 0x05, false);
    await sendKeypress(sharedPage, 0x04, false);
    await new Promise(r => setTimeout(r, 200));

    const events = await agent!.getKeyboardEvents();
    const bPresses = events.filter(ev => ev.code === KEY.B && ev.type === "key_press");
    const bReleases = events.filter(ev => ev.code === KEY.B && ev.type === "key_release");
    expect(bPresses.length, "B pressed exactly once").toBe(1);
    expect(bReleases.length, "B should have exactly one release").toBe(1);

    const holdDuration = bReleases[0].time_ms - bPresses[0].time_ms;
    expect(
      holdDuration,
      "B should be held for ~500ms, not auto-release at ~100ms from stale jitter state",
    ).toBeGreaterThanOrEqual(400);
  });

  // ═══════════════════════════════════════════
  // KEYBOARD: KEYS RELEASED ON DISCONNECT
  // ═══════════════════════════════════════════

  test("keyboard: all keys released when WebRTC session disconnects", async ({ browser }) => {
    test.setTimeout(30_000);

    const freshPage = await browser.newPage();
    await freshPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(freshPage);

    const readyEvents = await waitForKeyboardReady(agent!, freshPage, 15000);
    expect(readyEvents.length, "keyboard should work before disconnect test").toBeGreaterThan(0);

    await agent!.clearKeyboardEvents();

    await sendKeypress(freshPage, 0xe1, true);
    await new Promise(r => setTimeout(r, 20));
    await sendKeypress(freshPage, HID_KEY.SPACE, true);

    await expect
      .poll(
        async () => {
          const state = await getKeysDownState(freshPage);
          return state?.modifier === 0x02 && state.keys.includes(HID_KEY.SPACE);
        },
        {
          message: "LeftShift and Space should be held before disconnect",
          timeout: 5000,
          intervals: [100, 200, 500],
        },
      )
      .toBe(true);

    await expect
      .poll(
        async () => {
          const events = await agent!.getKeyboardEvents();
          return (
            events.some(ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_press") &&
            events.some(ev => ev.code === KEY.SPACE && ev.type === "key_press")
          );
        },
        {
          message: "Host should see LeftShift and Space presses",
          timeout: 5000,
          intervals: [100, 200, 500],
        },
      )
      .toBe(true);

    // Close the peer directly so browser blur/page-unload cleanup cannot satisfy the test.
    await freshPage.evaluate(() => {
      const peerConnection = (
        globalThis as typeof globalThis & {
          __kvmTestHooks?: { _getPeerConnection?: () => { close: () => void } | null };
        }
      ).__kvmTestHooks?._getPeerConnection?.();
      if (!peerConnection) throw new Error("Peer connection not available");
      peerConnection.close();
    });

    await expect
      .poll(
        async () => {
          const events = await agent!.getKeyboardEvents();
          return (
            events.some(ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release") &&
            events.some(ev => ev.code === KEY.SPACE && ev.type === "key_release")
          );
        },
        {
          message: "Host should see LeftShift and Space releases after disconnect",
          timeout: 5000,
          intervals: [100, 200, 500],
        },
      )
      .toBe(true);

    await freshPage.close();

    await sharedPage.goto("/", { waitUntil: "networkidle" });
    await ensureRpcReady(sharedPage);

    await expect
      .poll(
        async () => {
          const s = (await callJsonRpc(sharedPage, "getKeyDownState")) as {
            modifier: number;
            keys: number[];
          };
          return s.modifier === 0 && s.keys.every((k: number) => k === 0);
        },
        {
          message: "All key slots should be clear after disconnect",
          timeout: 5000,
          intervals: [200, 500],
        },
      )
      .toBe(true);
  });

  test("keyboard: held modifier released when WebRTC session is replaced", async ({ browser }) => {
    test.setTimeout(30_000);

    const oldPage = await browser.newPage();
    let replacementPage: Page | null = null;

    try {
      await oldPage.goto("/", { waitUntil: "networkidle" });
      await ensureRpcReady(oldPage);

      await agent!.clearKeyboardEvents();
      await callJsonRpc(oldPage, "keypressReport", { key: 0xe1, press: true });

      await expect
        .poll(
          async () => {
            const s = (await callJsonRpc(oldPage, "getKeyDownState")) as {
              modifier: number;
              keys: number[];
            };
            return s.modifier === 0x02 && s.keys.every((k: number) => k === 0);
          },
          {
            message: "Old session should hold LeftShift before replacement",
            timeout: 5000,
            intervals: [100, 200, 500],
          },
        )
        .toBe(true);

      replacementPage = await browser.newPage();
      await replacementPage.goto("/", { waitUntil: "networkidle" });
      await ensureRpcReady(replacementPage);

      await expect
        .poll(
          async () => {
            const events = await agent!.getKeyboardEvents();
            return events.some(ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release");
          },
          {
            message: "Replacing the session should release LeftShift",
            timeout: 5000,
            intervals: [200, 500],
          },
        )
        .toBe(true);

      await expect
        .poll(
          async () => {
            const s = (await callJsonRpc(replacementPage!, "getKeyDownState")) as {
              modifier: number;
              keys: number[];
            };
            return s.modifier === 0 && s.keys.every((k: number) => k === 0);
          },
          {
            message: "Keyboard state should be clear after session replacement",
            timeout: 5000,
            intervals: [200, 500],
          },
        )
        .toBe(true);
    } finally {
      if (replacementPage) {
        await callJsonRpc(replacementPage, "keypressReport", { key: 0xe1, press: false }).catch(
          () => {},
        );
        await replacementPage.close().catch(() => {});
      }
      await oldPage.close().catch(() => {});

      await sharedPage.goto("/", { waitUntil: "networkidle" });
      await ensureRpcReady(sharedPage);
    }
  });

  // ═══════════════════════════════════════════
  // INPUT: MACROS
  // ═══════════════════════════════════════════

  test("input: keyboard macros", async () => {
    test.setTimeout(30_000);

    // Single key press (A) — retry in case the remote agent is still
    // re-opening input devices after the previous USB mode switch.
    const keyABtn = sharedPage.getByRole("button", { name: "E2E KeyA" });
    await keyABtn.waitFor({ state: "visible", timeout: 5000 });

    const macroDeadline = Date.now() + 15000;
    let macroEvents: RAKeyboardEvent[] = [];
    while (Date.now() < macroDeadline) {
      await agent!.clearKeyboardEvents();
      await keyABtn.click();
      try {
        macroEvents = await agent!.waitForKeyboardEvent(
          ev => ev.code === KEY.A && ev.type === "key_press",
          3000,
        );
        break;
      } catch {
        /* agent not ready, retry */
      }
    }
    expect(macroEvents.length).toBeGreaterThan(0);

    // Modifier combo (Ctrl+A)
    await agent!.clearKeyboardEvents();
    await sharedPage.getByRole("button", { name: "E2E Ctrl+A" }).click();

    const ctrlDeadline = Date.now() + 3000;
    let gotCtrl = false,
      gotA = false;
    while (Date.now() < ctrlDeadline && (!gotCtrl || !gotA)) {
      macroEvents = (await agent!.getKeyboardEvents()).filter(ev => ev.type === "key_press");
      for (const ev of macroEvents) {
        if (ev.code === KEY.LEFT_CTRL) gotCtrl = true;
        if (ev.code === KEY.A) gotA = true;
      }
      if (!gotCtrl || !gotA) await new Promise(r => setTimeout(r, 50));
    }
    expect(gotCtrl, "Ctrl key should arrive").toBe(true);
    expect(gotA, "A key should arrive").toBe(true);

    // Key sequence (A, B, C)
    await agent!.clearKeyboardEvents();
    await sharedPage.getByRole("button", { name: "E2E ABC" }).click();

    const expectedSeq = [KEY.A, KEY.B, KEY.C];
    const seqDeadline = Date.now() + 3000;
    let matched = false;
    while (Date.now() < seqDeadline && !matched) {
      const seqEvents = await agent!.getKeyboardEvents();
      const presses = seqEvents.filter(ev => ev.type === "key_press").map(ev => ev.code);
      let idx = 0;
      for (const code of presses) {
        if (code === expectedSeq[idx]) {
          idx++;
          if (idx === expectedSeq.length) {
            matched = true;
            break;
          }
        }
      }
      if (!matched) await new Promise(r => setTimeout(r, 50));
    }
    expect(matched, "Keys A, B, C should arrive in order").toBe(true);
  });
});
