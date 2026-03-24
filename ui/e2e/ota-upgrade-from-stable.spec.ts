import { test, expect } from "@playwright/test";
import * as fs from "fs";
import * as os from "os";
import * as path from "path";

import {
  getCurrentVersion,
  rebootDeviceViaSSH,
  ensureLocalAuthMode,
  verifyHidAndVideo,
  createMockUpdateServer,
  deployBinaryToDevice,
  configureDeviceUpdateUrl,
  restoreDeviceUpdateUrl,
  getOTAEnvVars,
  fetchLatestStableRelease,
  downloadFile,
  waitForDeviceReady,
  getDeviceHost,
  type MockUpdateServer,
  type StableReleaseInfo,
  type OTAEnvVars,
} from "./helpers";

/**
 * Validates that a device running the latest stable production release can
 * OTA-upgrade to the locally-built binary with a config reset, simulating
 * a real major-version upgrade path.
 *
 * Uses custom_app_version to bypass GPG signature checks (the locally-built
 * binary is unsigned). This mirrors the customVersionUpdate code path.
 */
test.describe("OTA Upgrade from Latest Stable", () => {
  test.setTimeout(420000);

  let mockServer: MockUpdateServer;
  let stableRelease: StableReleaseInfo;
  let downloadedBinaryPath: string;
  let env: OTAEnvVars;

  test.beforeAll(async ({ browser }) => {
    env = getOTAEnvVars();
    stableRelease = await fetchLatestStableRelease();

    downloadedBinaryPath = path.join(os.tmpdir(), `jetkvm_stable_${stableRelease.appVersion}`);
    await downloadFile(stableRelease.appUrl, downloadedBinaryPath);

    const context = await browser.newContext({ baseURL: process.env.JETKVM_URL });
    const page = await context.newPage();
    try {
      await ensureLocalAuthMode(page, { mode: "noPassword" });
    } finally {
      await page.close();
      await context.close();
    }

    await deployBinaryToDevice(downloadedBinaryPath);
    await rebootDeviceViaSSH();

    mockServer = await createMockUpdateServer({
      binaryPath: env.releasePath,
      version: env.releaseVersion,
    });

    await configureDeviceUpdateUrl(mockServer.url);
    await rebootDeviceViaSSH();
  });

  test("upgrade from latest stable succeeds with config reset", async ({ page }) => {
    await test.step("Trigger update with config reset", async () => {
      await page.goto(
        `/settings/general/update?custom_app_version=${env.releaseVersion}&reset_config=true`,
      );
      await page.waitForLoadState("networkidle");

      const initialVersion = await getCurrentVersion(page);
      expect(initialVersion).not.toBeNull();
      expect(initialVersion, "Baseline should be production version").toBe(
        stableRelease.appVersion,
      );

      const updateButton = page.locator('[data-testid="update-now-button"]');
      await expect(updateButton).toBeVisible({ timeout: 20000 });
      await updateButton.click();

      await expect(
        page.getByText(/downloading|verifying|installing|awaiting reboot/i),
      ).toBeVisible({ timeout: 30000 });
    });

    await test.step("Wait for device to come back after reboot", async () => {
      await page.waitForTimeout(35000);
      await waitForDeviceReady(getDeviceHost(), 60000);
    });

    await test.step("Re-setup device after config reset", async () => {
      await ensureLocalAuthMode(page, { mode: "noPassword" });
    });

    await test.step("Verify version changed", async () => {
      await page.goto("/");
      await page.waitForLoadState("networkidle");

      const finalVersion = await getCurrentVersion(page);
      expect(finalVersion).not.toBeNull();
      expect(finalVersion).not.toBe(stableRelease.appVersion);
    });

    await test.step("Verify HID and video", async () => {
      await verifyHidAndVideo(page);
    });
  });

  test.afterAll(async () => {
    await restoreDeviceUpdateUrl();
    await mockServer?.close();

    if (downloadedBinaryPath && fs.existsSync(downloadedBinaryPath)) {
      fs.unlinkSync(downloadedBinaryPath);
    }
  });
});
