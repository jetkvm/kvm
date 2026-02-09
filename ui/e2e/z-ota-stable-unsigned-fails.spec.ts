import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  waitForTerminalReady,
  sendTerminalCommand,
  reconnectAfterReboot,
} from "./helpers";

test.describe("OTA Stable Unsigned Failure", () => {
  test.setTimeout(420000);

  test("regular stable update fails when signature is missing", async ({ page }) => {
    const mockServerUrl = process.env.MOCK_SERVER_URL;
    const stableVersion = process.env.TEST_STABLE_VERSION;

    if (!mockServerUrl) throw new Error("MOCK_SERVER_URL environment variable is required");
    if (!stableVersion) throw new Error("TEST_STABLE_VERSION environment variable is required");

    let configModified = false;

    const restoreConfig = async () => {
      if (!configModified) return;
      try {
        await page.goto("/");
        await waitForWebRTCReady(page);
        await waitForTerminalReady(page, 10000);
        await sendTerminalCommand(
          page,
          `sed -i 's|"update_api_url": "[^"]*"|"update_api_url": "https://api.jetkvm.com"|' /userdata/kvm_config.json`,
          1000,
        );
        await sendTerminalCommand(
          page,
          `sed -i 's|"include_pre_release": [^,]*|"include_pre_release": false|' /userdata/kvm_config.json`,
          1000,
        );
      } catch {
        // Best effort cleanup.
      }
    };

    try {
      await test.step("Configure mock API and stable channel", async () => {
        await page.goto("/");
        await waitForWebRTCReady(page);
        await waitForTerminalReady(page);

        expect(
          await sendTerminalCommand(
            page,
            `sed -i 's|"update_api_url": "[^"]*"|"update_api_url": "${mockServerUrl}"|' /userdata/kvm_config.json`,
            1000,
          ),
        ).toBe(true);
        expect(
          await sendTerminalCommand(
            page,
            `sed -i 's|"include_pre_release": [^,]*|"include_pre_release": false|' /userdata/kvm_config.json`,
            1000,
          ),
        ).toBe(true);
        configModified = true;

        await sendTerminalCommand(page, "reboot", 0);
        await reconnectAfterReboot(page);
      });

      await test.step(`Downgrade to ${stableVersion}`, async () => {
        await page.goto(`/settings/general/update?custom_app_version=${stableVersion}&reset_config=false`);
        await page.waitForLoadState("networkidle");

        const updateButton = page.locator('[data-testid="update-now-button"]');
        await expect(updateButton).toBeVisible({ timeout: 20000 });
        await updateButton.click();
        await reconnectAfterReboot(page, 35000);
      });

      await test.step("Attempt regular update and assert signature error", async () => {
        await page.goto("/settings/general/update");
        await page.waitForLoadState("networkidle");

        const updateButton = page.getByRole("button", { name: "Update Now" });
        await expect(updateButton).toBeVisible({ timeout: 20000 });
        await updateButton.click();

        await expect(page.getByText(/requires GPG signature/i)).toBeVisible({ timeout: 30000 });
      });
    } finally {
      await restoreConfig();
    }
  });
});
