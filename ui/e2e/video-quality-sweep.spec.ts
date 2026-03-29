/**
 * Video quality sweep benchmark.
 *
 * Tests increasing quality factors (including >1.0 which overdrives the
 * bitrate formula) to find the highest bitrate the RV1106 encoder can
 * sustain without introducing lag (measured via jitter buffer avg delay).
 *
 * Run: JETKVM_URL=http://<ip> npx playwright test --project=video-quality --grep "sweep"
 */
import { test, expect, type Page } from "@playwright/test";

import {
  ensureLocalAuthMode,
  waitForWebRTCReady,
  callJsonRpc,
  startVisualNoise,
  stopVisualNoise,
} from "./helpers";

interface Measurement {
  factor: number;
  bitrateKbps: number;
  jitterBufMs: number;
  fps: number;
}

async function measureAtFactor(page: Page, factor: number): Promise<Measurement> {
  await callJsonRpc(page, "setStreamQualityFactor", { factor });
  // RPC blocks ~5s (useExtraLock), then wait for pipeline to stabilize.
  await page.waitForTimeout(2000);

  await expect
    .poll(() => page.evaluate(() => window.__kvmTestHooks?.isVideoStreamActive()), {
      timeout: 10000,
    })
    .toBeTruthy();

  // Warm up — let the encoder settle at the new bitrate.
  await page.waitForTimeout(1000);

  const snap1 = await page.evaluate(() => window.__kvmTestHooks?.getInboundVideoStats());
  expect(snap1).not.toBeNull();

  await page.waitForTimeout(3000);

  const snap2 = await page.evaluate(() => window.__kvmTestHooks?.getInboundVideoStats());
  expect(snap2).not.toBeNull();

  const deltaBytes = snap2!.bytesReceived - snap1!.bytesReceived;
  const deltaSec = (snap2!.timestamp - snap1!.timestamp) / 1000;

  const deltaDelay = snap2!.jitterBufferDelay - snap1!.jitterBufferDelay;
  const deltaEmitted = snap2!.jitterBufferEmittedCount - snap1!.jitterBufferEmittedCount;

  return {
    factor,
    bitrateKbps: (deltaBytes * 8) / 1000 / deltaSec,
    jitterBufMs: deltaEmitted > 0 ? (deltaDelay / deltaEmitted) * 1000 : -1,
    fps: snap2!.framesPerSecond,
  };
}

test.describe("Video quality sweep", () => {
  // Each factor takes ~11s (5s lock + 2s settle + 1s warmup + 3s sample).
  // 7 factors × 11s = ~77s + setup overhead.
  test.setTimeout(180_000);

  test("sweep quality factors to find max sustainable bitrate", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");
    await ensureLocalAuthMode(page, { mode: "noPassword" });
    await waitForWebRTCReady(page);

    const originalQuality = (await callJsonRpc(page, "getStreamQualityFactor")) as number;

    await startVisualNoise();

    const factors = [0.1, 0.5, 1.0, 2.0, 3.0, 5.0, 8.0];
    const results: Measurement[] = [];

    try {
      for (const factor of factors) {
        const m = await measureAtFactor(page, factor);
        results.push(m);

        const targetKbps = 512 + Math.round((2000 - 512) * factor);
        console.log(
          `factor=${factor.toFixed(1).padStart(4)}  ` +
            `target=${String(targetKbps).padStart(6)} kbps  ` +
            `actual=${m.bitrateKbps.toFixed(0).padStart(6)} kbps  ` +
            `jitter-buf=${m.jitterBufMs.toFixed(1).padStart(6)}ms  ` +
            `fps=${m.fps.toFixed(0)}`,
        );
      }

      // Print summary
      console.log("\n--- SUMMARY ---");
      const baseline = results.find(r => r.factor === 1.0)!;
      console.log(`Baseline (1.0): ${baseline.bitrateKbps.toFixed(0)} kbps, ${baseline.jitterBufMs.toFixed(1)}ms`);

      // Find highest factor where jitter-buf stays within 2× of baseline
      const lagThreshold = baseline.jitterBufMs * 2;
      const sustainable = results.filter(r => r.jitterBufMs < lagThreshold && r.jitterBufMs > 0);
      const best = sustainable[sustainable.length - 1];
      if (best) {
        console.log(
          `Best sustainable: factor=${best.factor}, ${best.bitrateKbps.toFixed(0)} kbps, ${best.jitterBufMs.toFixed(1)}ms (threshold: ${lagThreshold.toFixed(1)}ms)`,
        );
      }

      // Basic sanity: the test should at least complete without crashing.
      expect(results.length).toBe(factors.length);
    } finally {
      await stopVisualNoise();
      await callJsonRpc(page, "setStreamQualityFactor", { factor: originalQuality });
    }
  });
});
