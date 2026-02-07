import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  waitForVideoStream,
  wakeDisplay,
  sendAbsMouseMove,
  getVideoStreamDimensions,
  captureVideoRegionFingerprint,
  fingerprintDistance,
  hidToPixelCoords,
  runMouseBidirectionalCheck,
} from "./helpers";

// Minimum video dimensions to consider valid (sanity check)
const MIN_VIDEO_DIMENSION = 100;

// Region size for cursor detection (pixels around the expected cursor position)
const CAPTURE_REGION_SIZE = 80;

// HID absolute coordinate range is 0-32767
const HID_MAX = 32767;
const HID_CENTER = Math.floor(HID_MAX / 2); // 16383

test.describe("Mouse Round-Trip Tests", () => {
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

    // Get video dimensions and validate them
    const dimensions = await getVideoStreamDimensions(page);
    expect(dimensions, "Video stream dimensions should be available").not.toBeNull();
    const { width: videoWidth, height: videoHeight } = dimensions!;
    expect(videoWidth, `Video width should be at least ${MIN_VIDEO_DIMENSION}px`).toBeGreaterThan(
      MIN_VIDEO_DIMENSION,
    );
    expect(videoHeight, `Video height should be at least ${MIN_VIDEO_DIMENSION}px`).toBeGreaterThan(
      MIN_VIDEO_DIMENSION,
    );

    // Calculate pixel position for center of screen
    const centerPixel = hidToPixelCoords(HID_CENTER, HID_CENTER, videoWidth, videoHeight);

    // Calculate capture region bounds (centered around the target position)
    const regionX = Math.max(0, centerPixel.x - CAPTURE_REGION_SIZE / 2);
    const regionY = Math.max(0, centerPixel.y - CAPTURE_REGION_SIZE / 2);
    const regionWidth = Math.min(CAPTURE_REGION_SIZE, videoWidth - regionX);
    const regionHeight = Math.min(CAPTURE_REGION_SIZE, videoHeight - regionY);

    // Move mouse to center and let it settle
    await sendAbsMouseMove(page, HID_CENTER, HID_CENTER);
    await page.waitForTimeout(100);

    // Capture the region where the cursor should be (state A: with cursor)
    const fpBefore = await captureVideoRegionFingerprint(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(fpBefore, "Failed to capture fingerprint with cursor at center").not.toBeNull();

    // Move mouse to top-left corner (away from center)
    await sendAbsMouseMove(page, 0, 0);
    await page.waitForTimeout(100);

    // Capture the same region (state B: cursor gone)
    const fpAfter = await captureVideoRegionFingerprint(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(fpAfter, "Failed to capture fingerprint after cursor moved away").not.toBeNull();

    // Assert the regions differ significantly (cursor left the area)
    const distance = fingerprintDistance(fpBefore!, fpAfter!);
    expect(
      distance,
      `Cursor movement should cause significant visual change (distance=${distance}, expected >10) — mouse HID path may be broken`,
    ).toBeGreaterThan(10);
  });

  test("mouse movement is bidirectionally verifiable", async ({ page }) => {
    // Wake display if screensaver/sleep is active
    await wakeDisplay(page);
    await waitForVideoStream(page);

    const { arrive, restore } = await runMouseBidirectionalCheck(page, {
      retries: 1,
      threshold: 10,
      settleMs: 100,
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
