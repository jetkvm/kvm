import { test, expect } from "@playwright/test";

import { waitForWebRTCReady, verifyHidAndVideo } from "./helpers";

// Time to wait after reset config before reloading (ms)
const RESET_CONFIG_DELAY = 2000;

// Time to wait for welcome screen animations (ms)
const ANIMATION_DELAY = 500;

test.describe("Config Reset and Welcome Screen Tests", () => {
  // This test modifies device configuration, so use a longer timeout
  test.setTimeout(180000); // 3 minutes

  test("reset config and walk through welcome screen without password", async ({ page }) => {
    // === Step 1: Navigate to the device ===
    await page.goto("/");
    await page.waitForLoadState("networkidle");

    // Check if we're already on the welcome screen (device was previously reset)
    const currentUrl = page.url();
    const isOnWelcome = currentUrl.includes("/welcome");

    if (!isOnWelcome) {
      // Device is set up, need to reset it first

      // === Step 2: Navigate to advanced settings ===
      await page.goto("/settings/advanced");
      await page.waitForLoadState("networkidle");

      // === Step 3: Enable Troubleshooting mode ===
      // SettingsItem renders as a <label> containing both the title and the checkbox
      const troubleshootingLabel = page
        .locator("label")
        .filter({ hasText: "Troubleshooting Mode" });
      await expect(troubleshootingLabel).toBeVisible({ timeout: 10000 });
      const troubleshootingCheckbox = troubleshootingLabel.locator('input[type="checkbox"]');
      await expect(troubleshootingCheckbox).toBeVisible({ timeout: 5000 });

      // Check if troubleshooting mode is already enabled
      const isChecked = await troubleshootingCheckbox.isChecked();
      if (!isChecked) {
        await troubleshootingCheckbox.click();
        await page.waitForTimeout(500);
      }

      // === Step 4: Click Reset Config button ===
      const resetConfigButton = page.getByRole("button", { name: /Reset Config/i });
      await expect(resetConfigButton).toBeVisible({ timeout: 10000 });
      await resetConfigButton.click();

      // === Step 5: Wait for reset to complete and reload ===
      await page.waitForTimeout(RESET_CONFIG_DELAY);
      await page.reload();

      // === Step 6: Should redirect to /welcome screen ===
      await page.waitForURL("**/welcome", { timeout: 10000 });
      await page.waitForLoadState("networkidle");
    } else {
      // Navigate to the base welcome page if we're on a sub-route
      if (!currentUrl.endsWith("/welcome")) {
        await page.goto("/welcome");
        await page.waitForLoadState("networkidle");
      }
    }

    // Wait for animations to complete
    await page.waitForTimeout(ANIMATION_DELAY);

    // === Step 6: Click "Set up your JetKVM" button to go to /welcome/mode ===
    const setupButton = page.getByRole("link", { name: /Set up your JetKVM/i });
    await expect(setupButton).toBeVisible({ timeout: 10000 });
    await setupButton.click();

    // === Step 7: Wait for mode selection page ===
    await page.waitForURL("**/welcome/mode", { timeout: 10000 });
    await page.waitForLoadState("networkidle");
    await page.waitForTimeout(500); // Wait for animations

    // === Step 8: Select "No Password" option ===
    const noPasswordRadio = page.locator('input[type="radio"][value="noPassword"]');
    await expect(noPasswordRadio).toBeVisible({ timeout: 5000 });
    await noPasswordRadio.click();

    // === Step 9: Click Continue button ===
    const continueButton = page.getByRole("button", { name: /Continue/i });
    await expect(continueButton).toBeEnabled({ timeout: 5000 });
    await continueButton.click();

    // === Step 10: Should redirect to main page ===
    await page.waitForURL("/", { timeout: 15000 });

    // === Step 11: Wait for WebRTC connection ===
    await waitForWebRTCReady(page, 45000);

    // === Step 12: Verify video, mouse, and keyboard all work ===
    await verifyHidAndVideo(page);
  });
});
