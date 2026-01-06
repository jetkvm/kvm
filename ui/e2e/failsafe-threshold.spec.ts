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
      await reconnectAfterReboot(page);
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
      await reconnectAfterReboot(page);
      await waitForWebRTCReady(page);

      await expect(page.getByText("Fail safe mode activated")).toBeVisible();
    });

    await test.step("Reboot from failsafe clears overlay", async () => {
      await page.getByRole("button", { name: "Reboot Device" }).click();
      const confirmReboot = page.getByRole("button", { name: "Yes" });
      await expect(confirmReboot).toBeVisible();
      await confirmReboot.click();
      await reconnectAfterReboot(page);
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
      await reconnectAfterReboot(page);
      await waitForWebRTCReady(page);
    });
  });

  test("third crash outside 10-minute window does not trigger failsafe", async ({ page }) => {
    const run = async (command: string, waitMs = 500) => {
      const sent = await sendTerminalCommand(page, command, waitMs);
      expect(sent, `Failed to send command: ${command}`).toBe(true);
    };

    await page.goto("/");
    await waitForWebRTCReady(page);
    await waitForTerminalReady(page);

    await test.step("Create 2 old crashes and a 3rd with current mtime (should not trigger failsafe)", async () => {
      await run("mkdir -p /userdata/jetkvm/crashdump");
      await run(
        "rm -f /userdata/jetkvm/crashdump/jetkvm-*.log /userdata/jetkvm/crashdump/last-crash.log",
      );

      // Create first two crash dumps with mtime 11 minutes in the past (using device's date)
      // touch -d @epoch works on BusyBox
      await run(
        "printf 'crash A\\n' > /userdata/jetkvm/crashdump/jetkvm-20200101-100000.log && touch -d @$(($(date +%s) - 660)) /userdata/jetkvm/crashdump/jetkvm-20200101-100000.log",
      );
      await run(
        "printf 'crash B\\n' > /userdata/jetkvm/crashdump/jetkvm-20200101-100500.log && touch -d @$(($(date +%s) - 660)) /userdata/jetkvm/crashdump/jetkvm-20200101-100500.log",
      );

      // Create third crash dump with current mtime
      // The 10-min window from this crash won't include A and B (they're 11 min older)
      await run("printf 'crash C\\n' > /userdata/jetkvm/crashdump/jetkvm-20200101-101100.log");

      // Symlink to the newest crash (the one with current mtime)
      await run(
        "ln -sf /userdata/jetkvm/crashdump/jetkvm-20200101-101100.log /userdata/jetkvm/crashdump/last-crash.log",
      );

      await run("reboot", 0);
      await reconnectAfterReboot(page);
      await waitForWebRTCReady(page);
      await waitForTerminalReady(page);

      // Should NOT trigger failsafe because only 1 crash (the newest) is within its 10-min window
      await expect(page.getByText("Fail safe mode activated")).toHaveCount(0);
    });

    await test.step("Cleanup crashdumps and reboot", async () => {
      await run(
        "rm -f /userdata/jetkvm/crashdump/jetkvm-20200101-100000.log /userdata/jetkvm/crashdump/jetkvm-20200101-100500.log /userdata/jetkvm/crashdump/jetkvm-20200101-101100.log /userdata/jetkvm/crashdump/last-crash.log",
      );
      await run("reboot", 0);
      await reconnectAfterReboot(page);
      await waitForWebRTCReady(page);
    });
  });
});
