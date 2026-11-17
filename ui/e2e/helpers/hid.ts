import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import {
  waitForDecodedFrames,
  waitForVideoDimensions,
  waitForVideoStream,
  wakeDisplay,
} from "./video";

export const HID_KEY = {
  LEFT_SHIFT: 0xe1, // 225
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
export const MIN_VIDEO_DIMENSION = 100;

// Mouse verification tuning
const MOUSE_DISTANCE_THRESHOLD = 10;

const MOUSE_VERIFY_RETRIES = 3;

const MOUSE_SETTLE_MS = 150;

// A HID move shows up in the video after a variable delay: encoder, network,
// jitter buffer and decoder all add to it, and right after a reboot it can be
// well above the settle time. Poll for the expected change instead of
// sampling once at a fixed delay.
const MOUSE_CHANGE_TIMEOUT_MS = 2000;

const MOUSE_POLL_MS = 50;

interface VideoRegion {
  x: number;
  y: number;
  width: number;
  height: number;
}

async function captureRegion(page: Page, region: VideoRegion): Promise<number[]> {
  const fp = await captureVideoRegionFingerprint(
    page,
    region.x,
    region.y,
    region.width,
    region.height,
  );
  expect(fp, "failed to capture video region").not.toBeNull();
  return fp!;
}

// Capture the region once two consecutive samples match, so the baseline is
// not a frame still catching up with the previous move.
async function captureStableRegion(
  page: Page,
  region: VideoRegion,
  timeoutMs: number,
): Promise<number[]> {
  const deadline = Date.now() + timeoutMs;
  let previous = await captureRegion(page, region);
  for (;;) {
    await page.waitForTimeout(MOUSE_POLL_MS * 2);
    const current = await captureRegion(page, region);
    if (fingerprintDistance(previous, current) === 0 || Date.now() >= deadline) return current;
    previous = current;
  }
}

// Poll the region until its distance from `reference` satisfies `done`, or
// the timeout passes. Returns the last distance either way.
async function waitForRegionDistance(
  page: Page,
  region: VideoRegion,
  reference: number[],
  done: (distance: number) => boolean,
  timeoutMs: number,
): Promise<number> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const distance = fingerprintDistance(reference, await captureRegion(page, region));
    if (done(distance) || Date.now() >= deadline) return distance;
    await page.waitForTimeout(MOUSE_POLL_MS);
  }
}

export interface MouseBidirCheckOptions {
  retries?: number;
  threshold?: number;
  settleMs?: number;
  changeTimeoutMs?: number;
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
    changeTimeoutMs = MOUSE_CHANGE_TIMEOUT_MS,
  } = options;

  // Wait for video dimensions to be available (with polling)
  const { width: videoWidth, height: videoHeight } = await waitForVideoDimensions(page);

  const testHidX = options.testHidX ?? Math.floor(HID_MAX * 0.7);
  const testHidY = options.testHidY ?? Math.floor(HID_MAX * 0.7);
  const testPixel = hidToPixelCoords(testHidX, testHidY, videoWidth, videoHeight);

  const region: VideoRegion = {
    x: Math.max(0, testPixel.x - CAPTURE_REGION_SIZE / 2),
    y: Math.max(0, testPixel.y - CAPTURE_REGION_SIZE / 2),
    width: 0,
    height: 0,
  };
  region.width = Math.min(CAPTURE_REGION_SIZE, videoWidth - region.x);
  region.height = Math.min(CAPTURE_REGION_SIZE, videoHeight - region.y);

  let lastDistArrive = -1;
  let lastDistRestore = -1;

  for (let attempt = 1; attempt <= retries; attempt++) {
    await sendAbsMouseMove(page, 0, 0);
    await page.waitForTimeout(settleMs);
    const fpA = await captureStableRegion(page, region, changeTimeoutMs);

    await sendAbsMouseMove(page, testHidX, testHidY);
    const distArrive = await waitForRegionDistance(
      page,
      region,
      fpA,
      distance => distance > threshold,
      changeTimeoutMs,
    );

    await sendAbsMouseMove(page, 0, 0);
    const distRestore = await waitForRegionDistance(
      page,
      region,
      fpA,
      distance => distance < distArrive / 2,
      changeTimeoutMs,
    );

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
  await waitForDecodedFrames(page);
  await runMouseBidirectionalCheck(page);
  await verifyKeyboardWorks(page);
}
