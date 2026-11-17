import { test, expect } from "@playwright/test";
import { ensureNoPasswordViaAPI, ensureRpcReady } from "./helpers";

for (const status of [401, 503]) {
  test(`signaling disconnect with HTTP ${status} ${status === 401 ? "returns to login" : "keeps retrying"}`, async ({
    page,
  }) => {
    await ensureNoPasswordViaAPI();
    await page.addInitScript(() => {
      const sockets: WebSocket[] = [];
      const NativeWebSocket = window.WebSocket;
      window.WebSocket = class extends NativeWebSocket {
        constructor(url: string | URL, protocols?: string | string[]) {
          super(url, protocols);
          sockets.push(this);
        }
      };
      Object.assign(window, { closeTestSockets: () => sockets.forEach(socket => socket.close()) });
    });
    await ensureRpcReady(page, { navigateFirst: true });
    const oldPeer = await page.evaluateHandle(() => window.__kvmTestHooks?._getPeerConnection?.());
    let probes = 0;
    await page.route("**/device", async route => {
      probes++;
      await route.fulfill({ status, json: { error: "test response" } });
    });
    await page.evaluate(() =>
      (window as unknown as { closeTestSockets(): void }).closeTestSockets(),
    );
    if (status === 401) {
      await expect(page).toHaveURL(/\/login-local$/, { timeout: 15000 });
      await expect(page.locator('input[name="password"]')).toBeVisible();
    } else {
      await expect.poll(() => probes).toBeGreaterThan(0);
      // Wait for signaling to replace the closed peer before the RPC helper
      // probes it; probing the old channel can trigger a test-driven reload.
      await page.waitForFunction(
        previous => {
          const peer = window.__kvmTestHooks?._getPeerConnection?.();
          return peer !== previous && peer?.connectionState === "connected";
        },
        oldPeer,
        { timeout: 20000 },
      );
      await ensureRpcReady(page);
      await expect(page).toHaveURL(/\/$/);
    }
    await oldPeer.dispose();
  });
}
