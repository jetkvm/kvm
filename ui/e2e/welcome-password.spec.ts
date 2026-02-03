import { test, expect } from "@playwright/test";

import {
  resetDeviceToWelcome,
  goToWelcomeMode,
  selectWelcomeAuthMode,
  submitWelcomePassword,
  loginLocal,
  logout,
} from "./helpers";

// Test password that meets the 8-character minimum requirement
const TEST_PASSWORD = "TestPassword123";

test.describe("Welcome Password Flow Tests", () => {
  test.setTimeout(180000); // 3 minutes

  // Note: "welcome flow with no password setup" is covered by config-reset.spec.ts
  // This file focuses on password-specific flows

  test("welcome flow with password setup and login", async ({ page }) => {
    await resetDeviceToWelcome(page);
    await goToWelcomeMode(page);
    await selectWelcomeAuthMode(page, "password");
    await submitWelcomePassword(page, TEST_PASSWORD);
    await logout(page);

    // Navigate to root - app will redirect to login since password is set
    await page.goto("/");
    await page.waitForURL("**/login-local", { timeout: 15000 });

    // Login with the password we just set
    await loginLocal(page, TEST_PASSWORD);

    // Verify we're on the main page
    expect(page.url()).not.toContain("/login");

    // Logout and verify we can reach login page
    await logout(page);
    await page.goto("/login-local");
    await page.waitForLoadState("networkidle");

    expect(page.url()).toContain("/login-local");
    await expect(page.locator('input[name="password"]')).toBeVisible({ timeout: 5000 });
  });

  test("password minimum length validation during welcome", async ({ page }) => {
    await resetDeviceToWelcome(page);
    await goToWelcomeMode(page);

    // Select password mode and navigate to password page
    await selectWelcomeAuthMode(page, "password");

    // Try to set a password that's too short (less than 8 characters)
    await submitWelcomePassword(page, "short", "short", false);

    // Should show password length error
    const errorMessage = page.locator(".text-red-500, .text-red-600").first();
    await expect(errorMessage).toBeVisible({ timeout: 5000 });
    const errorText = await errorMessage.textContent();
    expect(errorText).toMatch(/at least 8 characters/i);

    // Still on password page (not redirected)
    expect(page.url()).toContain("/welcome/password");
  });
});
