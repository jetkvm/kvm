import { test, expect } from "@playwright/test";
import { callJsonRpc, ensureNoPasswordViaAPI, ensureRpcReady, waitForWebRTCReady } from "./helpers";

test("the DHCP lease view does not install zero-delay intervals", async ({ page }) => {
  await ensureNoPasswordViaAPI();
  await page.addInitScript(() => {
    const original = window.setInterval;
    (window as unknown as { __intervalDelays: number[] }).__intervalDelays = [];
    (window as unknown as { __fastIntervals: number }).__fastIntervals = 0;
    window.setInterval = ((callback: TimerHandler, delay?: number, ...args: unknown[]) => {
      (window as unknown as { __intervalDelays: number[] }).__intervalDelays.push(delay ?? 0);
      if (!delay) (window as unknown as { __fastIntervals: number }).__fastIntervals++;
      return original(callback, delay, ...args);
    }) as typeof window.setInterval;
  });
  await page.goto("/settings/network", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  await page.goto("/settings/network", { waitUntil: "networkidle" });
  await waitForWebRTCReady(page);
  const state = (await callJsonRpc(page, "getNetworkState")) as {
    dhcp_lease?: { ip: string; lease_expiry?: string };
  };
  expect(state.dhcp_lease?.ip, "device should have an active DHCP lease").toBeTruthy();
  await expect(page.getByText("DHCP Lease Information", { exact: true })).toBeVisible();
  // Some DHCP clients omit expiry. Exercise the countdown using a network-state
  // notification with the real lease and an explicit future expiry.
  await page.evaluate(state => {
    window.__kvmTestHooks!._getRpcDataChannel!()!.dispatchEvent(
      new MessageEvent("message", {
        data: JSON.stringify({
          jsonrpc: "2.0",
          method: "networkState",
          params: {
            ...state,
            dhcp_lease: {
              ...state.dhcp_lease,
              lease_expiry: new Date(Date.now() + 3_600_000).toISOString(),
            },
          },
        }),
      }),
    );
  }, state);
  const leaseRow = page.getByText("Lease Expires", { exact: true }).locator("..");
  await expect(leaseRow).toBeVisible();
  await expect(leaseRow).not.toContainText("Invalid Date");
  await expect
    .poll(() =>
      page.evaluate(() => (window as unknown as { __intervalDelays: number[] }).__intervalDelays),
    )
    .toContain(30_000);
  await page.waitForTimeout(2_200);
  expect(
    await page.evaluate(() => (window as unknown as { __fastIntervals: number }).__fastIntervals),
  ).toBe(0);
});
