/**
 * Virtual keyboard UI tests (no host required).
 *
 * Asserts the parts of the virtual keyboard whose behaviour is purely
 * client-side and observable via DOM:
 *  - toggle visibility from the action bar / hide button
 *  - detach/attach buttons swap correctly
 *  - clicking a modifier keycap with latching enabled holds it (data-layer
 *    on .vkb reflects the shift / altgr layer)
 *  - clicking again releases the latch (data-layer returns to "all")
 *
 * Tests that need round-trip HID verification live in
 * remote-agent/keyboard-paste.spec.ts and keyboard-macros.spec.ts.
 *
 * Run with:
 *   JETKVM_URL=http://<kvm-ip> npx playwright test keyboard-ui --project=ui
 */
import { test, expect, type Page } from "@playwright/test";
import { ensureLocalAuthMode, goToSession } from "./helpers";

// Standard USB HID scancodes for the modifier keys we exercise.
const HID_LSHIFT = 0xe1;
const HID_RALTGR = 0xe6;

test.describe.configure({ mode: "serial" });

let sharedPage: Page;

test.beforeAll(async ({ browser }) => {
  test.setTimeout(60_000);
  sharedPage = await browser.newPage();
  await ensureLocalAuthMode(sharedPage, { mode: "noPassword" });
  await goToSession(sharedPage);
});

test.afterAll(async () => {
  await sharedPage?.close();
});

async function ensureVirtualKeyboardVisible(page: Page): Promise<void> {
  const vkb = page.locator(".vkb");
  if (await vkb.isVisible().catch(() => false)) return;
  // ActionBar exposes the toggle by its localized label; finding it by role
  // works regardless of icon font order.
  const toggle = page.getByRole("button", { name: /Virtual Keyboard/i }).first();
  await toggle.click();
  await expect(vkb).toBeVisible({ timeout: 5000 });
}

// modifierLatching defaults to `true` in the settings store. These tests
// rely on that default; if a future change flips the default, set it via
// the settings page or through a window-exposed setter.

test.describe("virtual keyboard: visibility", () => {
  test("toggle button shows the keyboard, hide button collapses it", async () => {
    await ensureVirtualKeyboardVisible(sharedPage);

    const vkb = sharedPage.locator(".vkb");
    await expect(vkb).toBeVisible();

    const hideButton = sharedPage.getByTestId("virtual-keyboard-hide");
    await hideButton.click();

    // The wrapper stays in the DOM but its container slides out — the .vkb
    // node leaves the viewport. The clearer signal is the toggle button
    // becoming clickable again with the keyboard re-shown.
    const toggle = sharedPage.getByRole("button", { name: /Virtual Keyboard/i }).first();
    await toggle.click();
    await expect(vkb).toBeVisible({ timeout: 5000 });
  });

  test("detach/attach buttons swap based on state", async () => {
    await ensureVirtualKeyboardVisible(sharedPage);

    const detach = sharedPage.getByTestId("virtual-keyboard-detach");
    const attach = sharedPage.getByTestId("virtual-keyboard-attach");

    if (await detach.isVisible().catch(() => false)) {
      await detach.click();
      await expect(attach).toBeVisible({ timeout: 3000 });
      await attach.click();
      await expect(detach).toBeVisible({ timeout: 3000 });
    } else {
      // Already detached from a prior test
      await attach.click();
      await expect(detach).toBeVisible({ timeout: 3000 });
    }
  });
});

test.describe("virtual keyboard: layout layer switching", () => {
  test("clicking a latched shift key flips data-layer to shift, click again releases", async () => {
    await ensureVirtualKeyboardVisible(sharedPage);

    const vkb = sharedPage.locator(".vkb");
    await expect(vkb).toHaveAttribute("data-layer", "all", { timeout: 5000 });

    // Click LeftShift via its data-scancode.
    const lshift = vkb.locator(`[data-scancode="${HID_LSHIFT}"]`).first();
    await lshift.click();
    await expect(vkb).toHaveAttribute("data-layer", "shift", { timeout: 3000 });

    // Click again — latch toggles off, layer returns to "all".
    await lshift.click();
    await expect(vkb).toHaveAttribute("data-layer", "all", { timeout: 3000 });
  });

  test("AltGr latch produces altgr layer", async () => {
    // RAlt (HID 0xE6) is the AltGr scancode; every built-in layout's
    // physical right-alt key carries it, so the keycap is always present.
    await ensureVirtualKeyboardVisible(sharedPage);

    const vkb = sharedPage.locator(".vkb");
    await expect(vkb).toHaveAttribute("data-layer", "all", { timeout: 5000 });

    const altgr = vkb.locator(`[data-scancode="${HID_RALTGR}"]`).first();
    await altgr.click();
    await expect(vkb).toHaveAttribute("data-layer", "altgr", { timeout: 3000 });
    await altgr.click();
    await expect(vkb).toHaveAttribute("data-layer", "all", { timeout: 3000 });
  });
});

test.describe("virtual keyboard: keycap rendering", () => {
  test("keycaps carry data-scancode for every typeable key", async () => {
    await ensureVirtualKeyboardVisible(sharedPage);
    const vkb = sharedPage.locator(".vkb");

    // Every standard letter A–Z (HID 0x04..0x1d) should be present somewhere
    // in the rendered keyboard. We check three representative keys.
    for (const scancode of [0x04, 0x16, 0x1d]) {
      // A, S, Z
      const key = vkb.locator(`[data-scancode="${scancode}"]`).first();
      await expect(key, `keycap with scancode ${scancode.toString(16)}`).toBeVisible();
    }
  });
});
