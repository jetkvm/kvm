import { test, expect } from "@playwright/test";
import type { Page } from "@playwright/test";

import {
  waitForWebRTCReady,
  getCurrentVersion,
  verifyHidAndVideo,
  reconnectAfterReboot,
  ensureLocalAuthMode,
  getDeviceHost,
} from "./helpers";
import { exec } from "child_process";
import { promisify } from "util";
const execAsync = promisify(exec);

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
/** Run a command on the device via SSH */
async function sshCmd(host: string, cmd: string): Promise<string> {
  const sshCommand = `ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -o ConnectTimeout=10 root@${host} '${cmd}'`;
  try {
    const { stdout } = await execAsync(sshCommand);
    return stdout;
  } catch {
    return "";
  }
}

/** Wait for device to go down (become unreachable), then come back up */
async function waitForOtaReboot(host: string, page: Page): Promise<void> {
  const url = `http://${host}`;

  // Phase 1: Wait for device to become unreachable (OTA flash + reboot started)
  const downDeadline = Date.now() + 120000; // 2 min max for flash + reboot to start
  let wentDown = false;
  while (Date.now() < downDeadline) {
    try {
      const resp = await fetch(url, { signal: AbortSignal.timeout(2000) });
      // Still up - keep waiting
      await new Promise(r => setTimeout(r, 1000));
    } catch {
      wentDown = true;
      break;
    }
  }
  if (!wentDown) {
    throw new Error("Device never went down after OTA update - flash may have failed");
  }

  // Phase 2: Wait for device to come back
  const upDeadline = Date.now() + 60000; // 1 min for reboot
  while (Date.now() < upDeadline) {
    try {
      const resp = await fetch(url, { signal: AbortSignal.timeout(3000) });
      if (resp.ok || resp.status === 401 || resp.status === 302) {
        // Give it a moment to fully initialize
        await new Promise(r => setTimeout(r, 2000));
        // Reconnect the page
        await page.goto("/", { timeout: 10000 });
        await waitForWebRTCReady(page, 15000);
        return;
      }
    } catch {
      // Not ready yet
    }
    await new Promise(r => setTimeout(r, 1000));
  }
  throw new Error("Device did not come back after OTA reboot");
}

test.describe("OTA Update Flow", () => {
  test.setTimeout(360000); // 6 minutes (down from 7)

  // Ensure device is in noPassword mode before tests
  // This handles cases where previous tests left the device with password protection
  test.beforeAll(async ({ browser }) => {
    const baseURL = process.env.JETKVM_URL;
    const context = await browser.newContext({ baseURL });
    const page = await context.newPage();
    try {
      await ensureLocalAuthMode(page, { mode: "noPassword" });
    } finally {
      await page.close();
      await context.close();
    }
  });

  test("complete OTA upgrade from stable to new build", async ({ page }) => {
    // Get environment variables
    const mockServerUrl = process.env.MOCK_SERVER_URL;
    const expectedVersion = process.env.TEST_UPDATE_VERSION;
    const stableVersion = process.env.TEST_STABLE_VERSION;

    test.skip(!mockServerUrl, "MOCK_SERVER_URL environment variable is required");
    test.skip(!expectedVersion, "TEST_UPDATE_VERSION environment variable is required");
    test.skip(!stableVersion, "TEST_STABLE_VERSION environment variable is required");

    const host = getDeviceHost();

    // Track if config was modified so we can restore it on failure
    let configModified = false;

    // Helper to restore config via SSH - no WebRTC dependency
    const restoreConfig = async () => {
      if (!configModified) return;
      try {
        await sshCmd(host, `sed -i 's|"update_api_url": "[^"]*"|"update_api_url": "https://api.jetkvm.com"|' /userdata/kvm_config.json`);
      } catch {
        // Device may be left pointing to dead mock server - needs manual fix
      }
    };

    try {
      // Phase 1: Configure mock API via SSH (faster than WebRTC terminal)
      await test.step("Configure mock API", async () => {
        await sshCmd(host, `sed -i 's|"update_api_url": "[^"]*"|"update_api_url": "${mockServerUrl}"|' /userdata/kvm_config.json`);
        configModified = true;

        // Reboot to apply config change
        await sshCmd(host, "reboot");
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

        // Wait for device to go down (flash) then come back
        await waitForOtaReboot(host, page);

        const afterDowngrade = await getCurrentVersion(page);
        expect(afterDowngrade).toBe(stableVersion);
      });

      // Phase 3: OTA upgrade to new version
      await test.step(`Upgrade to ${expectedVersion}`, async () => {
        await page.goto("/settings/general/update");
        await page.waitForLoadState("networkidle");

        // The stable version may show "System is up to date" initially.
        // Click "Check Again" to trigger an update check against the mock API.
        const checkAgainButton = page.getByRole("button", { name: "Check Again" });
        if (await checkAgainButton.isVisible({ timeout: 5000 }).catch(() => false)) {
          await checkAgainButton.click();
          await page.waitForLoadState("networkidle");
        }

        // Use text selector - stable version doesn't have data-testid
        const otaUpdateButton = page.getByRole("button", { name: "Update Now" });
        await expect(otaUpdateButton).toBeVisible({ timeout: 20000 });
        await otaUpdateButton.click();

        // Wait for device to go down (flash) then come back
        await waitForOtaReboot(host, page);

        const finalVersion = await getCurrentVersion(page);
        expect(finalVersion, "Failed to get version after OTA upgrade").not.toBeNull();
        expect(finalVersion).toBe(expectedVersion);
      });

      // Phase 4: Verify HID and video work
      await test.step("Verify HID and video", async () => {
        await verifyHidAndVideo(page);
      });

      // Phase 5: Restore config via SSH (no WebRTC terminal needed)
      await test.step("Restore config", async () => {
        await sshCmd(host, `sed -i 's|"update_api_url": "[^"]*"|"update_api_url": "https://api.jetkvm.com"|' /userdata/kvm_config.json`);
        configModified = false;
      });
    } finally {
      // Always attempt to restore config if test fails mid-way
      await restoreConfig();
    }
  });
});
