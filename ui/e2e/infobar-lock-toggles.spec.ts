import { test, expect } from "@playwright/test";

import { ensureLocalAuthMode, waitForWebRTCReady, getLedState, waitForLedState } from "./helpers";

test.describe("InfoBar lock toggle buttons", () => {
  test.setTimeout(60_000);

  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");
    await ensureLocalAuthMode(page, { mode: "noPassword" });
    await waitForWebRTCReady(page);
  });

  test("Caps Lock toggle button sends scancode and reflects LED state", async ({ page }) => {
    const initial = await getLedState(page);
    expect(initial, "LED state should be available").not.toBeNull();
    const initialCapsLock = initial!.caps_lock;

    const capsBtn = page.getByTestId("caps-lock-toggle");
    await expect(capsBtn).toBeVisible();

    await capsBtn.click();
    await waitForLedState(page, "caps_lock", !initialCapsLock);

    await capsBtn.click();
    await waitForLedState(page, "caps_lock", initialCapsLock);
  });

  test("Num Lock toggle button sends scancode and reflects LED state", async ({ page }) => {
    const initial = await getLedState(page);
    expect(initial, "LED state should be available").not.toBeNull();
    const initialNumLock = initial!.num_lock;

    const numBtn = page.getByTestId("num-lock-toggle");
    await expect(numBtn).toBeVisible();

    await numBtn.click();
    await waitForLedState(page, "num_lock", !initialNumLock);

    await numBtn.click();
    await waitForLedState(page, "num_lock", initialNumLock);
  });
});
