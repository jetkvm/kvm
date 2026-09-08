import { test, expect } from "@playwright/test";
import { callJsonRpc, ensureRpcReady, ensureNoPasswordViaAPI } from "./device";
import { captureHardwareState, restoreHardwareState, type HardwareState } from "./hardware-state";

/** Preserve the test rig through destructive reset without using a device shell.
 * Uploaded files are erased by factory reset; run on a disposable test device.
 */
export function registerResetCleanup(): void {
  let hardware: HardwareState | undefined;
  const settings: { setter: string; params: Record<string, unknown> }[] = [];
  test.beforeAll(async ({ browser }) => {
    test.setTimeout(90_000);
    await ensureNoPasswordViaAPI();
    const page = await browser.newPage();
    try {
      await ensureRpcReady(page, { navigateFirst: true });
      hardware = await captureHardwareState(page);
      if (hardware.media?.source === "Storage")
        throw new Error(
          "Unmount stored media before destructive reset tests; reset erases uploaded files",
        );
      settings.push({
        setter: "setKeyboardMacros",
        params: { params: { macros: await callJsonRpc(page, "getKeyboardMacros") } },
      });
      for (const [getter, setter, key] of [
        ["getNetworkSettings", "setNetworkSettings", "settings"],
        ["getBacklightSettings", "setBacklightSettings", "params"],
        ["getDisplayRotation", "setDisplayRotation", "params"],
        ["getVideoCodecPreference", "setVideoCodecPreference", "codec"],
        ["getStreamQualityFactor", "setStreamQualityFactor", "factor"],
        ["getKeyboardLayout", "setKeyboardLayout", "layout"],
        ["getHostDisplayIdleMode", "setHostDisplayIdleMode", ""],
        ["getSSHKeyState", "setSSHKeyState", "sshKey"],
        ["getDevModeState", "setDevModeState", ""],
      ]) {
        try {
          const value = await callJsonRpc(page, getter);
          settings.push({
            setter,
            params: key ? { [key]: value } : (value as Record<string, unknown>),
          });
        } catch (error) {
          if (!/method not found/i.test(String(error))) throw error;
        }
      }
    } finally {
      await page.close();
    }
  });
  test.afterAll(async ({ browser }) => {
    test.setTimeout(120_000);
    if (!hardware) return;
    const page = await browser.newPage();
    const errors: unknown[] = [];
    try {
      // A failed welcome test may leave either onboarding or its known password.
      const status = await (await page.request.get("/device/status")).json();
      if (status.isSetup && (await page.request.get("/device")).status() === 401) {
        const origin = new URL(process.env.JETKVM_URL!).origin;
        const login = await page.request.post("/auth/login-local", {
          headers: { Origin: origin },
          data: { password: "TestPassword123" },
        });
        expect(login.ok()).toBe(true);
        const disabled = await page.request.delete("/auth/local-password", {
          headers: { Origin: origin },
          data: { password: "TestPassword123" },
        });
        expect(disabled.ok()).toBe(true);
      }
      await ensureNoPasswordViaAPI();
      await ensureRpcReady(page, { navigateFirst: true });
      for (const setting of settings) {
        try {
          await callJsonRpc(page, setting.setter, setting.params);
        } catch (error) {
          errors.push(error);
        }
      }
      try {
        await restoreHardwareState(page, hardware);
      } catch (error) {
        errors.push(error);
      }
      if (errors.length) throw new AggregateError(errors, "Reset cleanup failed");
    } finally {
      await page.close();
    }
  });
}
