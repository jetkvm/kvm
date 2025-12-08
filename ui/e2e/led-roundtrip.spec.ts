import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  getLedState,
  tapKey,
  waitForLedState,
  HID_KEY,
} from "./helpers";

test.describe("LED Round-Trip Tests", () => {
  test.beforeEach(async ({ page }) => {
    // Navigate to the device page (on-device mode uses "/" as the device route)
    await page.goto("/");

    // Wait for WebRTC connection to be established
    await waitForWebRTCReady(page);
  });

  test("CAPS_LOCK round-trip toggles LED state", async ({ page }) => {
    // Get initial CAPS_LOCK state
    const initialState = await getLedState(page);
    expect(initialState).not.toBeNull();
    const initialCapsLock = initialState!.caps_lock;

    console.log(`Initial CAPS_LOCK state: ${initialCapsLock}`);

    // Send CAPS_LOCK key tap
    await tapKey(page, HID_KEY.CAPS_LOCK);

    // Wait for the LED state to toggle
    await waitForLedState(page, "caps_lock", !initialCapsLock);

    // Verify the state changed
    const newState = await getLedState(page);
    expect(newState).not.toBeNull();
    expect(newState!.caps_lock).toBe(!initialCapsLock);

    console.log(`New CAPS_LOCK state: ${newState!.caps_lock}`);

    // Restore original state by tapping again
    await tapKey(page, HID_KEY.CAPS_LOCK);
    await waitForLedState(page, "caps_lock", initialCapsLock);

    // Verify we're back to original
    const restoredState = await getLedState(page);
    expect(restoredState!.caps_lock).toBe(initialCapsLock);
  });

  test("NUM_LOCK round-trip toggles LED state", async ({ page }) => {
    // Get initial NUM_LOCK state
    const initialState = await getLedState(page);
    expect(initialState).not.toBeNull();
    const initialNumLock = initialState!.num_lock;

    console.log(`Initial NUM_LOCK state: ${initialNumLock}`);

    // Send NUM_LOCK key tap
    await tapKey(page, HID_KEY.NUM_LOCK);

    // Wait for the LED state to toggle
    await waitForLedState(page, "num_lock", !initialNumLock);

    // Verify the state changed
    const newState = await getLedState(page);
    expect(newState).not.toBeNull();
    expect(newState!.num_lock).toBe(!initialNumLock);

    console.log(`New NUM_LOCK state: ${newState!.num_lock}`);

    // Restore original state by tapping again
    await tapKey(page, HID_KEY.NUM_LOCK);
    await waitForLedState(page, "num_lock", initialNumLock);

    // Verify we're back to original
    const restoredState = await getLedState(page);
    expect(restoredState!.num_lock).toBe(initialNumLock);
  });
});
