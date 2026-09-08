import { test, expect, type BrowserContext } from "@playwright/test";
import {
  callJsonRpc,
  ensureNoPasswordViaAPI,
  ensureLocalAuthMode,
  ensureRpcReady,
  reconnectAfterReboot,
} from "./helpers";

const TEST_PASSWORD = "TestPassword123";
let passwordEnabled = false;
let loginContext: BrowserContext | undefined;

test("rate limiting after multiple failed login attempts", async ({ page, browser }) => {
  test.setTimeout(180_000);
  await ensureNoPasswordViaAPI();
  passwordEnabled = true;
  await ensureLocalAuthMode(page, { mode: "password", password: TEST_PASSWORD });

  // Keep this authenticated session for cleanup. Failed logins happen in an
  // independent cookie jar and must not require logging out the cleanup owner.
  loginContext = await browser.newContext({ baseURL: process.env.JETKVM_URL });
  const login = await loginContext.newPage();
  login.setDefaultTimeout(10_000);
  await login.goto("/login-local");
  let limited = false;
  for (let attempt = 0; attempt < 10; attempt++) {
    await login.locator('input[name="password"]').fill("wrongpassword123");
    const reply = login.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === "/auth/login-local" &&
        response.request().method() === "POST",
    );
    await login.getByRole("button", { name: /^Log in$/i }).click();
    const response = await reply;
    if (response.status() === 429) {
      limited = true;
      break;
    }
    expect(response.status()).toBe(401);
  }
  expect(limited, "failed logins must produce HTTP 429").toBe(true);
  await expect(login.locator(".text-red-500, .text-red-600").first()).toContainText(
    /too many|try again/i,
  );
});

test.afterEach(async ({ page, request }) => {
  test.setTimeout(150_000);
  try {
    await loginContext?.close();
  } finally {
    loginContext = undefined;
    if (passwordEnabled && !(await request.get("/device")).ok()) {
      const origin = new URL(process.env.JETKVM_URL!).origin;
      const response = await page.request.delete("/auth/local-password", {
        headers: { Origin: origin },
        data: { password: TEST_PASSWORD },
      });
      expect(response.ok(), "restore no-password auth using the retained session").toBe(true);
      await ensureNoPasswordViaAPI();
      await ensureRpcReady(page, { navigateFirst: true });
      // Lockouts can last minutes. Reboot clears the in-memory limiter without
      // changing its policy or relying on device shell access.
      await callJsonRpc(page, "reboot", { force: true });
      await reconnectAfterReboot(page);
      await ensureNoPasswordViaAPI();
    }
    passwordEnabled = false;
  }
});
