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

// TypeScript declarations for the test hooks on window
declare global {
  interface Window {
    __kvmTestHooks?: {
      getKeyboardLedState: () => KeyboardLedState | null;
      getKeysDownState: () => { modifier: number; keys: number[] } | null;
      sendKeypress: (key: number, press: boolean) => void;
      isWebRTCConnected: () => boolean;
      isHidRpcReady: () => boolean;
      isVideoStreamActive: () => boolean;
    };
  }
}
