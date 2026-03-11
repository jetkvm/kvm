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
        intervals: [200, 500, 1000],
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
      intervals: [200, 500, 1000],
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
export async function wakeDisplay(page: Page, taps = 3, delayMs = 100): Promise<void> {
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
export async function tapKey(page: Page, keyCode: number, holdMs = 20): Promise<void> {
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
 * Wait for video stream dimensions to be available (frames rendered).
 * This polls until getVideoStreamDimensions returns non-null with valid dimensions.
 */
export async function waitForVideoDimensions(
  page: Page,
  timeout = 10000,
): Promise<VideoStreamDimensions> {
  let dims: VideoStreamDimensions | null = null;
  await expect
    .poll(
      async () => {
        dims = await getVideoStreamDimensions(page);
        return dims !== null && dims.width > MIN_VIDEO_DIMENSION && dims.height > MIN_VIDEO_DIMENSION;
      },
      {
        message: "Waiting for video dimensions to be available",
        timeout,
        intervals: [200, 500, 1000],
      },
    )
    .toBe(true);
  return dims!;
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

// Mouse verification tuning
const MOUSE_DISTANCE_THRESHOLD = 10;
const MOUSE_VERIFY_RETRIES = 3;
const MOUSE_SETTLE_MS = 150;

export interface MouseBidirCheckOptions {
  retries?: number;
  threshold?: number;
  settleMs?: number;
  testHidX?: number;
  testHidY?: number;
}

export interface MouseBidirCheckResult {
  arrive: number;
  restore: number;
}

export async function runMouseBidirectionalCheck(
  page: Page,
  options: MouseBidirCheckOptions = {},
): Promise<MouseBidirCheckResult> {
  const {
    retries = MOUSE_VERIFY_RETRIES,
    threshold = MOUSE_DISTANCE_THRESHOLD,
    settleMs = MOUSE_SETTLE_MS,
  } = options;

  // Wait for video dimensions to be available (with polling)
  const { width: videoWidth, height: videoHeight } = await waitForVideoDimensions(page);

  const testHidX = options.testHidX ?? Math.floor(HID_MAX * 0.7);
  const testHidY = options.testHidY ?? Math.floor(HID_MAX * 0.7);
  const testPixel = hidToPixelCoords(testHidX, testHidY, videoWidth, videoHeight);

  const regionX = Math.max(0, testPixel.x - CAPTURE_REGION_SIZE / 2);
  const regionY = Math.max(0, testPixel.y - CAPTURE_REGION_SIZE / 2);
  const regionWidth = Math.min(CAPTURE_REGION_SIZE, videoWidth - regionX);
  const regionHeight = Math.min(CAPTURE_REGION_SIZE, videoHeight - regionY);

  let lastDistArrive = -1;
  let lastDistRestore = -1;

  for (let attempt = 1; attempt <= retries; attempt++) {
    await sendAbsMouseMove(page, 0, 0);
    await page.waitForTimeout(settleMs);
    const fpA = await captureVideoRegionFingerprint(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(fpA, `Failed to capture fingerprint A on attempt ${attempt}`).not.toBeNull();

    await sendAbsMouseMove(page, testHidX, testHidY);
    await page.waitForTimeout(settleMs);
    const fpB = await captureVideoRegionFingerprint(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(fpB, `Failed to capture fingerprint B on attempt ${attempt}`).not.toBeNull();

    await sendAbsMouseMove(page, 0, 0);
    await page.waitForTimeout(settleMs);
    const fpA2 = await captureVideoRegionFingerprint(
      page,
      regionX,
      regionY,
      regionWidth,
      regionHeight,
    );
    expect(fpA2, `Failed to capture fingerprint A2 on attempt ${attempt}`).not.toBeNull();

    const distArrive = fingerprintDistance(fpA!, fpB!);
    const distRestore = fingerprintDistance(fpA!, fpA2!);
    lastDistArrive = distArrive;
    lastDistRestore = distRestore;

    if (distArrive > threshold && distRestore < distArrive) {
      return { arrive: distArrive, restore: distRestore };
    }
  }

  expect(
    lastDistArrive,
    `Cursor movement should cause significant visual change (arrive=${lastDistArrive}, expected >${threshold}) — mouse HID path may be broken`,
  ).toBeGreaterThan(threshold);
  expect(
    lastDistRestore,
    `Region should restore after cursor leaves (restore=${lastDistRestore} should be < arrive=${lastDistArrive})`,
  ).toBeLessThan(lastDistArrive);
  return { arrive: lastDistArrive, restore: lastDistRestore };
}

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

  // Small delay to ensure key state is stable before second toggle
  await page.waitForTimeout(50);

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
  await runMouseBidirectionalCheck(page, {
    retries: MOUSE_VERIFY_RETRIES,
    threshold: MOUSE_DISTANCE_THRESHOLD,
    settleMs: MOUSE_SETTLE_MS,
  });
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

  // Wait for video dimensions to be available (frames need to render)
  await waitForVideoDimensions(page);

  // Verify mouse works (use longer settle time for post-reboot scenarios)
  await runMouseBidirectionalCheck(page, {
    retries: MOUSE_VERIFY_RETRIES,
    threshold: MOUSE_DISTANCE_THRESHOLD,
    settleMs: 150,
  });

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
  waitMs = 200,
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
  waitBeforeRetry = 5000,
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

// Time to wait for welcome screen animations (ms)
const ANIMATION_DELAY = 500;

// Known test passwords - used when device is in unknown state and needs login
const KNOWN_TEST_PASSWORDS = ["TestPassword123", "NewPassword456"];

/**
 * Try to login with known test passwords if on login page.
 * Returns true if login was successful or not needed.
 *
 * @param page - Playwright page object
 */
async function tryLoginIfNeeded(page: Page): Promise<boolean> {
  const currentUrl = page.url();
  if (!currentUrl.includes("/login")) {
    return true; // Not on login page, no login needed
  }

  // Try each known test password
  for (const password of KNOWN_TEST_PASSWORDS) {
    const passwordInput = page.locator('input[name="password"]');
    if (!(await passwordInput.isVisible({ timeout: 2000 }).catch(() => false))) {
      return true; // No password input visible, probably not a login page
    }

    await passwordInput.fill(password);
    const submitButton = page.getByRole("button", { name: /Log in/i });
    await submitButton.click();
    await page.waitForTimeout(500);

    // Check if we're no longer on login page
    const newUrl = page.url();
    if (!newUrl.includes("/login")) {
      return true;
    }

    // Clear for next attempt (may fail if element is detached/disabled after rate limiting)
    await passwordInput.clear().catch(() => {});
  }

  return false; // Could not login with any known password
}

/**
 * Reset the device to onboarding/welcome state.
 * Uses SSH to delete config and reboot if device is already configured.
 * Use this for tests that need to test the welcome/onboarding UI flow itself.
 * For tests that just need device in a specific auth mode, use ensureLocalAuthMode() instead.
 *
 * @param page - Playwright page object
 */
export async function resetDeviceToWelcome(page: Page): Promise<void> {
  // Check if already on welcome page
  await page.goto("/");
  await page.waitForLoadState("networkidle");

  const currentUrl = page.url();
  if (currentUrl.includes("/welcome")) {
    // Already on welcome - navigate to base if on sub-route
    if (!currentUrl.endsWith("/welcome")) {
      await page.goto("/welcome");
      await page.waitForLoadState("networkidle");
    }
    await page.waitForTimeout(ANIMATION_DELAY);
    return;
  }

  // Device is configured - reset via SSH (most reliable approach)
  await resetConfigViaSSH();
  await rebootDeviceViaSSH();
  await page.goto("/");
  await page.waitForLoadState("networkidle");

  // Wait for animations to complete
  await page.waitForTimeout(ANIMATION_DELAY);
}

// ============================================================================
// Welcome Flow Primitives (internal building blocks for ensureLocalAuthMode)
// Prefer using ensureLocalAuthMode() or resetDeviceToWelcome() in tests.
// ============================================================================

/**
 * Navigate to the welcome mode selection page and wait for it to load.
 * Prerequisite: page should be on /welcome.
 *
 * @param page - Playwright page object
 */
export async function goToWelcomeMode(page: Page): Promise<void> {
  const setupButton = page.getByRole("link", { name: /Set up your JetKVM/i });
  await expect(setupButton).toBeVisible({ timeout: 10000 });
  await setupButton.click();

  await page.waitForURL("**/welcome/mode", { timeout: 10000 });
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(200); // Wait for animations
}

/**
 * Select an auth mode on the welcome/mode page and click Continue.
 * Prerequisite: page should be on /welcome/mode.
 *
 * @param page - Playwright page object
 * @param mode - "password" or "noPassword"
 */
export async function selectWelcomeAuthMode(
  page: Page,
  mode: "password" | "noPassword",
): Promise<void> {
  const radio = page.locator(`input[type="radio"][value="${mode}"]`);
  await expect(radio).toBeVisible({ timeout: 5000 });
  await radio.click();

  const continueButton = page.getByRole("button", { name: /Continue/i });
  await expect(continueButton).toBeEnabled({ timeout: 5000 });
  await continueButton.click();
}

/**
 * Submit password on the welcome/password page.
 * Prerequisite: page should be on /welcome/password.
 *
 * @param page - Playwright page object
 * @param password - Password to enter
 * @param confirmPassword - Confirm password (defaults to same as password)
 * @param expectSuccess - If true, wait for redirect to /. If false, stay on page (for validation tests)
 */
export async function submitWelcomePassword(
  page: Page,
  password: string,
  confirmPassword?: string,
  expectSuccess = true,
): Promise<void> {
  await page.waitForURL("**/welcome/password", { timeout: 10000 });
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(200); // Wait for animations

  const passwordInput = page.locator('input[name="password"]');
  const confirmPasswordInput = page.locator('input[name="confirmPassword"]');

  await passwordInput.fill(password);
  await confirmPasswordInput.fill(confirmPassword ?? password);

  const submitButton = page.getByRole("button", { name: /Set Password/i });
  await expect(submitButton).toBeEnabled({ timeout: 5000 });
  await submitButton.click();

  if (expectSuccess) {
    await page.waitForURL("/", { timeout: 15000 });
  } else {
    // Wait for error to appear (form stays on same page)
    await page.waitForTimeout(500);
  }
}

// ============================================================================
// Login/Logout Helpers
// ============================================================================

/**
 * Login with the given password on the login page.
 * Prerequisite: page should be on /login-local.
 *
 * @param page - Playwright page object
 * @param password - Password to use
 * @param expectSuccess - If true, wait for redirect away from login. If false, stay on page.
 * @returns Object with success status and any error message
 */
export async function loginLocal(
  page: Page,
  password: string,
  expectSuccess = true,
): Promise<{ success: boolean; error?: string }> {
  const passwordInput = page.locator('input[name="password"]');
  await expect(passwordInput).toBeVisible({ timeout: 5000 });

  // Check if input is enabled (might be disabled due to rate-limiting)
  const isEnabled = await passwordInput.isEnabled({ timeout: 3000 }).catch(() => false);
  if (!isEnabled) {
    if (expectSuccess) {
      throw new Error("Login failed: password input is disabled (likely rate-limited)");
    }
    return { success: false, error: "Rate limited - input disabled" };
  }

  await passwordInput.fill(password, { timeout: 5000 });

  const submitButton = page.getByRole("button", { name: /Log in/i });
  const submitEnabled = await submitButton.isEnabled({ timeout: 3000 }).catch(() => false);
  if (!submitEnabled) {
    if (expectSuccess) {
      throw new Error("Login failed: submit button is disabled");
    }
    return { success: false, error: "Submit button disabled" };
  }
  await submitButton.click();

  // Wait for navigation away from login page or for error to appear
  try {
    await page.waitForURL(url => !url.toString().includes("/login"), { timeout: 5000 });
    return { success: true };
  } catch {
    // Still on login page - check for error
  }

  // Still on login page - get error message (with timeout to avoid hanging)
  const errorLocator = page.locator(".text-red-500, .text-red-600").first();
  const errorText = await errorLocator.isVisible({ timeout: 3000 }).catch(() => false)
    ? await errorLocator.textContent({ timeout: 3000 }).catch(() => null)
    : null;

  if (expectSuccess) {
    // Test expected success but login failed
    throw new Error(`Login failed: ${errorText || "Unknown error"}`);
  }

  return { success: false, error: errorText || undefined };
}

/**
 * Logout from the device (clears auth cookie).
 *
 * @param page - Playwright page object
 */
export async function logout(page: Page): Promise<void> {
  await page.evaluate(async () => {
    await fetch("/auth/logout", { method: "POST" });
  });
  await page.waitForTimeout(200);
}

/**
 * Dismiss the "Another Active Session Detected" dialog if it appears.
 * This dialog shows when another WebRTC session is active.
 *
 * @param page - Playwright page object
 */
export async function dismissSessionTakeoverDialog(page: Page): Promise<void> {
  const useHereButton = page.getByRole("button", { name: /Use Here/i });
  if (await useHereButton.isVisible({ timeout: 2000 }).catch(() => false)) {
    await useHereButton.click();
    await page.waitForTimeout(500);
  }
}

// ============================================================================
// Settings Access Page Helpers
// ============================================================================

/**
 * Navigate to /settings/access and wait for the local auth section to load.
 *
 * @param page - Playwright page object
 */
export async function openAccessSettings(page: Page): Promise<void> {
  await page.goto("/settings/access");
  await page.waitForLoadState("networkidle");
  await dismissSessionTakeoverDialog(page);

  // Wait for the local auth section to appear (indicates loaderData is loaded)
  const localSectionHeader = page.locator("text=Authentication Mode");
  await expect(localSectionHeader).toBeVisible({ timeout: 15000 });
}

/**
 * Enable password protection from settings when in noPassword mode.
 * Prerequisite: page should be on /settings/access with noPassword mode active.
 *
 * @param page - Playwright page object
 * @param password - Password to set
 * @param confirmPassword - Confirm password (defaults to same as password)
 * @param expectSuccess - If true, wait for success modal. If false, expect error.
 */
export async function enablePasswordFromSettings(
  page: Page,
  password: string,
  confirmPassword?: string,
  expectSuccess = true,
): Promise<void> {
  const enablePasswordButton = page.getByRole("button").filter({ hasText: /Enable Password/i });
  await expect(enablePasswordButton).toBeVisible({ timeout: 10000 });
  await enablePasswordButton.click();

  // Wait for modal to appear
  const passwordInput = page.locator('input[type="password"]').first();
  await expect(passwordInput).toBeVisible({ timeout: 5000 });

  const confirmPasswordInput = page.locator('input[type="password"]').nth(1);
  await passwordInput.fill(password);
  await confirmPasswordInput.fill(confirmPassword ?? password);

  const secureButton = page.getByRole("button", { name: /Secure|Set Password/i });
  await secureButton.click();

  if (expectSuccess) {
    const successMessage = page.locator("text=Password Set Successfully");
    await expect(successMessage).toBeVisible({ timeout: 5000 });

    const closeButton = page.getByRole("button", { name: /Close/i });
    await closeButton.click();
  }
}

/**
 * Change password from settings when in password mode.
 * Prerequisite: page should be on /settings/access with password mode active.
 *
 * @param page - Playwright page object
 * @param oldPassword - Current password
 * @param newPassword - New password to set
 * @param confirmNewPassword - Confirm new password (defaults to same as newPassword)
 * @param expectSuccess - If true, wait for success modal. If false, expect error.
 */
export async function changePasswordFromSettings(
  page: Page,
  oldPassword: string,
  newPassword: string,
  confirmNewPassword?: string,
  expectSuccess = true,
): Promise<void> {
  const changePasswordButton = page.getByRole("button").filter({ hasText: /Change Password/i });
  await expect(changePasswordButton).toBeVisible({ timeout: 10000 });
  await changePasswordButton.click();

  // Wait for modal to appear
  const oldPasswordInput = page.locator('input[type="password"]').first();
  await expect(oldPasswordInput).toBeVisible({ timeout: 5000 });

  const newPasswordInput = page.locator('input[type="password"]').nth(1);
  const confirmNewPasswordInput = page.locator('input[type="password"]').nth(2);

  await oldPasswordInput.fill(oldPassword);
  await newPasswordInput.fill(newPassword);
  await confirmNewPasswordInput.fill(confirmNewPassword ?? newPassword);

  const updateButton = page.getByRole("button", { name: /Update Password/i });
  await updateButton.click();

  if (expectSuccess) {
    const successMessage = page.locator("text=Password Updated Successfully");
    await expect(successMessage).toBeVisible({ timeout: 5000 });

    const closeButton = page.getByRole("button", { name: /Close/i });
    await closeButton.click();
  }
}

/**
 * Disable password protection from settings when in password mode.
 * Prerequisite: page should be on /settings/access with password mode active.
 *
 * @param page - Playwright page object
 * @param currentPassword - Current password to confirm deletion
 * @param expectSuccess - If true, wait for success modal. If false, expect error.
 */
export async function disablePasswordFromSettings(
  page: Page,
  currentPassword: string,
  expectSuccess = true,
): Promise<void> {
  const disableButton = page.getByRole("button").filter({ hasText: /Disable Protection/i });
  await expect(disableButton).toBeVisible({ timeout: 10000 });
  await disableButton.click();

  // Wait for modal to appear
  const passwordInput = page.locator('input[type="password"]').first();
  await expect(passwordInput).toBeVisible({ timeout: 5000 });
  await passwordInput.fill(currentPassword);

  const confirmDisableButton = page.getByRole("button", { name: /Disable.*Protection/i });
  await confirmDisableButton.click();

  if (expectSuccess) {
    const successMessage = page.locator("text=Password Protection Disabled");
    await expect(successMessage).toBeVisible({ timeout: 5000 });

    const closeButton = page.getByRole("button", { name: /Close/i });
    await closeButton.click();
  }
}

// ============================================================================
// SSH Helpers (DRY implementation)
// ============================================================================

/**
 * Execute a command on the device via SSH.
 * This is the single internal helper for all SSH operations.
 *
 * @param cmd - Command to execute on the device
 * @param ignoreErrors - If true, don't throw on command failure (default: false)
 * @returns The stdout from the command
 */
export async function sshExec(cmd: string, ignoreErrors = false): Promise<string> {
  const { exec } = await import("child_process");
  const { promisify } = await import("util");
  const execAsync = promisify(exec);

  const host = getDeviceHost();
  const sshCmd = `ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -o ConnectTimeout=10 root@${host} '${cmd}'`;

  try {
    const { stdout } = await execAsync(sshCmd);
    return stdout;
  } catch (error) {
    if (ignoreErrors) {
      return "";
    }
    throw error;
  }
}

export async function resetConfigViaSSH(): Promise<void> {
  await sshExec("rm /userdata/kvm_config.json");
  await sshExec("sync");
}

// ============================================================================
// Local Auth Mode Management
// ============================================================================

/** Desired local auth mode configuration */
export type LocalAuthModeConfig = { mode: "noPassword" } | { mode: "password"; password: string };

/**
 * Ensure the device is configured with the desired local auth mode.
 * This is the preferred way to set up device state at the start of tests.
 *
 * Handles all states:
 * - If on /welcome: completes onboarding with desired mode
 * - If on /login-local: either logs in (password mode) or clears password via SSH (noPassword mode)
 * - If already configured: uses SSH to adjust if needed
 *
 * @param page - Playwright page object
 * @param desired - The desired auth mode configuration
 */
export async function ensureLocalAuthMode(page: Page, desired: LocalAuthModeConfig): Promise<void> {
  await page.goto("/");
  await page.waitForLoadState("networkidle");

  const currentUrl = page.url();

  if (currentUrl.includes("/welcome")) {
    // Device is in onboarding mode - complete setup
    await goToWelcomeMode(page);
    if (desired.mode === "noPassword") {
      await selectWelcomeAuthMode(page, "noPassword");
      await page.waitForURL("/", { timeout: 15000 });
    } else {
      await selectWelcomeAuthMode(page, "password");
      await submitWelcomePassword(page, desired.password);
    }
    return;
  }

  if (currentUrl.includes("/login")) {
    // Device has password protection - try to login with known passwords
    const passwordsToTry = desired.mode === "password"
      ? [desired.password, ...KNOWN_TEST_PASSWORDS.filter(p => p !== desired.password)]
      : [...KNOWN_TEST_PASSWORDS];

    let loggedIn = false;
    let usedPassword: string | null = null;
    for (const pwd of passwordsToTry) {
      const result = await loginLocal(page, pwd, false);
      if (result.success) {
        loggedIn = true;
        usedPassword = pwd;
        break;
      }
      // Re-navigate to login if needed (page may have changed)
      if (!page.url().includes("/login")) break;
    }

    if (loggedIn) {
      if (desired.mode === "password" && usedPassword === desired.password) {
        return; // Already has correct password
      }
      if (desired.mode === "password") {
        // Change password via settings UI
        await openAccessSettings(page);
        await changePasswordFromSettings(page, usedPassword!, desired.password);
        return;
      }
      // desired.mode === "noPassword" - disable via settings UI
      await openAccessSettings(page);
      await disablePasswordFromSettings(page, usedPassword!);
      return;
    }

    // Could not login - fall back to SSH reset
    await resetConfigViaSSH();
    await rebootDeviceViaSSH();
    await page.goto("/");
    await page.waitForLoadState("networkidle");
    if (desired.mode === "password") {
      await goToWelcomeMode(page);
      await selectWelcomeAuthMode(page, "password");
      await submitWelcomePassword(page, desired.password);
    } else {
      await goToWelcomeMode(page);
      await selectWelcomeAuthMode(page, "noPassword");
      await page.waitForURL("/", { timeout: 15000 });
    }
    return;
  }

  // Device is configured and we're logged in (or no password) - check current mode
  await openAccessSettings(page);

  // Detect current mode by checking which buttons are visible
  const hasDisableButton = await page
    .getByRole("button")
    .filter({ hasText: /Disable Protection/i })
    .isVisible({ timeout: 3000 })
    .catch(() => false);

  if (desired.mode === "password") {
    if (hasDisableButton) {
      // Already has password - nothing to do (we're already logged in)
      return;
    }
    // No password currently - enable it
    await enablePasswordFromSettings(page, desired.password);
  } else {
    if (!hasDisableButton) {
      // Already no password - nothing to do
      return;
    }
    // Has password - try disabling with known passwords
    for (const pwd of KNOWN_TEST_PASSWORDS) {
      try {
        await disablePasswordFromSettings(page, pwd);
        return;
      } catch {
        // Wrong password, try next
        await openAccessSettings(page);
      }
    }
    // Fall back to SSH
    await clearPasswordViaSSH();
    await page.goto("/");
    await page.waitForLoadState("networkidle");
  }
}

/**
 * Clear password from device config via SSH without resetting the entire config.
 * This keeps the device in a configured state (not onboarding) but removes password protection.
 *
 * The config file is at /userdata/kvm_config.json with fields:
 * - hashed_password: the bcrypt hash
 * - local_auth_token: the session token
 * - local_auth_mode: "password" or "noPassword"
 */
export async function clearPasswordViaSSH(): Promise<void> {
  try {
    // Run separate sed commands to avoid complex quoting issues
    // Note: JSON has space after colon, e.g. "key": "value"
    // Clear hashed_password
    await sshExec(
      'sed -i "s/\\"hashed_password\\": \\"[^\\"]*\\"/\\"hashed_password\\": \\"\\"/g" /userdata/kvm_config.json',
    );
    // Clear local_auth_token
    await sshExec(
      'sed -i "s/\\"local_auth_token\\": \\"[^\\"]*\\"/\\"local_auth_token\\": \\"\\"/g" /userdata/kvm_config.json',
    );
    // Set localAuthMode to noPassword (note: camelCase in JSON)
    await sshExec(
      'sed -i "s/\\"localAuthMode\\": \\"[^\\"]*\\"/\\"localAuthMode\\": \\"noPassword\\"/g" /userdata/kvm_config.json',
    );

    // Reboot to apply the config change (the app loads config on startup)
    await rebootDeviceViaSSH(true);
  } catch (error) {
    console.error("[E2E Cleanup] Error clearing password:", error);
    throw error; // Don't swallow errors silently
  }
}

/**
 * Submit wrong password attempts until rate limited or max attempts reached.
 * Returns true if rate limit message was shown.
 *
 * @param page - Playwright page object
 * @param maxAttempts - Maximum number of attempts before giving up (default: 10)
 * @returns Whether rate limit message was detected
 */
export async function triggerRateLimit(page: Page, maxAttempts = 10): Promise<boolean> {
  for (let i = 0; i < maxAttempts; i++) {
    const result = await loginLocal(page, "wrongpassword123", false);

    if (result.error && /too many|rate.?limit|try again/i.test(result.error)) {
      return true;
    }

    // Small delay between attempts
    await page.waitForTimeout(300);
  }

  return false;
}

/**
 * Get the device IP from the JETKVM_URL environment variable.
 *
 * @returns The device IP/hostname
 */
export function getDeviceHost(): string {
  const url = process.env.JETKVM_URL;
  if (!url) {
    throw new Error("JETKVM_URL environment variable is not set");
  }
  return new URL(url).hostname;
}

/**
 * Wait for the device to be reachable via HTTP.
 *
 * @param host - The device hostname/IP
 * @param timeout - Maximum time to wait in milliseconds (default: 60000)
 */
async function waitForDeviceReady(host: string, timeout = 60000): Promise<void> {
  const startTime = Date.now();
  const url = `http://${host}`;

  while (Date.now() - startTime < timeout) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(5000) });
      if (response.ok || response.status === 401 || response.status === 302) {
        // Device is responding (even if it redirects to login)
        return;
      }
    } catch {
      // Device not ready yet, continue waiting
    }
    await new Promise(resolve => setTimeout(resolve, 500));
  }

  throw new Error(`Device at ${host} did not become ready within ${timeout}ms`);
}

/**
 * Reboot the device via SSH to clear in-memory state like rate limiting.
 * This is useful after rate limiting tests to reset the device state.
 *
 * @param waitForReady - Whether to wait for the device to come back online (default: true)
 */
export async function rebootDeviceViaSSH(waitForReady = true): Promise<void> {
  const host = getDeviceHost();

  // SSH connection may be terminated by the reboot, which is expected
  await sshExec("reboot", true);

  if (waitForReady) {
    await new Promise(resolve => setTimeout(resolve, 3000));

    // Wait for device to come back up
    await waitForDeviceReady(host, 60000);

    // Give it a moment to fully initialize
    await new Promise(resolve => setTimeout(resolve, 1000));
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
