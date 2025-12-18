import { test, expect } from "@playwright/test";

import { waitForWebRTCReady, waitForVideoStream, wakeDisplay, verifyHidAndVideo } from "./helpers";

// Time to wait for device to reboot (ms)
const REBOOT_DELAY = 15000;

// Time to wait for settings to apply (ms)
const SETTINGS_APPLY_DELAY = 1000;

test.describe("HDMI Sleep Mode and Reboot Tests", () => {
  // This test involves rebooting the device, so use a longer timeout
  test.setTimeout(180000); // 3 minutes

  // Restore HDMI sleep mode to its original state after tests (optional cleanup)
  test.afterAll(async ({ browser }) => {
    const page = await browser.newPage();
    try {
      await page.goto("/settings/hardware");
      await page.waitForLoadState("networkidle");
      // We don't necessarily need to restore since we're just disabling it
      // The test leaves it in a known state (disabled)
    } finally {
      await page.close();
    }
  });

  test("disable HDMI sleep mode, reboot, and verify HID/video works", async ({ page }) => {
    // === Step 1: Navigate to hardware settings ===
    await page.goto("/settings/hardware");
    await page.waitForLoadState("networkidle");

    // === Step 2: Find and disable HDMI sleep mode checkbox ===
    // SettingsItem renders as a <label> containing both the title and the checkbox
    // Find the label containing "HDMI Sleep Mode" text and get its checkbox
    const hdmiSleepLabel = page.locator("label").filter({ hasText: "HDMI Sleep Mode" });
    await expect(hdmiSleepLabel).toBeVisible({ timeout: 10000 });
    const hdmiSleepCheckbox = hdmiSleepLabel.locator('input[type="checkbox"]');
    await expect(hdmiSleepCheckbox).toBeVisible({ timeout: 5000 });

    // Check if it's currently enabled and disable it
    const isChecked = await hdmiSleepCheckbox.isChecked();
    if (isChecked) {
      await hdmiSleepCheckbox.click();
      await page.waitForTimeout(SETTINGS_APPLY_DELAY);
      console.log("✓ Disabled HDMI sleep mode");
    } else {
      console.log("✓ HDMI sleep mode was already disabled");
    }

    // === Step 3: Navigate to reboot page ===
    await page.goto("/settings/general/reboot");
    await page.waitForLoadState("networkidle");

    // === Step 4: Confirm reboot by clicking "Yes" button ===
    const yesButton = page.getByRole("button", { name: /Yes/i });
    await expect(yesButton).toBeVisible({ timeout: 5000 });
    await yesButton.click();

    console.log("✓ Reboot initiated, waiting for device to come back online...");

    // === Step 5: Wait for device to reboot ===
    await page.waitForTimeout(REBOOT_DELAY);

    // === Step 6: Navigate back to main page ===
    // The device may redirect automatically, but we explicitly go to "/" to be sure
    await page.goto("/");

    // === Step 7: Wait for WebRTC connection with extended timeout ===
    // After reboot, it may take longer to establish connection
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

    // === Step 8: Verify video, mouse, and keyboard all work ===
    await verifyHidAndVideo(page);

    console.log("✓ Device rebooted successfully");
    console.log("✓ Video stream is active");
    console.log("✓ Mouse is working");
    console.log("✓ Keyboard is working");
  });
});
