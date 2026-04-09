import { test, expect } from "@playwright/test";

import {
  ensureLocalAuthMode,
  waitForWebRTCReady,
  dismissSessionTakeoverDialog,
  callJsonRpc,
  reconnectAfterReboot,
  rebootDeviceViaSSH,
  sshExec,
} from "./helpers";

const DEVICE_LAST_LOG_PATH = "/userdata/jetkvm/last.log";
const TEST_LOG_PATTERN = "JSON-RPC test log probe";
const LOG_LEVELS = ["TRACE", "DEBUG", "INFO", "WARN", "ERROR"] as const;
type LogLevel = (typeof LOG_LEVELS)[number];

function shellEscapeDoubleQuotes(value: string): string {
  return value.replace(/["\\$`]/g, "\\$&");
}

async function clearDeviceLog(): Promise<void> {
  await sshExec(`: > ${DEVICE_LAST_LOG_PATH}`, true);
}

async function countDeviceLogMatches(pattern: string): Promise<number> {
  const escapedPattern = shellEscapeDoubleQuotes(pattern);
  const output = await sshExec(
    `sh -lc "grep -F -c -- \\"${escapedPattern}\\" ${DEVICE_LAST_LOG_PATH} 2>/dev/null || true"`,
    true,
  );
  return Number.parseInt(output.trim() || "0", 10);
}

async function emitTestLog(
  page: Parameters<typeof test>[0] extends never ? never : any,
  level: LogLevel,
): Promise<void> {
  await callJsonRpc(page, "emitTestLog", { level });
}

test.describe("Log level filtering", () => {
  test.setTimeout(45_000);

  test("live changes filter TRACE/DEBUG/INFO/WARN/ERROR logs without restart", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");
    await ensureLocalAuthMode(page, { mode: "noPassword" });
    await dismissSessionTakeoverDialog(page);
    await waitForWebRTCReady(page);

    const originalLevel = (await callJsonRpc(page, "getDefaultLogLevel")) as LogLevel;

    try {
      for (const [index, level] of LOG_LEVELS.entries()) {
        await clearDeviceLog();
        await callJsonRpc(page, "setDefaultLogLevel", { level });
        await page.waitForTimeout(250);
        await emitTestLog(page, level);

        await expect
          .poll(() => countDeviceLogMatches(TEST_LOG_PATTERN), {
            message: `Waiting for ${level} probe log to be written`,
            timeout: 5_000,
            intervals: [200, 500, 1000],
          })
          .toBe(1);

        const moreVerboseLevel = LOG_LEVELS[index - 1];
        if (!moreVerboseLevel) continue;

        await clearDeviceLog();
        await callJsonRpc(page, "setDefaultLogLevel", { level });
        await page.waitForTimeout(250);
        await emitTestLog(page, moreVerboseLevel);
        await page.waitForTimeout(1_000);

        expect(
          await countDeviceLogMatches(TEST_LOG_PATTERN),
          `${moreVerboseLevel} log should be suppressed when default log level is ${level}`,
        ).toBe(0);
      }
    } finally {
      await callJsonRpc(page, "setDefaultLogLevel", { level: originalLevel }).catch(() => {});
    }
  });

  test("reverts INFO to WARN after reboot", async ({ page }) => {
    test.setTimeout(90_000);

    await page.goto("/");
    await page.waitForLoadState("networkidle");
    await ensureLocalAuthMode(page, { mode: "noPassword" });
    await dismissSessionTakeoverDialog(page);
    await waitForWebRTCReady(page);

    const originalLevel = (await callJsonRpc(page, "getDefaultLogLevel")) as LogLevel;

    try {
      await callJsonRpc(page, "setDefaultLogLevel", { level: "INFO" });
      await expect
        .poll(() => callJsonRpc(page, "getDefaultLogLevel"), {
          message: "Waiting for default log level to be INFO after setDefaultLogLevel",
          intervals: [200, 500, 1000],
        })
        .toBe("INFO");

      await rebootDeviceViaSSH();
      await reconnectAfterReboot(page, 3000, 20);

      await expect
        .poll(() => callJsonRpc(page, "getDefaultLogLevel"), {
          message: "Waiting for default log level to revert to WARN after reboot",
          intervals: [200, 500, 1000],
        })
        .toBe("WARN");
    } finally {
      await callJsonRpc(page, "setDefaultLogLevel", { level: originalLevel }).catch(() => {});
    }
  });
});
