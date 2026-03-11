import { test, expect } from "@playwright/test";

import {
  rebootDeviceViaSSH,
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
 * OTA Prerelease Rejected (Not Opted-In)
 *
 * Verifies that a prerelease update served to a device that has NOT opted into
 * the dev channel is rejected with a GPG signature error. This is a regression
 * test for a bug where a compromised server could push an unsigned prerelease
 * version string to any device, bypassing signature checks regardless of the
 * device's include_pre_release setting.
 *
 * Setup:
 *   1. Deploy baseline binary, configure mock server with a prerelease version
 *   2. Set include_pre_release = false on the device
 *   3. Attempt the update — it must fail because signature is required
 */
test.describe("OTA Prerelease Rejected (Not Opted-In)", () => {
  test.setTimeout(420000); // 7 minutes

  let mockServer: MockUpdateServer;

  test.beforeAll(async ({ browser }) => {
    const baselinePath = process.env.BASELINE_BINARY_PATH;
    const releasePath = process.env.RELEASE_BINARY_PATH;
    const releaseVersion = process.env.TEST_UPDATE_VERSION;

    if (!baselinePath) throw new Error("BASELINE_BINARY_PATH is required");
    if (!releasePath) throw new Error("RELEASE_BINARY_PATH is required");
    if (!releaseVersion) throw new Error("TEST_UPDATE_VERSION is required");

    const preReleaseVersion = releaseVersion.includes("-")
      ? releaseVersion
      : `${releaseVersion}-dev.1`;

    const context = await browser.newContext({ baseURL: process.env.JETKVM_URL });
    const page = await context.newPage();
    try {
      await ensureLocalAuthMode(page, { mode: "noPassword" });
    } finally {
      await page.close();
      await context.close();
    }

    mockServer = await createMockUpdateServer({
      binaryPath: releasePath,
      version: preReleaseVersion,
    });

    await deployBinaryToDevice(baselinePath);
    await rebootDeviceViaSSH();
    await configureDeviceUpdateUrl(mockServer.url);
    await setIncludePreRelease(false);
    await rebootDeviceViaSSH();
  });

  test("unsigned prerelease update is rejected when not opted in", async ({ page }) => {
    await page.goto("/settings/general/update");
    await page.waitForLoadState("networkidle");

    const updateButton = page.getByRole("button", { name: "Update Now" });
    await expect(updateButton).toBeVisible({ timeout: 30000 });
    await updateButton.click();

    await expect(page.getByText(/requires GPG signature/i)).toBeVisible({
      timeout: 30000,
    });
  });

  test.afterAll(async () => {
    await restoreDeviceUpdateUrl();
    await mockServer?.close();
  });
});
