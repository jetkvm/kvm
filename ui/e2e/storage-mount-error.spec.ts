import { test, expect } from "@playwright/test";
import { callJsonRpc, ensureNoPasswordViaAPI, ensureRpcReady } from "./helpers";

// A failed storage mount must leave its error on screen. The image is deleted
// after the dialog lists it, so the device refuses the mount.
test("a failed storage mount shows the mount error", async ({ page }) => {
  const filename = `e2e-mount-error-${Date.now()}.img`;
  await ensureNoPasswordViaAPI();
  await ensureRpcReady(page, { navigateFirst: true });
  await callJsonRpc(page, "unmountImage").catch(() => undefined);
  const start = (await callJsonRpc(page, "startStorageFileUpload", {
    filename,
    size: 512,
  })) as { dataChannel: string };
  const upload = await page.request.post(
    `/storage/upload?uploadId=${encodeURIComponent(start.dataChannel)}`,
    { data: Buffer.alloc(512) },
  );
  expect(upload.status()).toBe(200);

  try {
    await page.getByRole("button", { name: "Virtual Media" }).click();
    await page.getByRole("button", { name: "Add New Media" }).click();
    await page.getByText("JetKVM Storage Mount").click();
    await page.getByRole("button", { name: "Continue" }).click();
    const file = page.getByText(filename);
    const next = page.getByRole("button", { name: "Next" });
    await expect(file.or(next).first()).toBeVisible();
    while (!(await file.isVisible())) await next.click();
    await file.click();
    await callJsonRpc(page, "deleteStorageFile", { filename });
    await page.getByRole("button", { name: "Mount File" }).click();

    // The broken dialog showed the error only until the state sync that
    // follows the reply, then closed; the error must outlast that sync.
    const error = page.getByRole("heading", { name: "Mount Error" });
    await expect(error).toBeVisible();
    await page.waitForTimeout(1_000);
    await expect(error).toBeVisible();
  } finally {
    await callJsonRpc(page, "deleteStorageFile", { filename }).catch(() => undefined);
  }
});
