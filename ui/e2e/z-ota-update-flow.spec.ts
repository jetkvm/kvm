import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  getCurrentVersion,
  waitForTerminalReady,
  sendTerminalCommand,
  verifyHidAndVideo,
  reconnectAfterReboot,
} from "./helpers";

/**
 * OTA Update Flow E2E Test
 *
 * Tests the complete OTA update flow from stable → new version:
 * 1. Modify config to use mock API (BEFORE downgrade - need terminal hook)
 * 2. Reboot to apply config
 * 3. Downgrade to stable version
 * 4. OTA upgrade to new version via mock API
 * 5. Verify upgrade succeeded
 * 6. Restore config
 *
 * Required environment variables:
 * - JETKVM_URL: Device URL (e.g., http://192.168.1.77)
 * - MOCK_SERVER_URL: Mock API server URL (e.g., http://192.168.1.50:8443)
 * - TEST_UPDATE_VERSION: Version to upgrade to
 * - TEST_STABLE_VERSION: Stable version to downgrade to first
 */
test.describe("OTA Update Flow", () => {
  test.setTimeout(420000); // 7 minutes

  test("complete OTA upgrade from stable to new build", async ({ page }) => {
    // Get environment variables
    const mockServerUrl = process.env.MOCK_SERVER_URL;
    const expectedVersion = process.env.TEST_UPDATE_VERSION;
    const stableVersion = process.env.TEST_STABLE_VERSION;

    if (!mockServerUrl) {
      throw new Error("MOCK_SERVER_URL environment variable is required");
    }
    if (!expectedVersion) {
      throw new Error("TEST_UPDATE_VERSION environment variable is required");
    }
    if (!stableVersion) {
      throw new Error("TEST_STABLE_VERSION environment variable is required");
    }

    console.log(`\n========================================`);
    console.log(`OTA Update Test`);
    console.log(`  Stable version: ${stableVersion}`);
    console.log(`  Target version: ${expectedVersion}`);
    console.log(`  Mock server: ${mockServerUrl}`);
    console.log(`========================================\n`);

    // Track if config was modified so we can restore it on failure
    let configModified = false;
    let finalVersion = "";

    // Helper to restore config - used in finally block
    const restoreConfig = async () => {
      if (!configModified) return;

      console.log("\n=== RESTORING CONFIG (cleanup) ===\n");
      try {
        // Try to reconnect if needed
        await page.goto("/", { timeout: 10000 });
        await waitForWebRTCReady(page, 30000);
        await waitForTerminalReady(page, 10000);

        const restoreCommand = `sed -i 's|"update_api_url": "[^"]*"|"update_api_url": "https://api.jetkvm.com"|' /userdata/kvm_config.json`;
        await sendTerminalCommand(page, restoreCommand, 1000);
        console.log("✓ Config restored to use production API");
      } catch (err) {
        console.error("⚠️ WARNING: Failed to restore config!");
        console.error("  Device may be left pointing to dead mock server.");
        console.error("  Manual fix: ssh root@<device> and edit /userdata/kvm_config.json");
        console.error(`  Error: ${err}`);
      }
    };

    try {
      // ========================================
      // PHASE 1: CONFIGURE MOCK API (before downgrade!)
      // ========================================
      console.log("\n=== PHASE 1: CONFIGURE MOCK API ===\n");
      console.log("(Must do this before downgrade - stable version lacks terminal hook)");

      console.log("\nStep 1.1: Connect to device");
      await page.goto("/");
      await waitForWebRTCReady(page);
      const initialVersion = await getCurrentVersion(page);
      console.log(`✓ Connected - Current version: ${initialVersion}`);

      console.log("\nStep 1.2: Wait for terminal data channel");
      await waitForTerminalReady(page);
      console.log("✓ Terminal ready");

      console.log("\nStep 1.3: Modify config via terminal");
      const sedCommand = `sed -i 's|"update_api_url": "[^"]*"|"update_api_url": "${mockServerUrl}"|' /userdata/kvm_config.json`;
      const sent = await sendTerminalCommand(page, sedCommand, 1000);
      expect(sent, "Failed to send config modification command").toBe(true);
      configModified = true; // Mark as modified for cleanup
      console.log("✓ Config modified to use mock server");

      console.log("\nStep 1.4: Reboot to apply config");
      await sendTerminalCommand(page, "reboot", 0);
      console.log("✓ Reboot command sent");

      console.log("\nStep 1.5: Wait for reboot and reconnect");
      await reconnectAfterReboot(page, 30000);
      console.log("✓ Reconnected - config now points to mock server");

      // ========================================
      // PHASE 2: DOWNGRADE TO STABLE
      // ========================================
      console.log("\n=== PHASE 2: DOWNGRADE TO STABLE ===\n");

      console.log("Step 2.1: Navigate to downgrade URL");
      const downgradeUrl = `/settings/general/update?custom_app_version=${stableVersion}&reset_config=false`;
      await page.goto(downgradeUrl);
      await page.waitForLoadState("networkidle");
      console.log(`✓ Navigated to: ${downgradeUrl}`);

      console.log("\nStep 2.2: Click Update Now to downgrade");
      const updateButton = page.locator('[data-testid="update-now-button"]');
      await expect(updateButton).toBeVisible({ timeout: 20000 });
      await updateButton.click();
      console.log("✓ Clicked Update Now");

      console.log("\nStep 2.3: Wait for downgrade + reboot and reconnect");
      await reconnectAfterReboot(page, 60000);

      const afterDowngrade = await getCurrentVersion(page);
      console.log(`✓ Reconnected - Version after downgrade: ${afterDowngrade}`);
      expect(afterDowngrade).toBe(stableVersion);
      console.log("✓ Downgrade verified!");

      // ========================================
      // PHASE 3: OTA UPGRADE
      // ========================================
      console.log("\n=== PHASE 3: OTA UPGRADE ===\n");

      console.log("Step 3.1: Navigate to update page");
      await page.goto("/settings/general/update");
      await page.waitForLoadState("networkidle");
      console.log("✓ On update page");

      console.log("\nStep 3.2: Wait for Update Now button");
      // Use text selector - stable version doesn't have data-testid
      const otaUpdateButton = page.getByRole("button", { name: "Update Now" });
      await expect(otaUpdateButton).toBeVisible({ timeout: 20000 });
      console.log("✓ Update available from mock server");

      console.log("\nStep 3.3: Click Update Now");
      await otaUpdateButton.click();
      console.log("✓ Clicked Update Now");

      console.log("\nStep 3.4: Wait for OTA update + reboot and reconnect");
      await reconnectAfterReboot(page, 60000);

      const version = await getCurrentVersion(page);
      expect(version, "Failed to get version after OTA upgrade").not.toBeNull();
      finalVersion = version!;
      console.log(`✓ Reconnected - Final version: ${finalVersion}`);
      expect(finalVersion).toBe(expectedVersion);
      console.log("✓ OTA upgrade verified!");

      console.log("\nStep 3.6: Verify HID and video after upgrade");
      await verifyHidAndVideo(page);
      console.log("✓ Mouse and keyboard working after OTA upgrade");

      // ========================================
      // PHASE 4: CLEANUP - RESTORE CONFIG
      // ========================================
      console.log("\n=== PHASE 4: CLEANUP ===\n");

      console.log("Step 4.1: Restore original config");
      await waitForTerminalReady(page);
      const restoreCommand = `sed -i 's|"update_api_url": "[^"]*"|"update_api_url": "https://api.jetkvm.com"|' /userdata/kvm_config.json`;
      await sendTerminalCommand(page, restoreCommand, 1000);
      configModified = false; // Successfully restored
      console.log("✓ Config restored to use production API");

      console.log("\n========================================");
      console.log("✓ OTA UPDATE TEST PASSED!");
      console.log(`  ${stableVersion} → ${finalVersion}`);
      console.log("========================================\n");
    } finally {
      // Always attempt to restore config if test fails mid-way
      await restoreConfig();
    }
  });
});
