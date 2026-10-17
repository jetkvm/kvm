import { test, expect } from "@playwright/test";

import {
  ensureNoPasswordViaAPI,
  ensureRpcReady,
  getDeviceHost,
  rawJsonRpc,
  waitForDeviceDown,
  waitForDeviceReady,
} from "./helpers";

// After willReboot the page shows the rebooting overlay, which polls
// /device/status and redirects once the device answers. The signaling socket
// stays open through a short reboot (its heartbeat runs every 25 s) and only
// reopens when a heartbeat reaches the restarted device. When that happened
// before the overlay's next poll, the socket's onOpen cleared the reboot
// state, the overlay went away with its redirect, and the page stayed where
// it was instead of going to the page the reboot was for (after an update,
// the update page).
//
// This test holds the overlay's health check until the socket has reopened,
// so the reopen always comes first, and then expects the overlay's redirect.

test("the rebooting overlay redirects when the signaling socket reopens first", async ({
  page,
}) => {
  test.setTimeout(240_000);
  await ensureNoPasswordViaAPI();

  let signalingSockets = 0;
  let reopened = false;
  page.on("websocket", socket => {
    if (!socket.url().includes("/webrtc/signaling")) return;
    signalingSockets += 1;
    if (signalingSockets < 2) return;
    socket.on("framereceived", frame => {
      if (String(frame.payload).includes("device-metadata")) reopened = true;
    });
  });

  await page.goto("/");
  await ensureRpcReady(page);
  expect(signalingSockets, "one signaling socket before the reboot").toBe(1);

  // page.route covers the page's own fetches (the overlay's health check);
  // page.request, which the helpers use to watch the device, is not routed.
  // The hold starts once the device is down: the page itself reads
  // /device/status while the device still answers, and a failed read there
  // ends the session, which cancels the reboot.
  let holdHealthCheck = false;
  await page.route("**/device/status", route =>
    holdHealthCheck ? route.abort() : route.continue(),
  );
  // The overlay's redirect loads a new document, which drops this marker.
  // (framenavigated would also count the UI's history API route changes.)
  type Marked = { __beforeReboot?: boolean };
  await page.evaluate(() => {
    (window as unknown as Marked).__beforeReboot = true;
  });
  // While the page loads its context is replaced; that reads as "not yet".
  const reloaded = () =>
    page.evaluate(() => !(window as unknown as Marked).__beforeReboot).catch(() => false);

  await Promise.all([
    rawJsonRpc(page, "reboot", { force: true }, 5000).catch(() => {
      // The reply can be lost in the reset; the device going down proves it.
    }),
    waitForDeviceDown(page, "device must go down after the reboot request", 30_000),
  ]);
  holdHealthCheck = true;
  await expect(page.getByText("Device is Rebooting")).toBeVisible();
  await waitForDeviceReady(getDeviceHost(), 90_000);

  await expect
    .poll(() => reopened, {
      message: "signaling socket reopened after the reboot",
      timeout: 60_000,
    })
    .toBe(true);
  expect(await reloaded(), "no page load before the health check answers").toBe(false);

  holdHealthCheck = false;
  await expect
    .poll(reloaded, {
      message: "the rebooting overlay redirects once /device/status answers",
      timeout: 20_000,
    })
    .toBe(true);
  await ensureRpcReady(page, { timeoutMs: 60_000 });
});
