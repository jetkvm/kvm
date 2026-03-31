/**
 * Video pipeline end-to-end benchmark.
 *
 * Measures real-world video pipeline performance under stress (visual noise):
 * - End-to-end latency via binary stripe pattern timestamps
 * - Jitter buffer delay, frame drops, and freezes via WebRTC stats
 *
 * The remote agent renders vertical stripes encoding the current unix millisecond
 * timestamp. The browser decodes the stripes from the WebRTC video and computes
 * the latency. WebRTC inbound-rtp stats are captured over the same window.
 *
 * Run: JETKVM_URL=http://<kvm-ip> JETKVM_REMOTE_HOST=<user@host-ip> \
 *      npx playwright test --config=benchmarks/playwright.config.ts pipeline-benchmark
 */
import { test, expect, type Page } from "@playwright/test";

import { ensureLocalAuthMode, waitForWebRTCReady, waitForVideoStream } from "../e2e/helpers";
import { createRemoteAgent } from "../e2e/remote-agent/remote-agent";

function pad(s: string, n: number): string {
  return s.padStart(n);
}

function percentile(sorted: number[], p: number): number {
  const idx = Math.floor(sorted.length * p);
  return sorted[Math.min(idx, sorted.length - 1)];
}

async function getVideoStats(page: Page) {
  return page.evaluate(() => window.__kvmTestHooks?.getInboundVideoStats());
}

// Must match the constants in e2e/remote-agent/latency.go
const STRIPE_WIDTH = 4;
const STRIPE_SEP = 1;
const STRIPE_STEP = STRIPE_WIDTH + STRIPE_SEP;
const NUM_SYNC_STRIPES = 3;
const NUM_DATA_BITS = 43;
const TOTAL_STRIPES = NUM_SYNC_STRIPES + NUM_DATA_BITS;
const CAPTURE_WIDTH = TOTAL_STRIPES * STRIPE_STEP; // 230
const CAPTURE_HEIGHT = 200;

const COLLECTION_SECONDS = 30;

test.describe("Video pipeline benchmark", () => {
  test.setTimeout(300_000); // 5 min

  test("end-to-end pipeline performance under stress", async ({ page }) => {
    const agent = createRemoteAgent();
    expect(agent, "JETKVM_REMOTE_HOST must be set").not.toBeNull();

    // 1. Deploy remote agent
    await agent!.ensureDeployed();

    // 2. Connect to KVM
    await page.goto("/");
    await page.waitForLoadState("networkidle");
    await ensureLocalAuthMode(page, { mode: "noPassword" });
    await waitForWebRTCReady(page);
    await waitForVideoStream(page);

    // 3. Calibrate clocks
    console.log("\nCalibrating clock offset...");
    const clockOffset = await agent!.calibrateClockOffset(10);
    console.log(`Clock offset: ${clockOffset.toFixed(1)}ms`);

    // 4. Wait for video dimensions
    let dims: { width: number; height: number } | null = null;
    for (let i = 0; i < 30; i++) {
      dims = await page.evaluate(() => window.__kvmTestHooks?.getVideoStreamDimensions() ?? null);
      if (dims) break;
      await page.waitForTimeout(500);
    }
    expect(dims, "Video stream must be active").not.toBeNull();
    console.log(`Video dimensions: ${dims!.width}x${dims!.height}`);

    // 5. Start visual noise + stripe rendering
    await agent!.startVisualNoise();
    await page.waitForTimeout(1000);
    await agent!.startQRDisplay();
    console.log("Stripe display + visual noise started, stabilizing...");
    await page.waitForTimeout(3000);

    // 6. Snapshot WebRTC stats before
    const statsBefore = await getVideoStats(page);
    expect(statsBefore).not.toBeNull();

    // 7. Start latency capture
    await page.evaluate(
      ({ captureW, captureH, stripeStep, numSync, numBits, offset }) => {
        const video = window.__kvmTestHooks?._getVideoElement?.();
        if (!video) throw new Error("No video element");

        const samples: Array<{ timestampMs: number; decodedMs: number; latencyMs: number }> = [];
        const canvas = document.createElement("canvas");
        canvas.width = captureW;
        canvas.height = captureH;
        const ctx = canvas.getContext("2d")!;

        const onFrame = () => {
          ctx.drawImage(video, 0, 0, captureW, captureH, 0, 0, captureW, captureH);
          const imageData = ctx.getImageData(0, 0, captureW, captureH);
          const pixels = imageData.data;

          // Sample each stripe across multiple rows for robustness
          const sampleRows = [
            Math.floor(captureH * 0.25),
            Math.floor(captureH * 0.5),
            Math.floor(captureH * 0.75),
          ];
          const stripeValues: number[] = [];

          for (let i = 0; i < numSync + numBits; i++) {
            const x0 = i * stripeStep;
            let lum = 0;
            let count = 0;
            for (const sampleY of sampleRows) {
              for (let dx = 1; dx < stripeStep - 2; dx++) {
                const off = (sampleY * captureW + x0 + dx) * 4;
                lum += pixels[off] * 0.299 + pixels[off + 1] * 0.587 + pixels[off + 2] * 0.114;
                count++;
              }
            }
            stripeValues.push(lum / count);
          }

          // Check sync pattern: dark, bright, dark
          const threshold = stripeValues[1] / 2;
          const sync0 = stripeValues[0] < threshold;
          const sync1 = stripeValues[1] > threshold;
          const sync2 = stripeValues[2] < threshold;

          if (sync0 && sync1 && sync2 && threshold > 30) {
            let ts = 0;
            for (let bit = 0; bit < numBits; bit++) {
              if (stripeValues[numSync + bit] > threshold) {
                ts += Math.pow(2, numBits - 1 - bit);
              }
            }

            if (ts > 1000000000000) {
              const wallNow = Date.now();
              samples.push({
                timestampMs: wallNow,
                decodedMs: ts,
                latencyMs: wallNow - ts - offset,
              });
            }
          }

          if ((window as any).__latencyActive) {
            video.requestVideoFrameCallback(onFrame);
          }
        };

        (window as any).__latencyActive = true;
        (window as any).__latencySamples = samples;
        video.requestVideoFrameCallback(onFrame);
      },
      {
        captureW: CAPTURE_WIDTH,
        captureH: CAPTURE_HEIGHT,
        stripeStep: STRIPE_STEP,
        numSync: NUM_SYNC_STRIPES,
        numBits: NUM_DATA_BITS,
        offset: clockOffset,
      },
    );

    // 8. Collect
    console.log(`Collecting for ${COLLECTION_SECONDS}s under visual noise stress...`);
    await page.waitForTimeout(COLLECTION_SECONDS * 1000);

    // 9. Stop and gather results
    const samples = await page.evaluate(() => {
      (window as any).__latencyActive = false;
      return (window as any).__latencySamples ?? [];
    });

    const statsAfter = await getVideoStats(page);
    expect(statsAfter).not.toBeNull();

    await agent!.stopQRDisplay();
    await agent!.stopVisualNoise();

    // 10. Compute latency stats
    expect(samples.length).toBeGreaterThan(0);
    const latencies = samples.map((s: any) => s.latencyMs as number).sort((a, b) => a - b);

    const latencyStats = {
      samples: latencies.length,
      rate: latencies.length / COLLECTION_SECONDS,
      min: latencies[0],
      mean: latencies.reduce((a, b) => a + b, 0) / latencies.length,
      median: percentile(latencies, 0.5),
      p95: percentile(latencies, 0.95),
      max: latencies[latencies.length - 1],
    };

    // 11. Compute WebRTC pipeline stats (delta over collection window)
    const droppedFrames = statsAfter!.framesDropped - statsBefore!.framesDropped;
    const freezeCount = statsAfter!.freezeCount - statsBefore!.freezeCount;
    const freezeDurationMs =
      (statsAfter!.totalFreezesDuration - statsBefore!.totalFreezesDuration) * 1000;

    // 12. Print results
    const w = 50;
    console.log("\n" + "=".repeat(w));
    console.log("VIDEO PIPELINE BENCHMARK (under stress)");
    console.log("=".repeat(w));

    console.log("\n  Latency (screen change → browser frame)");
    console.log("  " + "-".repeat(w - 2));
    console.log(pad("Samples:", 16) + pad(latencyStats.samples.toString(), 10));
    console.log(pad("Sample rate:", 16) + pad(`${latencyStats.rate.toFixed(1)}/sec`, 10));
    console.log(pad("Min:", 16) + pad(`${latencyStats.min.toFixed(1)}ms`, 10));
    console.log(pad("Mean:", 16) + pad(`${latencyStats.mean.toFixed(1)}ms`, 10));
    console.log(pad("Median:", 16) + pad(`${latencyStats.median.toFixed(1)}ms`, 10));
    console.log(pad("P95:", 16) + pad(`${latencyStats.p95.toFixed(1)}ms`, 10));
    console.log(pad("Max:", 16) + pad(`${latencyStats.max.toFixed(1)}ms`, 10));

    console.log("\n  Pipeline health");
    console.log("  " + "-".repeat(w - 2));
    console.log(pad("Frames dropped:", 16) + pad(droppedFrames.toString(), 10));
    console.log(pad("Freezes:", 16) + pad(freezeCount.toString(), 10));
    console.log(pad("Freeze time:", 16) + pad(`${freezeDurationMs.toFixed(0)}ms`, 10));

    console.log("\n" + "=".repeat(w));

    if (droppedFrames === 0 && freezeCount === 0) {
      console.log("Pipeline healthy — no drops or freezes");
    }

    // Sanity checks
    expect(latencyStats.median).toBeGreaterThan(0);
    expect(latencyStats.median).toBeLessThan(5000);
    expect(latencyStats.rate).toBeGreaterThan(1);
  });
});
