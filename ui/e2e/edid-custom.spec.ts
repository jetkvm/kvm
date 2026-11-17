import { test, expect } from "@playwright/test";

import {
  callJsonRpc,
  ensureLocalAuthMode,
  skipWithoutCapability,
  waitForWebRTCReady,
} from "./helpers";

// A device that reports custom_edid accepts an EDID typed into the video
// settings page. An invalid one is refused and leaves the preset in use.

interface EDIDPreset {
  name: string;
  edid: string;
}

test("custom EDID validation preserves the selected preset @custom-edid", async ({ page }) => {
  test.setTimeout(90_000);
  await ensureLocalAuthMode(page, { mode: "noPassword" });
  await waitForWebRTCReady(page);
  await skipWithoutCapability(page, "custom_edid");
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
