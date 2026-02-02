import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  verifyHidAndVideo,
  ensureWelcomeState,
  goToWelcomeMode,
  completeWelcomeNoPassword,
  completeWelcomeWithPassword,
  logout,
  triggerRateLimit,
  rebootDeviceViaSSH,
  clearPasswordViaSSH,
} from "./helpers";

// Test password that meets the 8-character minimum requirement
const TEST_PASSWORD = "TestPassword123";

// Clean up after all tests - clear any password and reboot to ensure clean state for other tests
test.afterAll(async () => {
  await clearPasswordViaSSH();
});

test.describe("Welcome Password Flow Tests", () => {
  // These tests modify device configuration, so use a longer timeout
  test.setTimeout(180000); // 3 minutes

  test("welcome flow with no password setup", async ({ page }) => {
    // Reset to welcome state
    await ensureWelcomeState(page);

    // Navigate to mode selection
    await goToWelcomeMode(page);

    // Complete setup without password
    await completeWelcomeNoPassword(page);

    // Navigate to root after reboot (page was disconnected during reboot)
    await page.goto("/");
    await page.waitForLoadState("networkidle");

    // Wait for WebRTC connection
    await waitForWebRTCReady(page, 45000);

    // Verify video, mouse, and keyboard all work
    await verifyHidAndVideo(page);
  });

  test("welcome flow with password setup", async ({ page }) => {
    // Reset to welcome state
    await ensureWelcomeState(page);

    // Navigate to mode selection
    await goToWelcomeMode(page);

    // Complete setup with password
    await completeWelcomeWithPassword(page, TEST_PASSWORD);
    await logout(page);

    // Navigate to root - app will redirect to login since password is set
    await page.goto("/");
    await page.waitForURL("**/login-local", { timeout: 15000 });

    // Login with the password we just set
    await page.locator('input[name="password"]').fill(TEST_PASSWORD);
    await page.getByRole("button", { name: "Log In" }).click();
    await page.waitForURL(/\/$/, { timeout: 15000 });

    // Logout and verify we can reach login page
    await logout(page);
    await page.goto("/login-local");
    await page.waitForLoadState("networkidle");

    // Should be on login page
    expect(page.url()).toContain("/login-local");

    // Verify password input is visible
    const passwordInput = page.locator('input[name="password"]');
    await expect(passwordInput).toBeVisible({ timeout: 5000 });
  });

  test("password minimum length validation during welcome", async ({ page }) => {
    // Reset to welcome state
    await ensureWelcomeState(page);

    // Navigate to mode selection
    await goToWelcomeMode(page);

    // Select password mode
    const passwordRadio = page.locator('input[type="radio"][value="password"]');
    await expect(passwordRadio).toBeVisible({ timeout: 5000 });
    await passwordRadio.click();

    const continueButton = page.getByRole("button", { name: /Continue/i });
    await expect(continueButton).toBeEnabled({ timeout: 5000 });
    await continueButton.click();

    // Wait for password page
    await page.waitForURL("**/welcome/password", { timeout: 10000 });
    await page.waitForLoadState("networkidle");
    await page.waitForTimeout(1000);

    // Try to set a password that's too short (less than 8 characters)
    const passwordInput = page.locator('input[name="password"]');
    const confirmPasswordInput = page.locator('input[name="confirmPassword"]');

    await passwordInput.fill("short");
    await confirmPasswordInput.fill("short");

    // Submit the form
    const submitButton = page.getByRole("button", { name: /Set Password/i });
    await submitButton.click();

    // Wait for error message
    await page.waitForTimeout(500);

    // Should show password length error
    const errorMessage = page.locator(".text-red-500, .text-red-600");
    await expect(errorMessage.first()).toBeVisible({ timeout: 5000 });
    const errorText = await errorMessage.first();
    expect(errorText).toBeVisible();

    // Still on password page (not redirected)
    expect(page.url()).toContain("/welcome/password");
  });
});

test.describe("Login Rate Limiting Tests", () => {
  test.setTimeout(180000); // 3 minutes

  // Reboot device after rate limiting tests to clear the in-memory rate limiter state
  test.afterAll(async () => {
    await rebootDeviceViaSSH();
  });

  test("rate limiting on login page after multiple failed attempts", async ({ page }) => {
    // First, set up the device with a password
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

    // Should eventually show rate limit message
    expect(rateLimited).toBe(true);

    // Verify the rate limit message is visible
    const errorMessage = page.locator(".text-red-500, .text-red-600").first();
    const errorText = await errorMessage.textContent();
    expect(errorText).toMatch(/too many|try again/i);
  });
});
