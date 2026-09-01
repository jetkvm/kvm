import { test, expect, type Page } from "@playwright/test";
import {
  DWC3_PATH,
  UDC_NAME,
  callJsonRpc,
  ensureNoPasswordViaAPI,
  ensureRpcReady,
  sshExec,
  waitForUdcState,
  waitForWebRTCReady,
} from "../helpers";
import { createRemoteAgent, waitForKeyboardReady } from "./remote-agent";

const agent = createRemoteAgent();

const TEST_IMAGE = "e2e-recovery-test.iso";

test.describe.configure({ mode: "serial" });

let page: Page;

async function waitForKeyboardRoundTrip(timeoutMs: number): Promise<boolean> {
  return (await waitForKeyboardReady(agent!, page, timeoutMs)).length > 0;
}

test.beforeAll(async ({ browser }) => {
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");
  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);

  page = await browser.newPage();
  await page.goto("/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 30_000);
});

test.afterAll(async () => {
  try {
    await callJsonRpc(page, "unmountImage");
  } catch {}
  await sshExec(`rm -f /userdata/jetkvm/images/${TEST_IMAGE}`, true);
  if (page) await page.close();
});

test("USB recovery succeeds and keyboard survives with virtual media mounted (#1314)", async () => {
  test.setTimeout(180_000);

  expect(await waitForKeyboardRoundTrip(30_000), "keyboard must work before the test").toBe(true);

  await sshExec(
    `mkdir -p /userdata/jetkvm/images && dd if=/dev/zero of=/userdata/jetkvm/images/${TEST_IMAGE} bs=1M count=8 2>/dev/null`,
  );
  try {
    await callJsonRpc(page, "unmountImage");
  } catch {}
  await callJsonRpc(page, "mountWithStorage", { filename: TEST_IMAGE, mode: "CDROM" });

  const mounted = (await callJsonRpc(page, "getVirtualMediaState")) as {
    source: string;
    filename?: string;
  } | null;
  expect(mounted).not.toBeNull();
  expect(mounted!.filename).toBe(TEST_IMAGE);

  await new Promise(r => setTimeout(r, 3_000));

  try {
    await sshExec(`echo ${UDC_NAME} > ${DWC3_PATH}/unbind 2>/dev/null`, true);

    await waitForUdcState("configured", 45_000);
    await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 15_000);
    await waitForWebRTCReady(page, 15_000);

    expect(
      await waitForKeyboardRoundTrip(60_000),
      "keyboard must self-recover after UDC unbind while media is mounted",
    ).toBe(true);

    const stateAfter = (await callJsonRpc(page, "getVirtualMediaState")) as {
      filename?: string;
    } | null;
    expect(stateAfter, "virtual media state must survive USB recovery").not.toBeNull();
    expect(stateAfter!.filename).toBe(TEST_IMAGE);

    const recoveryLog = await sshExec(
      'cat /var/log/jetkvm-stdout.log* /tmp/jetkvm-stdout.log* 2>/dev/null | grep -aE "failed to update usbgadget configuration|unable to update gadget config|unable to initialize USB stack" || true',
      true,
    );
    console.log(`[recovery-log]\n${recoveryLog}`);
    expect(
      recoveryLog,
      "recovery reconfigure must not abort on a busy mass-storage LUN",
    ).not.toMatch(/failed to update usbgadget configuration|unable to update gadget config/);
  } catch (err) {
    await sshExec(`echo ${UDC_NAME} > ${DWC3_PATH}/bind 2>/dev/null`, true);
    throw err;
  }
});
