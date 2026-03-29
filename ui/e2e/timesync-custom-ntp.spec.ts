import { test, expect } from "@playwright/test";

import { callJsonRpc, waitForWebRTCReady } from "./helpers";
import type { Page } from "@playwright/test";

interface NetworkSettings {
  time_sync_mode?: string;
  time_sync_ntp_servers?: string[];
  time_sync_ordering?: string[];
  [key: string]: unknown;
}

/** Fetch timesync NTP metrics from the /metrics endpoint. */
async function getNtpMetrics(page: Page) {
  return page.evaluate(async () => {
    const response = await fetch("/metrics");
    if (!response.ok) throw new Error(`/metrics returned ${response.status}`);
    const text = await response.text();

    const requests: Record<string, number> = {};
    for (const match of text.matchAll(
      /jetkvm_timesync_ntp_request_total\{url="([^"]+)"\}\s+(\d+)/g,
    )) {
      requests[match[1]] = Number(match[2]);
    }

    const successes: Record<string, number> = {};
    for (const match of text.matchAll(
      /jetkvm_timesync_ntp_success_total\{url="([^"]+)"\}\s+(\d+)/g,
    )) {
      successes[match[1]] = Number(match[2]);
    }

    const statusMatch = text.match(/jetkvm_timesync_status\s+(\d+)/);
    const status = statusMatch ? Number(statusMatch[1]) : null;

    return { requests, successes, status };
  });
}

test.describe("Custom NTP time sync", () => {
  test.setTimeout(60000);

  let originalSettings: NetworkSettings;

  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ baseURL: process.env.JETKVM_URL });
    const page = await context.newPage();
    try {
      await page.goto("/");
      await waitForWebRTCReady(page);
      originalSettings = (await callJsonRpc(page, "getNetworkSettings")) as NetworkSettings;
    } finally {
      await page.close();
      await context.close();
    }
  });

  test.afterAll(async ({ browser }) => {
    const context = await browser.newContext({ baseURL: process.env.JETKVM_URL });
    const page = await context.newPage();
    try {
      await page.goto("/");
      await waitForWebRTCReady(page);
      await callJsonRpc(page, "setNetworkSettings", {
        settings: originalSettings,
      });
    } finally {
      await page.close();
      await context.close();
    }
  });

  test("custom NTP server is queried after settings change", async ({ page }) => {
    await page.goto("/");
    await waitForWebRTCReady(page);

    // Set custom NTP mode with pool.ntp.org
    await callJsonRpc(page, "setNetworkSettings", {
      settings: {
        ...originalSettings,
        time_sync_mode: "custom",
        time_sync_ntp_servers: ["pool.ntp.org"],
      },
    });

    // Wait for the sync to complete (triggered immediately on settings change)
    await page.waitForTimeout(5000);

    const metrics = await getNtpMetrics(page);

    // pool.ntp.org should have been queried
    expect(metrics.requests["pool.ntp.org"]).toBeGreaterThanOrEqual(1);
    expect(metrics.successes["pool.ntp.org"]).toBeGreaterThanOrEqual(1);
    expect(metrics.status).toBe(1);
  });

  test("invalid NTP server falls back to defaults", async ({ page }) => {
    await page.goto("/");
    await waitForWebRTCReady(page);

    // Set a bogus NTP server
    await callJsonRpc(page, "setNetworkSettings", {
      settings: {
        ...originalSettings,
        time_sync_mode: "custom",
        time_sync_ntp_servers: ["ntp.invalid.example"],
      },
    });

    // Wait for sync attempt + fallback
    await page.waitForTimeout(10000);

    const metrics = await getNtpMetrics(page);

    // The bogus server should have been attempted
    expect(metrics.requests["ntp.invalid.example"]).toBeGreaterThanOrEqual(1);

    // It should not have succeeded
    expect(metrics.successes["ntp.invalid.example"]).toBeUndefined();

    // But overall sync should still succeed via fallback
    expect(metrics.status).toBe(1);
  });
});
