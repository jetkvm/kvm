import { createRemoteAgent } from "../remote-agent/remote-agent";
import { expect, type Page } from "@playwright/test";
import { callJsonRpc } from "./device";
import { captureHardwareState, configureTestUSB, restoreHardwareState } from "./hardware-state";

export async function expectHostImageHash(
  page: Page,
  filename: string,
  size: number,
  sha256: string,
): Promise<void> {
  const host = process.env.JETKVM_REMOTE_HOST;
  if (!host)
    throw new Error("Byte verification requires JETKVM_REMOTE_HOST when device SSH is unavailable");
  const original = await captureHardwareState(page);
  try {
    await configureTestUSB(page, original);
    await callJsonRpc(page, "mountWithStorage", { filename, mode: "Disk" });
    await expectMountedImageHash(page, size, sha256);
  } finally {
    await restoreHardwareState(page, original);
  }
}

/** Read the currently mounted disk without changing USB state. */
export async function expectMountedImageHash(
  page: Page,
  size: number,
  sha256: string,
): Promise<void> {
  const agent = createRemoteAgent();
  if (!agent) throw new Error("JETKVM_REMOTE_HOST is required for USB readback");
  await agent.ensureDeployed();
  const config = (await callJsonRpc(page, "getUsbConfig")) as import("./hardware-state").UsbConfig;
  const result = await agent.hashUsbMedium({
    vendor: config.vendor_id.replace(/^0x/i, "").toLowerCase(),
    product: config.product_id.replace(/^0x/i, "").toLowerCase(),
    serial: config.serial_number,
    size,
  });
  expect(result).toMatchObject({ bytes: size, sha256 });
}
