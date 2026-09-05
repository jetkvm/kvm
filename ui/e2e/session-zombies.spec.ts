import { test, expect } from "@playwright/test";
import { ensureNoPasswordViaAPI, sshExec, waitForWebRTCReady } from "./helpers";

// Every WebRTC session opens a terminal data channel, and the device spawns a
// shell for it. A closed session has to reap that shell (#1578).

// The scan ends with a sentinel so a failed or truncated SSH call cannot
// read as "no zombies". Errors propagate instead of being ignored.
async function zombieChildrenOfApp(): Promise<string[]> {
  const out = await sshExec(
    "for p in /proc/[0-9]*; do " +
      'set -- $(awk "{print \\$2, \\$3, \\$4}" $p/stat 2>/dev/null); ' +
      '[ "$2" = Z ] || continue; ' +
      'grep -q jetkvm_app /proc/$3/comm 2>/dev/null && echo "$(basename $p) $1"; ' +
      "done; echo scan-done",
  );
  const lines = out.trim().split("\n").filter(Boolean);
  if (lines.pop() !== "scan-done")
    throw new Error(`zombie scan did not complete: ${JSON.stringify(out)}`);
  return lines;
}

test("closed sessions leave no zombie shells behind", async ({ browser }) => {
  test.setTimeout(90_000);
  await ensureNoPasswordViaAPI();

  for (let i = 0; i < 3; i++) {
    const page = await browser.newPage();
    await page.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(page);
    // The terminal channel opens on its own after the HID channel, and the
    // device spawns the shell only once it does. Closing earlier would leave
    // nothing to reap and pass without the fix.
    await expect
      .poll(() => page.evaluate(() => window.__kvmTestHooks?.isTerminalReady() ?? false), {
        message: "terminal channel should open",
        timeout: 15_000,
      })
      .toBe(true);
    await page.close();
  }

  // Give the last session's channels a moment to close on the device.
  await expect.poll(zombieChildrenOfApp, { timeout: 10_000, intervals: [1000] }).toEqual([]);
});
