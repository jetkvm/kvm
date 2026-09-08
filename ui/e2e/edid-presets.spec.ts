import { test, expect } from "@playwright/test";

import { callJsonRpc, ensureLocalAuthMode, waitForWebRTCReady } from "./helpers";

// The device reports the EDID presets it offers; the video settings page lists
// exactly those presets and applies the one picked. Custom EDID is separate.

interface EDIDPreset {
  name: string;
  edid: string;
}

test("video settings lists the device's EDID presets and applies one", async ({ page }) => {
  test.setTimeout(90_000);

  await page.goto("/");
  await page.waitForLoadState("networkidle");
  await ensureLocalAuthMode(page, { mode: "noPassword" });
  await waitForWebRTCReady(page);

  const presets = (await callJsonRpc(page, "getEDIDPresets")) as EDIDPreset[];
  expect(presets.length).toBeGreaterThan(1);
  const original = (await callJsonRpc(page, "getEDID")) as string;

  try {
    await page.goto("/settings/video");
    await page.waitForLoadState("networkidle");
    await waitForWebRTCReady(page);

    const select = page
      .locator("select")
      .filter({ has: page.locator("option").filter({ hasText: presets[0].name }) });
    await expect(select).toBeVisible();
    await expect(select).toBeEnabled();
    await expect(select.locator('option:not([value="custom"])')).toHaveText(
      presets.map(p => p.name),
    );

    const target = presets.find(p => p.edid.toLowerCase() !== original.toLowerCase())!;
    await select.selectOption({ label: target.name });

    await expect
      .poll(async () => ((await callJsonRpc(page, "getEDID")) as string).toLowerCase(), {
        timeout: 15_000,
      })
      .toBe(target.edid.toLowerCase());
    await expect(select).toHaveValue(target.edid);
  } finally {
    await callJsonRpc(page, "setEDID", { edid: original });
  }
});

test("custom EDID validation preserves the selected preset @custom-edid", async ({ page }) => {
  test.setTimeout(90_000);
  await ensureLocalAuthMode(page, { mode: "noPassword" });
  await waitForWebRTCReady(page);
  const presets = (await callJsonRpc(page, "getEDIDPresets")) as EDIDPreset[];
  const original = (await callJsonRpc(page, "getEDID")) as string;
  const target = presets[0];
  try {
    await callJsonRpc(page, "setEDID", { edid: target.edid });
    await page.goto("/settings/video");
    await waitForWebRTCReady(page);
    const select = page.locator("select").filter({ has: page.locator('option[value="custom"]') });
    await select.selectOption("custom");
    await page.getByPlaceholder("00F...").fill("invalid EDID");
    await page.getByRole("button", { name: "Set Custom EDID", exact: true }).click();
    await expect(page.getByText(/Failed to set EDID/)).toBeVisible();
    expect(((await callJsonRpc(page, "getEDID")) as string).toLowerCase()).toBe(
      target.edid.toLowerCase(),
    );
    await expect(page.getByPlaceholder("00F...")).toHaveValue("invalid EDID");
    await expect(select).toHaveValue("custom");

    await page.getByRole("button", { name: "Restore to default", exact: true }).click();
    await expect(select).toHaveValue(presets[0].edid, { timeout: 20_000 });
    await expect(page.getByPlaceholder("00F...")).toHaveCount(0);
    expect(((await callJsonRpc(page, "getEDID")) as string).toLowerCase()).toBe(
      presets[0].edid.toLowerCase(),
    );
  } finally {
    await callJsonRpc(page, "setEDID", { edid: original });
  }
});
