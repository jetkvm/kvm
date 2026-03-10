import { test, expect } from "@playwright/test";

import { ensureLocalAuthMode, logout, triggerRateLimit } from "./helpers";

const TEST_PASSWORD = "TestPassword123";

// This file is prefixed with "zz-" so it runs last in alphabetical order.
// Running last means we skip the afterAll reboot that would otherwise be
// needed to clear in-memory rate-limit state for subsequent test files.
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
