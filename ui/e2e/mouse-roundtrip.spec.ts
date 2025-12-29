import fs from "fs/promises";

import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  waitForVideoStream,
  wakeDisplay,
  sendAbsMouseMove,
  getVideoStreamDimensions,
  captureVideoRegion,
  captureVideoRegionFingerprint,
  fingerprintDistance,
  hidToPixelCoords,
} from "./helpers";

// Minimum video dimensions to consider valid (sanity check)
const MIN_VIDEO_DIMENSION = 100;

function dataUrlToPngBuffer(dataUrl: string): Buffer {
  const prefix = "data:image/png;base64,";
  if (!dataUrl.startsWith(prefix)) {
    throw new Error(`Unexpected data URL prefix: ${dataUrl.slice(0, 32)}...`);
  }
  return Buffer.from(dataUrl.slice(prefix.length), "base64");
}

async function persistPng(
  testInfo: ReturnType<typeof test.info>,
  filename: string,
  dataUrl: string,
) {
  const buf = dataUrlToPngBuffer(dataUrl);
  await fs.writeFile(testInfo.outputPath(filename), buf);
  await testInfo.attach(filename, { body: buf, contentType: "image/png" });
}

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
    const regionBefore = await captureVideoRegion(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(regionBefore, "Failed to capture PNG with cursor at center").not.toBeNull();
    await persistPng(test.info(), "mouse-region-before.png", regionBefore!);

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
    const regionAfter = await captureVideoRegion(page, regionX, regionY, regionWidth, regionHeight);
    expect(regionAfter, "Failed to capture PNG after cursor moved away").not.toBeNull();
    await persistPng(test.info(), "mouse-region-after.png", regionAfter!);

    // Assert the regions differ significantly (cursor left the area)
    const distance = fingerprintDistance(fpBefore!, fpAfter!);
    const distText = `distance=${distance}\n`;
    await fs.writeFile(test.info().outputPath("mouse-fingerprint-distance.txt"), distText, "utf-8");
    await test.info().attach("mouse-fingerprint-distance.txt", {
      body: Buffer.from(distText, "utf-8"),
      contentType: "text/plain",
    });

    expect(
      distance,
      `Cursor movement should cause significant visual change (distance=${distance}, expected >10) — mouse HID path may be broken`,
    ).toBeGreaterThan(10);
  });

  test("mouse movement is bidirectionally verifiable", async ({ page }) => {
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

    // Use a position offset from center to avoid any UI elements
    const testHidX = Math.floor(HID_MAX * 0.7);
    const testHidY = Math.floor(HID_MAX * 0.7);

    const testPixel = hidToPixelCoords(testHidX, testHidY, videoWidth, videoHeight);
    const regionX = Math.max(0, testPixel.x - CAPTURE_REGION_SIZE / 2);
    const regionY = Math.max(0, testPixel.y - CAPTURE_REGION_SIZE / 2);
    const regionWidth = Math.min(CAPTURE_REGION_SIZE, videoWidth - regionX);
    const regionHeight = Math.min(CAPTURE_REGION_SIZE, videoHeight - regionY);

    // Move cursor away first to establish baseline
    await sendAbsMouseMove(page, 0, 0);
    await page.waitForTimeout(100);

    // Capture fingerprint without cursor (state A)
    const fpWithoutCursor = await captureVideoRegionFingerprint(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(
      fpWithoutCursor,
      "Failed to capture fingerprint A (baseline without cursor)",
    ).not.toBeNull();
    const regionWithoutCursorPng = await captureVideoRegion(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(regionWithoutCursorPng, "Failed to capture PNG for state A").not.toBeNull();
    await persistPng(test.info(), "mouse-bidir-A-without-cursor.png", regionWithoutCursorPng!);

    // Move cursor into the target region
    await sendAbsMouseMove(page, testHidX, testHidY);
    await page.waitForTimeout(100);

    // Capture fingerprint with cursor (state B)
    const fpWithCursor = await captureVideoRegionFingerprint(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(fpWithCursor, "Failed to capture fingerprint B (with cursor in region)").not.toBeNull();
    const regionWithCursorPng = await captureVideoRegion(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(regionWithCursorPng, "Failed to capture PNG for state B").not.toBeNull();
    await persistPng(test.info(), "mouse-bidir-B-with-cursor.png", regionWithCursorPng!);

    // Move cursor away again
    await sendAbsMouseMove(page, 0, 0);
    await page.waitForTimeout(100);

    // Capture fingerprint again without cursor (state A2)
    const fpWithoutCursorAgain = await captureVideoRegionFingerprint(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(
      fpWithoutCursorAgain,
      "Failed to capture fingerprint A2 (after cursor left again)",
    ).not.toBeNull();
    const regionWithoutCursorAgainPng = await captureVideoRegion(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(regionWithoutCursorAgainPng, "Failed to capture PNG for state A2").not.toBeNull();
    await persistPng(
      test.info(),
      "mouse-bidir-A2-without-cursor-again.png",
      regionWithoutCursorAgainPng!,
    );

    const distArrive = fingerprintDistance(fpWithoutCursor!, fpWithCursor!);
    const distRestore = fingerprintDistance(fpWithoutCursor!, fpWithoutCursorAgain!);
    const distText = `arrive=${distArrive}\nrestore=${distRestore}\n`;
    await fs.writeFile(
      test.info().outputPath("mouse-bidir-fingerprint-distances.txt"),
      distText,
      "utf-8",
    );
    await test.info().attach("mouse-bidir-fingerprint-distances.txt", {
      body: Buffer.from(distText, "utf-8"),
      contentType: "text/plain",
    });

    // Verify: cursor arrival changes the region significantly
    expect(
      distArrive,
      `Cursor arrival should cause significant visual change (distArrive=${distArrive}, expected >10) — mouse HID path may be broken`,
    ).toBeGreaterThan(10);

    // Verify: after moving away, the region is closer to baseline than when cursor was present
    expect(
      distRestore,
      `Region should restore after cursor leaves (distRestore=${distRestore} should be < distArrive=${distArrive}) — cursor may not have moved away`,
    ).toBeLessThan(distArrive);
  });
});
