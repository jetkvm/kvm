import { test, expect } from "@playwright/test";
import { resetDeviceToWelcome, ensureNoPasswordViaAPI, ensureRpcReady } from "../helpers";
import { registerResetCleanup } from "../helpers/reset";

registerResetCleanup();

test("factory-reset: reset via RPC and complete setup after reboot @destructive", async ({
  page,
}) => {
  test.setTimeout(180_000);
  await resetDeviceToWelcome(page);
  await expect(page).toHaveURL(/\/welcome$/);
  const status = await page.request.get("/device/status");
  expect(await status.json()).toMatchObject({ isSetup: false });
  await ensureNoPasswordViaAPI();
  await ensureRpcReady(page, { navigateFirst: true });
  expect(await (await page.request.get("/device/status")).json()).toMatchObject({ isSetup: true });
});
