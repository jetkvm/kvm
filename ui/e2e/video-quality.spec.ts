import { test, expect } from "@playwright/test";
import {
  callJsonRpc,
  ensureRpcReady,
  waitForVideoStream,
  wakeDisplay,
  rebootAndReconnect,
} from "./helpers";
import { waitForDecodedFrames } from "./helpers/video";

test("Auto quality persists and video survives preset changes", async ({ page }) => {
  test.setTimeout(180000);
  await page.goto("/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  const original = (await callJsonRpc(page, "getStreamQualityFactor")) as number;
  const devicePath = new URL(page.url()).pathname.replace(/\/$/, "");

  try {
    await page.goto(`${devicePath}/settings/video`, { waitUntil: "networkidle" });
    await ensureRpcReady(page);
    const quality = page.locator("select").filter({ has: page.locator('option[value="0.5"]') });
    await expect(quality.locator('option[value="0"]')).toHaveText("Auto");
    await expect(quality).toBeEnabled();
    await quality.selectOption("0");
    // The native settings RPC deliberately waits five seconds after a change.
    await expect(page.getByText("Stream quality set to Auto", { exact: true })).toBeVisible({
      timeout: 15000,
    });
    await expect.poll(() => callJsonRpc(page, "getStreamQualityFactor")).toBe(0);
    await page.reload({ waitUntil: "networkidle" });
    await ensureRpcReady(page);
    await expect(quality).toHaveValue("0");

    await rebootAndReconnect(page);
    expect(await callJsonRpc(page, "getStreamQualityFactor")).toBe(0);
    await page.goto(devicePath || "/", { waitUntil: "networkidle" });
    await ensureRpcReady(page);
    await wakeDisplay(page);
    await waitForVideoStream(page);
    await waitForDecodedFrames(page);
    for (const factor of [0.1, 0.5, 1, 0]) {
      await callJsonRpc(page, "setStreamQualityFactor", { factor });
      await waitForDecodedFrames(page);
      expect(await callJsonRpc(page, "getStreamQualityFactor")).toBe(factor);
    }
  } finally {
    await ensureRpcReady(page);
    await callJsonRpc(page, "setStreamQualityFactor", { factor: original });
  }
});
