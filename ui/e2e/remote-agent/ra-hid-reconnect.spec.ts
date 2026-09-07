import { test, expect } from "@playwright/test";
import { ensureNoPasswordViaAPI, ensureRpcReady, sendAbsMouseMove } from "../helpers";
import { createRemoteAgent } from "./remote-agent";

const agent = createRemoteAgent();

test("mouse buttons reach the host after a session takeover and reconnect", async ({
  page,
  browser,
}) => {
  test.setTimeout(90_000);
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");
  await agent!.ensureDeployed();
  await ensureNoPasswordViaAPI();
  await page.addInitScript(() => {
    const original = RTCDataChannel.prototype.send;
    (window as unknown as { __pointerChannels: string[] }).__pointerChannels = [];
    RTCDataChannel.prototype.send = function (data: string | ArrayBuffer | ArrayBufferView | Blob) {
      const bytes = ArrayBuffer.isView(data)
        ? new Uint8Array(data.buffer, data.byteOffset, data.byteLength)
        : data instanceof ArrayBuffer
          ? new Uint8Array(data)
          : null;
      if (bytes?.[0] === 3)
        (window as unknown as { __pointerChannels: string[] }).__pointerChannels.push(this.label);
      return original.call(this, data as never);
    };
  });
  await page.goto("/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  await expect
    .poll(
      async () => (await agent!.getJetKVMInputDevices()).some(d => d.type === "absolute_mouse"),
      { timeout: 30_000 },
    )
    .toBe(true);
  await agent!.clearMouseEvents();
  await sendAbsMouseMove(page, 16000, 16000, 1);
  await expect
    .poll(async () =>
      (await agent!.getMouseEvents()).some(
        e => e.type === "mouse_button" && e.code === 272 && e.value === 1,
      ),
    )
    .toBe(true);

  const replacement = await browser.newPage();
  try {
    await replacement.goto("/", { waitUntil: "networkidle" });
    await ensureRpcReady(replacement);
    // Release the first session's held button before observing a new press.
    await sendAbsMouseMove(replacement, 16000, 16000, 0);
    await expect
      .poll(async () =>
        (await agent!.getMouseEvents()).some(
          e => e.type === "mouse_button" && e.code === 272 && e.value === 0,
        ),
      )
      .toBe(true);
    await expect(page.getByRole("button", { name: "Use Here" })).toBeVisible();
    await page.getByRole("button", { name: "Use Here" }).click();
    await ensureRpcReady(page);
    await agent!.clearMouseEvents();
    await page.evaluate(() => {
      (window as unknown as { __pointerChannels: string[] }).__pointerChannels = [];
    });
    await sendAbsMouseMove(page, 17000, 17000, 1);
    await expect
      .poll(async () =>
        (await agent!.getMouseEvents()).some(
          e => e.type === "mouse_button" && e.code === 272 && e.value === 1,
        ),
      )
      .toBe(true);
    await sendAbsMouseMove(page, 17000, 17000, 0);
    await expect
      .poll(async () =>
        (await agent!.getMouseEvents())
          .filter(e => e.type === "mouse_button" && e.code === 272)
          .map(e => e.value),
      )
      .toEqual([1, 0]);
    expect(
      await page.evaluate(
        () => (window as unknown as { __pointerChannels: string[] }).__pointerChannels,
      ),
    ).toEqual(["hidrpc", "hidrpc"]);
  } finally {
    await sendAbsMouseMove(page, 17000, 17000, 0);
    await replacement.close();
  }
});
