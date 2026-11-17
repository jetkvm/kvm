import { test, expect, type Page } from "@playwright/test";
import { callJsonRpc, waitForWebRTCReady, getDeviceHost, skipWithoutRpc } from "../helpers";
import { registerSharedSession } from "./shared";

test.describe.configure({ mode: "serial" });

let sharedPage: Page;
registerSharedSession(page => (sharedPage = page));

test.describe("Remote Host Agent: TLS", () => {
  // ═══════════════════════════════════════════
  // HTTPS VIA RPC
  // ═══════════════════════════════════════════

  test("https: TLS round-trip via RPC @tls", async ({ browser }) => {
    test.setTimeout(60_000);
    await skipWithoutRpc(sharedPage, "getTLSState", "TLS");

    const host = getDeviceHost();
    const httpsUrl = `https://${host}:443`;

    // Enable self-signed TLS via RPC (no UI navigation needed)
    await callJsonRpc(sharedPage, "setTLSState", {
      state: { mode: "self-signed", certificate: "", privateKey: "" },
    });

    // Poll until HTTPS listener is ready (setTLSState returns before the listener starts)
    const httpsContext = await browser.newContext({ ignoreHTTPSErrors: true });
    const probePage = await httpsContext.newPage();
    const probeDeadline = Date.now() + 10000;
    while (Date.now() < probeDeadline) {
      try {
        await probePage.goto(httpsUrl, { timeout: 3000 });
        break;
      } catch {
        await new Promise(r => setTimeout(r, 250));
      }
    }

    // Verify HTTPS works: WebRTC connects over TLS
    try {
      await waitForWebRTCReady(probePage, 30000);
    } finally {
      await probePage.close();
      await httpsContext.close();
    }

    // Restore TLS to disabled via RPC.
    // sharedPage was never navigated during this test, so its WebRTC connection is still alive.
    try {
      await callJsonRpc(sharedPage, "setTLSState", {
        state: { mode: "", certificate: "", privateKey: "" },
      });
    } catch {
      // WebRTC dropped; restore via UI and re-establish
      await sharedPage.goto("/settings/access");
      await sharedPage.waitForLoadState("networkidle");
      const tlsDropdown = sharedPage.locator("select").filter({
        has: sharedPage.locator('option[value="self-signed"]'),
      });
      await expect(tlsDropdown).toBeVisible({ timeout: 5000 });
      await tlsDropdown.selectOption("disabled");
      await sharedPage.waitForTimeout(500);
      await sharedPage.goto("/", { waitUntil: "networkidle" });
      await waitForWebRTCReady(sharedPage);
    }
  });
});
