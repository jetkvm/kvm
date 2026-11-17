import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { HID_KEY, MIN_VIDEO_DIMENSION, tapKey } from "./hid";

export async function waitForVideoStream(page: Page, timeout = 30000): Promise<void> {
  await expect
    .poll(async () => page.evaluate(() => window.__kvmTestHooks?.isVideoStreamActive()), {
      message: "Waiting for video stream to be active",
      timeout,
      intervals: [200, 500, 1000],
    })
    .toBe(true);
}

export async function waitForAudioStream(page: Page, timeout = 10000): Promise<void> {
  await expect
    .poll(async () => page.evaluate(() => window.__kvmTestHooks?.isAudioStreamActive()), {
      message: "Waiting for audio stream to be active",
      timeout,
      intervals: [250, 500],
    })
    .toBe(true);
}

export async function wakeDisplay(page: Page, taps = 3, delayMs = 100): Promise<void> {
  for (let i = 0; i < taps; i++) {
    await tapKey(page, HID_KEY.SPACE);
    await page.waitForTimeout(delayMs);
  }
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

export async function getVideoStreamDimensions(page: Page): Promise<VideoStreamDimensions | null> {
  return page.evaluate(() => {
    const hooks = window.__kvmTestHooks;
    if (!hooks) return null;
    return hooks.getVideoStreamDimensions();
  });
}

// A live track and known dimensions do not mean the browser is decoding yet.
// Right after a reboot the first frames can take a while, and a check that
// samples the video before then compares identical black frames.
export async function waitForDecodedFrames(page: Page, timeout = 15000): Promise<void> {
  const framesDecoded = () =>
    page.evaluate(
      async () => (await window.__kvmTestHooks?.getInboundVideoStats())?.framesDecoded ?? 0,
    );
  const start = await framesDecoded();
  await expect
    .poll(async () => (await framesDecoded()) > start, {
      message: "Waiting for the browser to decode video frames",
      timeout,
      intervals: [200, 300, 500],
    })
    .toBe(true);
}
