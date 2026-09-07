import { test, expect } from "@playwright/test";

import { ensureLocalAuthMode, logout, triggerRateLimit } from "./helpers";

const TEST_PASSWORD = "TestPassword123";

// Runs in its own project after "ui" (see playwright.config.ts): the
// in-memory rate-limit state it leaves behind must not affect other files,
// and a reboot to clear it would cost more than the ordering.
test.describe("Login Rate Limiting", () => {
  test.setTimeout(180000);

  test("rate limiting after multiple failed login attempts", async ({ page }) => {
    await ensureLocalAuthMode(page, { mode: "password", password: TEST_PASSWORD });
    await logout(page);
    await page.goto("/login-local");

    const wasRateLimited = await triggerRateLimit(page);
    expect(wasRateLimited, "Rate limiting should trigger after failed attempts").toBe(true);
  });
});
