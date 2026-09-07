import { test, expect, type Page } from "@playwright/test";
import { callJsonRpc, ensureRpcReady, getDeviceHost } from "../helpers";
import { registerSharedSession } from "./shared";

test.describe.configure({ mode: "serial" });

let sharedPage: Page;
registerSharedSession(page => (sharedPage = page));

test.describe("Remote Host Agent: factory reset", () => {
  // ═══════════════════════════════════════════
  // FACTORY RESET (must be last — erases all user data and reboots)
  // ═══════════════════════════════════════════

  test("factory-reset: reset device via RPC and verify setup endpoint after reboot", async () => {
    test.setTimeout(120_000);
    const host = getDeviceHost();

    await callJsonRpc(sharedPage, "factoryReset");

    // First, wait for the device to go DOWN (become unreachable).
    // Without this, we may poll /device/status before the reboot starts
    // and get the stale pre-reset isSetup=true response.
    const waitForDeviceDown = async (timeout: number) => {
      const start = Date.now();
      while (Date.now() - start < timeout) {
        try {
          await fetch(`http://${host}/device/status`, {
            signal: AbortSignal.timeout(2000),
          });
          // Still reachable — keep waiting
        } catch {
          return; // Device is down
        }
        await new Promise(r => setTimeout(r, 500));
      }
      throw new Error(`Device did not go down within ${timeout}ms`);
    };

    const waitForDeviceUp = async (timeout: number) => {
      const start = Date.now();
      while (Date.now() - start < timeout) {
        try {
          const res = await fetch(`http://${host}/device/status`, {
            signal: AbortSignal.timeout(2000),
          });
          if (res.ok) return (await res.json()) as { isSetup: boolean };
        } catch {
          // Device is still rebooting
        }
        await new Promise(r => setTimeout(r, 2000));
      }
      throw new Error(`Device did not come back within ${timeout}ms`);
    };

    await waitForDeviceDown(30_000);
    const status = await waitForDeviceUp(90_000);
    expect(status.isSetup, "Device should be not set up after factory reset").toBe(false);

    const setupRes = await fetch(`http://${host}/device/setup`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ localAuthMode: "noPassword" }),
    });
    expect(setupRes.ok, `Setup POST failed: ${setupRes.status}`).toBe(true);

    const verifyRes = await fetch(`http://${host}/device/status`);
    const verify = (await verifyRes.json()) as { isSetup: boolean };
    expect(verify.isSetup, "Device should be set up after POST /device/setup").toBe(true);

    // Restore SSH key so subsequent test runs can SSH into the device.
    const context = await sharedPage
      .context()
      .browser()!
      .newContext({
        baseURL: `http://${host}`,
      });
    const freshPage = await context.newPage();
    try {
      await freshPage.goto("/");
      await ensureRpcReady(freshPage);

      const fs = await import("fs");
      const os = await import("os");
      const path = await import("path");
      const sshPubKeyPath = path.join(os.homedir(), ".ssh", "id_ed25519.pub");
      let sshKey: string;
      try {
        sshKey = fs.readFileSync(sshPubKeyPath, "utf-8").trim();
      } catch {
        const rsaPath = path.join(os.homedir(), ".ssh", "id_rsa.pub");
        sshKey = fs.readFileSync(rsaPath, "utf-8").trim();
      }

      await callJsonRpc(freshPage, "setSSHKeyState", { sshKey });
    } finally {
      await freshPage.close();
      await context.close();
    }
  });
});
