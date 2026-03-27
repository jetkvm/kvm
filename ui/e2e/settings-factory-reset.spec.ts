import { test, expect } from "@playwright/test";

import { ensureLocalAuthMode } from "./helpers";

test.describe("Factory Reset UI", () => {
  test.setTimeout(120000);

  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({
      baseURL: process.env.JETKVM_URL,
    });
    const page = await context.newPage();
    try {
      await ensureLocalAuthMode(page, { mode: "noPassword" });
    } finally {
      await page.close();
      await context.close();
    }
  });

  test("shows factory reset button and confirmation dialog, no reset config button", async ({
    page,
  }) => {
    await page.goto("/settings/advanced");
    await page.waitForLoadState("networkidle");

    // Enable Troubleshooting Mode to reveal nested debug settings
    const checkbox = page.getByRole("checkbox", {
      name: "Troubleshooting Mode",
    });
    await checkbox.scrollIntoViewIfNeeded();
    if (!(await checkbox.isChecked())) {
      await checkbox.click();
    }

    // Wait for factory reset section to appear
    const factoryResetText = page.getByText("Factory Reset").first();
    await factoryResetText.scrollIntoViewIfNeeded();
    await expect(factoryResetText).toBeVisible({ timeout: 10000 });

    // Verify the old "Reset Config" button is gone
    await expect(page.getByRole("button", { name: "Reset Config" })).not.toBeVisible();

    // Verify Factory Reset description is visible
    await expect(
      page.getByText("Erase all data and restore the device to its original state"),
    ).toBeVisible();

    // Click the Factory Reset button to open the confirmation dialog
    await page.getByRole("button", { name: "Factory Reset" }).click();

    // Verify confirmation dialog appears with correct copy
    await expect(page.getByText("Factory reset this device?")).toBeVisible();
    await expect(page.getByText("This will permanently erase all configuration")).toBeVisible();
    await expect(page.getByRole("button", { name: "Erase all data and reset" })).toBeVisible();

    // Close dialog without confirming
    await page.getByRole("button", { name: /cancel/i }).click();
    await expect(page.getByText("Factory reset this device?")).not.toBeVisible();
  });
});
