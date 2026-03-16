import { test, expect } from "@playwright/test";

import {
  rebootDeviceViaSSH,
  ensureLocalAuthMode,
  createMockUpdateServer,
  deployBinaryToDevice,
  configureDeviceUpdateUrl,
  restoreDeviceUpdateUrl,
  setIncludePreRelease,
  getOTAEnvVars,
  toPreReleaseVersion,
  triggerUpdate,
  type MockUpdateServer,
} from "./helpers";

/**
 * Verifies that a prerelease update is rejected when the device has NOT
 * opted into the dev channel (include_pre_release = false).
 */
test.describe("OTA Prerelease Rejected (Not Opted-In)", () => {
  test.setTimeout(420000);

  let mockServer: MockUpdateServer;

  test.beforeAll(async ({ browser }) => {
    const env = getOTAEnvVars();
    const preReleaseVersion = toPreReleaseVersion(env.releaseVersion);

    const context = await browser.newContext({ baseURL: process.env.JETKVM_URL });
    const page = await context.newPage();
    try {
      await ensureLocalAuthMode(page, { mode: "noPassword" });
    } finally {
      await page.close();
      await context.close();
    }

    mockServer = await createMockUpdateServer({
      binaryPath: env.releasePath,
      version: preReleaseVersion,
    });

    await deployBinaryToDevice(env.baselinePath);
    await rebootDeviceViaSSH();
    await configureDeviceUpdateUrl(mockServer.url);
    await setIncludePreRelease(false);
    await rebootDeviceViaSSH();
  });

  test("unsigned prerelease update is rejected when not opted in", async ({ page }) => {
    await triggerUpdate(page);
    await expect(page.getByText(/requires GPG signature/i)).toBeVisible({ timeout: 30000 });
  });

  test.afterAll(async () => {
    await restoreDeviceUpdateUrl();
    await mockServer?.close();
  });
});
