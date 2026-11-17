import { createHash, randomBytes } from "node:crypto";
import { expectMountedImageHash } from "../helpers/storage-readback";
import { ensureUSBEmulationState } from "../helpers/hardware-state";
import { test, expect, type Page } from "@playwright/test";
import { callJsonRpc, ensureRpcReady, waitForLedState } from "../helpers";
import { agent, registerSharedSession, remoteHostExec } from "./shared";
import { waitForKeyboardReady } from "./remote-agent";

let page: Page;
let uploadedFilename: string | undefined;
registerSharedSession(p => {
  page = p;
});
test.describe.configure({ mode: "serial" });

test("USB emulation detaches from the host and recovers keyboard input", async () => {
  test.setTimeout(90_000);
  try {
    await callJsonRpc(page, "setUsbEmulationState", { enabled: false });
    expect(await callJsonRpc(page, "getUsbEmulationState")).toBe(false);
    await expect.poll(() => agent!.getJetKVMInputDevices(), { timeout: 15_000 }).toEqual([]);
    await callJsonRpc(page, "setUsbEmulationState", { enabled: false });
    await callJsonRpc(page, "setUsbEmulationState", { enabled: true });
    expect(await callJsonRpc(page, "getUsbEmulationState")).toBe(true);
    expect((await waitForKeyboardReady(agent!, page, 30_000)).length).toBeGreaterThan(0);
  } finally {
    await ensureUSBEmulationState(page, true);
  }
});

test("repeated USB enable preserves host-driven keyboard LED updates", async () => {
  const leds = JSON.parse(
    remoteHostExec(`python3 - <<'PY'
import json
from pathlib import Path
leds = []
for p in Path('/sys/class/leds').glob('*::capslock'):
    if 'JetKVM' in (p / 'device/name').read_text():
        leds.append({'path': str(p), 'enabled': (p / 'brightness').read_text().strip() != '0'})
print(json.dumps(leds))
PY`),
  ) as { path: string; enabled: boolean }[];
  expect(leds, "host must expose the JetKVM keyboard's Caps Lock LED").toHaveLength(1);
  const led = leds[0];
  expect(led.path).toMatch(/^\/sys\/class\/leds\/input\d+::capslock$/);
  const setHostLED = (enabled: boolean) =>
    remoteHostExec(`echo ${Number(enabled)} | sudo -n tee '${led.path}/brightness' > /dev/null`);
  try {
    // Establish that host output reports reach the existing listener.
    setHostLED(!led.enabled);
    await waitForLedState(page, "caps_lock", !led.enabled);
    setHostLED(led.enabled);
    await waitForLedState(page, "caps_lock", led.enabled);

    await callJsonRpc(page, "setUsbEmulationState", { enabled: true });
    await callJsonRpc(page, "setUsbEmulationState", { enabled: true });

    // Send no keyboard input: a HID write could reopen a wrongly closed handle
    // and hide the lost listener. Reusing the LED path also detects re-enumeration.
    setHostLED(!led.enabled);
    await waitForLedState(page, "caps_lock", !led.enabled);
  } finally {
    setHostLED(led.enabled);
  }
});

test.afterEach(async () => {
  if (!uploadedFilename) return;
  const filename = uploadedFilename;
  uploadedFilename = undefined;
  // Leave the upload page to abort any still-running browser transfer.
  await page.goto("about:blank");
  await ensureRpcReady(page, { navigateFirst: true });
  const errors: unknown[] = [];
  try {
    await ensureUSBEmulationState(page, true);
  } catch (error) {
    errors.push(error);
  }
  try {
    await callJsonRpc(page, "unmountImage");
  } catch (error) {
    errors.push(error);
  }
  // Delete the partial name first in case the transfer finished while the
  // page was closing. Then remove a completed upload, if one exists.
  for (const name of [`${filename}.incomplete`, filename]) {
    try {
      await callJsonRpc(page, "deleteStorageFile", { filename: name });
    } catch (error) {
      if (!String(error).includes("file does not exist:")) errors.push(error);
    }
  }
  if (errors.length) throw new AggregateError(errors, "USB media test cleanup failed");
});

test("USB reconnect preserves mounted media bytes and keyboard input", async () => {
  // Three cycles allow 15 s detach + 30 s input + 45 s media readback each.
  // Reserve another two minutes for upload, initial readback and cleanup.
  test.setTimeout(3 * (15_000 + 30_000 + 45_000) + 120_000);
  const filename = `e2e-usb-reconnect-${Date.now()}.img`;
  uploadedFilename = filename;
  const data = randomBytes(1024 * 1024);
  const sha256 = createHash("sha256").update(data).digest("hex");
  await page.goto("/mount");
  await ensureRpcReady(page);
  await page.getByText("JetKVM Storage Mount").click();
  await page.getByRole("button", { name: /^(next|continue)$/i }).click();
  await page.getByRole("button", { name: /^upload (a )?new image$/i }).click();
  await page
    .locator('input[type="file"]')
    .setInputFiles({ name: filename, mimeType: "application/octet-stream", buffer: data });
  await expect
    .poll(
      async () => {
        const result = (await callJsonRpc(page, "listStorageFiles")) as {
          files: { filename: string; size: number }[];
        };
        return result.files.find(file => file.filename === filename)?.size;
      },
      { timeout: 60_000, message: "uploaded image reaches its full size" },
    )
    .toBe(data.length);
  await page.goto("/");
  await ensureRpcReady(page);
  await callJsonRpc(page, "mountWithStorage", { filename, mode: "Disk" });
  const mounted = await callJsonRpc(page, "getVirtualMediaState");
  await expectMountedImageHash(page, data.length, sha256);
  for (let cycle = 0; cycle < 3; cycle++) {
    await callJsonRpc(page, "setUsbEmulationState", { enabled: false });
    await expect.poll(() => agent!.getJetKVMInputDevices(), { timeout: 15_000 }).toEqual([]);
    await callJsonRpc(page, "setUsbEmulationState", { enabled: true });
    expect((await waitForKeyboardReady(agent!, page, 30_000)).length).toBeGreaterThan(0);
    expect(await callJsonRpc(page, "getVirtualMediaState")).toEqual(mounted);
    await expectMountedImageHash(page, data.length, sha256);
  }
});

test("USB remains enumerated without a browser session and recovers input", async () => {
  // 30 s idle + 60 s RPC reconnect + 30 s keyboard readiness, plus overhead.
  test.setTimeout(150_000);
  const identity = (await callJsonRpc(page, "getUsbConfig")) as {
    vendor_id: string;
    product_id: string;
  };
  const usbID =
    `${identity.vendor_id.replace(/^0x/, "")}:${identity.product_id.replace(/^0x/, "")}`.toLowerCase();
  const device = async () =>
    (await agent!.getUSBDevices()).filter(d => d.id.toLowerCase() === usbID);
  const before = await device();
  expect(before).toHaveLength(1);
  expect(before[0].bus).toMatch(/^[1-9]\d*$/);
  expect(before[0].device).toMatch(/^[1-9]\d*$/);
  try {
    await page.goto("about:blank");
    // A changed bus/device number reveals an unwanted USB re-enumeration.
    for (let sample = 0; sample < 6; sample++) {
      await page.waitForTimeout(5000);
      expect(await device()).toEqual(before);
    }
  } finally {
    await ensureRpcReady(page, { navigateFirst: true });
  }
  expect((await waitForKeyboardReady(agent!, page, 30_000)).length).toBeGreaterThan(0);
});
