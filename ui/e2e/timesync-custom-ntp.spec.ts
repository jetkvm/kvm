import { test, expect } from "@playwright/test";

import { callJsonRpc, rpcAvailable, waitForWebRTCReady } from "./helpers";
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
  test.setTimeout(120_000);

  let originalSettings: NetworkSettings;

  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ baseURL: process.env.JETKVM_URL });
    const page = await context.newPage();
    try {
      await page.goto("/");
      await waitForWebRTCReady(page);
      test.skip(
        !(await rpcAvailable(page, "getNetworkSettings")),
        "device has no network settings (getNetworkSettings)",
      );
      originalSettings = (await callJsonRpc(page, "getNetworkSettings")) as NetworkSettings;
    } finally {
      await page.close();
      await context.close();
    }
  });

  test.afterAll(async ({ browser }) => {
    if (!originalSettings) return;
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

  /**
   * Apply NTP settings, then poll the request metrics until a sync with the
   * new server list has run. The sync triggered by setNetworkSettings is
   * silently skipped when another sync holds the lock (timesync.Sync uses
   * TryLock), so re-trigger the settings change each round until one lands.
   */
  async function setNtpServerAndAwaitRequest(page: Page, server: string): Promise<number> {
    const baseline = (await getNtpMetrics(page)).requests[server] ?? 0;
    const settings = {
      ...originalSettings,
      time_sync_mode: "custom",
      time_sync_ntp_servers: [server],
    };
    await expect
      .poll(
        async () => {
          await callJsonRpc(page, "setNetworkSettings", { settings });
          return (await getNtpMetrics(page)).requests[server] ?? 0;
        },
        {
          // Each retrigger reapplies network settings (flash write + interface
          // reconfig on the device) — keep the cadence gentle.
          message: `${server} should be queried after settings change`,
          timeout: 30_000,
          intervals: [5000],
        },
      )
      .toBeGreaterThanOrEqual(baseline + 1);
    return baseline;
  }

  test("custom NTP server is queried after settings change @network", async ({ page }) => {
    await page.goto("/");
    await waitForWebRTCReady(page);

    const successBaseline = (await getNtpMetrics(page)).successes["pool.ntp.org"] ?? 0;
    await setNtpServerAndAwaitRequest(page, "pool.ntp.org");

    // The query should succeed (poll: success is recorded after the response)
    await expect
      .poll(async () => (await getNtpMetrics(page)).successes["pool.ntp.org"] ?? 0, {
        message: "pool.ntp.org query should succeed",
        timeout: 15_000,
      })
      .toBeGreaterThanOrEqual(successBaseline + 1);
    expect((await getNtpMetrics(page)).status).toBe(1);
  });

  test("invalid NTP server falls back to defaults @network", async ({ page }) => {
    await page.goto("/");
    await waitForWebRTCReady(page);

    await setNtpServerAndAwaitRequest(page, "ntp.invalid.example");

    const metrics = await getNtpMetrics(page);

    // It should not have succeeded
    expect(metrics.successes["ntp.invalid.example"]).toBeUndefined();

    // But overall sync should still succeed via fallback
    expect(metrics.status).toBe(1);
  });
});
