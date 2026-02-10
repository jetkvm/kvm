import { test, expect } from "@playwright/test";

import {
  getCurrentVersion,
  reconnectAfterReboot,
  rebootDeviceViaSSH,
  ensureLocalAuthMode,
  verifyHidAndVideo,
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
 * OTA Prerelease Unsigned
 *
 * Verifies that prerelease OTA updates bypass the signature requirement.
 * This mirrors the dev-release lane where prerelease updates are expected
 * to work without appSigUrl.
 */
test.describe("OTA Prerelease Unsigned", () => {
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
      // No signaturePath on purpose.
    });

    await deployBinaryToDevice(baselinePath);
    await rebootDeviceViaSSH();
    await configureDeviceUpdateUrl(mockServer.url);
    await setIncludePreRelease(true);
    await rebootDeviceViaSSH();
  });

  test("unsigned prerelease update succeeds", async ({ page }) => {
    await page.goto("/settings/general/update");
    await page.waitForLoadState("networkidle");

    const initialVersion = await getCurrentVersion(page);
    expect(initialVersion, "Initial version should be detectable from /metrics").not.toBeNull();

    const updateButton = page.getByRole("button", { name: "Update Now" });
    await expect(updateButton).toBeVisible({ timeout: 30000 });
    await updateButton.click();

    await reconnectAfterReboot(page, 35000);

    const finalVersion = await getCurrentVersion(page);
    expect(finalVersion, "Version should be detectable after reboot").not.toBeNull();
    expect(finalVersion).not.toBe(initialVersion);

    await verifyHidAndVideo(page);
  });

  test.afterAll(async () => {
    await setIncludePreRelease(false);
    await restoreDeviceUpdateUrl();
    await mockServer?.close();
  });
});
