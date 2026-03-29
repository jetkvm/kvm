import { test, expect, type Page } from "@playwright/test";

import { ensureLocalAuthMode, waitForWebRTCReady, callJsonRpc } from "./helpers";

/**
 * Wait for inbound video stats to report a non-empty codec mimeType.
 */
async function getActiveCodec(page: Page, timeout = 15000): Promise<string> {
  let codec = "";
  await expect
    .poll(
      async () => {
        const stats = await page.evaluate(() =>
          window.__kvmTestHooks?.getInboundVideoStats(),
        );
        if (stats?.codecMimeType) codec = stats.codecMimeType;
        return codec;
      },
      { timeout, message: "waiting for codec mimeType in inbound-rtp stats" },
    )
    .toBeTruthy();
  return codec;
}

/**
 * Verify that RTP bytes are flowing by sampling bytesReceived twice.
 */
async function assertBytesFlowing(page: Page, sampleMs = 2000): Promise<number> {
  const snap1 = await page.evaluate(() => window.__kvmTestHooks?.getInboundVideoStats());
  expect(snap1, "first stats snapshot").not.toBeNull();

  await page.waitForTimeout(sampleMs);

  const snap2 = await page.evaluate(() => window.__kvmTestHooks?.getInboundVideoStats());
  expect(snap2, "second stats snapshot").not.toBeNull();

  const deltaBytes = snap2!.bytesReceived - snap1!.bytesReceived;
  expect(deltaBytes, "RTP bytes should be flowing").toBeGreaterThan(0);
  return deltaBytes;
}

/**
 * Reconnect by navigating away and back, then wait for WebRTC.
 */
async function reconnect(page: Page): Promise<void> {
  await page.goto("about:blank");
  await page.waitForTimeout(500);
  await page.goto("/");
  await page.waitForLoadState("networkidle");
  await ensureLocalAuthMode(page, { mode: "noPassword" });
  await waitForWebRTCReady(page);
}

test.describe("Video codec negotiation", () => {
  test.setTimeout(90_000);

  test("H.264 explicit mode: stream active with correct codec in stats", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");
    await ensureLocalAuthMode(page, { mode: "noPassword" });
    await waitForWebRTCReady(page);

    const originalCodec = (await callJsonRpc(page, "getVideoCodecPreference")) as string;

    try {
      await callJsonRpc(page, "setVideoCodecPreference", { codec: "h264" });
      await reconnect(page);

      await expect
        .poll(() => page.evaluate(() => window.__kvmTestHooks?.isVideoStreamActive()), {
          timeout: 15000,
        })
        .toBeTruthy();

      const codec = await getActiveCodec(page);
      const bytes = await assertBytesFlowing(page);
      console.log(`H.264 mode: codec=${codec}, bytes=${bytes}`);
      expect(codec.toLowerCase()).toContain("h264");
    } finally {
      await callJsonRpc(page, "setVideoCodecPreference", { codec: originalCodec || "auto" });
    }
  });

  test("H.265 preference gracefully falls back to H.264 when browser lacks support", async ({
    page,
  }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");
    await ensureLocalAuthMode(page, { mode: "noPassword" });
    await waitForWebRTCReady(page);

    const originalCodec = (await callJsonRpc(page, "getVideoCodecPreference")) as string;

    try {
      await callJsonRpc(page, "setVideoCodecPreference", { codec: "h265" });
      // Playwright's Chromium doesn't offer H.265 — resolveCodec should
      // detect this and fall back to H.264 instead of breaking the session.
      await reconnect(page);

      await expect
        .poll(() => page.evaluate(() => window.__kvmTestHooks?.isVideoStreamActive()), {
          timeout: 15000,
        })
        .toBeTruthy();

      const codec = await getActiveCodec(page);
      const bytes = await assertBytesFlowing(page);
      console.log(`H.265 pref (fallback): codec=${codec}, bytes=${bytes}`);
      // Should have fallen back to H.264 since browser doesn't support H.265.
      expect(codec.toLowerCase()).toContain("h264");
    } finally {
      await callJsonRpc(page, "setVideoCodecPreference", { codec: originalCodec || "auto" });
    }
  });

  test("Auto mode: falls back to H.264 when browser lacks H.265 support", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");
    await ensureLocalAuthMode(page, { mode: "noPassword" });
    await waitForWebRTCReady(page);

    const originalCodec = (await callJsonRpc(page, "getVideoCodecPreference")) as string;

    try {
      await callJsonRpc(page, "setVideoCodecPreference", { codec: "auto" });
      await reconnect(page);

      await expect
        .poll(() => page.evaluate(() => window.__kvmTestHooks?.isVideoStreamActive()), {
          timeout: 15000,
        })
        .toBeTruthy();

      const codec = await getActiveCodec(page);
      const bytes = await assertBytesFlowing(page);
      console.log(`Auto mode: codec=${codec}, bytes=${bytes}`);
      expect(codec.toLowerCase()).toContain("h264");
    } finally {
      await callJsonRpc(page, "setVideoCodecPreference", { codec: originalCodec || "auto" });
    }
  });

  test("codec preference round-trips correctly and rejects invalid values", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");
    await ensureLocalAuthMode(page, { mode: "noPassword" });
    await waitForWebRTCReady(page);

    const originalCodec = (await callJsonRpc(page, "getVideoCodecPreference")) as string;

    try {
      for (const codec of ["h264", "h265", "auto"]) {
        await callJsonRpc(page, "setVideoCodecPreference", { codec });
        const result = await callJsonRpc(page, "getVideoCodecPreference");
        expect(result).toBe(codec);
      }

      await expect(
        callJsonRpc(page, "setVideoCodecPreference", { codec: "vp9" }),
      ).rejects.toThrow();
    } finally {
      await callJsonRpc(page, "setVideoCodecPreference", { codec: originalCodec || "auto" });
    }
  });
});
