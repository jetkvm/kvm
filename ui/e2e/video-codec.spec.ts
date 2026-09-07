import { test, expect, type Page } from "@playwright/test";

import {
  ensureLocalAuthMode,
  waitForWebRTCReady,
  waitForVideoStream,
  wakeDisplay,
  callJsonRpc,
} from "./helpers";

/**
 * Wait for inbound video stats to report a non-empty codec mimeType.
 */
async function getActiveCodec(page: Page, timeout = 15000): Promise<string> {
  let codec = "";
  await expect
    .poll(
      async () => {
        const stats = await page.evaluate(() => window.__kvmTestHooks?.getInboundVideoStats());
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
  await wakeDisplay(page);
  await waitForVideoStream(page);
}

test.describe("Video codec negotiation", () => {
  test.setTimeout(90_000);

  test("H.264 explicit mode: stream active with correct codec in stats", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");
    await ensureLocalAuthMode(page, { mode: "noPassword" });
    await waitForWebRTCReady(page);
    await wakeDisplay(page);
    await waitForVideoStream(page);

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
    await wakeDisplay(page);
    await waitForVideoStream(page);

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
    await wakeDisplay(page);
    await waitForVideoStream(page);

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

test("codec settings use device capabilities and retain an unavailable saved preference", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await page.goto("/");
  await ensureLocalAuthMode(page, { mode: "noPassword" });
  await waitForWebRTCReady(page);
  const supported = await callJsonRpc(page, "getSupportedVideoCodecs");
  expect(supported).toEqual(["h264", "h265"]);
  const original = await callJsonRpc(page, "getVideoCodecPreference");
  try {
    await callJsonRpc(page, "setVideoCodecPreference", { codec: "h265" });
    await page.goto("/settings/video", { waitUntil: "networkidle" });
    await waitForWebRTCReady(page);
    const select = page.locator("select").filter({ has: page.locator('option[value="auto"]') });
    await expect(select).toHaveValue("h265");
    await expect(select.locator('option[value="h265"]')).toHaveJSProperty("disabled", true);
    await expect(select.locator('option[value="h264"]')).toHaveJSProperty("disabled", false);
    await expect(select).toBeEnabled();
    await Promise.all([page.waitForEvent("load"), select.selectOption("h264")]);
    await page.waitForLoadState("networkidle");
    await waitForWebRTCReady(page);
    expect(await callJsonRpc(page, "getVideoCodecPreference")).toBe("h264");
    await page.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(page);
    await wakeDisplay(page);
    await waitForVideoStream(page);
    expect((await getActiveCodec(page)).toLowerCase()).toContain("h264");
    await assertBytesFlowing(page);
    const frames = (await page.evaluate(() => window.__kvmTestHooks?.getInboundVideoStats()))!
      .framesDecoded;
    await expect
      .poll(
        async () =>
          (await page.evaluate(() => window.__kvmTestHooks?.getInboundVideoStats()))?.framesDecoded,
      )
      .toBeGreaterThan(frames);
  } finally {
    await callJsonRpc(page, "setVideoCodecPreference", { codec: original });
  }
});
