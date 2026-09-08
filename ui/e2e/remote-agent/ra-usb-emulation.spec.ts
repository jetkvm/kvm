import { createHash, randomBytes } from "node:crypto";
import { expectMountedImageHash } from "../helpers/storage-readback";
import { test, expect, type Page } from "@playwright/test";
import { callJsonRpc, ensureRpcReady } from "../helpers";
import { agent, registerSharedSession } from "./shared";
import { waitForKeyboardReady } from "./remote-agent";

let page: Page;
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
    await callJsonRpc(page, "setUsbEmulationState", { enabled: true });
    expect(await callJsonRpc(page, "getUsbEmulationState")).toBe(true);
    expect((await waitForKeyboardReady(agent!, page, 30_000)).length).toBeGreaterThan(0);
  } finally {
    await callJsonRpc(page, "setUsbEmulationState", { enabled: true });
  }
});

test("USB reconnect preserves mounted media bytes and keyboard input", async () => {
  test.setTimeout(120_000);
  const filename = `e2e-usb-reconnect-${Date.now()}.img`;
  const data = randomBytes(1024 * 1024);
  const sha256 = createHash("sha256").update(data).digest("hex");
  try {
    await page.goto("/mount");
    await ensureRpcReady(page);
    await page.getByText("JetKVM Storage Mount").click();
    await page.getByRole("button", { name: /^(next|continue)$/i }).click();
    await page.getByRole("button", { name: /^upload (a )?new image$/i }).click();
    await page
      .locator('input[type="file"]')
      .setInputFiles({ name: filename, mimeType: "application/octet-stream", buffer: data });
    await expect
      .poll(async () => {
        const result = (await callJsonRpc(page, "listStorageFiles")) as {
          files: { filename: string; size: number }[];
        };
        return result.files.find(file => file.filename === filename)?.size;
      })
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
  } finally {
    await callJsonRpc(page, "setUsbEmulationState", { enabled: true });
    try {
      await callJsonRpc(page, "unmountImage");
    } finally {
      await callJsonRpc(page, "deleteStorageFile", { filename });
    }
  }
});

test("USB remains enumerated without a browser session and recovers input", async () => {
  test.setTimeout(90_000);
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
