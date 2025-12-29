import { test, expect } from "@playwright/test";

import { verifyHidAndVideo, reconnectAfterReboot } from "./helpers";

// Time to wait for device to reboot (ms)
const REBOOT_DELAY = 15000;

// Time to wait for settings to apply (ms)
const SETTINGS_APPLY_DELAY = 1000;

test.describe("HDMI Sleep Mode and Reboot Tests", () => {
  // This test involves rebooting the device, so use a longer timeout
  test.setTimeout(180000); // 3 minutes

  test("toggle HDMI sleep mode, reboot, and verify setting persists", async ({ page }) => {
    // Navigate to hardware settings
    await page.goto("/settings/hardware");
    await page.waitForLoadState("networkidle");

    // Find HDMI sleep mode checkbox and get initial state
    const hdmiSleepLabel = page.locator("label").filter({ hasText: "HDMI Sleep Mode" });
    await expect(hdmiSleepLabel).toBeVisible({ timeout: 10000 });
    const hdmiSleepCheckbox = hdmiSleepLabel.locator('input[type="checkbox"]');
    await expect(hdmiSleepCheckbox).toBeVisible({ timeout: 5000 });

    // Get initial state and toggle it
    const initialState = await hdmiSleepCheckbox.isChecked();
    const expectedStateAfterToggle = !initialState;

    // Toggle HDMI sleep mode
    await hdmiSleepCheckbox.click();
    await page.waitForTimeout(SETTINGS_APPLY_DELAY);

    // Navigate to reboot page
    await page.goto("/settings/general/reboot");
    await page.waitForLoadState("networkidle");

    // Confirm reboot by clicking "Yes" button
    const yesButton = page.getByRole("button", { name: /Yes/i });
    await expect(yesButton).toBeVisible({ timeout: 5000 });
    await yesButton.click();

    // Wait for reboot and reconnect
    await reconnectAfterReboot(page, REBOOT_DELAY);

    // Verify video, mouse, and keyboard all work
    await verifyHidAndVideo(page);

    // Verify HDMI sleep mode setting persisted
    await page.goto("/settings/hardware");
    await page.waitForLoadState("networkidle");

    const hdmiSleepLabelAfter = page.locator("label").filter({ hasText: "HDMI Sleep Mode" });
    await expect(hdmiSleepLabelAfter).toBeVisible({ timeout: 10000 });
    const hdmiSleepCheckboxAfter = hdmiSleepLabelAfter.locator('input[type="checkbox"]');
    await expect(hdmiSleepCheckboxAfter).toBeVisible({ timeout: 5000 });

    const stateAfterReboot = await hdmiSleepCheckboxAfter.isChecked();
    expect(
      stateAfterReboot,
      `HDMI sleep mode should be ${expectedStateAfterToggle ? "enabled" : "disabled"} after reboot`,
    ).toBe(expectedStateAfterToggle);

    // Ensure HDMI sleep mode is enabled for the next test run
    if (!stateAfterReboot) {
      await hdmiSleepCheckboxAfter.click();
      await page.waitForTimeout(SETTINGS_APPLY_DELAY);
    }
  });
});
