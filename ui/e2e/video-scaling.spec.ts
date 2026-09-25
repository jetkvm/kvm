import { test, expect } from "@playwright/test";
import { ensureRpcReady, waitForVideoDimensions, waitForVideoStream, wakeDisplay } from "./helpers";

test("Actual size shows one stream pixel per device pixel and never crops", async ({ page }) => {
  test.setTimeout(120000);
  await page.goto("/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  const devicePath = new URL(page.url()).pathname.replace(/\/$/, "");
  const video = page.locator("video");
  const scaling = page.locator("select").filter({ has: page.locator('option[value="actual"]') });
  const clientSize = () => video.evaluate(v => [v.clientWidth, v.clientHeight]);
  const fillsContainer = () =>
    video.evaluate(
      v =>
        v.clientWidth === v.parentElement!.clientWidth &&
        v.clientHeight === v.parentElement!.clientHeight,
    );
  const insideContainer = () =>
    video.evaluate(v => {
      const inner = v.getBoundingClientRect();
      const outer = v.parentElement!.getBoundingClientRect();
      return (
        inner.left >= outer.left &&
        inner.top >= outer.top &&
        inner.right <= outer.right &&
        inner.bottom <= outer.bottom
      );
    });
  const openVideo = async () => {
    await page.goto(devicePath || "/", { waitUntil: "networkidle" });
    await ensureRpcReady(page);
    await wakeDisplay(page);
    await waitForVideoStream(page);
    return waitForVideoDimensions(page);
  };

  const { width, height } = await openVideo();
  const ratio = await page.evaluate(() => window.devicePixelRatio);
  const actualSize = [Math.round(width / ratio), Math.round(height / ratio)];
  await page.setViewportSize({ width: actualSize[0] + 400, height: actualSize[1] + 600 });

  await page.goto(`${devicePath}/settings/video`, { waitUntil: "networkidle" });
  await scaling.selectOption("actual");
  await page.reload({ waitUntil: "networkidle" });
  await expect(scaling).toHaveValue("actual");

  await openVideo();
  await expect.poll(clientSize).toEqual(actualSize);

  await page.setViewportSize({ width: 800, height: 600 });
  await expect.poll(insideContainer).toBe(true);
  expect(await clientSize()).not.toEqual(actualSize);

  await page.setViewportSize({ width: actualSize[0] + 400, height: actualSize[1] + 600 });
  await page.goto(`${devicePath}/settings/video`, { waitUntil: "networkidle" });
  await scaling.selectOption("fit");
  await openVideo();
  await expect.poll(fillsContainer).toBe(true);
});
