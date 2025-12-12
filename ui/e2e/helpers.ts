import { Page, expect } from "@playwright/test";

/**
 * USB HID Key Codes
 */
export const HID_KEY = {
  SPACE: 0x2c,     // 44
  CAPS_LOCK: 0x39, // 57
  NUM_LOCK: 0x53,  // 83
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
    .poll(
      async () => page.evaluate(() => window.__kvmTestHooks?.isVideoStreamActive()),
      {
        message: "Waiting for video stream to be active",
        timeout,
        intervals: [500, 1000, 2000],
      },
    )
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
export async function getVideoStreamDimensions(
  page: Page,
): Promise<VideoStreamDimensions | null> {
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

// TypeScript declarations for the test hooks on window
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
    };
  }
}
