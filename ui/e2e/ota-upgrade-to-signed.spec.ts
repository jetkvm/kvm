import { test, expect } from "@playwright/test";
import * as fs from "fs";
import * as os from "os";
import * as path from "path";

import {
  getCurrentVersion,
  reconnectAfterReboot,
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
  type MockUpdateServer,
  type StableReleaseInfo,
  type OTAEnvVars,
} from "./helpers";

/**
 * Validates that the locally-built binary can accept a signed OTA update.
 *
 * Deploys the dev build to the device, then serves the latest production
 * binary (with its real GPG signature) via a mock update server advertising
 * a higher version. The device must fetch, verify, and install the update —
 * proving the OTA client in the current build works end-to-end with
 * signature enforcement.
 */
test.describe("OTA Upgrade to Signed Release", () => {
  test.setTimeout(420000);

  let mockServer: MockUpdateServer;
  let stableRelease: StableReleaseInfo;
  let downloadedBinaryPath: string;
  let downloadedSigPath: string;
  let env: OTAEnvVars;

  test.beforeAll(async ({ browser }) => {
    env = getOTAEnvVars();

    stableRelease = await fetchLatestStableRelease();
    if (!stableRelease.appSigUrl) {
      throw new Error("Latest stable release has no signature URL — cannot run this test");
    }

    downloadedBinaryPath = path.join(os.tmpdir(), `jetkvm_stable_${stableRelease.appVersion}`);
    downloadedSigPath = path.join(os.tmpdir(), `jetkvm_stable_${stableRelease.appVersion}.sig`);

    await Promise.all([
      downloadFile(stableRelease.appUrl, downloadedBinaryPath),
      downloadFile(stableRelease.appSigUrl, downloadedSigPath),
    ]);

    const context = await browser.newContext({ baseURL: process.env.JETKVM_URL });
    const page = await context.newPage();
    try {
      await ensureLocalAuthMode(page, { mode: "noPassword" });
    } finally {
      await page.close();
      await context.close();
    }

    // Deploy the locally-built binary as the device's current version
    await deployBinaryToDevice(env.releasePath);
    await rebootDeviceViaSSH();

    // Serve the production binary+signature as a "future" version
    mockServer = await createMockUpdateServer({
      binaryPath: downloadedBinaryPath,
      version: "99.99.99",
      signaturePath: downloadedSigPath,
    });

    await configureDeviceUpdateUrl(mockServer.url);
    await rebootDeviceViaSSH();
  });

  test("current build accepts signed update", async ({ page }) => {
    await test.step("Verify device is running the dev build", async () => {
      await page.goto("/settings/general/update");
      await page.waitForLoadState("networkidle");

      const initialVersion = await getCurrentVersion(page);
      expect(initialVersion).not.toBeNull();
      expect(initialVersion, "Device should be running the dev build").toBe(env.releaseVersion);
    });

    await test.step("Trigger signed update", async () => {
      const updateButton = page.getByRole("button", { name: "Update Now" });
      await expect(updateButton).toBeVisible({ timeout: 30000 });
      await updateButton.click();

      await expect(page.getByText(/downloading|verifying|installing|awaiting reboot/i)).toBeVisible(
        { timeout: 30000 },
      );
    });

    await test.step("Reconnect after reboot", async () => {
      await reconnectAfterReboot(page, 35000);
    });

    await test.step("Verify update installed", async () => {
      const finalVersion = await getCurrentVersion(page);
      expect(finalVersion).not.toBeNull();
      // The installed binary reports its compiled-in version, not the
      // advertised mock version. Verify we're now running the production
      // release, not the dev build we started with.
      expect(finalVersion).toBe(stableRelease.appVersion);
    });

    await test.step("Verify HID and video", async () => {
      await verifyHidAndVideo(page);
    });
  });

  test.afterAll(async () => {
    await restoreDeviceUpdateUrl();
    await mockServer?.close();

    for (const p of [downloadedBinaryPath, downloadedSigPath]) {
      if (p && fs.existsSync(p)) {
        fs.unlinkSync(p);
      }
    }
  });
});
