import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  waitForVideoStream,
  getLedState,
  tapKey,
  waitForLedState,
  HID_KEY,
  type KeyboardLedState,
} from "./helpers";

// Parameterized test data for LED round-trip tests
const LED_TESTS = [
  { name: "CAPS_LOCK", key: HID_KEY.CAPS_LOCK, led: "caps_lock" as keyof KeyboardLedState },
  { name: "NUM_LOCK", key: HID_KEY.NUM_LOCK, led: "num_lock" as keyof KeyboardLedState },
] as const;

test.describe("LED Round-Trip Tests", () => {
  test.beforeEach(async ({ page }) => {
    // Navigate to the device page (on-device mode uses "/" as the device route)
    await page.goto("/");

    // Wait for WebRTC connection to be established
    await waitForWebRTCReady(page);
  });

  for (const { name, key, led } of LED_TESTS) {
    test(`${name} round-trip toggles LED state`, async ({ page }) => {
      // Get initial state
      const initialState = await getLedState(page);
      expect(initialState).not.toBeNull();
      const initialValue = initialState![led];

      // Toggle and verify
      await tapKey(page, key);
      await waitForLedState(page, led, !initialValue);
      expect((await getLedState(page))![led]).toBe(!initialValue);

      // Restore and verify
      await tapKey(page, key);
      await waitForLedState(page, led, initialValue);
      expect((await getLedState(page))![led]).toBe(initialValue);
    });
  }

  test("video stream is active", async ({ page }) => {
    // Send a few SPACE keys to wake display if screensaver/sleep is active
    for (let i = 0; i < 3; i++) {
      await tapKey(page, HID_KEY.SPACE);
      await page.waitForTimeout(200);
    }

    await waitForVideoStream(page);
    const isActive = await page.evaluate(() => window.__kvmTestHooks?.isVideoStreamActive());
    expect(isActive).toBe(true);
  });
});
