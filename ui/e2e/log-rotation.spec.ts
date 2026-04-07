import { test, expect } from "@playwright/test";

import { sshExec, getDeviceHost, waitForDeviceReady } from "./helpers";

const APP_LOG_PATH = "/userdata/jetkvm/app.log";
const MAX_LOG_SIZE_BYTES = 50 * 1024 * 1024; // 50 MB

test.describe("Log rotation and diagnostics", () => {
  test("app.log exists and contains version banner", async () => {
    const ls = await sshExec(`ls -la ${APP_LOG_PATH}`);
    expect(ls).toContain("app.log");

    const appVersion = await sshExec(`grep "app_version" ${APP_LOG_PATH}`);
    expect(appVersion.trim().length).toBeGreaterThan(0);

    const systemVersion = await sshExec(`grep "system_version" ${APP_LOG_PATH}`);
    expect(systemVersion.trim().length).toBeGreaterThan(0);
  });

  test("app.log is within the size limit", async () => {
    const sizeStr = await sshExec(`stat -c%s ${APP_LOG_PATH}`);
    const size = parseInt(sizeStr.trim(), 10);
    expect(size).toBeGreaterThan(0);
    expect(size).toBeLessThan(MAX_LOG_SIZE_BYTES);
  });

  test("diagnostics zip contains app.log", async ({ page }) => {
    const host = getDeviceHost();
    const resp = await page.request.get(`http://${host}/diagnostics`);
    expect(resp.status()).toBe(200);

    const body = await resp.body();
    expect(body.length).toBeGreaterThan(0);

    const zipContent = body.toString("binary");
    expect(zipContent).toContain("app.log");
  });

  test("app.log continues growing after restart", async () => {
    test.setTimeout(120_000);

    const host = getDeviceHost();

    const beforeLines = await sshExec(`wc -l < ${APP_LOG_PATH}`);
    const lineCountBefore = parseInt(beforeLines.trim(), 10);

    await sshExec("killall jetkvm_app", true);
    await new Promise(r => setTimeout(r, 2000));

    await waitForDeviceReady(host, 60000);

    const afterLines = await sshExec(`wc -l < ${APP_LOG_PATH}`);
    const lineCountAfter = parseInt(afterLines.trim(), 10);
    expect(lineCountAfter).toBeGreaterThan(lineCountBefore);

    const versionAfterRestart = await sshExec(`grep "app_version" ${APP_LOG_PATH}`);
    expect(versionAfterRestart.trim().length).toBeGreaterThan(0);
  });
});
