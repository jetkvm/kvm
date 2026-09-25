import { test, expect } from "@playwright/test";
import { ensureRpcReady, waitForVideoDimensions, waitForVideoStream, wakeDisplay } from "./helpers";

test("1:1 pixel mapping sizes the video to the stream resolution", async ({ page }) => {
  test.setTimeout(120000);
  await page.goto("/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  const devicePath = new URL(page.url()).pathname.replace(/\/$/, "");
  const video = page.locator("video");
  const clientSize = () => video.evaluate(v => [v.clientWidth, v.clientHeight]);
  const fillsContainer = () =>
    video.evaluate(
      v =>
        v.clientWidth === v.parentElement!.clientWidth &&
        v.clientHeight === v.parentElement!.clientHeight,
    );

  await page.goto(`${devicePath}/settings/video`, { waitUntil: "networkidle" });
  const checkbox = page.getByLabel("1:1 pixel mapping");
  await checkbox.check();
  await page.reload({ waitUntil: "networkidle" });
  await expect(checkbox).toBeChecked();

  await page.goto(devicePath || "/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  await wakeDisplay(page);
  await waitForVideoStream(page);
  const { width, height } = await waitForVideoDimensions(page);
  const ratio = await page.evaluate(() => window.devicePixelRatio);
  await expect.poll(clientSize).toEqual([Math.round(width / ratio), Math.round(height / ratio)]);

  await page.goto(`${devicePath}/settings/video`, { waitUntil: "networkidle" });
  await checkbox.uncheck();
  await page.goto(devicePath || "/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  await waitForVideoStream(page);
  await expect.poll(fillsContainer).toBe(true);
});
