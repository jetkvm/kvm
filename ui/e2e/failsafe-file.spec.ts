import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  waitForTerminalReady,
  sendTerminalCommand,
  reconnectAfterReboot,
} from "./helpers";

test.describe("Failsafe file trigger", () => {
  test.setTimeout(300000); // 5 minutes

  test(".enablefailsafe file triggers failsafe mode and reboot clears it", async ({ page }) => {
    const run = async (command: string, waitMs = 500) => {
      const sent = await sendTerminalCommand(page, command, waitMs);
      expect(sent, `Failed to send command: ${command}`).toBe(true);
    };

    await page.goto("/");
    await waitForWebRTCReady(page);
    await waitForTerminalReady(page);

    await test.step("Create .enablefailsafe file and reboot", async () => {
      await run("mkdir -p /userdata/jetkvm");
      await run("touch /userdata/jetkvm/.enablefailsafe");

      await run("reboot", 0);
      await reconnectAfterReboot(page);
      await waitForWebRTCReady(page);

      await expect(page.getByText("Fail safe mode activated")).toBeVisible();
    });

    await test.step("Verify downloaded diagnostics contains expected sections", async () => {
      // Start waiting for download before clicking
      const downloadPromise = page.waitForEvent("download");

      // Click the download button in the failsafe overlay
      await page.getByRole("button", { name: /Download Logs/i }).click();

      // Wait for the download to start
      const download = await downloadPromise;

      // Get the downloaded content as a string
      const stream = await download.createReadStream();
      const chunks: Buffer[] = [];
      for await (const chunk of stream) {
        chunks.push(chunk);
      }
      const diagnosticsContent = Buffer.concat(chunks).toString("utf-8");

      expect(diagnosticsContent, "Downloaded file should have content").toBeTruthy();

      // Verify expected section headers are present (loose matching)
      const expectedSections = [
        "=== APPLICATION LOG ===",
        "=== SYSTEM DIAGNOSTICS ===",
        "=== LAST CRASH LOG ===",
        "=== RECENT CRASH DUMPS ===",
      ];

      for (const section of expectedSections) {
        expect(diagnosticsContent, `Diagnostics should contain section: ${section}`).toContain(
          section,
        );
      }
    });

    await test.step("Reboot from failsafe clears overlay and returns to normal", async () => {
      await page.getByRole("button", { name: "Reboot Device" }).click();
      const confirmReboot = page.getByRole("button", { name: "Yes" });
      await expect(confirmReboot).toBeVisible();
      await confirmReboot.click();
      await reconnectAfterReboot(page);
      await waitForWebRTCReady(page);
      await waitForTerminalReady(page);

      await expect(page.getByText("Fail safe mode activated")).toHaveCount(0);
    });
  });
});
