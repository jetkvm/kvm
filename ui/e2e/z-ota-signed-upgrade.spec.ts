import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  getCurrentVersion,
  verifyHidAndVideo,
  reconnectAfterReboot,
} from "./helpers";

/**
 * Signed OTA Upgrade E2E Test
 *
 * This test verifies that GPG signature verification actually runs during upgrades.
 * Unlike the regular OTA test (which downgrades to stable first), this test:
 *
 * 1. Starts with a baseline binary already deployed (has GPG verification code)
 * 2. Configures device to use mock API that serves signed target
 * 3. Triggers upgrade - the baseline binary will verify the GPG signature
 * 4. Verifies the upgrade succeeded
 *
 * This proves the signature verification path is exercised, because:
 * - The running binary (baseline) contains the GPG verifier code
 * - The mock API always includes appSigUrl
 * - The upgrade would fail if signature verification failed
 *
 * Prerequisites (set up by test_signed_ota.sh):
 * - Device is running a baseline binary built from the signed-capable branch
 * - Device config is already pointing to the mock API server
 * - Mock server serves a signed target binary with appSigUrl
 *
 * Required environment variables:
 * - JETKVM_URL: Device URL (e.g., http://192.168.1.77)
 * - TEST_UPDATE_VERSION: Version of the target binary
 * - TEST_BASELINE_VERSION: Version of the baseline binary (optional, for logging)
 * - SIGNED_OTA_TEST: Must be "1" to confirm this is a signed OTA test
 */
test.describe("Signed OTA Upgrade", () => {
  test.setTimeout(300000); // 5 minutes

  test("upgrade with GPG signature verification", async ({ page }) => {
    const expectedVersion = process.env.TEST_UPDATE_VERSION;
    const baselineVersion = process.env.TEST_BASELINE_VERSION || "baseline";
    const isSignedTest = process.env.SIGNED_OTA_TEST === "1";

    if (!expectedVersion) {
      throw new Error("TEST_UPDATE_VERSION environment variable is required");
    }
    if (!isSignedTest) {
      throw new Error(
        "SIGNED_OTA_TEST=1 is required - this test should be run via test_signed_ota.sh",
      );
    }

    // Phase 1: Verify baseline is running and has GPG verification capability
    await test.step("Verify baseline version is running", async () => {
      await page.goto("/");
      await waitForWebRTCReady(page);

      const currentVersion = await getCurrentVersion(page);
      console.log(`Current version: ${currentVersion}, expected baseline: ${baselineVersion}`);

      // We expect the baseline to be running (deployed by test_signed_ota.sh)
      expect(currentVersion, "Device should be running the baseline version").not.toBeNull();
    });

    // Phase 2: Trigger signed upgrade
    // The device config is already pointing to the mock API (set up by test_signed_ota.sh).
    // The baseline binary will:
    // 1. Fetch update metadata from mock API (includes appSigUrl)
    // 2. Download the target binary
    // 3. Download the signature
    // 4. Verify the GPG signature (this is what we're testing!)
    // 5. Apply the update
    await test.step(`Signed upgrade to ${expectedVersion}`, async () => {
      await page.goto("/settings/general/update");
      await page.waitForLoadState("networkidle");

      // Wait for update check to complete and button to appear
      const updateButton = page.getByRole("button", { name: "Update Now" });
      await expect(updateButton).toBeVisible({ timeout: 30000 });

      // Click to start the upgrade
      // If signature verification fails, this upgrade will fail
      await updateButton.click();

      // Wait for device to reboot after upgrade
      await reconnectAfterReboot(page, 35000);

      // Verify we're now running the target version
      const finalVersion = await getCurrentVersion(page);
      expect(finalVersion, "Failed to get version after signed OTA upgrade").not.toBeNull();
      expect(finalVersion, `Expected version ${expectedVersion} after signed upgrade`).toBe(
        expectedVersion,
      );

      console.log(`✓ Signed upgrade successful: ${baselineVersion} → ${finalVersion}`);
    });

    // Phase 3: Verify device functionality after upgrade
    await test.step("Verify HID and video work after upgrade", async () => {
      await verifyHidAndVideo(page);
    });
  });
});
