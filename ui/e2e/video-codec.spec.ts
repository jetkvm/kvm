import { test, expect, type Page } from "@playwright/test";

import {
  ensureLocalAuthMode,
  ensureRpcReady,
  waitForWebRTCReady,
  waitForVideoStream,
  wakeDisplay,
  callJsonRpc,
} from "./helpers";

declare global {
  interface Window {
    __e2eCodecRequests: { channel: RTCDataChannel; id: string | number }[];
  }
}

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

  test("H.265 preference gracefully falls back to H.264 when browser lacks support @h265", async ({
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
      const supported = (await callJsonRpc(page, "getSupportedVideoCodecs")) as string[];
      expect(supported).toContain("h264");
      for (const codec of [...supported, "auto"]) {
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

test("codec settings use device capabilities and retain an unavailable saved preference @h265", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await page.goto("/");
  await ensureLocalAuthMode(page, { mode: "noPassword" });
  await ensureRpcReady(page);
  const supported = await callJsonRpc(page, "getSupportedVideoCodecs");
  expect(supported).toEqual(["h264", "h265"]);
  const original = await callJsonRpc(page, "getVideoCodecPreference");
  try {
    await callJsonRpc(page, "setVideoCodecPreference", { codec: "h265" });
    await page.getByRole("button", { name: "Settings", exact: true }).click();
    await page.getByRole("link", { name: "Video", exact: true }).click();
    const select = page.locator("select").filter({ has: page.locator('option[value="auto"]') });
    await expect(select).toHaveValue("h265");
    await expect(select.locator('option[value="h265"]')).toHaveJSProperty("disabled", true);
    await expect(select.locator('option[value="h264"]')).toHaveJSProperty("disabled", false);
    await expect(select).toBeEnabled();
    await Promise.all([page.waitForEvent("load"), select.selectOption("h264")]);
    await page.waitForLoadState("networkidle");
    await ensureRpcReady(page);
    expect(await callJsonRpc(page, "getVideoCodecPreference")).toBe("h264");
    await page.goto("/", { waitUntil: "networkidle" });
    await ensureRpcReady(page);
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

test("codec capability requests recover from errors, invalid replies and timeouts without accepting stale replies", async ({
  page,
}) => {
  test.setTimeout(60_000);
  // Hold only capability requests. The page, RPC callbacks and remaining
  // device traffic use the normal application and WebRTC connection.
  await page.addInitScript(() => {
    window.__e2eCodecRequests = [];
    const send = RTCDataChannel.prototype.send;
    RTCDataChannel.prototype.send = function (data) {
      if (this.label === "rpc" && typeof data === "string") {
        const request = JSON.parse(data);
        if (request.method === "getSupportedVideoCodecs") {
          window.__e2eCodecRequests.push({ channel: this, id: request.id });
          return;
        }
      }
      return send.call(this, data as never);
    };
  });
  const pageErrors: string[] = [];
  page.on("pageerror", error => pageErrors.push(error.message));

  async function expectRequests(count: number) {
    await expect.poll(() => page.evaluate(() => window.__e2eCodecRequests.length)).toBe(count);
  }

  async function reply(index: number, response: object) {
    await page.evaluate(
      ({ index, response }) => {
        const { channel, id } = window.__e2eCodecRequests[index];
        channel.dispatchEvent(
          new MessageEvent("message", {
            data: JSON.stringify({ jsonrpc: "2.0", id, ...response }),
          }),
        );
      },
      { index, response },
    );
  }

  await page.goto("/");
  await ensureLocalAuthMode(page, { mode: "noPassword" });
  await ensureRpcReady(page);
  const original = await callJsonRpc(page, "getVideoCodecPreference");
  try {
    await callJsonRpc(page, "setVideoCodecPreference", { codec: "auto" });
    await page.getByRole("button", { name: "Settings", exact: true }).click();
    await page.getByRole("link", { name: "Video", exact: true }).click();
    const select = page.locator("select").filter({ has: page.locator('option[value="auto"]') });
    const retry = page.getByTestId("video-codec-retry");
    await expectRequests(1);
    await expect(select).toBeDisabled();
    await expect(retry).toBeHidden();

    await reply(0, { error: { code: -32000, message: "temporary failure" } });
    await expect(retry).toBeVisible();
    await expect(select).toBeDisabled();
    await retry.click();
    await expectRequests(2);
    await expect(retry).toBeHidden();

    await reply(1, { result: { invalid: true } });
    await expect(retry).toBeVisible();
    await expect(select).toBeDisabled();
    await retry.click();
    await expectRequests(3);
    await expect(retry).toBeHidden();

    // Leave the third request unanswered to exercise the real 10-second timeout.
    await expect(retry).toBeVisible({ timeout: 15_000 });
    await expect(select).toBeDisabled();
    await retry.click();
    await expectRequests(4);
    await expect(retry).toBeHidden();
    await reply(2, { result: ["h264", "h265"] });
    // Give React time to render any incorrectly accepted stale response.
    await page.evaluate(
      () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))),
    );
    await expect(select).toBeDisabled();
    await expect(retry).toBeHidden();

    await reply(3, { result: ["h264"] });
    await expect(select).toBeEnabled();
    await expect(select).toHaveValue("auto");
    await expect
      .poll(() =>
        select
          .locator("option")
          .evaluateAll(options => options.map(option => (option as HTMLOptionElement).value)),
      )
      .toEqual(["auto", "h264"]);
    await expect(retry).toBeHidden();
    expect(pageErrors).toEqual([]);
  } finally {
    await callJsonRpc(page, "setVideoCodecPreference", { codec: original });
  }
});
