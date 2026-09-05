import { createHash, randomBytes } from "crypto";
import { mkdtempSync, writeFileSync } from "fs";
import { tmpdir } from "os";
import { join } from "path";
import { test, expect, type Page } from "@playwright/test";
import { ensureNoPasswordViaAPI, ensureRpcReady, sshExec } from "./helpers";

// Cancelling an upload has to stop the request that is streaming the file,
// not just reset the view. The device keeps the partial file for a resume,
// so the retry has to produce a byte-identical image.

const FILE_NAME = "e2e-upload-cancel.img";
const FILE_SIZE = 16 * 1024 * 1024;
const REMOTE = `/userdata/jetkvm/images/${FILE_NAME}`;
const THROTTLED_UPLOAD_BYTES_PER_SEC = 2 * 1024 * 1024;

async function remoteSize(): Promise<number> {
  // The device has no stat binary.
  const out = await sshExec(
    `f=${REMOTE}.incomplete; [ -f "$f" ] || f=${REMOTE}; [ -f "$f" ] && wc -c < "$f" || echo 0`,
    true,
  );
  return parseInt(out.trim(), 10) || 0;
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

  test.beforeAll(async () => {
    await ensureNoPasswordViaAPI();
    const data = randomBytes(FILE_SIZE);
    localPath = join(mkdtempSync(join(tmpdir(), "jetkvm-e2e-")), FILE_NAME);
    writeFileSync(localPath, data);
    sha256 = createHash("sha256").update(data).digest("hex");
    await sshExec(`rm -f ${REMOTE} ${REMOTE}.incomplete`, true);
  });

  test.afterAll(async () => {
    await sshExec(`rm -f ${REMOTE} ${REMOTE}.incomplete`, true);
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
    await expect.poll(remoteSize, { timeout: 20_000 }).toBeGreaterThan(FILE_SIZE / 8);

    await page.getByRole("button", { name: "Cancel Upload" }).click();

    // Before the fix the request kept streaming after Cancel.
    await page.waitForTimeout(1_000);
    const afterCancel = await remoteSize();
    await page.waitForTimeout(1_500);
    expect(await remoteSize(), "upload kept streaming after Cancel").toBe(afterCancel);
    expect(afterCancel, "cancelled upload must not complete").toBeLessThan(FILE_SIZE);

    await throttle(-1);
    await openUploadView(page);
    await page.locator('input[type="file"]').setInputFiles(localPath);
    await expect(page.getByText("Upload successful")).toBeVisible({ timeout: 40_000 });

    const remote = (await sshExec(`sha256sum ${REMOTE} | cut -d" " -f1`, true)).trim();
    expect(remote, "resumed upload must be byte-identical").toBe(sha256);
  });
});
