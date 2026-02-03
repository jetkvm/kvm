import { test, expect } from "@playwright/test";

import { ensureLocalAuthMode, logout, triggerRateLimit, rebootDeviceViaSSH } from "./helpers";

const TEST_PASSWORD = "TestPassword123";

test.describe("Login Rate Limiting", () => {
  test.setTimeout(180000); // 3 minutes

  test.afterAll(async () => {
    // Reboot clears in-memory rate limit state
    await rebootDeviceViaSSH(true);
  });

  test("rate limiting after multiple failed login attempts", async ({ page }) => {
    // Set up device with password
    await ensureLocalAuthMode(page, { mode: "password", password: TEST_PASSWORD });
    await logout(page);
    await page.goto("/login-local");
    await page.waitForLoadState("networkidle");

    // Attempt wrong password multiple times until rate limited
    const wasRateLimited = await triggerRateLimit(page);
    expect(wasRateLimited, "Rate limiting should trigger after failed attempts").toBe(true);
  });
});
