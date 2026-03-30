import { test, expect } from "@playwright/test";

import {
  getCurrentVersion,
  reconnectAfterReboot,
  rebootDeviceViaSSH,
  verifyHidAndVideo,
  ensureLocalAuthMode,
  createMockUpdateServer,
  deployBinaryToDevice,
  configureDeviceUpdateUrl,
  restoreDeviceUpdateUrl,
  setIncludePreRelease,
  getOTAEnvVars,
  type MockUpdateServer,
  type OTAEnvVars,
} from "./helpers";

/**
 * Verifies that custom/specific-version updates bypass GPG signature checks.
 * Uses tryUpdateComponents with customVersionUpdate=true.
 */
test.describe("OTA Specific Version Unsigned", () => {
  test.setTimeout(420000);

  let mockServer: MockUpdateServer;
  let env: OTAEnvVars;

  test.beforeAll(async ({ browser }) => {
    test.setTimeout(420000);
    env = getOTAEnvVars();

    const context = await browser.newContext({ baseURL: process.env.JETKVM_URL });
    const page = await context.newPage();
    try {
      await ensureLocalAuthMode(page, { mode: "noPassword" });
    } finally {
      await page.close();
      await context.close();
    }

    await deployBinaryToDevice(env.baselinePath);
    await rebootDeviceViaSSH();

    mockServer = await createMockUpdateServer({
      binaryPath: env.releasePath,
      version: env.releaseVersion,
    });
  });

  test("specific-version update succeeds without signature", async ({ page }) => {
    await test.step("Configure mock API and stable channel", async () => {
      await configureDeviceUpdateUrl(mockServer.url);
      await setIncludePreRelease(false);
      await rebootDeviceViaSSH();
    });

    await test.step(`Custom version update to ${env.releaseVersion}`, async () => {
      await page.goto(
        `/settings/general/update?custom_app_version=${env.releaseVersion}&reset_config=false`,
      );
      await page.waitForLoadState("networkidle");

      const initialVersion = await getCurrentVersion(page);
      expect(initialVersion).not.toBeNull();
      expect(initialVersion, "Baseline and target must differ").not.toBe(env.releaseVersion);

      const updateButton = page.locator('[data-testid="update-now-button"]');
      await expect(updateButton).toBeVisible({ timeout: 20000 });
      await updateButton.click();

      await expect(page.getByText(/downloading|verifying|installing|awaiting reboot/i)).toBeVisible(
        { timeout: 30000 },
      );

      await reconnectAfterReboot(page, 35000, 30);

      const finalVersion = await getCurrentVersion(page);
      expect(finalVersion).toBe(env.releaseVersion);
    });

    await test.step("Verify HID and video", async () => {
      await verifyHidAndVideo(page);
    });
  });

  test.afterAll(async () => {
    test.setTimeout(420000);
    await restoreDeviceUpdateUrl();
    await mockServer?.close();
  });
});
