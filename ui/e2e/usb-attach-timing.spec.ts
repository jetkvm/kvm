import { test, expect } from "@playwright/test";

import { ensureLocalAuthMode, sshExec } from "./helpers";

const UDC_NAME = "ffb00000.usb";
const DWC3_PATH = "/sys/bus/platform/drivers/dwc3";
const UDC_STATE_PATH = `/sys/class/udc/${UDC_NAME}/state`;

async function readUdcState(): Promise<string> {
  try {
    const result = (await sshExec(`cat ${UDC_STATE_PATH} 2>/dev/null`, true)).trim();
    return result || "not attached";
  } catch {
    return "not attached";
  }
}

async function waitForUdcState(
  expected: string,
  timeoutMs: number,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastSeen = "";

  while (Date.now() < deadline) {
    lastSeen = await readUdcState();
    if (lastSeen === expected) return;
    await new Promise(resolve => setTimeout(resolve, 500));
  }

  throw new Error(
    `Timed out waiting for UDC state "${expected}" within ${timeoutMs}ms (last seen: "${lastSeen}")`,
  );
}

async function unbindUdc(): Promise<void> {
  await sshExec(`echo ${UDC_NAME} > ${DWC3_PATH}/unbind 2>/dev/null`, true);
}

test.describe("USB auto-recovery after UDC unbind", () => {
  test.setTimeout(120_000);

  test("recovers USB gadget after UDC is manually unbound", async ({ page }) => {
    await ensureLocalAuthMode(page, { mode: "noPassword" });

    // 1. Verify USB is currently attached
    await waitForUdcState("configured", 10_000);

    // 2. Unbind the UDC via SSH (simulates host disconnect / reboot scenario).
    //    This removes the UDC entirely, causing GetUsbState() to return "not attached".
    await unbindUdc();

    // 3. The fix should auto-recover: detect "not attached" + desired=true,
    //    rebind the UDC, and the host should re-enumerate it back to "configured".
    //    Recovery poll is 500ms with 5s retry interval, plus host enumeration time.
    //    Without the fix, this would time out because the app never rebinds.
    await waitForUdcState("configured", 30_000);
  });
});
