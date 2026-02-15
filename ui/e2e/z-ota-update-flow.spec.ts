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

    // Track if config was modified so we can restore it on failure
    let configModified = false;

    // Helper to restore config - used in finally block
    const restoreConfig = async () => {
      if (!configModified) return;

      try {
        await page.goto("/", { timeout: 10000 });
        await waitForWebRTCReady(page);
        await waitForTerminalReady(page, 10000);

        const restoreCommand = `sed -i 's|"update_api_url": "[^"]*"|"update_api_url": "https://api.jetkvm.com"|' /userdata/kvm_config.json`;
        await sendTerminalCommand(page, restoreCommand, 1000);
      } catch {
        // Device may be left pointing to dead mock server - needs manual fix
      }
    };

    try {
      // Phase 1: Configure mock API (before downgrade - stable version lacks terminal hook)
      await test.step("Configure mock API", async () => {
        await page.goto("/");
        await waitForWebRTCReady(page);
        await waitForTerminalReady(page);

        const sedCommand = `sed -i 's|"update_api_url": "[^"]*"|"update_api_url": "${mockServerUrl}"|' /userdata/kvm_config.json`;
        const sent = await sendTerminalCommand(page, sedCommand, 1000);
        expect(sent, "Failed to send config modification command").toBe(true);
        configModified = true;

        await sendTerminalCommand(page, "reboot", 0);
        await reconnectAfterReboot(page);
      });

      // Phase 2: Downgrade to stable version
      await test.step(`Downgrade to ${stableVersion}`, async () => {
        const downgradeUrl = `/settings/general/update?custom_app_version=${stableVersion}&reset_config=false`;
        await page.goto(downgradeUrl);
        await page.waitForLoadState("networkidle");

        const updateButton = page.locator('[data-testid="update-now-button"]');
        await expect(updateButton).toBeVisible({ timeout: 20000 });
        await updateButton.click();

        await reconnectAfterReboot(page, 35000);

        const afterDowngrade = await getCurrentVersion(page);
        expect(afterDowngrade).toBe(stableVersion);
      });

      // Phase 3: OTA upgrade to new version
      await test.step(`Upgrade to ${expectedVersion}`, async () => {
        await page.goto("/settings/general/update");
        await page.waitForLoadState("networkidle");

        // Use text selector - stable version doesn't have data-testid
        const otaUpdateButton = page.getByRole("button", { name: "Update Now" });
        await expect(otaUpdateButton).toBeVisible({ timeout: 20000 });
        await otaUpdateButton.click();

        await reconnectAfterReboot(page, 35000);

        const finalVersion = await getCurrentVersion(page);
        expect(finalVersion, "Failed to get version after OTA upgrade").not.toBeNull();
        expect(finalVersion).toBe(expectedVersion);
      });

      // Phase 4: Verify HID and video work
      await test.step("Verify HID and video", async () => {
        await verifyHidAndVideo(page);
      });

      // Phase 5: Restore config
      await test.step("Restore config", async () => {
        await waitForTerminalReady(page);
        const restoreCommand = `sed -i 's|"update_api_url": "[^"]*"|"update_api_url": "https://api.jetkvm.com"|' /userdata/kvm_config.json`;
        await sendTerminalCommand(page, restoreCommand, 1000);
        configModified = false;
      });
    } finally {
      // Always attempt to restore config if test fails mid-way
      await restoreConfig();
    }
  });
});
