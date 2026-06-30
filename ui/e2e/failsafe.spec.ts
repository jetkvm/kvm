import { test, expect, type Page } from "@playwright/test";

import {
  dismissSessionTakeoverDialog,
  ensureLocalAuthMode,
  restartAppViaSSH,
  sshExec,
} from "./helpers";

const CRASHDUMP_DIR = "/userdata/jetkvm/crashdump";
const LAST_CRASH_LOG = `${CRASHDUMP_DIR}/last-crash.log`;
const TEST_CRASH_PREFIX = `${CRASHDUMP_DIR}/e2e-failsafe`;
const NATIVE_FAILSAFE_SENTINEL = "failsafe::native.max_restart_attempts_reached";
const NATIVE_FAILSAFE_COPY = "Native stack disabled after repeated failures.";

async function prepareConfiguredDevice(page: Page): Promise<void> {
  await clearTestCrashdump();
  await restartAppViaSSH();
  await page.goto("/");
  await page.waitForLoadState("networkidle");
  await ensureLocalAuthMode(page, { mode: "noPassword" });
  await dismissSessionTakeoverDialog(page);
}

async function writeSupervisorCrashLog(name: string, content: string): Promise<void> {
  const path = `${TEST_CRASH_PREFIX}-${name}.log`;
  const encoded = Buffer.from(content).toString("base64");
  await sshExec(
    `mkdir -p ${CRASHDUMP_DIR} && ` +
      `rm -f ${LAST_CRASH_LOG} ${path} && ` +
      `printf %s ${encoded} | base64 -d > ${path} && ` +
      `ln -s ${path} ${LAST_CRASH_LOG} && sync`,
  );
}

async function clearTestCrashdump(): Promise<void> {
  await sshExec(`rm -f ${LAST_CRASH_LOG} ${TEST_CRASH_PREFIX}-*.log && sync`, true);
}

async function expectNativeFailsafeOverlay(page: Page): Promise<void> {
  await page.goto("/");
  await page.waitForLoadState("networkidle");
  await dismissSessionTakeoverDialog(page);
  await expect(page.getByText(NATIVE_FAILSAFE_COPY)).toBeVisible({ timeout: 30000 });
}

async function expectNoNativeFailsafeOverlay(page: Page): Promise<void> {
  await page.goto("/");
  await page.waitForLoadState("networkidle");
  await dismissSessionTakeoverDialog(page);
  await expect(page.getByText(NATIVE_FAILSAFE_COPY)).toBeHidden({ timeout: 15000 });
}

async function readAppLog(): Promise<string> {
  return sshExec("cat /userdata/jetkvm/last.log", true);
}

async function lastCrashLogState(): Promise<string> {
  return sshExec(
    `if [ -e ${LAST_CRASH_LOG} ] || [ -L ${LAST_CRASH_LOG} ]; then echo present; else echo missing; fi`,
    true,
  ).then(output => output.trim());
}

test.describe("Failsafe startup classification", () => {
  test.setTimeout(180000);
  test.describe.configure({ mode: "serial" });

  test.beforeEach(async ({ page }) => {
    await prepareConfiguredDevice(page);
  });

  test.afterEach(async () => {
    await clearTestCrashdump();
    await restartAppViaSSH();
  });

  test("plain app crash log is diagnostic only", async ({ page }) => {
    await writeSupervisorCrashLog(
      "plain-crash",
      `SIGILL: illegal instruction
github.com/vishvananda/netlink.(*Handle).LinkByName
github.com/jetkvm/kvm/pkg/nmlite.(*InterfaceManager).monitorInterfaceState
`,
    );

    await restartAppViaSSH();
    await expectNoNativeFailsafeOverlay(page);

    const log = await readAppLog();
    expect(log).not.toContain("failsafe mode activated");
    expect(await lastCrashLogState()).toBe("missing");
  });

  test("native restart exhaustion activates failsafe once", async ({ page }) => {
    await writeSupervisorCrashLog(
      "native-restart-exhausted",
      `max restart attempts reached, exiting: ${NATIVE_FAILSAFE_SENTINEL}
`,
    );

    await restartAppViaSSH();
    await expectNativeFailsafeOverlay(page);

    let log = await readAppLog();
    expect(log).toContain("failsafe mode active, using empty native interface");
    expect(await lastCrashLogState()).toBe("missing");

    await restartAppViaSSH();
    await expectNoNativeFailsafeOverlay(page);

    log = await readAppLog();
    expect(log).not.toContain("failsafe mode activated");
    expect(log).not.toContain("failsafe mode active, using empty native interface");
  });
});
