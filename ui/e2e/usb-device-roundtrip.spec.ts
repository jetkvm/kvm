import { test, expect } from "@playwright/test";
import type { Page } from "@playwright/test";

import {
  waitForWebRTCReady,
  waitForVideoStream,
  wakeDisplay,
  sendAbsMouseMove,
  getVideoStreamDimensions,
  captureVideoRegionFingerprint,
  fingerprintDistance,
} from "./helpers";

// Region size for cursor detection (pixels around the expected cursor position)
const CAPTURE_REGION_SIZE = 80;

/** Locate the USB device preset dropdown */
function getUsbDropdown(page: Page) {
  return page.locator("select").filter({
    has: page.locator('option[value="keyboard_only"]'),
  });
}

// HID absolute coordinate range is 0-32767
const HID_MAX = 32767;

// Time to wait for USB config change to apply (ms)
const USB_CONFIG_TIMEOUT = 10000;

// USB preset values (from UsbDeviceSetting.tsx)
const USB_PRESET_DEFAULT = "default"; // Keyboard, Mouse and Storage
const USB_PRESET_KEYBOARD_ONLY = "keyboard_only";

test.describe("USB Device Round-Trip Tests", () => {
  test.setTimeout(120000); // 2 minutes

  // Always restore USB to default mode after tests complete (for subsequent test runs)
  test.afterAll(async ({ browser }) => {
    const page = await browser.newPage();
    try {
      await page.goto("/settings/hardware");
      await page.waitForLoadState("networkidle");
      const usbDropdown = getUsbDropdown(page);
      if (await usbDropdown.isVisible({ timeout: 5000 })) {
        await usbDropdown.selectOption(USB_PRESET_DEFAULT);
        await page.waitForTimeout(3000); // Wait for USB reconfiguration
      } else {
        console.warn("[USB cleanup] USB dropdown not visible, skipping restoration");
      }
    } finally {
      await page.close();
    }
  });

  test("mouse is disabled when USB preset is keyboard-only", async ({ page }) => {
    // First, go to main page and set up WebRTC
    await page.goto("/");
    await waitForWebRTCReady(page);
    await wakeDisplay(page);
    await waitForVideoStream(page);

    // Get video dimensions for fingerprint capture
    const dimensions = await getVideoStreamDimensions(page);
    expect(dimensions, "Video dimensions should be available").not.toBeNull();
    const { width: videoWidth, height: videoHeight } = dimensions!;

    // Calculate a test region in the center of the screen
    const centerX = Math.floor(videoWidth / 2);
    const centerY = Math.floor(videoHeight / 2);
    const regionX = Math.max(0, centerX - CAPTURE_REGION_SIZE / 2);
    const regionY = Math.max(0, centerY - CAPTURE_REGION_SIZE / 2);
    const regionWidth = Math.min(CAPTURE_REGION_SIZE, videoWidth - regionX);
    const regionHeight = Math.min(CAPTURE_REGION_SIZE, videoHeight - regionY);

    // Define HID coordinates for test positions
    const hidCenter = Math.floor(HID_MAX / 2);
    const hidCorner = 0;

    // === Step 1: Switch to Keyboard Only mode ===
    await page.goto("/settings/hardware");
    await page.waitForLoadState("networkidle");

    // Find the USB device preset dropdown
    const usbDropdown = getUsbDropdown(page);
    await expect(usbDropdown).toBeVisible({ timeout: 10000 });
    await expect(usbDropdown).toBeEnabled({ timeout: 10000 });

    // Select keyboard-only preset
    await usbDropdown.selectOption(USB_PRESET_KEYBOARD_ONLY);
    await page.waitForTimeout(500);
    await expect(usbDropdown).toBeEnabled({ timeout: USB_CONFIG_TIMEOUT });

    // === Step 2: Verify mouse does NOT work ===
    await page.goto("/");
    await waitForWebRTCReady(page, 30000);
    await wakeDisplay(page);
    await waitForVideoStream(page, 30000);

    // Move mouse to corner first to establish baseline
    await sendAbsMouseMove(page, hidCorner, hidCorner);
    await page.waitForTimeout(200);

    // Capture baseline fingerprint
    const fpKeyboardOnlyBefore = await captureVideoRegionFingerprint(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(fpKeyboardOnlyBefore, "Failed to capture baseline fingerprint").not.toBeNull();

    // Try to move mouse to center
    await sendAbsMouseMove(page, hidCenter, hidCenter);
    await page.waitForTimeout(200);

    // Capture fingerprint after attempted move
    const fpKeyboardOnlyAfter = await captureVideoRegionFingerprint(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(fpKeyboardOnlyAfter, "Failed to capture after-move fingerprint").not.toBeNull();

    // In keyboard-only mode, the region should NOT change significantly
    // (cursor doesn't move because mouse is disabled)
    const distanceKeyboardOnly = fingerprintDistance(fpKeyboardOnlyBefore!, fpKeyboardOnlyAfter!);
    expect(
      distanceKeyboardOnly,
      `Mouse should NOT cause visual change in keyboard-only mode (distance=${distanceKeyboardOnly}, expected < 50)`,
    ).toBeLessThan(50);

    // === Step 3: Switch back to default mode (Keyboard, Mouse and Storage) ===
    await page.goto("/settings/hardware");
    await page.waitForLoadState("networkidle");

    const usbDropdown2 = getUsbDropdown(page);
    await expect(usbDropdown2).toBeVisible({ timeout: 10000 });
    await expect(usbDropdown2).toBeEnabled({ timeout: 10000 });

    // Select default preset
    await usbDropdown2.selectOption(USB_PRESET_DEFAULT);
    await page.waitForTimeout(500);
    await expect(usbDropdown2).toBeEnabled({ timeout: USB_CONFIG_TIMEOUT });

    // === Step 4: Verify mouse DOES work now ===
    await page.goto("/");
    await waitForWebRTCReady(page, 30000);
    await wakeDisplay(page);
    await waitForVideoStream(page, 30000);

    // Move mouse to corner first
    await sendAbsMouseMove(page, hidCorner, hidCorner);
    await page.waitForTimeout(200);

    // Capture baseline fingerprint
    const fpDefaultBefore = await captureVideoRegionFingerprint(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(fpDefaultBefore, "Failed to capture baseline fingerprint").not.toBeNull();

    // Move mouse to center
    await sendAbsMouseMove(page, hidCenter, hidCenter);
    await page.waitForTimeout(200);

    // Capture fingerprint after move
    const fpDefaultAfter = await captureVideoRegionFingerprint(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(fpDefaultAfter, "Failed to capture after-move fingerprint").not.toBeNull();

    // In default mode, the region SHOULD change (cursor moved)
    const distanceDefault = fingerprintDistance(fpDefaultBefore!, fpDefaultAfter!);
    expect(
      distanceDefault,
      `Mouse SHOULD cause visual change in default mode (distance=${distanceDefault}, expected > 10)`,
    ).toBeGreaterThan(10);
  });
});
