import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
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
  const host = process.env.JETKVM_REMOTE_HOST;
  if (!host) throw new Error("JETKVM_REMOTE_HOST is required for USB readback");
  const config = (await callJsonRpc(page, "getUsbConfig")) as import("./hardware-state").UsbConfig;
  const identity = Buffer.from(
    JSON.stringify({
      vendor: config.vendor_id.replace(/^0x/i, "").toLowerCase(),
      product: config.product_id.replace(/^0x/i, "").toLowerCase(),
      serial: config.serial_number,
      size,
    }),
  ).toString("base64");
  const script = readFileSync(
    new URL("../../../e2e/remote-agent/storage_readback.py", import.meta.url),
    "utf8",
  );
  // The only interpolated remote argument is base64, never shell source.
  const output = execFileSync(
    "ssh",
    [
      "-o",
      "BatchMode=yes",
      "-o",
      "ConnectTimeout=10",
      host,
      `timeout 45s sudo -n python3 - ${identity}`,
    ],
    { input: script, encoding: "utf8", timeout: 55_000 },
  );
  expect(JSON.parse(output)).toMatchObject({ bytes: size, sha256 });
}
