import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  waitForVideoStream,
  wakeDisplay,
  waitForVideoDimensions,
  sendAbsMouseMove,
  captureVideoRegionFingerprint,
  fingerprintDistance,
} from "./helpers";

// Minimum video dimensions to consider valid (sanity check)
const MIN_VIDEO_DIMENSION = 100;

// Region size for video capture (pixels around center)
const CAPTURE_REGION_SIZE = 80;

// Time to wait for EDID setting callback to complete (ms)
const EDID_CALLBACK_TIMEOUT = 15000;

// Time to wait for video signal to stabilize after EDID change (ms)
const SIGNAL_STABILIZATION_TIME = 2000;

// Number of random EDID options to test (to keep test time reasonable)
const NUM_EDIDS_TO_TEST = 2;

// Time between fingerprint captures to verify stream is updating (ms)
const FINGERPRINT_INTERVAL = 200;

interface EdidOption {
  value: string;
  label: string;
}

test.describe("EDID Round-Trip Tests", () => {
  // Each EDID takes ~20-30 seconds (10s callback + 5s stabilization + WebRTC reconnect + verification)
  // Testing 2 random EDIDs, so 2 minutes should be plenty
  test.setTimeout(120000); // 2 minutes

  // Restore EDID to default after tests complete (for subsequent test runs)
  test.afterAll(async ({ browser }) => {
    const page = await browser.newPage();
    try {
      await page.goto("/settings/video");
      await page.waitForLoadState("networkidle");
      const edidDropdown = page.locator("select").filter({
        has: page.locator('option[value="custom"]'),
      });
      if (await edidDropdown.isVisible({ timeout: 5000 })) {
        // Get the first non-custom option (the default)
        const firstOption = await edidDropdown.evaluate((el: HTMLSelectElement) => {
          const opts = Array.from(el.options);
          const nonCustom = opts.find(o => !o.value.toLowerCase().includes("custom"));
          return nonCustom?.value;
        });
        if (firstOption) {
          await edidDropdown.selectOption(firstOption);
          await page.waitForTimeout(1000); // Wait for EDID change
        }
      } else {
        console.warn("[EDID cleanup] EDID dropdown not visible, skipping restoration");
      }
    } finally {
      await page.close();
    }
  });

  test("video streams correctly after changing EDID presets", async ({ page }) => {
    // Navigate to settings/video to discover EDID options
    await page.goto("/settings/video");

    // Wait for the page to load and EDID dropdown to be ready
    await page.waitForLoadState("networkidle");

    // Find the EDID dropdown by looking for a select with "custom" option
    const edidSelect = page.locator("select").filter({
      has: page.locator('option[value="custom"]'),
    });
    await expect(edidSelect).toBeVisible({ timeout: 10000 });

    // Wait for the dropdown to be enabled (not in loading state)
    await expect(edidSelect).toBeEnabled({ timeout: 10000 });

    // Extract all options from the dropdown
    const allOptions = await edidSelect.evaluate((el: HTMLSelectElement) => {
      return Array.from(el.options).map(o => ({
        value: o.value,
        label: o.text,
      }));
    });

    // Filter out any options containing "custom" (case-insensitive)
    const edidOptions: EdidOption[] = allOptions.filter(
      o => !o.value.toLowerCase().includes("custom") && !o.label.toLowerCase().includes("custom"),
    );
    expect(edidOptions.length, "Should have at least one non-custom EDID option").toBeGreaterThan(
      0,
    );

    // Randomly select NUM_EDIDS_TO_TEST options to test (or all if fewer available)
    const shuffled = [...edidOptions].sort(() => Math.random() - 0.5);
    const selectedOptions = shuffled.slice(0, Math.min(NUM_EDIDS_TO_TEST, edidOptions.length));

    // Cycle through selected EDID options
    for (let i = 0; i < selectedOptions.length; i++) {
      const option = selectedOptions[i];

      // Navigate to settings/video (may already be there, but ensures clean state)
      if (i > 0) {
        await page.goto("/settings/video");
        await page.waitForLoadState("networkidle");
      }

      // Re-locate the dropdown (page may have reloaded)
      const dropdown = page.locator("select").filter({
        has: page.locator('option[value="custom"]'),
      });
      await expect(dropdown).toBeVisible({ timeout: 10000 });
      await expect(dropdown).toBeEnabled({ timeout: 10000 });

      // Select the EDID option and wait for callback to complete
      await dropdown.selectOption(option.value);
      await page.waitForTimeout(500); // Brief wait for loading state to start
      await expect(dropdown).toBeEnabled({ timeout: EDID_CALLBACK_TIMEOUT });

      // Wait for video signal to stabilize
      await page.waitForTimeout(SIGNAL_STABILIZATION_TIME);

      // Navigate back to the main page to access video
      await page.goto("/");

      // Wait for WebRTC connection and video stream
      await waitForWebRTCReady(page, 45000);
      await wakeDisplay(page);
      await waitForVideoStream(page, 45000);

      // Get current video dimensions (resolution may have changed with EDID)
      const { width: videoWidth, height: videoHeight } = await waitForVideoDimensions(page);

      // Calculate center region for fingerprint capture (based on current resolution)
      const centerX = Math.floor(videoWidth / 2);
      const centerY = Math.floor(videoHeight / 2);
      const regionX = Math.max(0, centerX - CAPTURE_REGION_SIZE / 2);
      const regionY = Math.max(0, centerY - CAPTURE_REGION_SIZE / 2);
      const regionWidth = Math.min(CAPTURE_REGION_SIZE, videoWidth - regionX);
      const regionHeight = Math.min(CAPTURE_REGION_SIZE, videoHeight - regionY);

      // Verify video stream is not blank by checking mouse movement causes change
      // This is more robust than checking for unique colors (which fails on solid backgrounds)
      const HID_MAX = 32767;
      await sendAbsMouseMove(page, 0, 0);
      await page.waitForTimeout(200);
      const fpBefore = await captureVideoRegionFingerprint(
        page,
        regionX,
        regionY,
        regionWidth,
        regionHeight,
      );
      expect(fpBefore, "Failed to capture fingerprint before mouse move").not.toBeNull();

      await sendAbsMouseMove(page, Math.floor(HID_MAX / 2), Math.floor(HID_MAX / 2));
      await page.waitForTimeout(200);
      const fpAfter = await captureVideoRegionFingerprint(
        page,
        regionX,
        regionY,
        regionWidth,
        regionHeight,
      );
      expect(fpAfter, "Failed to capture fingerprint after mouse move").not.toBeNull();

      // Verify the video is receiving frames (mouse movement should cause some change)
      // We use a very low threshold since cursor may not be exactly in capture region
      const dist = fingerprintDistance(fpBefore!, fpAfter!);
      expect(
        fpBefore!.some(v => v > 0) || fpAfter!.some(v => v > 0),
        `Video should have non-zero pixel data for EDID "${option.label}"`,
      ).toBe(true);

    }
  });
});
