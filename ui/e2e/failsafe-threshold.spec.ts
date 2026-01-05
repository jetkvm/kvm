import { test, expect } from "@playwright/test";

import {
  waitForWebRTCReady,
  waitForTerminalReady,
  sendTerminalCommand,
  reconnectAfterReboot,
} from "./helpers";

test.describe("Failsafe threshold", () => {
  test.setTimeout(300000); // 5 minutes

  test("failsafe activates only after threshold within window", async ({ page }) => {
    const run = async (command: string, waitMs = 500) => {
      const sent = await sendTerminalCommand(page, command, waitMs);
      expect(sent, `Failed to send command: ${command}`).toBe(true);
    };

    await page.goto("/");
    await waitForWebRTCReady(page);
    await waitForTerminalReady(page);

    await test.step("Create two crashdumps within window (should not trigger failsafe)", async () => {
      await run("mkdir -p /userdata/jetkvm/crashdump");
      await run(
        "rm -f /userdata/jetkvm/crashdump/jetkvm-*.log /userdata/jetkvm/crashdump/last-crash.log",
      );
      await run("printf 'crash A\\n' > /userdata/jetkvm/crashdump/jetkvm-20200101-000000.log");
      await run("printf 'crash B\\n' > /userdata/jetkvm/crashdump/jetkvm-20200101-000500.log");
      await run(
        "ln -sf /userdata/jetkvm/crashdump/jetkvm-20200101-000500.log /userdata/jetkvm/crashdump/last-crash.log",
      );

      await run("reboot", 0);
      await reconnectAfterReboot(page, 30000);
      await waitForWebRTCReady(page);
      await waitForTerminalReady(page);

      await expect(page.getByText("Fail safe mode activated")).toHaveCount(0);
    });

    await test.step("Add third crashdump within window (should trigger failsafe)", async () => {
      await run("printf 'crash C\\n' > /userdata/jetkvm/crashdump/jetkvm-20200101-000900.log");
      await run(
        "ln -sf /userdata/jetkvm/crashdump/jetkvm-20200101-000900.log /userdata/jetkvm/crashdump/last-crash.log",
      );

      await run("reboot", 0);
      await reconnectAfterReboot(page, 30000);
      await waitForWebRTCReady(page);

      await expect(page.getByText("Fail safe mode activated")).toBeVisible();
    });

    await test.step("Reboot from failsafe clears overlay", async () => {
      await page.getByRole("button", { name: "Reboot Device" }).click();
      const confirmReboot = page.getByRole("button", { name: "Yes" });
      await expect(confirmReboot).toBeVisible();
      await confirmReboot.click();
      await reconnectAfterReboot(page, 30000);
      await waitForWebRTCReady(page);
      await waitForTerminalReady(page);
      await expect(page.getByText("Fail safe mode activated")).toHaveCount(0);
    });

    await test.step("Cleanup crashdumps and reboot", async () => {
      await waitForTerminalReady(page);
      await run(
        "rm -f /userdata/jetkvm/crashdump/jetkvm-20200101-000000.log /userdata/jetkvm/crashdump/jetkvm-20200101-000500.log /userdata/jetkvm/crashdump/jetkvm-20200101-000900.log /userdata/jetkvm/crashdump/last-crash.log",
      );
      await run("reboot", 0);
      await reconnectAfterReboot(page, 30000);
      await waitForWebRTCReady(page);
    });
  });
});
