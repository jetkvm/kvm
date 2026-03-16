import { test, expect } from "@playwright/test";

import {
  resetDeviceToWelcome,
  goToWelcomeMode,
  selectWelcomeAuthMode,
  submitWelcomePassword,
  loginLocal,
  logout,
} from "./helpers";

const TEST_PASSWORD = "TestPassword123";

test.describe("Welcome Password Flow Tests", () => {
  test.setTimeout(180000);
  test.describe.configure({ mode: "serial" });

  // Validation runs first: SSH-resets into welcome mode, submits invalid
  // password, device stays in onboarding. The next test reuses that state
  // and skips a full SSH reset + reboot cycle (~15-20s saved).

  test("password minimum length validation during welcome", async ({ page }) => {
    await resetDeviceToWelcome(page);
    await goToWelcomeMode(page);
    await selectWelcomeAuthMode(page, "password");
    await submitWelcomePassword(page, "short", "short", false);

    const errorMessage = page.locator(".text-red-500, .text-red-600").first();
    await expect(errorMessage).toBeVisible({ timeout: 5000 });
    const errorText = await errorMessage.textContent();
    expect(errorText).toMatch(/at least 8 characters/i);

    expect(page.url()).toContain("/welcome/password");
  });

  test("welcome flow with password setup and login", async ({ page }) => {
    await resetDeviceToWelcome(page);
    await goToWelcomeMode(page);
    await selectWelcomeAuthMode(page, "password");
    await submitWelcomePassword(page, TEST_PASSWORD);
    await logout(page);

    await page.goto("/");
    await page.waitForURL("**/login-local", { timeout: 15000 });

    await loginLocal(page, TEST_PASSWORD);
    expect(page.url()).not.toContain("/login");

    await logout(page);
    await page.goto("/login-local");

    expect(page.url()).toContain("/login-local");
    await expect(page.locator('input[name="password"]')).toBeVisible({ timeout: 5000 });
  });
});
