/**
 * Bitrate sweep benchmark.
 *
 * Run: JETKVM_URL=http://<kvm-ip> JETKVM_REMOTE_HOST=<user@host-ip> npx playwright test --config=benchmarks/playwright.config.ts
 */
import { test, expect, type Page } from "@playwright/test";
import { execSync } from "child_process";

import { ensureLocalAuthMode, waitForWebRTCReady, callJsonRpc } from "../e2e/helpers";

const REMOTE_HOST = process.env.JETKVM_REMOTE_HOST ?? "tony@192.168.1.180";
const SSH_OPTS = "-o StrictHostKeyChecking=no -o ConnectTimeout=5";

function remoteSSH(cmd: string): string {
  return execSync(`ssh ${SSH_OPTS} ${REMOTE_HOST} ${JSON.stringify(cmd)}`, {
    timeout: 10000,
  })
    .toString()
    .trim();
}

function startVisualNoise(): void {
  remoteSSH(
    "DISPLAY=:0 gnome-terminal --full-screen -- bash -c 'while true; do head -c 2000 /dev/urandom | base64; sleep 0.05; done' &>/dev/null &",
  );
}

function stopVisualNoise(): void {
  try {
    remoteSSH(
      "pkill -f 'head -c 2000 /dev/urandom' || true; wmctrl -c :ACTIVE: 2>/dev/null || true",
    );
  } catch {
    // ignore
  }
}

interface Measurement {
  factor: number;
  targetKbps: number;
  actualKbps: number;
  jitterBufMs: number;
  fps: number;
  dropped: number;
  avgDecodeMs: number;
  freezes: number;
  freezeDurationMs: number;
}

async function measureAtFactor(page: Page, factor: number): Promise<Measurement> {
  await callJsonRpc(page, "setStreamQualityFactor", { factor });

  // Fresh WebRTC session — resets jitter buffer state
  await page.goto("about:blank");
  await page.waitForTimeout(500);
  await page.goto("/");
  await page.waitForLoadState("networkidle");
  await ensureLocalAuthMode(page, { mode: "noPassword" });
  await waitForWebRTCReady(page);

  // Wait for encoder to stabilize at new bitrate
  await page.waitForTimeout(3000);

  const snap1 = await page.evaluate(() => window.__kvmTestHooks?.getInboundVideoStats());
  expect(snap1).not.toBeNull();

  await page.waitForTimeout(5000);

  const snap2 = await page.evaluate(() => window.__kvmTestHooks?.getInboundVideoStats());
  expect(snap2).not.toBeNull();

  const deltaBytes = snap2!.bytesReceived - snap1!.bytesReceived;
  const deltaSec = (snap2!.timestamp - snap1!.timestamp) / 1000;

  const deltaDelay = snap2!.jitterBufferDelay - snap1!.jitterBufferDelay;
  const deltaEmitted = snap2!.jitterBufferEmittedCount - snap1!.jitterBufferEmittedCount;

  const deltaDecoded = snap2!.framesDecoded - snap1!.framesDecoded;
  const deltaDecodeTime = snap2!.totalDecodeTime - snap1!.totalDecodeTime;

  // target = 512 + (4000 - 512) * factor
  const targetKbps = 512 + Math.round((4000 - 512) * factor);

  return {
    factor,
    targetKbps,
    actualKbps: (deltaBytes * 8) / 1000 / deltaSec,
    jitterBufMs: deltaEmitted > 0 ? (deltaDelay / deltaEmitted) * 1000 : -1,
    fps: snap2!.framesPerSecond,
    dropped: snap2!.framesDropped - snap1!.framesDropped,
    avgDecodeMs: deltaDecoded > 0 ? (deltaDecodeTime / deltaDecoded) * 1000 : -1,
    freezes: snap2!.freezeCount - snap1!.freezeCount,
    freezeDurationMs: (snap2!.totalFreezesDuration - snap1!.totalFreezesDuration) * 1000,
  };
}

function pad(s: string, n: number): string {
  return s.padStart(n);
}

test.describe("Bitrate sweep benchmark", () => {
  test.setTimeout(600_000); // 10 min

  test("sweep: 4000kbps cap, 1.5x VBR ceiling", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");
    await ensureLocalAuthMode(page, { mode: "noPassword" });
    await waitForWebRTCReady(page);

    const originalQuality = (await callJsonRpc(page, "getStreamQualityFactor")) as number;

    startVisualNoise();
    await page.waitForTimeout(3000);

    const factors = [0.1, 0.3, 0.5, 0.7, 1.0, 0.7, 0.5, 0.3, 0.1];
    const results: Measurement[] = [];

    try {
      const header =
        pad("factor", 7) +
        pad("target", 10) +
        pad("actual", 10) +
        pad("fps", 5) +
        pad("dropped", 9) +
        pad("decode", 9) +
        pad("freezes", 9) +
        pad("jitter", 10);

      console.log("\n" + "=".repeat(header.length));
      console.log("BITRATE SWEEP — Visual noise on remote host");
      console.log("=".repeat(header.length));
      console.log(header);
      console.log("-".repeat(header.length));

      for (const factor of factors) {
        const m = await measureAtFactor(page, factor);
        results.push(m);

        console.log(
          pad(m.factor.toFixed(2), 7) +
            pad(`${m.targetKbps} kb`, 10) +
            pad(`${m.actualKbps.toFixed(0)} kb`, 10) +
            pad(m.fps.toFixed(0), 5) +
            pad(m.dropped.toString(), 9) +
            pad(`${m.avgDecodeMs.toFixed(1)}ms`, 9) +
            pad(m.freezes.toString(), 9) +
            pad(`${m.jitterBufMs.toFixed(1)}ms`, 10),
        );
      }

      // Summary
      console.log("\n" + "=".repeat(header.length));
      console.log("SUMMARY");
      console.log("=".repeat(header.length));

      const max = results.reduce((a, b) => (a.actualKbps > b.actualKbps ? a : b));
      const min = results.reduce((a, b) => (a.actualKbps < b.actualKbps ? a : b));

      console.log(
        `Max (${max.factor}): ${max.actualKbps.toFixed(0)} kbps, ${max.fps.toFixed(0)} fps, ${max.dropped} dropped, ${max.avgDecodeMs.toFixed(1)}ms decode, ${max.freezes} freezes`,
      );
      console.log(
        `Min (${min.factor}): ${min.actualKbps.toFixed(0)} kbps, ${min.fps.toFixed(0)} fps, ${min.dropped} dropped, ${min.avgDecodeMs.toFixed(1)}ms decode, ${min.freezes} freezes`,
      );
      console.log(`Dynamic range: ${(max.actualKbps / min.actualKbps).toFixed(1)}x bitrate`);

      const totalDropped = results.reduce((s, r) => s + r.dropped, 0);
      const totalFreezes = results.reduce((s, r) => s + r.freezes, 0);
      console.log(
        `\nHealth: ${totalDropped} total frames dropped, ${totalFreezes} total freezes across all factors`,
      );

      if (totalDropped === 0 && totalFreezes === 0) {
        console.log("✓ Pipeline healthy — no drops or freezes at any factor");
      }

      // Find first factor with drops
      const firstDrop = results.find(r => r.dropped > 0);
      if (firstDrop) {
        console.log(
          `⚠ First drops at factor ${firstDrop.factor}: ${firstDrop.dropped} frames dropped, ${firstDrop.actualKbps.toFixed(0)} kbps`,
        );
      }

      expect(results.length).toBe(factors.length);
    } finally {
      stopVisualNoise();
      await callJsonRpc(page, "setStreamQualityFactor", { factor: originalQuality });
    }
  });
});
