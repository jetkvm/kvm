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
  type MockUpdateServer,
} from "./ota-helpers";

/**
 * OTA Signature Verification Tests
 *
 * These tests verify GPG signature enforcement during OTA updates.
 * They share an expensive beforeAll that:
 *   1. Deploys a baseline binary (has GPG verification code, reports low version)
 *   2. Starts a mock update server advertising the release version
 *   3. Configures the device to use the mock server
 *
 * Test order matters:
 *   - Test 1 (unsigned): mock serves NO signature -> update must fail with GPG error.
 *     The device stays at baseline because the update was rejected.
 *   - Test 2 (signed): signature is enabled on the mock -> update must succeed.
 *
 * Required environment variables:
 *   - JETKVM_URL: Device URL (e.g., http://192.168.1.77)
 *   - BASELINE_BINARY_PATH: Absolute path to baseline binary (deployed to device)
 *   - RELEASE_BINARY_PATH: Absolute path to release binary (served by mock)
 *   - RELEASE_SIGNATURE_PATH: Absolute path to .sig file (served by mock in test 2)
 *   - TEST_UPDATE_VERSION: Version string of the release binary
 */
test.describe("OTA Signature Verification", () => {
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

    // 1. Start mock server WITHOUT signature
    mockServer = await createMockUpdateServer({
      binaryPath: releasePath,
      version,
      // No signaturePath -- unsigned initially
    });

    // 2. Deploy baseline binary to device
    await deployBinaryToDevice(baselinePath);
    await rebootDeviceViaSSH();

    // 3. Configure device to use mock API and reboot
    await configureDeviceUpdateUrl(mockServer.url);
    await rebootDeviceViaSSH();
  });

  test("unsigned stable update fails with GPG signature error", async ({ page }) => {
    // The mock server is NOT serving a signature.
    // A normal stable update should fail because GPG signature is required.
    await page.goto("/settings/general/update");
    await page.waitForLoadState("networkidle");

    const updateButton = page.getByRole("button", { name: "Update Now" });
    await expect(updateButton).toBeVisible({ timeout: 30000 });
    await updateButton.click();

    // The OTA code should reject the update with a GPG signature error
    await expect(page.getByText(/requires GPG signature/i)).toBeVisible({ timeout: 30000 });
  });

  test("signed stable update succeeds", async ({ page }) => {
    const sigPath = process.env.RELEASE_SIGNATURE_PATH;
    if (!sigPath) throw new Error("RELEASE_SIGNATURE_PATH is required");

    // Enable signature on the mock server so it now returns appSigUrl
    mockServer.enableSignature(sigPath);

    await page.goto("/settings/general/update");
    await page.waitForLoadState("networkidle");

    const initialVersion = await getCurrentVersion(page);
    expect(initialVersion, "Initial version should be detectable from /metrics").not.toBeNull();
    expect(
      initialVersion,
      "Baseline and target versions must differ to validate OTA behavior",
    ).not.toBe(process.env.TEST_UPDATE_VERSION);

    // The previous test's GPG error may be cached by the device backend.
    // If the error view is showing, click Retry to trigger a fresh update check.
    const retryButton = page.getByRole("button", { name: "Retry" });
    if (await retryButton.isVisible({ timeout: 5000 }).catch(() => false)) {
      await retryButton.click();
    }

    const updateButton = page.getByRole("button", { name: "Update Now" });
    await expect(updateButton).toBeVisible({ timeout: 30000 });
    await updateButton.click();

    // Wait for the device to complete the upgrade and reboot
    await reconnectAfterReboot(page, 35000);

    // Verify we're now running the release version
    const finalVersion = await getCurrentVersion(page);
    expect(finalVersion, "Device should be running the release version").not.toBeNull();
    expect(finalVersion).toBe(process.env.TEST_UPDATE_VERSION);

    await verifyHidAndVideo(page);
  });

  test.afterAll(async () => {
    // Restore device config to production API URL
    await restoreDeviceUpdateUrl();
    // Shut down mock server
    await mockServer?.close();
  });
});
