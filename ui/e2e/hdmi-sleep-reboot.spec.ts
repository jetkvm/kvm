import { test, expect } from "@playwright/test";

import { waitForWebRTCReady, waitForVideoStream, wakeDisplay, verifyHidAndVideo } from "./helpers";

// Time to wait for device to reboot (ms)
const REBOOT_DELAY = 15000;

// Time to wait for settings to apply (ms)
const SETTINGS_APPLY_DELAY = 1000;

test.describe("HDMI Sleep Mode and Reboot Tests", () => {
  // This test involves rebooting the device, so use a longer timeout
  test.setTimeout(180000); // 3 minutes

  test("toggle HDMI sleep mode, reboot, and verify setting persists", async ({ page }) => {
    // === Step 1: Navigate to hardware settings ===
    await page.goto("/settings/hardware");
    await page.waitForLoadState("networkidle");

    // === Step 2: Find HDMI sleep mode checkbox and get initial state ===
    // SettingsItem renders as a <label> containing both the title and the checkbox
    const hdmiSleepLabel = page.locator("label").filter({ hasText: "HDMI Sleep Mode" });
    await expect(hdmiSleepLabel).toBeVisible({ timeout: 10000 });
    const hdmiSleepCheckbox = hdmiSleepLabel.locator('input[type="checkbox"]');
    await expect(hdmiSleepCheckbox).toBeVisible({ timeout: 5000 });

    // Get initial state and toggle it
    const initialState = await hdmiSleepCheckbox.isChecked();
    const expectedStateAfterToggle = !initialState;
    console.log(`✓ Initial HDMI sleep mode state: ${initialState ? "enabled" : "disabled"}`);

    // === Step 3: Toggle HDMI sleep mode ===
    await hdmiSleepCheckbox.click();
    await page.waitForTimeout(SETTINGS_APPLY_DELAY);
    console.log(`✓ Toggled HDMI sleep mode to: ${expectedStateAfterToggle ? "enabled" : "disabled"}`);

    // === Step 4: Navigate to reboot page ===
    await page.goto("/settings/general/reboot");
    await page.waitForLoadState("networkidle");

    // === Step 5: Confirm reboot by clicking "Yes" button ===
    const yesButton = page.getByRole("button", { name: /Yes/i });
    await expect(yesButton).toBeVisible({ timeout: 5000 });
    await yesButton.click();

    console.log("✓ Reboot initiated, waiting for device to come back online...");

    // === Step 6: Wait for device to reboot ===
    await page.waitForTimeout(REBOOT_DELAY);

    // === Step 7: Navigate back to main page ===
    await page.goto("/");

    // === Step 8: Wait for WebRTC connection with extended timeout ===
    // Retry navigating to the page until WebRTC is ready
    let connected = false;
    const maxRetries = 10;
    for (let i = 0; i < maxRetries && !connected; i++) {
      try {
        await page.goto("/", { timeout: 10000 });
        await waitForWebRTCReady(page, 10000);
        connected = true;
      } catch {
        console.log(`Retry ${i + 1}/${maxRetries}: Device not ready yet...`);
        await page.waitForTimeout(3000);
      }
    }
    if (!connected) {
      throw new Error("Device did not come back online after reboot");
    }

    await waitForWebRTCReady(page, 30000);
    await wakeDisplay(page);
    await waitForVideoStream(page, 45000);

    // === Step 9: Verify video, mouse, and keyboard all work ===
    await verifyHidAndVideo(page);

    console.log("✓ Device rebooted successfully");
    console.log("✓ Video stream is active");
    console.log("✓ Mouse is working");
    console.log("✓ Keyboard is working");

    // === Step 10: Verify HDMI sleep mode setting persisted ===
    await page.goto("/settings/hardware");
    await page.waitForLoadState("networkidle");

    const hdmiSleepLabelAfter = page.locator("label").filter({ hasText: "HDMI Sleep Mode" });
    await expect(hdmiSleepLabelAfter).toBeVisible({ timeout: 10000 });
    const hdmiSleepCheckboxAfter = hdmiSleepLabelAfter.locator('input[type="checkbox"]');
    await expect(hdmiSleepCheckboxAfter).toBeVisible({ timeout: 5000 });

    const stateAfterReboot = await hdmiSleepCheckboxAfter.isChecked();
    console.log(`✓ HDMI sleep mode after reboot: ${stateAfterReboot ? "enabled" : "disabled"}`);

    expect(
      stateAfterReboot,
      `HDMI sleep mode should be ${expectedStateAfterToggle ? "enabled" : "disabled"} after reboot`,
    ).toBe(expectedStateAfterToggle);

    console.log("✓ HDMI sleep mode setting persisted correctly after reboot");

    // === Step 11: Ensure HDMI sleep mode is enabled for the next test run ===
    if (!stateAfterReboot) {
      await hdmiSleepCheckboxAfter.click();
      await page.waitForTimeout(SETTINGS_APPLY_DELAY);
      console.log("✓ Enabled HDMI sleep mode for next test run");
    } else {
      console.log("✓ HDMI sleep mode already enabled for next test run");
    }
  });
});
