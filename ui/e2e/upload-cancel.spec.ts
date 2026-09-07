import { createHash, randomBytes } from "crypto";
import { mkdtempSync, writeFileSync } from "fs";
import { tmpdir } from "os";
import { join } from "path";
import { test, expect, type Page } from "@playwright/test";
import {
  callJsonRpc,
  ensureNoPasswordViaAPI,
  ensureRpcReady,
  deviceShellAvailable,
  sshExec,
} from "./helpers";

// Cancelling an upload has to stop the request that is streaming the file,
// not just reset the view. The device keeps the partial file for a resume,
// so the retry has to produce a byte-identical image.

const FILE_NAME = "e2e-upload-cancel.img";
const FILE_SIZE = 16 * 1024 * 1024;
const THROTTLED_UPLOAD_BYTES_PER_SEC = 2 * 1024 * 1024;

interface StorageFile {
  filename: string;
  size: number;
}

async function listImages(page: Page): Promise<StorageFile[]> {
  const result = (await callJsonRpc(page, "listStorageFiles")) as { files: StorageFile[] };
  return result.files;
}

// The partial file is listed under its .incomplete name until the upload
// completes. A failed call throws, so an RPC hiccup cannot pass as a stopped
// upload.
async function remoteSize(page: Page, name: string): Promise<number> {
  const files = await listImages(page);
  const file =
    files.find(f => f.filename === `${name}.incomplete`) ?? files.find(f => f.filename === name);
  return file?.size ?? 0;
}

async function deleteImage(page: Page, name: string): Promise<void> {
  for (const filename of [name, `${name}.incomplete`]) {
    try {
      await callJsonRpc(page, "deleteStorageFile", { filename });
    } catch {
      // not present
    }
  }
}

// Byte-for-byte verification needs a shell on the device; without one the
// check is the final size, which still catches a truncated or doubled resume.
async function expectImageMatches(page: Page, name: string, sha256: string, size: number) {
  if (await deviceShellAvailable()) {
    const remote = (
      await sshExec(`sha256sum /userdata/jetkvm/images/${name} | cut -d" " -f1`)
    ).trim();
    expect(remote, "image must be byte-identical").toBe(sha256);
    return;
  }
  const files = await listImages(page);
  expect(files.find(f => f.filename === name)?.size, "image must have the full size").toBe(size);
}

async function openRpcPage(page: Page): Promise<void> {
  await page.goto("/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
}

async function openUploadView(page: Page): Promise<void> {
  await page.goto("/mount", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  await page.getByText("JetKVM Storage Mount").click();
  await page.getByRole("button", { name: /^(next|continue)$/i }).click();
  // "Upload New Image" with an empty store, "Upload a new image" otherwise.
  await page.getByRole("button", { name: /^upload (a )?new image$/i }).click();
}

test.describe("Upload cancel and resume", () => {
  let localPath = "";
  let sha256 = "";

  test.beforeAll(async ({ browser }) => {
    await ensureNoPasswordViaAPI();
    const data = randomBytes(FILE_SIZE);
    localPath = join(mkdtempSync(join(tmpdir(), "jetkvm-e2e-")), FILE_NAME);
    writeFileSync(localPath, data);
    sha256 = createHash("sha256").update(data).digest("hex");
    const page = await browser.newPage();
    await openRpcPage(page);
    await deleteImage(page, FILE_NAME);
    await page.close();
  });

  test.afterAll(async ({ browser }) => {
    const page = await browser.newPage();
    await openRpcPage(page);
    await deleteImage(page, FILE_NAME);
    await page.close();
  });

  test("cancelling an upload stops the transfer, and a retry resumes it", async ({ page }) => {
    test.setTimeout(90_000);

    // Throttle the upload so Cancel lands while the request is streaming.
    const cdp = await page.context().newCDPSession(page);
    const throttle = (uploadThroughput: number) =>
      cdp.send("Network.emulateNetworkConditions", {
        offline: false,
        latency: 0,
        downloadThroughput: -1,
        uploadThroughput,
      });
    await throttle(THROTTLED_UPLOAD_BYTES_PER_SEC);

    await openUploadView(page);
    await page.locator('input[type="file"]').setInputFiles(localPath);
    const size = () => remoteSize(page, FILE_NAME);
    await expect.poll(size, { timeout: 20_000 }).toBeGreaterThan(FILE_SIZE / 8);

    await page.getByRole("button", { name: "Cancel Upload" }).click();

    // Before the fix the request kept streaming after Cancel.
    await page.waitForTimeout(1_000);
    const afterCancel = await size();
    await page.waitForTimeout(1_500);
    expect(afterCancel, "partial file must exist after Cancel").toBeGreaterThan(0);
    expect(await size(), "upload kept streaming after Cancel").toBe(afterCancel);
    expect(afterCancel, "cancelled upload must not complete").toBeLessThan(FILE_SIZE);

    await throttle(-1);
    await openUploadView(page);
    await page.locator('input[type="file"]').setInputFiles(localPath);
    await expect(page.getByText("Upload successful")).toBeVisible({ timeout: 40_000 });

    await expectImageMatches(page, FILE_NAME, sha256, FILE_SIZE);
  });
});

// A cancelled data channel closes gracefully and can still deliver buffered
// chunks after the user has retried. The device ends the old transfer when a
// new one starts for the same file, so only one writer is ever appending.
test("a second start for the same file supersedes the first", async ({ page }) => {
  test.setTimeout(60_000);

  const name = "e2e-upload-supersede.img";
  const data = randomBytes(1024 * 1024);
  const cleanup = () => deleteImage(page, name);

  await ensureNoPasswordViaAPI();
  await openRpcPage(page);
  await cleanup();
  try {
    const start = async () =>
      (await callJsonRpc(page, "startStorageFileUpload", {
        filename: name,
        size: data.length,
      })) as { dataChannel: string };
    const first = await start();
    const second = await start();

    // Data for the first upload is rejected rather than appended beside the
    // second transfer's bytes.
    const stale = await page.request.post(
      `/storage/upload?uploadId=${encodeURIComponent(first.dataChannel)}`,
      { data: data.subarray(0, 4096) },
    );
    expect(stale.status(), "superseded upload must be rejected").toBe(404);

    const ok = await page.request.post(
      `/storage/upload?uploadId=${encodeURIComponent(second.dataChannel)}`,
      { data },
    );
    expect(ok.ok(), "second upload must complete").toBe(true);

    await expectImageMatches(
      page,
      name,
      createHash("sha256").update(data).digest("hex"),
      data.length,
    );
  } finally {
    await cleanup();
  }
});
