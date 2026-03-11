import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  waitForVideoStream,
  wakeDisplay,
  runMouseBidirectionalCheck,
  ensureLocalAuthMode,
} from "./helpers";

// HID absolute coordinate range is 0-32767
const HID_MAX = 32767;
const HID_CENTER = Math.floor(HID_MAX / 2); // 16383

test.describe("Mouse Round-Trip Tests", () => {
  // Ensure device is in noPassword mode before tests
  // This handles cases where previous tests left the device with password protection
  test.beforeAll(async ({ browser }) => {
    const baseURL = process.env.JETKVM_URL;
    const context = await browser.newContext({ baseURL });
    const page = await context.newPage();
    try {
      await ensureLocalAuthMode(page, { mode: "noPassword" });
    } finally {
      await page.close();
      await context.close();
    }
  });

  test.beforeEach(async ({ page }) => {
    // Navigate to the device page (on-device mode uses "/" as the device route)
    await page.goto("/");

    // Wait for WebRTC connection to be established
    await waitForWebRTCReady(page);
  });

  test("mouse movement changes video at cursor position", async ({ page }) => {
    // Wake display if screensaver/sleep is active
    await wakeDisplay(page);
    await waitForVideoStream(page);

    const { arrive: distance, restore } = await runMouseBidirectionalCheck(page, {
      retries: 3,
      threshold: 10,
      testHidX: HID_CENTER,
      testHidY: HID_CENTER,
    });

    expect(
      distance,
      `Cursor movement should cause significant visual change (distance=${distance}, expected >10) — mouse HID path may be broken`,
    ).toBeGreaterThan(10);
    expect(
      restore,
      `Region should restore after cursor leaves (distRestore=${restore} should be < distArrive=${distance}) — cursor may not have moved away`,
    ).toBeLessThan(distance);
  });

  test("mouse movement is bidirectionally verifiable", async ({ page }) => {
    // Wake display if screensaver/sleep is active
    await wakeDisplay(page);
    await waitForVideoStream(page);

    const { arrive, restore } = await runMouseBidirectionalCheck(page, {
      retries: 1,
      threshold: 10,
    });

    expect(
      arrive,
      `Cursor arrival should cause significant visual change (distArrive=${arrive}, expected >10) — mouse HID path may be broken`,
    ).toBeGreaterThan(10);
    expect(
      restore,
      `Region should restore after cursor leaves (distRestore=${restore} should be < distArrive=${arrive}) — cursor may not have moved away`,
    ).toBeLessThan(arrive);
  });
});
