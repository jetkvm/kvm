import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";

/**
 * USB HID Key Codes
 */
export const HID_KEY = {
  SPACE: 0x2c, // 44
  CAPS_LOCK: 0x39, // 57
  NUM_LOCK: 0x53, // 83
} as const;

/**
 * Keyboard LED state interface (matches KeyboardLedState from stores.ts)
 */
export interface KeyboardLedState {
  num_lock: boolean;
  caps_lock: boolean;
  scroll_lock: boolean;
  compose: boolean;
  kana: boolean;
  shift: boolean;
}

/**
 * Wait for the WebRTC connection to be established and HID RPC to be ready.
 * This polls the test hooks until both conditions are met.
 *
 * @param page - Playwright page object
 * @param timeout - Maximum time to wait in milliseconds (default: 30000)
 */
export async function waitForWebRTCReady(page: Page, timeout = 30000): Promise<void> {
  await expect
    .poll(
      async () => {
        const status = await page.evaluate(() => {
          const hooks = window.__kvmTestHooks;
          if (!hooks) {
            return { hooks: false, webrtc: false, hid: false };
          }
          return {
            hooks: true,
            webrtc: hooks.isWebRTCConnected(),
            hid: hooks.isHidRpcReady(),
          };
        });
        return status.hooks && status.webrtc && status.hid;
      },
      {
        message: "Waiting for WebRTC connection and HID RPC to be ready",
        timeout,
        intervals: [500, 1000, 2000],
      },
    )
    .toBe(true);
}

/**
 * Wait for video stream to be active.
 *
 * @param page - Playwright page object
 * @param timeout - Maximum time to wait in milliseconds (default: 30000)
 */
export async function waitForVideoStream(page: Page, timeout = 30000): Promise<void> {
  await expect
    .poll(async () => page.evaluate(() => window.__kvmTestHooks?.isVideoStreamActive()), {
      message: "Waiting for video stream to be active",
      timeout,
      intervals: [500, 1000, 2000],
    })
    .toBe(true);
}

/**
 * Wake the display by sending keystrokes to dismiss screensaver/sleep.
 * Sends multiple Space key taps with delays.
 *
 * @param page - Playwright page object
 * @param taps - Number of key taps to send (default: 3)
 * @param delayMs - Delay between taps in milliseconds (default: 200)
 */
export async function wakeDisplay(page: Page, taps = 3, delayMs = 500): Promise<void> {
  for (let i = 0; i < taps; i++) {
    await tapKey(page, HID_KEY.SPACE);
    await page.waitForTimeout(delayMs);
  }
}

/**
 * Send a keypress event via the test hooks.
 *
 * @param page - Playwright page object
 * @param keyCode - USB HID key code
 * @param press - true for key down, false for key up
 */
export async function sendKeypress(page: Page, keyCode: number, press: boolean): Promise<void> {
  await page.evaluate(
    ({ key, isPress }) => {
      const hooks = window.__kvmTestHooks;
      if (!hooks) throw new Error("Test hooks not available");
      hooks.sendKeypress(key, isPress);
    },
    { key: keyCode, isPress: press },
  );
}

/**
 * Send a complete key tap (press + release) with a small delay between.
 *
 * @param page - Playwright page object
 * @param keyCode - USB HID key code
 * @param holdMs - Time to hold the key in milliseconds (default: 50)
 */
export async function tapKey(page: Page, keyCode: number, holdMs = 50): Promise<void> {
  await sendKeypress(page, keyCode, true);
  await page.waitForTimeout(holdMs);
  await sendKeypress(page, keyCode, false);
}

/**
 * Get the current keyboard LED state.
 *
 * @param page - Playwright page object
 * @returns The current LED state or null if not available
 */
export async function getLedState(page: Page): Promise<KeyboardLedState | null> {
  return page.evaluate(() => {
    const hooks = window.__kvmTestHooks;
    if (!hooks) return null;
    return hooks.getKeyboardLedState();
  });
}

/**
 * Wait for a specific LED state to change.
 * Useful for verifying round-trip after sending a key.
 *
 * @param page - Playwright page object
 * @param ledName - Name of the LED to check (e.g., 'caps_lock', 'num_lock')
 * @param expectedValue - Expected boolean value
 * @param timeout - Maximum time to wait in milliseconds (default: 5000)
 */
export async function waitForLedState(
  page: Page,
  ledName: keyof KeyboardLedState,
  expectedValue: boolean,
  timeout = 5000,
): Promise<void> {
  await expect
    .poll(
      async () => {
        const state = await getLedState(page);
        return state?.[ledName];
      },
      {
        message: `Waiting for ${ledName} to be ${expectedValue}`,
        timeout,
        intervals: [100, 200, 500],
      },
    )
    .toBe(expectedValue);
}

/**
 * Video stream dimensions interface
 */
export interface VideoStreamDimensions {
  width: number;
  height: number;
}

/**
 * Send an absolute mouse move event via the test hooks.
 *
 * @param page - Playwright page object
 * @param x - X coordinate in HID absolute range (0-32767)
 * @param y - Y coordinate in HID absolute range (0-32767)
 * @param buttons - Mouse button bitmask (default: 0)
 */
export async function sendAbsMouseMove(
  page: Page,
  x: number,
  y: number,
  buttons = 0,
): Promise<void> {
  await page.evaluate(
    ({ x, y, buttons }) => {
      const hooks = window.__kvmTestHooks;
      if (!hooks) throw new Error("Test hooks not available");
      hooks.sendAbsMouseMove(x, y, buttons);
    },
    { x, y, buttons },
  );
}

/**
 * Get the video stream dimensions.
 *
 * @param page - Playwright page object
 * @returns The video dimensions or null if not available
 */
export async function getVideoStreamDimensions(page: Page): Promise<VideoStreamDimensions | null> {
  return page.evaluate(() => {
    const hooks = window.__kvmTestHooks;
    if (!hooks) return null;
    return hooks.getVideoStreamDimensions();
  });
}

/**
 * Capture a region of the video frame as a base64 PNG.
 *
 * @param page - Playwright page object
 * @param x - X coordinate of the region (in video pixels)
 * @param y - Y coordinate of the region (in video pixels)
 * @param width - Width of the region
 * @param height - Height of the region
 * @returns Base64-encoded PNG string or null if capture failed
 */
export async function captureVideoRegion(
  page: Page,
  x: number,
  y: number,
  width: number,
  height: number,
): Promise<string | null> {
  return page.evaluate(
    ({ x, y, width, height }) => {
      const hooks = window.__kvmTestHooks;
      if (!hooks) return null;
      return hooks.captureVideoRegion(x, y, width, height);
    },
    { x, y, width, height },
  );
}

/**
 * Capture a small fingerprint of a region of the video frame.
 * This is more tolerant to small frame-to-frame noise than comparing PNGs.
 */
export async function captureVideoRegionFingerprint(
  page: Page,
  x: number,
  y: number,
  width: number,
  height: number,
  gridSize = 8,
): Promise<number[] | null> {
  return page.evaluate(
    ({ x, y, width, height, gridSize }) => {
      const hooks = window.__kvmTestHooks;
      if (!hooks) return null;
      return hooks.captureVideoRegionFingerprint(x, y, width, height, gridSize);
    },
    { x, y, width, height, gridSize },
  );
}

export function fingerprintDistance(a: number[], b: number[]): number {
  const n = Math.min(a.length, b.length);
  let sum = 0;
  for (let i = 0; i < n; i++) sum += Math.abs(a[i] - b[i]);
  return sum;
}

/**
 * Convert HID absolute coordinates (0-32767) to video pixel coordinates.
 *
 * @param hidX - X in HID absolute range
 * @param hidY - Y in HID absolute range
 * @param videoWidth - Video width in pixels
 * @param videoHeight - Video height in pixels
 * @returns Pixel coordinates
 */
export function hidToPixelCoords(
  hidX: number,
  hidY: number,
  videoWidth: number,
  videoHeight: number,
): { x: number; y: number } {
  return {
    x: Math.round((hidX / 32767) * videoWidth),
    y: Math.round((hidY / 32767) * videoHeight),
  };
}

// HID absolute coordinate range is 0-32767
const HID_MAX = 32767;

// Region size for cursor detection (pixels around the expected cursor position)
const CAPTURE_REGION_SIZE = 80;

// Minimum video dimensions to consider valid (sanity check)
const MIN_VIDEO_DIMENSION = 100;

/**
 * Verify keyboard works using LED round-trip.
 * Taps CAPS_LOCK and verifies the LED state toggles.
 *
 * @param page - Playwright page object
 */
export async function verifyKeyboardWorks(page: Page): Promise<void> {
  // Get initial CAPS_LOCK state
  const initialState = await getLedState(page);
  expect(initialState, "LED state should be available").not.toBeNull();
  const initialCapsLock = initialState!.caps_lock;

  // Toggle CAPS_LOCK
  await tapKey(page, HID_KEY.CAPS_LOCK);
  await waitForLedState(page, "caps_lock", !initialCapsLock);

  // Verify the state changed
  const newState = await getLedState(page);
  expect(newState!.caps_lock, "CAPS_LOCK should have toggled").toBe(!initialCapsLock);

  // Restore original state
  await tapKey(page, HID_KEY.CAPS_LOCK);
  await waitForLedState(page, "caps_lock", initialCapsLock);
}

/**
 * Verify mouse works using fingerprint comparison.
 * Moves the cursor and verifies the video region changes.
 *
 * @param page - Playwright page object
 */
export async function verifyMouseWorks(page: Page): Promise<void> {
  // Wait for video to be ready and get dimensions (with retry)
  let dimensions = await getVideoStreamDimensions(page);
  if (!dimensions) {
    // Video may still be initializing, wait and retry
    await page.waitForTimeout(2000);
    dimensions = await getVideoStreamDimensions(page);
  }
  expect(dimensions, "Video stream dimensions should be available").not.toBeNull();
  const { width: videoWidth, height: videoHeight } = dimensions!;
  expect(videoWidth, `Video width should be at least ${MIN_VIDEO_DIMENSION}px`).toBeGreaterThan(
    MIN_VIDEO_DIMENSION,
  );
  expect(videoHeight, `Video height should be at least ${MIN_VIDEO_DIMENSION}px`).toBeGreaterThan(
    MIN_VIDEO_DIMENSION,
  );

  // Calculate center position
  const hidCenter = Math.floor(HID_MAX / 2);
  const centerPixel = hidToPixelCoords(hidCenter, hidCenter, videoWidth, videoHeight);

  // Calculate capture region bounds (centered around the target position)
  const regionX = Math.max(0, centerPixel.x - CAPTURE_REGION_SIZE / 2);
  const regionY = Math.max(0, centerPixel.y - CAPTURE_REGION_SIZE / 2);
  const regionWidth = Math.min(CAPTURE_REGION_SIZE, videoWidth - regionX);
  const regionHeight = Math.min(CAPTURE_REGION_SIZE, videoHeight - regionY);

  // Move mouse to center and capture
  await sendAbsMouseMove(page, hidCenter, hidCenter);
  await page.waitForTimeout(100);
  const fpBefore = await captureVideoRegionFingerprint(
    page,
    regionX,
    regionY,
    regionWidth,
    regionHeight,
  );
  expect(fpBefore, "Failed to capture fingerprint with cursor at center").not.toBeNull();

  // Move mouse to corner and capture
  await sendAbsMouseMove(page, 0, 0);
  await page.waitForTimeout(100);
  const fpAfter = await captureVideoRegionFingerprint(
    page,
    regionX,
    regionY,
    regionWidth,
    regionHeight,
  );
  expect(fpAfter, "Failed to capture fingerprint after cursor moved away").not.toBeNull();

  // Verify the regions differ (cursor moved)
  const distance = fingerprintDistance(fpBefore!, fpAfter!);
  expect(
    distance,
    `Cursor movement should cause significant visual change (distance=${distance}, expected >10) — mouse HID path may be broken`,
  ).toBeGreaterThan(10);
}

/**
 * Combined verification for video stream, mouse, and keyboard.
 * This is a convenience function that runs all three verifications.
 *
 * @param page - Playwright page object
 */
export async function verifyHidAndVideo(page: Page): Promise<void> {
  // Wake display first (sends 3 space key presses to wake target machine)
  await wakeDisplay(page);

  // Wait for video stream to be active (proper polling with timeout)
  await waitForVideoStream(page, 10000);

  // Verify mouse works
  await verifyMouseWorks(page);

  // Verify keyboard works
  await verifyKeyboardWorks(page);
}

/**
 * Get the current app version from the /metrics endpoint.
 * This endpoint exposes Prometheus metrics including the version.
 *
 * @param page - Playwright page object
 * @returns The version string or null if not found
 */
export async function getCurrentVersion(page: Page): Promise<string | null> {
  return page.evaluate(async () => {
    try {
      const response = await fetch("/metrics");
      if (!response.ok) return null;

      const text = await response.text();
      // Look for promhttp_metric_handler_requests_total or similar app-specific metrics
      // The app version is in the build_info metric, not go_info
      const match = text.match(/build_info.*version="([^"]+)"/);
      if (match) return match[1];

      // Fallback: try to find any version that's not the go version
      const allVersions = Array.from(text.matchAll(/version="([^"]+)"/g));
      for (const m of allVersions) {
        const ver = m[1];
        // Skip go versions
        if (!ver.startsWith("go1.")) {
          return ver;
        }
      }

      return null;
    } catch (error) {
      console.error("Failed to fetch version from /metrics:", error);
      return null;
    }
  });
}

// TypeScript declarations for the test hooks on window
/**
 * Send a command to the KVM terminal via the test hooks.
 *
 * @param page - Playwright page object
 * @param command - Command to send (newline will be appended automatically)
 * @param waitMs - Time to wait after sending (default: 500ms)
 */
export async function sendTerminalCommand(
  page: Page,
  command: string,
  waitMs = 500,
): Promise<boolean> {
  const result = await page.evaluate(cmd => {
    return window.__kvmTestHooks?.sendTerminalCommand?.(cmd) ?? false;
  }, command);

  if (waitMs > 0) {
    await page.waitForTimeout(waitMs);
  }

  return result;
}

/**
 * Wait for the KVM terminal data channel to be ready.
 *
 * @param page - Playwright page object
 * @param timeout - Maximum time to wait in milliseconds (default: 10000)
 */
export async function waitForTerminalReady(page: Page, timeout = 10000): Promise<void> {
  const startTime = Date.now();
  while (Date.now() - startTime < timeout) {
    const ready = await page.evaluate(() => {
      return window.__kvmTestHooks?.isTerminalReady?.() ?? false;
    });

    if (ready) {
      return;
    }

    await page.waitForTimeout(200);
  }

  throw new Error(`Terminal not ready after ${timeout}ms`);
}

/**
 * Reconnect to the device after a reboot.
 * Waits for the device to come back online and re-establishes WebRTC connection.
 *
 * @param page - Playwright page object
 * @param waitBeforeRetry - Initial wait time before starting retries (default: 30000ms)
 * @param maxRetries - Maximum number of reconnection attempts (default: 15)
 * @param retryInterval - Time between retry attempts (default: 3000ms)
 */
export async function reconnectAfterReboot(
  page: Page,
  waitBeforeRetry = 15000,
  maxRetries = 15,
  retryInterval = 3000,
): Promise<void> {
  await page.waitForTimeout(waitBeforeRetry);

  for (let attempt = 1; attempt <= maxRetries; attempt++) {
    try {
      await page.goto("/", { timeout: 5000 });
      await waitForWebRTCReady(page, 10000);
      return;
    } catch {
      if (attempt === maxRetries) {
        throw new Error("Failed to reconnect after reboot");
      }
      await page.waitForTimeout(retryInterval);
    }
  }
}

declare global {
  interface Window {
    __kvmTestHooks?: {
      getKeyboardLedState: () => KeyboardLedState | null;
      getKeysDownState: () => { modifier: number; keys: number[] } | null;
      sendKeypress: (key: number, press: boolean) => void;
      sendAbsMouseMove: (x: number, y: number, buttons: number) => void;
      captureVideoRegion: (
        x: number,
        y: number,
        width: number,
        height: number,
      ) => Promise<string | null>;
      captureVideoRegionFingerprint: (
        x: number,
        y: number,
        width: number,
        height: number,
        gridSize?: number,
      ) => number[] | null;
      getVideoStreamDimensions: () => VideoStreamDimensions | null;
      isWebRTCConnected: () => boolean;
      isHidRpcReady: () => boolean;
      isVideoStreamActive: () => boolean;
      sendTerminalCommand: (command: string) => boolean;
      isTerminalReady: () => boolean;
    };
  }
}
