import { test, expect, type Page } from "@playwright/test";
import {
  UDC_NAME,
  UDC_STATE_PATH,
  callJsonRpc,
  ensureNoPasswordViaAPI,
  ensureRpcReady,
  restartAppViaSSH,
  skipWithoutDeviceShell,
  sshExec,
  waitForUdcState,
} from "../helpers";
import { createRemoteAgent, waitForKeyboardReady } from "./remote-agent";
import { remoteHostExec } from "./shared";

const agent = createRemoteAgent();
const softConnect = `/sys/class/udc/${UDC_NAME}/soft_connect`;
const faultFile = "/tmp/e2e-usb-enumeration-state";
const noOptionalClasses = {
  keyboard: false,
  absolute_mouse: false,
  relative_mouse: false,
  mass_storage: false,
  serial_console: false,
  audio: false,
};
type Devices = typeof noOptionalClasses;
const keyboardOnly = { ...noOptionalClasses, keyboard: true };

function hostDevice(): { address: string; interfaces: string[] } | null {
  // The agent's `device` field is empty on sysfs-based discovery. Read the
  // actual host-assigned address and descriptors, not cached application state.
  const devices = JSON.parse(
    remoteHostExec(`python3 - <<'PY'
import json
from pathlib import Path
devices = []
for p in Path('/sys/bus/usb/devices').iterdir():
    try:
        if (p / 'manufacturer').read_text().strip() != 'JetKVM':
            continue
        interfaces = []
        for i in p.glob(p.name + ':*'):
            interfaces.append('/'.join((i / attr).read_text().strip()
                for attr in ('bInterfaceClass', 'bInterfaceSubClass', 'bInterfaceProtocol')))
        devices.append({'address': (p / 'busnum').read_text().strip() + ':' +
            (p / 'devnum').read_text().strip(), 'interfaces': sorted(interfaces)})
    except FileNotFoundError:
        pass
print(json.dumps(devices))
PY`),
  );
  expect(devices.length, "test rig must have at most one JetKVM USB device").toBeLessThanOrEqual(1);
  return devices[0] ?? null;
}

let savedDevices: Devices | undefined;
let savedEnabled: boolean;

test.beforeEach(async ({ page }) => {
  savedDevices = undefined;
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");
  await skipWithoutDeviceShell();
  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);
  await page.goto("/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  savedEnabled = (await callJsonRpc(page, "getUsbEmulationState")) as boolean;
  savedDevices = (await callJsonRpc(page, "getUsbDevices")) as Devices;
});

test.afterEach(async ({ page }, testInfo) => {
  if (!savedDevices) return;
  try {
    if (testInfo.status !== testInfo.expectedStatus) {
      await testInfo.attach("usb-device-log", {
        body: await sshExec("cat /userdata/jetkvm/last.log", true),
        contentType: "text/plain",
      });
      await testInfo.attach("host-input-devices", {
        body: JSON.stringify(await agent!.getJetKVMInputDevices()),
        contentType: "application/json",
      });
    }
  } finally {
    // A failed assertion must not leave the overlay hiding the real kernel state.
    await sshExec(
      `umount ${UDC_STATE_PATH} 2>/dev/null || true; rm -f ${faultFile}; ` +
        `echo connect > ${softConnect} 2>/dev/null || true`,
    );
    await ensureRpcReady(page);
    if (!(await callJsonRpc(page, "getUsbEmulationState"))) {
      await callJsonRpc(page, "setUsbEmulationState", { enabled: true });
    }
    try {
      await callJsonRpc(page, "setUsbDevices", { devices: savedDevices });
    } finally {
      if (!savedEnabled) {
        await callJsonRpc(page, "setUsbEmulationState", { enabled: false });
      }
    }
  }
});

async function startWithClasses(page: Page, devices: Devices) {
  await callJsonRpc(page, "setUsbDevices", { devices });
  // Exercise startup from persisted settings, not just a live configuration edit.
  // Stop the old page reconnecting while the process restarts: otherwise it
  // can take the WebRTC session back from the newly loaded page.
  await page.goto("about:blank");
  await restartAppViaSSH();
  await page.goto("/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  await waitForUdcState("configured", 15_000);
  await expect.poll(() => agent!.expectJetKVMConnected(), { timeout: 15_000 }).toBeTruthy();
  await agent!.waitForInputDevices(devices.keyboard ? ["keyboard"] : [], 15_000);
  expect(await callJsonRpc(page, "getUsbDevices")).toEqual(devices);
}

async function linkedFunctions(): Promise<string[]> {
  return (
    await sshExec(
      "for f in /sys/kernel/config/usb_gadget/jetkvm/configs/c.1/*; do " +
        '[ ! -L "$f" ] || basename "$f"; done | sort',
    )
  )
    .trim()
    .split("\n");
}

async function stallEnumeration() {
  // Fault injection at the kernel-status boundary, not an app recovery hook:
  // disconnect USB from the host, but report the stuck `default` state that the
  // old poller ignored. Real enumeration failures are not reliably inducible
  // with a good cable because the host often repairs them itself.
  // Unbinding the UDC removes this sysfs inode (and its bind mount), so only
  // recovery restores the real state file. Nothing here reconnects the cable.
  await sshExec(
    `echo default > ${faultFile}; ` +
      `mount --bind ${faultFile} ${UDC_STATE_PATH} && ` +
      `echo disconnect > ${softConnect}`,
  );
  await expect.poll(() => agent!.expectJetKVMConnected(), { timeout: 5_000 }).toBeFalsy();
  expect((await sshExec(`cat ${UDC_STATE_PATH}`)).trim()).toBe("default");
}

for (const [name, devices, functions] of [
  ["keyboard only", keyboardOnly, ["hid.usb0", "hid.usb3"]],
  // The wake HID is intentionally always present, even when every optional
  // class is disabled. No keyboard or mouse should be added by recovery.
  ["all optional classes disabled", noOptionalClasses, ["hid.usb3"]],
] as const) {
  test(`stalled enumeration recovers after startup with ${name} @ssh`, async ({ page }) => {
    test.setTimeout(180_000);
    await startWithClasses(page, devices);
    expect(await linkedFunctions()).toEqual(functions);
    if (devices.keyboard) {
      expect((await waitForKeyboardReady(agent!, page, 30_000)).length).toBeGreaterThan(0);
    }

    const interfaces = devices.keyboard ? ["03/00/00", "03/01/01"] : ["03/00/00"];
    const before = hostDevice();
    expect(before, "test rig must expose a USB device with manufacturer JetKVM").not.toBeNull();
    expect(before?.interfaces).toEqual(interfaces);
    await stallEnumeration();

    // This times out on the old binary: its poller ignores `default`, leaving
    // the host disconnected. Do not manually rebind or reconnect in this path.
    await waitForUdcState("configured", 40_000);
    await expect.poll(() => agent!.expectJetKVMConnected(), { timeout: 15_000 }).toBeTruthy();
    const recovered = hostDevice();
    expect(recovered?.interfaces).toEqual(interfaces);
    expect(recovered!.address, "host must observe a fresh enumeration").not.toBe(before!.address);
    expect(await linkedFunctions()).toEqual(functions);
    expect(await callJsonRpc(page, "getUsbDevices")).toEqual(devices);
    if (devices.keyboard) {
      expect(
        (await waitForKeyboardReady(agent!, page, 15_000)).length,
        "keyboard reports must reach the host after recovery",
      ).toBeGreaterThan(0);
    }

    // More than a full watchdog grace period: successful recovery must not
    // turn into repeated disconnects or silently restore disabled classes.
    const until = Date.now() + 20_000;
    while (Date.now() < until) {
      expect(hostDevice()).toEqual(recovered);
      await page.waitForTimeout(1_000);
    }
    expect(await linkedFunctions()).toEqual(functions);

    if (devices.keyboard) {
      // A successful enumeration must rearm the watchdog in this same process.
      // Restarting the app here would hide a latch that never rearms.
      await stallEnumeration();
      await waitForUdcState("configured", 40_000);
      await expect.poll(() => hostDevice()?.interfaces, { timeout: 15_000 }).toEqual(interfaces);
      expect(hostDevice()!.address).not.toBe(recovered!.address);
      expect((await waitForKeyboardReady(agent!, page, 15_000)).length).toBeGreaterThan(0);
      expect(await linkedFunctions()).toEqual(functions);
      expect(await callJsonRpc(page, "getUsbDevices")).toEqual(devices);
    }
    expect(await sshExec("cat /userdata/jetkvm/last.log")).not.toContain(
      "automatic enumeration retry exhausted",
    );
  });
}

test("disabling USB during stalled enumeration cancels recovery until explicitly enabled @ssh", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await startWithClasses(page, keyboardOnly);
  await stallEnumeration();
  await callJsonRpc(page, "setUsbEmulationState", { enabled: false });
  // Opening a new browser session while USB is off must not enable it either.
  await page.reload({ waitUntil: "networkidle" });
  await ensureRpcReady(page);

  const until = Date.now() + 25_000;
  while (Date.now() < until) {
    expect(await callJsonRpc(page, "getUsbEmulationState")).toBe(false);
    expect(await agent!.expectJetKVMConnected()).toBeFalsy();
    await page.waitForTimeout(1_000);
  }
  expect(await callJsonRpc(page, "getUsbDevices")).toEqual(keyboardOnly);

  await callJsonRpc(page, "setUsbEmulationState", { enabled: true });
  await waitForUdcState("configured", 15_000);
  expect((await waitForKeyboardReady(agent!, page, 15_000)).length).toBeGreaterThan(0);
  expect(await linkedFunctions()).toEqual(["hid.usb0", "hid.usb3"]);
});
