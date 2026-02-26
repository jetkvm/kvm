import { test, expect } from "@playwright/test";

import {
  ensureLocalAuthMode,
  openAccessSettings,
  enablePasswordFromSettings,
  changePasswordFromSettings,
  disablePasswordFromSettings,
} from "./helpers";

// Test passwords that meet the 8-character minimum requirement
const TEST_PASSWORD = "TestPassword123";
const NEW_PASSWORD = "NewPassword456";

test.describe("Settings Local Auth Tests", () => {
  test.setTimeout(180000); // 3 minutes

  test("create password from settings when in noPassword mode", async ({ page }) => {
    // Ensure device is in noPassword mode
    await ensureLocalAuthMode(page, { mode: "noPassword" });

    await openAccessSettings(page);
    await enablePasswordFromSettings(page, TEST_PASSWORD);

    // Verify the mode changed - should now show "Disable Protection" button
    const disableButton = page.getByRole("button").filter({ hasText: /Disable Protection/i });
    await expect(disableButton).toBeVisible({ timeout: 5000 });
  });

  test("update password from settings", async ({ page }) => {
    // Ensure device has password set
    await ensureLocalAuthMode(page, { mode: "password", password: TEST_PASSWORD });

    await openAccessSettings(page);
    await changePasswordFromSettings(page, TEST_PASSWORD, NEW_PASSWORD);

    // Verify we're still on settings page (success)
    expect(page.url()).toContain("/settings/access");
  });

  test("delete password from settings", async ({ page }) => {
    // Ensure device has password set
    await ensureLocalAuthMode(page, { mode: "password", password: TEST_PASSWORD });

    await openAccessSettings(page);
    await disablePasswordFromSettings(page, TEST_PASSWORD);

    // Verify the mode changed - should now show "Enable Password" button
    const enableButton = page.getByRole("button").filter({ hasText: /Enable Password/i });
    await expect(enableButton).toBeVisible({ timeout: 5000 });
  });

  test("password minimum length validation in settings create modal", async ({ page }) => {
    // Ensure device is in noPassword mode
    await ensureLocalAuthMode(page, { mode: "noPassword" });

    await openAccessSettings(page);

    // Try to enable with a short password
    await enablePasswordFromSettings(page, "short", "short", false);

    // Should show error message about password length
    const errorMessage = page.locator(".text-red-500").first();
    await expect(errorMessage).toBeVisible({ timeout: 5000 });
    const errorText = await errorMessage.textContent();
    expect(errorText).toMatch(/at least 8 characters/i);
  });

  test("password minimum length validation in settings update modal", async ({ page }) => {
    // Ensure device has password set
    await ensureLocalAuthMode(page, { mode: "password", password: TEST_PASSWORD });

    await openAccessSettings(page);

    // Try to change to a short password
    await changePasswordFromSettings(page, TEST_PASSWORD, "short", "short", false);

    // Should show error message about password length
    const errorMessage = page.locator(".text-red-500").first();
    await expect(errorMessage).toBeVisible({ timeout: 5000 });
    const errorText = await errorMessage.textContent();
    expect(errorText).toMatch(/at least 8 characters/i);
  });
});
