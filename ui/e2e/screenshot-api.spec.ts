import { test, expect } from "@playwright/test";

import { ensureLocalAuthMode, getDeviceHost } from "./helpers";

const TEST_PASSWORD = "TestPassword123";

function basicAuthHeader(password: string): string {
  return `Basic ${Buffer.from(`api:${password}`).toString("base64")}`;
}

const SCREENSHOT_PATHS = ["/screenshot.jpg", "/screenshot.png"] as const;

function expectedContentType(path: string): string {
  return path.endsWith(".png") ? "image/png" : "image/jpeg";
}

test.describe("Screenshot API", () => {
  test.setTimeout(60000);

  test.describe("password mode", () => {
    test.beforeEach(async ({ page }) => {
      // Leaves `page`'s browser context with a valid session cookie too, so
      // page.request below is already authenticated like a logged-in tab.
      await ensureLocalAuthMode(page, { mode: "password", password: TEST_PASSWORD });
    });

    for (const path of SCREENSHOT_PATHS) {
      test(`${path} rejects requests with no credentials`, async () => {
        const res = await fetch(`http://${getDeviceHost()}${path}`);
        expect(res.status).toBe(401);
      });

      test(`${path} rejects a bogus session cookie`, async () => {
        const res = await fetch(`http://${getDeviceHost()}${path}`, {
          headers: { Cookie: "authToken=not-a-real-token" },
        });
        expect(res.status).toBe(401);
      });

      test(`${path} succeeds with HTTP Basic Auth`, async () => {
        const res = await fetch(`http://${getDeviceHost()}${path}`, {
          headers: { Authorization: basicAuthHeader(TEST_PASSWORD) },
        });
        expect(res.status).toBe(200);
        expect(res.headers.get("content-type")).toBe(expectedContentType(path));

        const bytes = await res.arrayBuffer();
        expect(bytes.byteLength).toBeGreaterThan(1000);
      });

      test(`${path} succeeds with a logged-in browser session`, async ({ page }) => {
        const res = await page.request.get(path);
        expect(res.status()).toBe(200);
        expect(res.headers()["content-type"]).toBe(expectedContentType(path));

        const body = await res.body();
        expect(body.byteLength).toBeGreaterThan(1000);
      });
    }
  });

  test("succeeds with no credentials in noPassword mode", async ({ page }) => {
    await ensureLocalAuthMode(page, { mode: "noPassword" });

    const res = await fetch(`http://${getDeviceHost()}/screenshot.jpg`);
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("image/jpeg");
  });
});
