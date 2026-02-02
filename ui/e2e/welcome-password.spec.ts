import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  ensureWelcomeState,
  goToWelcomeMode,
  completeWelcomeWithPassword,
  selectWelcomeAuthMode,
  submitWelcomePassword,
  loginLocal,
  logout,
  triggerRateLimit,
  clearPasswordViaSSH,
} from "./helpers";

// Test password that meets the 8-character minimum requirement
const TEST_PASSWORD = "TestPassword123";

// Clean up after all tests - clear any password to ensure clean state for other tests
// Note: clearPasswordViaSSH already reboots the device, so no extra reboot needed
test.afterAll(async () => {
  await clearPasswordViaSSH();
});

test.describe("Welcome Password Flow Tests", () => {
  test.setTimeout(180000); // 3 minutes

  // Note: "welcome flow with no password setup" is covered by config-reset.spec.ts
  // This file focuses on password-specific flows

  test("welcome flow with password setup and login", async ({ page }) => {
    await ensureWelcomeState(page);
    await goToWelcomeMode(page);
    await completeWelcomeWithPassword(page, TEST_PASSWORD);
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
    await ensureWelcomeState(page);
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

test.describe("Login Rate Limiting Tests", () => {
  test.setTimeout(180000); // 3 minutes

  // Note: file-level afterAll already clears password and reboots, which resets rate limiter

  test("rate limiting on login page after multiple failed attempts", async ({ page }) => {
    // Set up the device with a password
    await ensureWelcomeState(page);
    await goToWelcomeMode(page);
    await completeWelcomeWithPassword(page, TEST_PASSWORD);
    await waitForWebRTCReady(page, 45000);

    // Logout and go to login page
    await logout(page);
    await page.goto("/login-local");
    await page.waitForLoadState("networkidle");

    // Trigger rate limit with wrong password attempts
    const rateLimited = await triggerRateLimit(page, 10);

    expect(rateLimited).toBe(true);

    // Verify the rate limit message is visible
    const errorMessage = page.locator(".text-red-500, .text-red-600").first();
    const errorText = await errorMessage.textContent();
    expect(errorText).toMatch(/too many|try again/i);
  });
});
