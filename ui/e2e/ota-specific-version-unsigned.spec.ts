import { test, expect } from "@playwright/test";

import {
  getCurrentVersion,
  reconnectAfterReboot,
  rebootDeviceViaSSH,
  verifyHidAndVideo,
  ensureLocalAuthMode,
} from "./helpers";

import {
  createMockUpdateServer,
  deployBinaryToDevice,
  configureDeviceUpdateUrl,
  restoreDeviceUpdateUrl,
  setIncludePreRelease,
  type MockUpdateServer,
} from "./ota-helpers";

/**
 * OTA Specific Version (Custom) Update Test
 *
 * Verifies that custom/specific-version updates bypass GPG signature checks.
 * When a user goes to Settings > Advanced and specifies a target version,
 * the update uses tryUpdateComponents with customVersionUpdate=true, which
 * skips signature verification.
 *
 * The test starts its own mock server, deploys a known low baseline binary,
 * and configures the device to use the mock API. This guarantees the test
 * validates a real OTA transition (not a no-op "already on target" path).
 *
 * Required environment variables:
 *   - JETKVM_URL: Device URL (e.g., http://192.168.1.77)
 *   - BASELINE_BINARY_PATH: Absolute path to baseline binary (deployed to device)
 *   - RELEASE_BINARY_PATH: Absolute path to release binary (served by mock)
 *   - TEST_UPDATE_VERSION: Version string of the release binary
 */
test.describe("OTA Specific Version Unsigned", () => {
  test.setTimeout(420000); // 7 minutes

  let mockServer: MockUpdateServer;

  test.beforeAll(async ({ browser }) => {
    const baselinePath = process.env.BASELINE_BINARY_PATH;
    const releasePath = process.env.RELEASE_BINARY_PATH;
    const version = process.env.TEST_UPDATE_VERSION;

    if (!baselinePath) throw new Error("BASELINE_BINARY_PATH is required");
    if (!releasePath) throw new Error("RELEASE_BINARY_PATH is required");
    if (!version) throw new Error("TEST_UPDATE_VERSION is required");

    // Ensure device is in noPassword mode
    const context = await browser.newContext({ baseURL: process.env.JETKVM_URL });
    const page = await context.newPage();
    try {
      await ensureLocalAuthMode(page, { mode: "noPassword" });
    } finally {
      await page.close();
      await context.close();
    }

    // Deploy a known baseline so the test validates a real update path.
    await deployBinaryToDevice(baselinePath);
    await rebootDeviceViaSSH();

    // Start mock server (no signature -- the whole point is that custom version
    // updates work WITHOUT a signature)
    mockServer = await createMockUpdateServer({
      binaryPath: releasePath,
      version,
    });
  });

  test("specific-version update succeeds without signature", async ({ page }) => {
    const targetVersion = process.env.TEST_UPDATE_VERSION!;

    await test.step("Configure mock API and stable channel", async () => {
      // Point device at our mock server and disable pre-release channel
      await configureDeviceUpdateUrl(mockServer.url);
      await setIncludePreRelease(false);
      await rebootDeviceViaSSH();
    });

    await test.step(`Custom version update to ${targetVersion}`, async () => {
      // Navigate with custom_app_version query param.
      // This sets forceCustomUpdate=true in the UI, so "Update Now" always shows
      // regardless of whether the device is already at this version.
      // On the backend, this triggers tryUpdateComponents with customVersionUpdate=true,
      // which bypasses GPG signature checks.
      await page.goto(
        `/settings/general/update?custom_app_version=${targetVersion}&reset_config=false`,
      );
      await page.waitForLoadState("networkidle");

      const initialVersion = await getCurrentVersion(page);
      expect(initialVersion, "Initial version should be detectable from /metrics").not.toBeNull();
      expect(
        initialVersion,
        "Baseline and target versions must differ to validate OTA behavior",
      ).not.toBe(targetVersion);

      const updateButton = page.locator('[data-testid="update-now-button"]');
      await expect(updateButton).toBeVisible({ timeout: 20000 });
      await updateButton.click();

      await expect(
        page.getByText(/downloading|verifying|installing|awaiting reboot/i),
        "Expected OTA progress state after triggering custom update",
      ).toBeVisible({ timeout: 30000 });

      await reconnectAfterReboot(page, 35000);

      const finalVersion = await getCurrentVersion(page);
      expect(finalVersion).toBe(targetVersion);
    });

    await test.step("Verify HID and video", async () => {
      await verifyHidAndVideo(page);
    });
  });

  test.afterAll(async () => {
    await restoreDeviceUpdateUrl();
    await mockServer?.close();
  });
});
