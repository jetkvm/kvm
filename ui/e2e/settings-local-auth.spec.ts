import { test, expect, type Page } from "@playwright/test";

import {
  ensureLocalAuthMode,
  openAccessSettings,
  enablePasswordFromSettings,
  changePasswordFromSettings,
  disablePasswordFromSettings,
  loginLocal,
} from "./helpers";

const TEST_PASSWORD = "TestPassword123";
const NEW_PASSWORD = "NewPassword456";

async function loginAndOpenSettings(page: Page, password: string) {
  await page.goto("/");
  await page.waitForLoadState("networkidle");
  if (page.url().includes("/login")) {
    await loginLocal(page, password);
  }
  await openAccessSettings(page);
}

test.describe("Settings Local Auth Tests", () => {
  test.setTimeout(180000);
  test.describe.configure({ mode: "serial" });

  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ baseURL: process.env.JETKVM_URL });
    const page = await context.newPage();
    try {
      await ensureLocalAuthMode(page, { mode: "noPassword" });
    } finally {
      await page.close();
      await context.close();
    }
  });

  test("password minimum length validation in settings create modal", async ({ page }) => {
    await openAccessSettings(page);
    await enablePasswordFromSettings(page, "short", "short", false);

    const errorMessage = page.locator(".text-red-500").first();
    await expect(errorMessage).toBeVisible({ timeout: 5000 });
    const errorText = await errorMessage.textContent();
    expect(errorText).toMatch(/at least 8 characters/i);
  });

  test("create password from settings when in noPassword mode", async ({ page }) => {
    await openAccessSettings(page);
    await enablePasswordFromSettings(page, TEST_PASSWORD);

    const disableButton = page.getByRole("button").filter({ hasText: /Disable Protection/i });
    await expect(disableButton).toBeVisible({ timeout: 5000 });
  });

  test("password minimum length validation in settings update modal", async ({ page }) => {
    await loginAndOpenSettings(page, TEST_PASSWORD);
    await changePasswordFromSettings(page, TEST_PASSWORD, "short", "short", false);

    const errorMessage = page.locator(".text-red-500").first();
    await expect(errorMessage).toBeVisible({ timeout: 5000 });
    const errorText = await errorMessage.textContent();
    expect(errorText).toMatch(/at least 8 characters/i);
  });

  test("update password from settings", async ({ page }) => {
    await loginAndOpenSettings(page, TEST_PASSWORD);
    await changePasswordFromSettings(page, TEST_PASSWORD, NEW_PASSWORD);

    expect(page.url()).toContain("/settings/access");
  });

  test("delete password from settings", async ({ page }) => {
    await loginAndOpenSettings(page, NEW_PASSWORD);
    await disablePasswordFromSettings(page, NEW_PASSWORD);

    const enableButton = page.getByRole("button").filter({ hasText: /Enable Password/i });
    await expect(enableButton).toBeVisible({ timeout: 5000 });
  });
});
