import { test, expect } from "@playwright/test";
import { ensureLocalAuthMode, logout } from "./helpers";
import { registerResetCleanup } from "./helpers/reset";

test.describe("Reset cleanup after an interrupted password test", () => {
  test.describe.configure({ mode: "serial" });

  test.describe("test leaves a changed password behind", () => {
    registerResetCleanup();

    test("leave the device protected for the cleanup hook", async ({ page, request }) => {
      await ensureLocalAuthMode(page, { mode: "password", password: "NewPassword456" });
      await logout(page);
      expect((await request.get("/device")).status()).toBe(401);
    });
  });

  // This runs after the nested suite's cleanup, before global teardown can
  // repair the device through SSH. Its request has no authenticated session.
  test("cleanup restores unauthenticated device access", async ({ request }) => {
    expect((await request.get("/device")).status()).toBe(200);
    const status = await (await request.get("/device/status")).json();
    expect(status.isSetup).toBe(true);
  });
});
