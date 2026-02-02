import { test, expect } from "@playwright/test";

import {
  ensureWelcomeState,
  goToWelcomeMode,
  completeWelcomeNoPassword,
  completeWelcomeWithPassword,
  waitForWebRTCReady,
  rebootDeviceViaSSH,
  dismissSessionTakeoverDialog,
  clearPasswordViaSSH,
} from "./helpers";

// Test passwords that meet the 8-character minimum requirement
const TEST_PASSWORD = "TestPassword123";
const NEW_PASSWORD = "NewPassword456";

// Clean up after all tests - clear any password and reboot to ensure clean state for other tests
test.afterAll(async () => {
  await clearPasswordViaSSH();
});

test.describe("Settings Local Auth Tests", () => {
  // These tests modify device configuration, so use a longer timeout
  test.setTimeout(180000); // 3 minutes

  test("create password from settings when in noPassword mode", async ({ page }) => {
    // Set up device without password first
    await ensureWelcomeState(page);
    await goToWelcomeMode(page);
    await completeWelcomeNoPassword(page);
    await waitForWebRTCReady(page, 45000);

    // Navigate to access settings
    await page.goto("/settings/access");
    await page.waitForLoadState("networkidle");

    // Dismiss session takeover dialog if it appears
    await dismissSessionTakeoverDialog(page);

    // Wait for the local auth section to appear (indicates loaderData is loaded)
    // The section header "Local" should be visible when authMode data is available
    const localSectionHeader = page.locator("text=Authentication Mode");
    await expect(localSectionHeader).toBeVisible({ timeout: 15000 });

    // Find and click the "Enable Password" button
    // Note: The button's accessible name includes parent text, so we filter by child text
    const enablePasswordButton = page.getByRole("button").filter({ hasText: /Enable Password/i });
    await expect(enablePasswordButton).toBeVisible({ timeout: 10000 });
    await enablePasswordButton.click();

    // Wait for modal to appear
    await page.waitForTimeout(500);

    // Fill in the password fields in the modal
    const passwordInput = page.locator('input[type="password"]').first();
    const confirmPasswordInput = page.locator('input[type="password"]').nth(1);

    await passwordInput.fill(TEST_PASSWORD);
    await confirmPasswordInput.fill(TEST_PASSWORD);

    // Click the secure/set password button
    const secureButton = page.getByRole("button", { name: /Secure|Set Password/i });
    await secureButton.click();

    // Wait for success modal
    await page.waitForTimeout(1000);

    // Should show success message
    const successMessage = page.locator("text=Password Set Successfully");
    await expect(successMessage).toBeVisible({ timeout: 5000 });

    // Close the modal
    const closeButton = page.getByRole("button", { name: /Close/i });
    await closeButton.click();

    // Verify the mode changed - should now show "Disable Protection" button
    await page.waitForTimeout(500);
    const disableButton = page.getByRole("button").filter({ hasText: /Disable Protection/i });
    await expect(disableButton).toBeVisible({ timeout: 5000 });
  });

  test("update password from settings", async ({ page }) => {
    // Set up device with password first
    await ensureWelcomeState(page);
    await goToWelcomeMode(page);
    await completeWelcomeWithPassword(page, TEST_PASSWORD);
    await waitForWebRTCReady(page, 45000);

    // Navigate to access settings
    await page.goto("/settings/access");
    await page.waitForLoadState("networkidle");

    // Wait for the local auth section to appear
    const localSectionHeader = page.locator("text=Authentication Mode");
    await expect(localSectionHeader).toBeVisible({ timeout: 15000 });

    // Find and click the "Change Password" button
    const changePasswordButton = page.getByRole("button").filter({ hasText: /Change Password/i });
    await expect(changePasswordButton).toBeVisible({ timeout: 10000 });
    await changePasswordButton.click();

    // Wait for modal to appear
    await page.waitForTimeout(500);

    // Fill in the password fields in the modal
    const oldPasswordInput = page.locator('input[type="password"]').first();
    const newPasswordInput = page.locator('input[type="password"]').nth(1);
    const confirmNewPasswordInput = page.locator('input[type="password"]').nth(2);

    await oldPasswordInput.fill(TEST_PASSWORD);
    await newPasswordInput.fill(NEW_PASSWORD);
    await confirmNewPasswordInput.fill(NEW_PASSWORD);

    // Click the update password button
    const updateButton = page.getByRole("button", { name: /Update Password/i });
    await updateButton.click();

    // Wait for success modal
    await page.waitForTimeout(1000);

    // Should show success message
    const successMessage = page.locator("text=Password Updated Successfully");
    await expect(successMessage).toBeVisible({ timeout: 5000 });

    // Close the modal
    const closeButton = page.getByRole("button", { name: /Close/i });
    await closeButton.click();
  });

  test("delete password from settings", async ({ page }) => {
    // Set up device with password first
    await ensureWelcomeState(page);
    await goToWelcomeMode(page);
    await completeWelcomeWithPassword(page, TEST_PASSWORD);
    await waitForWebRTCReady(page, 45000);

    // Navigate to access settings
    await page.goto("/settings/access");
    await page.waitForLoadState("networkidle");

    // Wait for the local auth section to appear
    const localSectionHeader = page.locator("text=Authentication Mode");
    await expect(localSectionHeader).toBeVisible({ timeout: 15000 });

    // Find and click the "Disable Protection" button
    const disableButton = page.getByRole("button").filter({ hasText: /Disable Protection/i });
    await expect(disableButton).toBeVisible({ timeout: 10000 });
    await disableButton.click();

    // Wait for modal to appear
    await page.waitForTimeout(500);

    // Fill in the current password
    const passwordInput = page.locator('input[type="password"]').first();
    await passwordInput.fill(TEST_PASSWORD);

    // Click the disable protection button in modal
    const confirmDisableButton = page.getByRole("button", { name: /Disable.*Protection/i });
    await confirmDisableButton.click();

    // Wait for success modal
    await page.waitForTimeout(1000);

    // Should show success message
    const successMessage = page.locator("text=Password Protection Disabled");
    await expect(successMessage).toBeVisible({ timeout: 5000 });

    // Close the modal
    const closeButton = page.getByRole("button", { name: /Close/i });
    await closeButton.click();

    // Verify the mode changed - should now show "Enable Password" button
    await page.waitForTimeout(500);
    const enableButton = page.getByRole("button").filter({ hasText: /Enable Password/i });
    await expect(enableButton).toBeVisible({ timeout: 5000 });
  });

  test("password minimum length validation in settings create modal", async ({ page }) => {
    // Set up device without password first
    await ensureWelcomeState(page);
    await goToWelcomeMode(page);
    await completeWelcomeNoPassword(page);
    await waitForWebRTCReady(page, 45000);

    // Navigate to access settings
    await page.goto("/settings/access");
    await page.waitForLoadState("networkidle");

    // Wait for the local auth section to appear
    const localSectionHeader = page.locator("text=Authentication Mode");
    await expect(localSectionHeader).toBeVisible({ timeout: 15000 });

    // Find and click the "Enable Password" button
    const enablePasswordButton = page.getByRole("button").filter({ hasText: /Enable Password/i });
    await expect(enablePasswordButton).toBeVisible({ timeout: 10000 });
    await enablePasswordButton.click();

    // Wait for modal to appear
    await page.waitForTimeout(500);

    // Try to set a password that's too short
    const passwordInput = page.locator('input[type="password"]').first();
    const confirmPasswordInput = page.locator('input[type="password"]').nth(1);

    await passwordInput.fill("short");
    await confirmPasswordInput.fill("short");

    // Click the secure button
    const secureButton = page.getByRole("button", { name: /Secure|Set Password/i });
    await secureButton.click();

    // Should show error message about password length
    await page.waitForTimeout(500);
    const errorMessage = page.locator(".text-red-500");
    await expect(errorMessage.first()).toBeVisible({ timeout: 5000 });
    const errorText = await errorMessage.first().textContent();
    expect(errorText).toMatch(/at least 8 characters/i);
  });

  test("password minimum length validation in settings update modal", async ({ page }) => {
    // Set up device with password first
    await ensureWelcomeState(page);
    await goToWelcomeMode(page);
    await completeWelcomeWithPassword(page, TEST_PASSWORD);
    await waitForWebRTCReady(page, 45000);

    // Navigate to access settings
    await page.goto("/settings/access");
    await page.waitForLoadState("networkidle");

    // Wait for the local auth section to appear
    const localSectionHeader = page.locator("text=Authentication Mode");
    await expect(localSectionHeader).toBeVisible({ timeout: 15000 });

    // Find and click the "Change Password" button
    const changePasswordButton = page.getByRole("button").filter({ hasText: /Change Password/i });
    await expect(changePasswordButton).toBeVisible({ timeout: 10000 });
    await changePasswordButton.click();

    // Wait for modal to appear
    await page.waitForTimeout(500);

    // Fill in the password fields - use short new password
    const oldPasswordInput = page.locator('input[type="password"]').first();
    const newPasswordInput = page.locator('input[type="password"]').nth(1);
    const confirmNewPasswordInput = page.locator('input[type="password"]').nth(2);

    await oldPasswordInput.fill(TEST_PASSWORD);
    await newPasswordInput.fill("short");
    await confirmNewPasswordInput.fill("short");

    // Click the update password button
    const updateButton = page.getByRole("button", { name: /Update Password/i });
    await updateButton.click();

    // Should show error message about password length
    await page.waitForTimeout(500);
    const errorMessage = page.locator(".text-red-500");
    await expect(errorMessage.first()).toBeVisible({ timeout: 5000 });
    const errorText = await errorMessage.first().textContent();
    expect(errorText).toMatch(/at least 8 characters/i);
  });
});

test.describe("Settings Rate Limiting Tests", () => {
  test.setTimeout(180000); // 3 minutes

  // Reboot device after rate limiting tests to clear the in-memory rate limiter state
  test.afterAll(async () => {
    await rebootDeviceViaSSH();
  });

  test("rate limiting on update password with wrong old password", async ({ page }) => {
    // Set up device with password first
    await ensureWelcomeState(page);
    await goToWelcomeMode(page);
    await completeWelcomeWithPassword(page, TEST_PASSWORD);
    await waitForWebRTCReady(page, 45000);

    // Navigate to access settings
    await page.goto("/settings/access");
    await page.waitForLoadState("networkidle");

    // Wait for the local auth section to appear
    const localSectionHeader = page.locator("text=Authentication Mode");
    await expect(localSectionHeader).toBeVisible({ timeout: 15000 });

    // Find and click the "Change Password" button
    const changePasswordButton = page.getByRole("button").filter({ hasText: /Change Password/i });
    await expect(changePasswordButton).toBeVisible({ timeout: 10000 });
    await changePasswordButton.click();

    // Wait for modal to appear
    await page.waitForTimeout(500);

    // Try multiple wrong old password attempts
    let rateLimited = false;
    for (let i = 0; i < 10; i++) {
      // Fill in the password fields with wrong old password
      const oldPasswordInput = page.locator('input[type="password"]').first();
      const newPasswordInput = page.locator('input[type="password"]').nth(1);
      const confirmNewPasswordInput = page.locator('input[type="password"]').nth(2);

      await oldPasswordInput.fill("wrongpassword");
      await newPasswordInput.fill(NEW_PASSWORD);
      await confirmNewPasswordInput.fill(NEW_PASSWORD);

      // Click the update password button
      const updateButton = page.getByRole("button", { name: /Update Password/i });
      await updateButton.click();

      // Wait for response
      await page.waitForTimeout(500);

      // Check for rate limit message
      const errorMessage = page.locator(".text-red-500");
      const errorText = await errorMessage.first().textContent();
      if (errorText && /too many|try again/i.test(errorText)) {
        rateLimited = true;
        break;
      }

      // Clear inputs for next attempt
      await oldPasswordInput.clear();
      await newPasswordInput.clear();
      await confirmNewPasswordInput.clear();
      await page.waitForTimeout(200);
    }

    expect(rateLimited).toBe(true);
  });

  test("rate limiting on delete password with wrong password", async ({ page }) => {
    // Reboot device first to clear any rate limiting from previous test
    await rebootDeviceViaSSH();

    // Set up device with password first
    await ensureWelcomeState(page);
    await goToWelcomeMode(page);
    await completeWelcomeWithPassword(page, TEST_PASSWORD);
    await waitForWebRTCReady(page, 45000);

    // Navigate to access settings
    await page.goto("/settings/access");
    await page.waitForLoadState("networkidle");

    // Wait for the local auth section to appear
    const localSectionHeader = page.locator("text=Authentication Mode");
    await expect(localSectionHeader).toBeVisible({ timeout: 15000 });

    // Find and click the "Disable Protection" button
    const disableButton = page.getByRole("button").filter({ hasText: /Disable Protection/i });
    await expect(disableButton).toBeVisible({ timeout: 10000 });
    await disableButton.click();

    // Wait for modal to appear
    await page.waitForTimeout(500);

    // Try multiple wrong password attempts
    let rateLimited = false;
    for (let i = 0; i < 10; i++) {
      // Fill in wrong password
      const passwordInput = page.locator('input[type="password"]').first();
      await passwordInput.fill("wrongpassword");

      // Click the disable protection button
      const confirmDisableButton = page.getByRole("button", { name: /Disable.*Protection/i });
      await confirmDisableButton.click();

      // Wait for response
      await page.waitForTimeout(500);

      // Check for rate limit message
      const errorMessage = page.locator(".text-red-500");
      const errorText = await errorMessage.first().textContent();
      if (errorText && /too many|try again/i.test(errorText)) {
        rateLimited = true;
        break;
      }

      // Clear input for next attempt
      await passwordInput.clear();
      await page.waitForTimeout(200);
    }

    expect(rateLimited).toBe(true);
  });
});
