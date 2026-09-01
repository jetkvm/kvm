import { execSync } from "child_process";
import { test, expect, type Page } from "@playwright/test";
import { HID_KEY, SSH_OPTS, ensureNoPasswordViaAPI, ensureRpcReady, tapKey } from "../helpers";
import { createRemoteAgent, waitForKeyboardReady } from "./remote-agent";

const agent = createRemoteAgent();

test.describe.configure({ mode: "serial" });

let page: Page;

function remoteHostExec(cmd: string, timeoutMs = 15_000): string {
  const target = process.env.JETKVM_REMOTE_HOST;
  if (!target) throw new Error("JETKVM_REMOTE_HOST not set");
  const escaped = cmd.replace(/'/g, "'\\''");
  return execSync(`ssh ${SSH_OPTS} ${target} '${escaped}'`, {
    encoding: "utf8",
    timeout: timeoutMs,
  });
}

function remoteHostRootWrite(value: string, file: string) {
  remoteHostExec(
    `if [ "$(id -u)" = 0 ]; then echo -n "${value}" > ${file}; else echo -n "${value}" | sudo -n tee ${file} > /dev/null; fi`,
    10_000,
  );
}

function findGadgetKeyboardInterface(): string {
  const out = remoteHostExec(
    "for d in /sys/bus/usb/devices/*-*/; do " +
      '[ -f "$d/manufacturer" ] || continue; ' +
      'grep -qi jetkvm "$d/manufacturer" 2>/dev/null || continue; ' +
      'for i in "$d"*:*/; do ' +
      '[ -f "$i/bInterfaceClass" ] || continue; ' +
      '[ "$(cat "$i/bInterfaceClass")" = "03" ] || continue; ' +
      '[ "$(cat "$i/bInterfaceProtocol")" = "01" ] && basename "$i"; ' +
      "done; " +
      "done",
  );
  return out.trim().split("\n")[0] ?? "";
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
  if (page) await page.close();
});

test("keyboard self-recovers when HID writes time out while USB stays configured (#1512)", async () => {
  test.setTimeout(240_000);

  const before = await waitForKeyboardReady(agent!, page, 30_000);
  expect(before.length, "keyboard round-trip must work before the test").toBeGreaterThan(0);

  const iface = findGadgetKeyboardInterface();
  expect(iface, "JetKVM keyboard interface not found on remote host").toMatch(/^[\d.-]+:\d+\.\d+$/);

  try {
    remoteHostRootWrite(iface, "/sys/bus/usb/drivers/usbhid/unbind");

    for (let i = 0; i < 8; i++) {
      await tapKey(page, HID_KEY.SPACE);
      await new Promise(r => setTimeout(r, 250));
    }

    const events = await waitForKeyboardReady(agent!, page, 120_000);
    expect(
      events.length,
      "keyboard did not self-recover after HID write timeouts (issue #1512)",
    ).toBeGreaterThan(0);
  } finally {
    remoteHostExec(
      `if [ "$(id -u)" = 0 ]; then echo -n "${iface}" > /sys/bus/usb/drivers/usbhid/bind 2>/dev/null || true; else echo -n "${iface}" | sudo -n tee /sys/bus/usb/drivers/usbhid/bind > /dev/null 2>&1 || true; fi`,
      10_000,
    );
  }
});
