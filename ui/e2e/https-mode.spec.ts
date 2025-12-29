import { test, expect } from "@playwright/test";

import { waitForWebRTCReady, verifyHidAndVideo } from "./helpers";

// Time to wait for TLS settings to apply (ms)
const TLS_APPLY_DELAY = 2000;

test.describe("HTTPS Mode Tests", () => {
  // This test modifies TLS settings, so use a longer timeout
  test.setTimeout(180000); // 3 minutes

  // Restore TLS mode to disabled after tests complete
  test.afterAll(async ({ browser }) => {
    const page = await browser.newPage();
    try {
      // Try HTTP first (the original URL)
      const baseUrl = process.env.JETKVM_URL || "http://localhost";
      await page.goto(`${baseUrl}/settings/access`);
      await page.waitForLoadState("networkidle");

      // Find the TLS mode dropdown
      const tlsDropdown = page.locator("select").filter({
        has: page.locator('option[value="self-signed"]'),
      });

      if (await tlsDropdown.isVisible({ timeout: 5000 })) {
        await tlsDropdown.selectOption("disabled");
        await page.waitForTimeout(TLS_APPLY_DELAY);
      }
    } catch {
      // Cleanup failed - device may need manual intervention
    } finally {
      await page.close();
    }
  });

  test("enable self-signed HTTPS and verify HID/video works", async ({ page, browser }) => {
    // Navigate to access settings
    await page.goto("/settings/access");
    await page.waitForLoadState("networkidle");

    // Find the TLS mode dropdown (has options: disabled, self-signed, custom)
    const tlsDropdown = page.locator("select").filter({
      has: page.locator('option[value="self-signed"]'),
    });
    await expect(tlsDropdown).toBeVisible({ timeout: 10000 });
    await expect(tlsDropdown).toBeEnabled({ timeout: 10000 });

    // Change to self-signed mode
    await tlsDropdown.selectOption("self-signed");
    await page.waitForTimeout(TLS_APPLY_DELAY);

    // Extract device hostname/IP from current URL
    const currentUrl = new URL(page.url());
    const deviceHost = currentUrl.hostname;
    const httpsUrl = `https://${deviceHost}:443`;

    // Create new browser context with ignoreHTTPSErrors for self-signed certificates
    const httpsContext = await browser.newContext({
      ignoreHTTPSErrors: true,
    });
    const httpsPage = await httpsContext.newPage();

    try {
      // Navigate to HTTPS version
      await httpsPage.goto(httpsUrl, { timeout: 30000 });
      await httpsPage.waitForLoadState("networkidle");

      // Wait for WebRTC connection
      await waitForWebRTCReady(httpsPage, 45000);

      // Verify video, mouse, and keyboard all work
      await verifyHidAndVideo(httpsPage);
    } finally {
      // Clean up the HTTPS context
      await httpsPage.close();
      await httpsContext.close();
    }

    // Restore TLS mode to disabled using the original HTTP page
    await page.goto("/settings/access");
    await page.waitForLoadState("networkidle");

    const tlsDropdownRestore = page.locator("select").filter({
      has: page.locator('option[value="self-signed"]'),
    });
    await expect(tlsDropdownRestore).toBeVisible({ timeout: 10000 });
    await tlsDropdownRestore.selectOption("disabled");
    await page.waitForTimeout(TLS_APPLY_DELAY);
  });
});
