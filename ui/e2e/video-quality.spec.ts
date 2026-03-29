import { test, expect, type Page } from "@playwright/test";

import {
  ensureLocalAuthMode,
  waitForWebRTCReady,
  callJsonRpc,
  startVisualNoise,
  stopVisualNoise,
} from "./helpers";

interface QualityMeasurement {
  bitrateKbps: number;
  jitterBufferAvgDelayMs: number;
  fps: number;
}

/**
 * Measures bitrate AND jitter buffer average delay over a sampling window.
 * Jitter buffer avg delay = delta(jitterBufferDelay) / delta(jitterBufferEmittedCount) * 1000
 */
async function measureQuality(page: Page, sampleMs = 3000): Promise<QualityMeasurement> {
  const snap1 = await page.evaluate(() => window.__kvmTestHooks?.getInboundVideoStats());
  expect(snap1, "first stats snapshot").not.toBeNull();

  await page.waitForTimeout(sampleMs);

  const snap2 = await page.evaluate(() => window.__kvmTestHooks?.getInboundVideoStats());
  expect(snap2, "second stats snapshot").not.toBeNull();

  const deltaBytes = snap2!.bytesReceived - snap1!.bytesReceived;
  const deltaSec = (snap2!.timestamp - snap1!.timestamp) / 1000;
  expect(deltaSec).toBeGreaterThan(0);

  const bitrateKbps = (deltaBytes * 8) / 1000 / deltaSec;

  // Jitter buffer average delay (same formula as connectionStats.tsx)
  const deltaDelay = snap2!.jitterBufferDelay - snap1!.jitterBufferDelay;
  const deltaEmitted = snap2!.jitterBufferEmittedCount - snap1!.jitterBufferEmittedCount;
  const jitterBufferAvgDelayMs =
    deltaEmitted > 0 ? (deltaDelay / deltaEmitted) * 1000 : -1;

  return {
    bitrateKbps,
    jitterBufferAvgDelayMs,
    fps: snap2!.framesPerSecond,
  };
}

test.describe("Video stream quality", () => {
  test.setTimeout(90_000);

  test("changing quality factor produces a measurable bitrate difference", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");
    await ensureLocalAuthMode(page, { mode: "noPassword" });
    await waitForWebRTCReady(page);

    const originalQuality = (await callJsonRpc(page, "getStreamQualityFactor")) as number;

    await startVisualNoise();

    try {
      // ── Low quality (0.1) ──
      await callJsonRpc(page, "setStreamQualityFactor", { factor: 0.1 });
      await page.waitForTimeout(2000);
      await expect
        .poll(() => page.evaluate(() => window.__kvmTestHooks?.isVideoStreamActive()), {
          timeout: 10000,
        })
        .toBeTruthy();

      const low = await measureQuality(page);

      // ── High quality (1.0) ──
      await callJsonRpc(page, "setStreamQualityFactor", { factor: 1.0 });
      await page.waitForTimeout(2000);
      await expect
        .poll(() => page.evaluate(() => window.__kvmTestHooks?.isVideoStreamActive()), {
          timeout: 10000,
        })
        .toBeTruthy();

      const high = await measureQuality(page);

      const ratio = high.bitrateKbps / low.bitrateKbps;

      console.log(
        [
          `LOW:  ${low.bitrateKbps.toFixed(0)} kbps, ${low.jitterBufferAvgDelayMs.toFixed(1)}ms jitter-buf, ${low.fps.toFixed(0)} fps`,
          `HIGH: ${high.bitrateKbps.toFixed(0)} kbps, ${high.jitterBufferAvgDelayMs.toFixed(1)}ms jitter-buf, ${high.fps.toFixed(0)} fps`,
          `RATIO: ${ratio.toFixed(2)}x`,
        ].join("\n"),
      );

      // Quality assertion: high must produce noticeably more data.
      expect(high.bitrateKbps).toBeGreaterThan(low.bitrateKbps * 1.3);

      // Lag guard: jitter buffer delay at high quality should stay reasonable.
      // Baseline is ~55-75ms with visual noise. If bitrate is cranked too high
      // and the encoder can't keep up, this will spike well above 100ms.
      if (high.jitterBufferAvgDelayMs > 0) {
        expect(high.jitterBufferAvgDelayMs).toBeLessThan(100);
      }
    } finally {
      await stopVisualNoise();
      await callJsonRpc(page, "setStreamQualityFactor", { factor: originalQuality });
    }
  });
});
