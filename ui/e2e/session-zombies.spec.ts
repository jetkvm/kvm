import { test, expect } from "@playwright/test";
import { ensureNoPasswordViaAPI, sshExec, waitForWebRTCReady } from "./helpers";

// Every WebRTC session opens a terminal data channel, and the device spawns a
// shell for it. A closed session has to reap that shell (#1578).

async function zombieChildrenOfApp(): Promise<string[]> {
  const out = await sshExec(
    "for p in /proc/[0-9]*; do " +
      'set -- $(awk "{print \\$2, \\$3, \\$4}" $p/stat 2>/dev/null); ' +
      '[ "$2" = Z ] || continue; ' +
      'grep -q jetkvm_app /proc/$3/comm 2>/dev/null && echo "$(basename $p) $1"; ' +
      "done",
    true,
  );
  return out.trim().split("\n").filter(Boolean);
}

test("closed sessions leave no zombie shells behind", async ({ browser }) => {
  test.setTimeout(90_000);
  await ensureNoPasswordViaAPI();

  for (let i = 0; i < 3; i++) {
    const page = await browser.newPage();
    await page.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(page);
    await page.close();
  }

  // Give the last session's channels a moment to close on the device.
  await expect.poll(zombieChildrenOfApp, { timeout: 10_000, intervals: [1000] }).toEqual([]);
});
