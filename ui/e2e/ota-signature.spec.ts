import * as crypto from "crypto";
import { test, expect } from "@playwright/test";

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
  triggerUpdate,
  withTempSignature,
  type MockUpdateServer,
  type OTAEnvVars,
} from "./helpers";

/**
 * OTA Signature Verification Tests
 *
 * Test order matters (serial, shared mock server):
 *   1. unsigned  -> rejected (requires GPG signature)
 *   2. wrong key -> rejected (GPG verification failed)
 *   3. empty sig -> rejected (signature file is empty)
 *   4. signed    -> succeeds
 */
test.describe("OTA Signature Verification", () => {
  test.setTimeout(420000);

  let mockServer: MockUpdateServer;
  let env: OTAEnvVars;

  test.beforeAll(async ({ browser }) => {
    test.setTimeout(420000);
    env = getOTAEnvVars({ requireSignature: true });

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
      version: env.releaseVersion,
    });

    await deployBinaryToDevice(env.baselinePath);
    await rebootDeviceViaSSH();
    await configureDeviceUpdateUrl(mockServer.url);
    await rebootDeviceViaSSH();
  });

  test("unsigned stable update fails with GPG signature error", async ({ page }) => {
    await triggerUpdate(page);
    await expect(page.getByText(/requires GPG signature/i)).toBeVisible({ timeout: 30000 });
  });

  test("wrong-key signature fails with GPG verification error", async ({ page }) => {
    await withTempSignature(mockServer, crypto.randomBytes(256), async () => {
      await triggerUpdate(page);
      await expect(page.getByText(/GPG signature verification failed/i)).toBeVisible({
        timeout: 30000,
      });
    });
  });

  test("empty signature file is rejected", async ({ page }) => {
    await withTempSignature(mockServer, Buffer.alloc(0), async () => {
      await triggerUpdate(page);
      await expect(page.getByText(/signature file is empty/i)).toBeVisible({
        timeout: 30000,
      });
    });
  });

  test("signed stable update succeeds", async ({ page }) => {
    mockServer.enableSignature(env.signaturePath!);

    await page.goto("/settings/general/update");
    await page.waitForLoadState("networkidle");

    const initialVersion = await getCurrentVersion(page);
    expect(initialVersion, "Initial version should be detectable from /metrics").not.toBeNull();
    expect(initialVersion, "Baseline and target must differ").not.toBe(env.releaseVersion);

    const retryButton = page.getByRole("button", { name: "Retry" });
    if (await retryButton.isVisible({ timeout: 5000 }).catch(() => false)) {
      await retryButton.click();
    }

    const updateButton = page.getByRole("button", { name: "Update Now" });
    await expect(updateButton).toBeVisible({ timeout: 30000 });
    await updateButton.click();

    await reconnectAfterReboot(page, 35000, 30);

    const finalVersion = await getCurrentVersion(page);
    expect(finalVersion).toBe(env.releaseVersion);

    await verifyHidAndVideo(page);
  });

  test.afterAll(async () => {
    test.setTimeout(420000);
    await restoreDeviceUpdateUrl();
    await mockServer?.close();
  });
});
