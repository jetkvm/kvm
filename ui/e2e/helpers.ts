import * as http from "http";
import * as https from "https";
import * as fs from "fs";
import * as crypto from "crypto";
import * as os from "os";
import * as path from "path";
import { exec, execSync } from "child_process";
import { promisify } from "util";
import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";

const execAsync = promisify(exec);

export const HID_KEY = {
  SPACE: 0x2c, // 44
  CAPS_LOCK: 0x39, // 57
  NUM_LOCK: 0x53, // 83
} as const;

export interface KeyboardLedState {
  num_lock: boolean;
  caps_lock: boolean;
  scroll_lock: boolean;
  compose: boolean;
  kana: boolean;
  shift: boolean;
}

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

export async function waitForVideoStream(page: Page, timeout = 30000): Promise<void> {
  await expect
    .poll(async () => page.evaluate(() => window.__kvmTestHooks?.isVideoStreamActive()), {
      message: "Waiting for video stream to be active",
      timeout,
      intervals: [200, 500, 1000],
    })
    .toBe(true);
}

export async function wakeDisplay(page: Page, taps = 3, delayMs = 100): Promise<void> {
  for (let i = 0; i < taps; i++) {
    await tapKey(page, HID_KEY.SPACE);
    await page.waitForTimeout(delayMs);
  }
}

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
 * Temporarily pause browser keypress keepalives while preserving held keys.
 */
export async function pauseKeepAlive(page: Page, ms: number): Promise<void> {
  await page.evaluate(durationMs => {
    const hooks = window.__kvmTestHooks;
    if (!hooks) throw new Error("Test hooks not available");
    hooks.pauseKeepAlive(durationMs);
  }, ms);
}

export async function tapKey(page: Page, keyCode: number, holdMs = 20): Promise<void> {
  await sendKeypress(page, keyCode, true);
  await page.waitForTimeout(holdMs);
  await sendKeypress(page, keyCode, false);
}

export async function getLedState(page: Page): Promise<KeyboardLedState | null> {
  return page.evaluate(() => {
    const hooks = window.__kvmTestHooks;
    if (!hooks) return null;
    return hooks.getKeyboardLedState();
  });
}

export interface KeysDownState {
  modifier: number;
  keys: number[];
}

export async function getKeysDownState(page: Page): Promise<KeysDownState | null> {
  return page.evaluate(() => {
    const hooks = window.__kvmTestHooks;
    if (!hooks) return null;
    return hooks.getKeysDownState();
  });
}

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

export interface VideoStreamDimensions {
  width: number;
  height: number;
}

export async function waitForVideoDimensions(
  page: Page,
  timeout = 10000,
): Promise<VideoStreamDimensions> {
  let dims: VideoStreamDimensions | null = null;
  await expect
    .poll(
      async () => {
        dims = await getVideoStreamDimensions(page);
        return (
          dims !== null && dims.width > MIN_VIDEO_DIMENSION && dims.height > MIN_VIDEO_DIMENSION
        );
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

export async function getVideoStreamDimensions(page: Page): Promise<VideoStreamDimensions | null> {
  return page.evaluate(() => {
    const hooks = window.__kvmTestHooks;
    if (!hooks) return null;
    return hooks.getVideoStreamDimensions();
  });
}

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

export async function verifyKeyboardWorks(page: Page): Promise<void> {
  const initialState = await getLedState(page);
  expect(initialState, "LED state should be available").not.toBeNull();
  const initialCapsLock = initialState!.caps_lock;

  await tapKey(page, HID_KEY.CAPS_LOCK);
  await waitForLedState(page, "caps_lock", !initialCapsLock);

  const newState = await getLedState(page);
  expect(newState!.caps_lock, "CAPS_LOCK should have toggled").toBe(!initialCapsLock);

  await tapKey(page, HID_KEY.CAPS_LOCK);
  await waitForLedState(page, "caps_lock", initialCapsLock);
}

/** Verifies video stream, mouse movement, and keyboard LED round-trip. */
export async function verifyHidAndVideo(page: Page): Promise<void> {
  await wakeDisplay(page);
  await waitForVideoStream(page, 10000);
  await waitForVideoDimensions(page);
  await runMouseBidirectionalCheck(page);
  await verifyKeyboardWorks(page);
}

/** Get the current app version from the /metrics endpoint. */
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

export async function reconnectAfterReboot(
  page: Page,
  waitBeforeRetry = 2000,
  maxRetries = 15,
  retryInterval = 2000,
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

const ANIMATION_DELAY = 150;

// Known test passwords - used when device is in unknown state and needs login
const KNOWN_TEST_PASSWORDS = ["TestPassword123", "NewPassword456"];

/**
 * Reset the device to onboarding/welcome state via SSH.
 * Prefer ensureLocalAuthMode() unless testing the welcome flow itself.
 */
export async function resetDeviceToWelcome(page: Page): Promise<void> {
  await page.goto("/");
  await page.waitForLoadState("networkidle");

  const currentUrl = page.url();
  if (currentUrl.includes("/welcome")) {
    if (!currentUrl.endsWith("/welcome")) {
      await page.goto("/welcome");
    }
    await page.waitForTimeout(ANIMATION_DELAY);
    return;
  }

  await resetConfigViaSSH();
  await restartAppViaSSH();
  await page.goto("/");
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(ANIMATION_DELAY);
}

// ── Welcome Flow Primitives ──

export async function goToWelcomeMode(page: Page): Promise<void> {
  const setupButton = page.getByRole("link", { name: /Set up your JetKVM/i });
  await expect(setupButton).toBeVisible({ timeout: 10000 });
  await setupButton.click();

  await page.waitForURL("**/welcome/mode", { timeout: 10000 });
}

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

export async function submitWelcomePassword(
  page: Page,
  password: string,
  confirmPassword?: string,
  expectSuccess = true,
): Promise<void> {
  await page.waitForURL("**/welcome/password", { timeout: 10000 });

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
    await page.waitForTimeout(200);
  }
}

// ── Login/Logout ──

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

  // Race between successful navigation and error message appearance so failed
  // logins resolve quickly (~500ms) instead of waiting for the full URL timeout.
  const errorLocator = page.locator(".text-red-500, .text-red-600").first();
  const outcome = await Promise.race([
    page
      .waitForURL(url => !url.toString().includes("/login"), { timeout: 5000 })
      .then(() => "navigated" as const),
    errorLocator.waitFor({ state: "visible", timeout: 5000 }).then(() => "error" as const),
  ]).catch(() => "timeout" as const);

  if (outcome === "navigated") {
    return { success: true };
  }

  const errorText = await errorLocator.textContent({ timeout: 1000 }).catch(() => null);

  if (expectSuccess) {
    // Test expected success but login failed
    throw new Error(`Login failed: ${errorText || "Unknown error"}`);
  }

  return { success: false, error: errorText || undefined };
}

export async function logout(page: Page): Promise<void> {
  await page.evaluate(async () => {
    await fetch("/auth/logout", { method: "POST" });
  });
  await page.waitForTimeout(100);
}

export async function dismissSessionTakeoverDialog(page: Page): Promise<void> {
  const useHereButton = page.getByRole("button", { name: /Use Here/i });
  if (await useHereButton.isVisible({ timeout: 2000 }).catch(() => false)) {
    await useHereButton.click();
    await page.waitForTimeout(200);
  }
}

// ── Settings Access Page ──

export async function openAccessSettings(page: Page): Promise<void> {
  await page.goto("/settings/access");
  await page.waitForLoadState("networkidle");
  await dismissSessionTakeoverDialog(page);

  // Wait for the local auth section to appear (indicates loaderData is loaded)
  const localSectionHeader = page.locator("text=Authentication Mode");
  await expect(localSectionHeader).toBeVisible({ timeout: 15000 });
}

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

// ── SSH ──

export const SSH_OPTS = [
  "-o UserKnownHostsFile=/dev/null",
  "-o StrictHostKeyChecking=no",
  "-o LogLevel=ERROR",
  "-o ConnectTimeout=30",
  "-o ServerAliveInterval=5",
  "-o ServerAliveCountMax=3",
].join(" ");

const SSH_MAX_RETRIES = 3;
const SSH_RETRY_BASE_DELAY_MS = 2000;
const SSH_COMMAND_TIMEOUT_MS = 15000;

function escapeForSingleQuotedShell(cmd: string): string {
  return cmd.replace(/'/g, "'\\''");
}

export async function sshExec(cmd: string, ignoreErrors = false): Promise<string> {
  const host = getDeviceHost();
  const escapedCmd = escapeForSingleQuotedShell(cmd);
  const sshCmd = `ssh ${SSH_OPTS} root@${host} '${escapedCmd}'`;

  for (let attempt = 1; attempt <= SSH_MAX_RETRIES; attempt++) {
    try {
      const { stdout } = await execAsync(sshCmd, { timeout: SSH_COMMAND_TIMEOUT_MS });
      return stdout;
    } catch (error) {
      if (ignoreErrors) return "";

      const msg = error instanceof Error ? error.message : String(error);
      const isTransient =
        msg.includes("Connection reset") ||
        msg.includes("Connection refused") ||
        msg.includes("Connection timed out") ||
        msg.includes("No route to host") ||
        msg.includes("ETIMEDOUT") ||
        msg.includes("timed out");

      if (isTransient && attempt < SSH_MAX_RETRIES) {
        const delay = SSH_RETRY_BASE_DELAY_MS * attempt;
        console.log(
          `[ssh] Attempt ${attempt}/${SSH_MAX_RETRIES} failed (${msg.split("\n")[0]}), retrying in ${delay}ms...`,
        );
        await new Promise(r => setTimeout(r, delay));
        continue;
      }
      throw error;
    }
  }
  throw new Error("sshExec: unreachable");
}

export async function resetConfigViaSSH(): Promise<void> {
  await sshExec("rm -f /userdata/kvm_config.json");
  await sshExec("sync");
}

export interface SSHDevState {
  sshKey: string;
  devModeEnabled: boolean;
}

export async function saveSSHDevState(): Promise<SSHDevState> {
  const sshKey = await sshExec("cat /userdata/dropbear/.ssh/authorized_keys 2>/dev/null", true);
  const devMode = await sshExec(
    "test -f /userdata/jetkvm/devmode.enable && echo 1 || echo 0",
    true,
  );
  return { sshKey: sshKey.trim(), devModeEnabled: devMode.trim() === "1" };
}

export async function restoreSSHDevState(state: SSHDevState): Promise<void> {
  if (state.sshKey) {
    await sshExec("mkdir -p /userdata/dropbear/.ssh && chmod 700 /userdata/dropbear/.ssh");
    const b64 = Buffer.from(state.sshKey).toString("base64");
    await sshExec(
      `echo ${b64} | base64 -d > /userdata/dropbear/.ssh/authorized_keys && chmod 600 /userdata/dropbear/.ssh/authorized_keys`,
    );
  }
  if (state.devModeEnabled) {
    await sshExec("mkdir -p /userdata/jetkvm && touch /userdata/jetkvm/devmode.enable");
  }
}

export async function restartAppViaSSH(): Promise<void> {
  await sshExec("killall jetkvm_app", true);
  await new Promise(r => setTimeout(r, 500));
  await sshExec(
    "setsid env LD_LIBRARY_PATH=/oem/usr/lib:/oem/lib /userdata/jetkvm/bin/jetkvm_app > /userdata/jetkvm/last.log 2>&1 &",
    true,
  );
  await new Promise(r => setTimeout(r, 1000));
  await waitForDeviceReady(getDeviceHost(), 15000);
}

// ── Local Auth Mode Management ──

export type LocalAuthModeConfig = { mode: "noPassword" } | { mode: "password"; password: string };

/**
 * Ensure the device is in the desired local auth mode.
 * Handles welcome, login, and already-configured states transparently.
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
    const passwordsToTry =
      desired.mode === "password"
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

    await resetConfigViaSSH();
    await restartAppViaSSH();
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

  // Fresh browser context at "/" with no cookies and no redirect means no password is set.
  if (desired.mode === "noPassword") {
    return;
  }

  // Device is configured and we're logged in (or no password) - check current mode
  await openAccessSettings(page);

  const hasDisableButton = await page
    .getByRole("button")
    .filter({ hasText: /Disable Protection/i })
    .isVisible({ timeout: 2000 })
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

/** Clear password fields from device config via SSH (keeps device configured). */
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

    await restartAppViaSSH();
  } catch (error) {
    console.error("[E2E Cleanup] Error clearing password:", error);
    throw error; // Don't swallow errors silently
  }
}

export async function triggerRateLimit(page: Page, maxAttempts = 10): Promise<boolean> {
  for (let i = 0; i < maxAttempts; i++) {
    const result = await loginLocal(page, "wrongpassword123", false);

    if (result.error && /too many|rate.?limit|try again/i.test(result.error)) {
      return true;
    }

    await page.waitForTimeout(100);
  }

  return false;
}

export function getDeviceHost(): string {
  const url = process.env.JETKVM_URL;
  if (!url) {
    throw new Error("JETKVM_URL environment variable is not set");
  }
  return new URL(url).hostname;
}

export async function waitForDeviceReady(host: string, timeout = 60000): Promise<void> {
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

export async function rebootDeviceViaSSH(waitForReady = true): Promise<void> {
  const host = getDeviceHost();

  // SSH connection may be terminated by the reboot, which is expected
  await sshExec("reboot", true);

  if (waitForReady) {
    await new Promise(resolve => setTimeout(resolve, 2000));
    await waitForDeviceReady(host, 60000);
    await new Promise(resolve => setTimeout(resolve, 500));
  }
}

export async function callJsonRpc(
  page: Page,
  method: string,
  params: Record<string, unknown> = {},
  timeoutMs?: number,
): Promise<unknown> {
  return page.evaluate(
    ({ method, params, timeoutMs }) => {
      return new Promise((resolve, reject) => {
        const hooks = window.__kvmTestHooks;
        if (!hooks) return reject(new Error("Test hooks not available"));
        hooks.sendJsonRpc(
          method,
          params,
          (resp: { error?: { message: string; data?: string }; result?: unknown }) => {
            if (resp.error)
              reject(
                new Error(`${resp.error.message}${resp.error.data ? `: ${resp.error.data}` : ""}`),
              );
            else resolve(resp.result);
          },
          timeoutMs,
        );
      });
    },
    { method, params, timeoutMs },
  );
}

// ── OTA: Mock Update Server ──

export interface MockUpdateServerConfig {
  binaryPath: string;
  version: string;
  signaturePath?: string;
  port?: number;
}

export interface MockUpdateServer {
  url: string;
  port: number;
  close: () => Promise<void>;
  enableSignature: (sigPath: string) => void;
  disableSignature: () => void;
}

export async function createMockUpdateServer(
  config: MockUpdateServerConfig,
): Promise<MockUpdateServer> {
  const { binaryPath, version } = config;
  const port = config.port ?? 0;

  if (!fs.existsSync(binaryPath)) {
    throw new Error(`Binary not found: ${binaryPath}`);
  }

  const binaryHash = await computeFileHash(binaryPath);
  const localIP = getLocalNetworkIP();
  const timestamp = Date.now();

  let signaturePath: string | undefined = config.signaturePath;

  const server = http.createServer((req, res) => {
    const url = new URL(req.url!, `http://localhost`);

    if (url.pathname === "/releases") {
      handleReleasesRequest(url, res);
    } else if (url.pathname === `/app/${version}/jetkvm_app`) {
      streamFile(binaryPath, res);
    } else if (url.pathname === `/app/${version}/jetkvm_app.sig` && signaturePath) {
      streamFile(signaturePath, res);
    } else {
      res.writeHead(404);
      res.end("Not found");
    }
  });

  function handleReleasesRequest(url: URL, res: http.ServerResponse) {
    const query = Object.fromEntries(url.searchParams);
    const isCustomVersion = "appVersion" in query || "systemVersion" in query;
    const appVersion = isCustomVersion ? (query.appVersion ?? version) : version;

    const actualPort = (server.address() as { port: number }).port;

    const response: Record<string, unknown> = {
      appVersion,
      appUrl: `http://${localIP}:${actualPort}/app/${version}/jetkvm_app`,
      appHash: binaryHash,
      appCachedAt: timestamp,
      appMaxSatisfying: "*",
      systemVersion: "0.0.1",
      systemUrl: "",
      systemHash: "",
      systemCachedAt: timestamp,
      systemMaxSatisfying: "*",
    };

    if (signaturePath) {
      response.appSigUrl = `http://${localIP}:${actualPort}/app/${version}/jetkvm_app.sig`;
    }

    const body = JSON.stringify(response);
    res.writeHead(200, {
      "Content-Type": "application/json",
      "Content-Length": Buffer.byteLength(body),
    });
    res.end(body);
  }

  function streamFile(filePath: string, res: http.ServerResponse) {
    const stat = fs.statSync(filePath);
    res.writeHead(200, {
      "Content-Length": stat.size,
      "Content-Type": "application/octet-stream",
    });
    fs.createReadStream(filePath).pipe(res);
  }

  await new Promise<void>((resolve, reject) => {
    server.on("error", reject);
    server.listen(port, "0.0.0.0", () => resolve());
  });

  const actualPort = (server.address() as { port: number }).port;
  const serverUrl = `http://${localIP}:${actualPort}`;

  return {
    url: serverUrl,
    port: actualPort,
    close: () =>
      new Promise<void>((resolve, reject) => {
        server.close(err => (err ? reject(err) : resolve()));
      }),
    enableSignature: (sigPath: string) => {
      signaturePath = sigPath;
    },
    disableSignature: () => {
      signaturePath = undefined;
    },
  };
}

// ── OTA: Binary Deployment ──

export async function deployBinaryToDevice(binaryPath: string): Promise<void> {
  if (!fs.existsSync(binaryPath)) {
    throw new Error(`Binary not found: ${binaryPath}`);
  }

  const host = getDeviceHost();
  const sshCmd = `ssh ${SSH_OPTS} root@${host} "cat > /userdata/jetkvm/jetkvm_app.update"`;
  await execAsync(`${sshCmd} < "${binaryPath}"`);
}

// ── OTA: Device Config ──

const PRODUCTION_API_URL = "https://api.jetkvm.com";

export async function configureDeviceUpdateUrl(url: string): Promise<void> {
  await sshExec(
    `sed -i "s|\\"update_api_url\\": \\"[^\\"]*\\"|\\"update_api_url\\": \\"${url}\\"|" /userdata/kvm_config.json`,
  );
}

export async function restoreDeviceUpdateUrl(): Promise<void> {
  try {
    await configureDeviceUpdateUrl(PRODUCTION_API_URL);
  } catch {
    // Best-effort cleanup
  }
}

export async function setIncludePreRelease(value: boolean): Promise<void> {
  await sshExec(
    `sed -i "s|\\"include_pre_release\\": [^,]*|\\"include_pre_release\\": ${value}|" /userdata/kvm_config.json`,
  );
}

// ── OTA: Utilities ──

export async function computeFileHash(filePath: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash("sha256");
    const stream = fs.createReadStream(filePath);
    stream.on("data", data => hash.update(data));
    stream.on("end", () => resolve(hash.digest("hex")));
    stream.on("error", reject);
  });
}

export function getLocalNetworkIP(): string {
  try {
    const routeOutput = execSync("ip route get 1", {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    });
    const routeMatch = routeOutput.match(/\bsrc\s+(\d+\.\d+\.\d+\.\d+)\b/);
    if (routeMatch?.[1]) {
      return routeMatch[1];
    }
  } catch {
    // Fall through to interface scan
  }

  const interfaces = os.networkInterfaces();
  for (const name of Object.keys(interfaces)) {
    for (const iface of interfaces[name]!) {
      if (iface.family === "IPv4" && !iface.internal) {
        return iface.address;
      }
    }
  }
  throw new Error("Could not detect local network IP address");
}

// ── OTA: Production Release Fetching ──

export interface StableReleaseInfo {
  appVersion: string;
  appUrl: string;
  appHash: string;
  appSigUrl?: string;
}

export async function fetchLatestStableRelease(): Promise<StableReleaseInfo> {
  const url = "https://api.jetkvm.com/releases?deviceId=e2e-test";
  const body = await new Promise<string>((resolve, reject) => {
    https
      .get(url, res => {
        if (res.statusCode !== 200) {
          reject(new Error(`Release API returned ${res.statusCode}`));
          res.resume();
          return;
        }
        let data = "";
        res.on("data", chunk => (data += chunk));
        res.on("end", () => resolve(data));
        res.on("error", reject);
      })
      .on("error", reject);
  });

  const json = JSON.parse(body);
  if (!json.appVersion || !json.appUrl || !json.appHash) {
    throw new Error(`Unexpected release API response: ${body}`);
  }
  return {
    appVersion: json.appVersion,
    appUrl: json.appUrl,
    appHash: json.appHash,
    appSigUrl: json.appSigUrl,
  };
}

export async function downloadFile(url: string, destPath: string): Promise<void> {
  const proto = url.startsWith("https") ? https : http;
  const file = fs.createWriteStream(destPath);

  await new Promise<void>((resolve, reject) => {
    const request = (requestUrl: string) => {
      proto
        .get(requestUrl, res => {
          if (
            res.statusCode &&
            res.statusCode >= 300 &&
            res.statusCode < 400 &&
            res.headers.location
          ) {
            request(res.headers.location);
            return;
          }
          if (res.statusCode !== 200) {
            reject(new Error(`Download failed: ${res.statusCode} for ${requestUrl}`));
            res.resume();
            return;
          }
          res.pipe(file);
          file.on("finish", () => file.close(() => resolve()));
          res.on("error", reject);
        })
        .on("error", reject);
    };
    request(url);
  });
}

// ── OTA: Test Helpers ──

export interface OTAEnvVars {
  baselinePath: string;
  releasePath: string;
  releaseVersion: string;
  signaturePath?: string;
}

export function getOTAEnvVars(opts?: { requireSignature?: boolean }): OTAEnvVars {
  const baselinePath = process.env.BASELINE_BINARY_PATH;
  const releasePath = process.env.RELEASE_BINARY_PATH;
  const releaseVersion = process.env.TEST_UPDATE_VERSION;
  const signaturePath = process.env.RELEASE_SIGNATURE_PATH;

  if (!baselinePath) throw new Error("BASELINE_BINARY_PATH is required");
  if (!releasePath) throw new Error("RELEASE_BINARY_PATH is required");
  if (!releaseVersion) throw new Error("TEST_UPDATE_VERSION is required");
  if (opts?.requireSignature && !signaturePath) {
    throw new Error("RELEASE_SIGNATURE_PATH is required");
  }

  return { baselinePath, releasePath, releaseVersion, signaturePath };
}

export function toPreReleaseVersion(version: string): string {
  return version.includes("-") ? version : `${version}-dev.1`;
}

/**
 * Navigate to the update page, dismiss a cached error (Retry button) if present,
 * and click "Update Now".
 */
export async function triggerUpdate(page: Page): Promise<void> {
  await page.goto("/settings/general/update");
  await page.waitForLoadState("networkidle");

  const retryButton = page.getByRole("button", { name: "Retry" });
  if (await retryButton.isVisible({ timeout: 5000 }).catch(() => false)) {
    await retryButton.click();
  }

  const updateButton = page.getByRole("button", { name: "Update Now" });
  await expect(updateButton).toBeVisible({ timeout: 30000 });
  await updateButton.click();
}

/**
 * Create a temporary signature file, run the callback with the mock server
 * configured to serve it, then clean up.
 */
export async function withTempSignature(
  mockServer: MockUpdateServer,
  content: Buffer,
  fn: () => Promise<void>,
): Promise<void> {
  const sigPath = path.join(os.tmpdir(), `e2e_sig_${Date.now()}.sig`);
  fs.writeFileSync(sigPath, content);
  try {
    mockServer.enableSignature(sigPath);
    await fn();
  } finally {
    mockServer.disableSignature();
    fs.unlinkSync(sigPath);
  }
}

/**
 * Compare two semver strings (ignoring prerelease suffixes).
 * Returns true if `version` >= `minimum`.
 */
export function semverGte(version: string, minimum: string): boolean {
  const v = version.replace(/^v/, "").split("-")[0].split(".").map(Number);
  const m = minimum.split(".").map(Number);
  for (let i = 0; i < 3; i++) {
    if ((v[i] ?? 0) > (m[i] ?? 0)) return true;
    if ((v[i] ?? 0) < (m[i] ?? 0)) return false;
  }
  return true;
}

declare global {
  interface Window {
    __kvmTestHooks?: {
      getKeyboardLedState: () => KeyboardLedState | null;
      getKeysDownState: () => { modifier: number; keys: number[] } | null;
      sendKeypress: (key: number, press: boolean) => void;
      sendAbsMouseMove: (x: number, y: number, buttons: number) => void;
      sendJsonRpc: (
        method: string,
        params: Record<string, unknown>,
        callback: (resp: { error?: { message: string; data?: string }; result?: unknown }) => void,
      ) => void;
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
