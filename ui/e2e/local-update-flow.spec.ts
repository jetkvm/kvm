import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  getCurrentVersion,
  verifyHidAndVideo,
} from "./helpers";

/**
 * Local Update Flow E2E Test
 *
 * This test verifies the complete OTA update flow using a local build.
 * It requires the test to be run via scripts/test_local_update.sh which:
 * - Sets up /etc/hosts MITM to redirect api.jetkvm.com
 * - Starts a mock HTTP server with the local binary
 * - Sets environment variables (JETKVM_URL, TEST_UPDATE_VERSION)
 *
 * The test monitors the entire update process including:
 * - Automatic update detection on page load
 * - Download progress
 * - Verification progress
 * - Installation
 * - Device reboot
 * - Post-update verification
 */
test.describe("Local Update Flow", () => {
  // This test takes time: download, verify, install, reboot, reconnect
  test.setTimeout(300000); // 5 minutes

  test("complete OTA update with local build", async ({ page }) => {
    // Get environment variables set by test script
    const baseUrl = process.env.JETKVM_URL || "http://localhost";
    const expectedVersion = process.env.TEST_UPDATE_VERSION;

    if (!expectedVersion) {
      throw new Error("TEST_UPDATE_VERSION environment variable is required");
    }

    console.log(`Testing update to version: ${expectedVersion}`);
    console.log(`Device URL: ${baseUrl}`);

    // Step 1: Get baseline version
    console.log("\n=== Step 1: Get baseline version ===");
    await page.goto("/");
    await waitForWebRTCReady(page);

    const initialVersion = await getCurrentVersion(page);
    expect(initialVersion, "Initial version should be available").not.toBeNull();
    console.log(`Initial version: ${initialVersion}`);

    // Step 2: Navigate to update page
    console.log("\n=== Step 2: Navigate to update page ===");
    await page.goto("/settings/general/update");
    await page.waitForLoadState("networkidle");
    console.log("Navigated to update page - automatic check will start");

    // Step 3: Wait for update check to complete and show available update
    console.log("\n=== Step 3: Wait for update detection ===");
    // The page automatically checks for updates on load
    // Wait for the loading state to complete and update to be detected
    await page.waitForTimeout(2000);

    // Step 4: Verify update available
    console.log("\n=== Step 4: Verify update available ===");
    const availableVersion = page.locator('[data-testid="available-version"]');
    await expect(availableVersion).toBeVisible({ timeout: 15000 });

    const displayedVersion = await availableVersion.textContent();
    console.log(`Available version displayed: ${displayedVersion}`);
    expect(displayedVersion?.trim()).toBe(expectedVersion);
    expect(displayedVersion?.trim()).not.toBe(initialVersion);
    console.log("✓ Update detected correctly");

    // Step 5: Start update
    console.log("\n=== Step 5: Start update ===");
    const updateButton = page.locator('[data-testid="update-now-button"]');
    await expect(updateButton).toBeVisible({ timeout: 10000 });
    await updateButton.click();
    console.log("Clicked 'Update now' button");

    // Step 6: Monitor download progress
    console.log("\n=== Step 6: Monitor download progress ===");

    // Wait for either app or system download progress to appear
    const downloadProgress = page.locator('[data-testid="app-download-progress"]');
    await expect(downloadProgress).toBeVisible({ timeout: 15000 });
    console.log("Download started");

    // Poll download progress until it exceeds 90%
    await expect
      .poll(
        async () => {
          const progressText = await page
            .locator('[data-testid="app-download-progress-text"]')
            .textContent();
          if (!progressText) return 0;
          const match = progressText.match(/(\d+)%/);
          const progress = match ? parseInt(match[1], 10) : 0;
          if (progress > 0) {
            console.log(`Download progress: ${progress}%`);
          }
          return progress;
        },
        {
          message: "Waiting for download to reach >90%",
          timeout: 60000, // Increased to 60s for slower networks
          intervals: [500, 1000, 2000],
        }
      )
      .toBeGreaterThan(90);
    console.log("✓ Download completed");

    // Step 7: Monitor verification progress
    console.log("\n=== Step 7: Monitor verification progress ===");

    // Wait for verification to start
    const verificationProgress = page.locator('[data-testid="app-verification-progress"]');
    await expect(verificationProgress).toBeVisible({ timeout: 15000 });
    console.log("Verification started");

    // Poll verification progress until it exceeds 90%
    await expect
      .poll(
        async () => {
          const progressText = await page
            .locator('[data-testid="app-verification-progress-text"]')
            .textContent();
          if (!progressText) return 0;
          const match = progressText.match(/(\d+)%/);
          const progress = match ? parseInt(match[1], 10) : 0;
          if (progress > 0) {
            console.log(`Verification progress: ${progress}%`);
          }
          return progress;
        },
        {
          message: "Waiting for verification to reach >90%",
          timeout: 60000, // Increased to 60s
          intervals: [500, 1000, 2000],
        }
      )
      .toBeGreaterThan(90);
    console.log("✓ Verification completed");

    // Step 8: Wait for reboot indication
    console.log("\n=== Step 8: Wait for reboot ===");
    await page.waitForTimeout(15000); // Give device time to initiate reboot
    console.log("Device should be rebooting now");

    // Step 9: Wait for device boot
    console.log("\n=== Step 9: Wait for device boot ===");
    await page.waitForTimeout(30000); // Wait for device to boot
    console.log("Boot wait period complete");

    // Step 10: Reconnect
    console.log("\n=== Step 10: Reconnect to device ===");
    let reconnected = false;
    const maxAttempts = 30;
    const attemptDelay = 2000;

    for (let attempt = 1; attempt <= maxAttempts; attempt++) {
      try {
        console.log(`Reconnection attempt ${attempt}/${maxAttempts}...`);

        await page.goto("/", { waitUntil: "domcontentloaded", timeout: 10000 });

        // Try to establish WebRTC connection with short timeout
        await waitForWebRTCReady(page, 10000);

        console.log("✓ Reconnected successfully!");
        reconnected = true;
        break;
      } catch (error) {
        if (attempt < maxAttempts) {
          await page.waitForTimeout(attemptDelay);
        }
      }
    }

    expect(reconnected, "Failed to reconnect to device after update").toBe(true);

    // Step 11: Verify new version
    console.log("\n=== Step 11: Verify new version ===");
    const newVersion = await getCurrentVersion(page);
    console.log(`New version: ${newVersion}`);

    expect(newVersion, "New version should be available").not.toBeNull();
    expect(newVersion).toBe(expectedVersion);
    expect(newVersion).not.toBe(initialVersion);
    console.log("✓ Version updated successfully");

    // Step 11: Verify functionality
    console.log("\n=== Step 11: Verify post-update functionality ===");
    await verifyHidAndVideo(page);
    console.log("✓ HID and video working after update");

    console.log("\n=== ✓ Update flow test completed successfully! ===");
  });
});

