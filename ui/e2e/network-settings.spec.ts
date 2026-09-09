import { isIPv4 } from "node:net";
import { test, expect, type Page } from "@playwright/test";
import { callJsonRpc, ensureNoPasswordViaAPI, ensureRpcReady } from "./helpers";
import type { NetworkSettings, NetworkState } from "../src/hooks/stores";

let original: NetworkSettings | undefined;

async function openNetworkSettings(page: Page, settings: NetworkSettings) {
  await ensureRpcReady(page);
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page.getByRole("link", { name: "Network", exact: true }).click();
  // Both cards replace the skeleton only after async form defaults finish loading.
  if (settings.ipv4_mode === "static") {
    await expect(page.locator('[name="ipv4_static.address"]')).toHaveValue(
      settings.ipv4_static!.address,
      { timeout: 20_000 },
    );
  } else {
    await expect(
      page.getByRole("heading", { name: "DHCP Lease Information", exact: true }),
    ).toBeVisible({ timeout: 20_000 });
  }
}

test.afterAll(async ({ browser }) => {
  test.setTimeout(180_000);
  if (!original) return;
  const page = await browser.newPage();
  try {
    await ensureRpcReady(page, { navigateFirst: true, timeoutMs: 90_000 });
    await callJsonRpc(page, "setNetworkSettings", { settings: original });
    await page.waitForTimeout(5_000);
    await ensureRpcReady(page, { navigateFirst: true, timeoutMs: 90_000 });
    expect(await callJsonRpc(page, "getNetworkSettings")).toEqual(original);
  } finally {
    await page.close();
  }
});

test("static IPv4 accepts empty gateway and DNS and preserves a saved hostname", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const address = new URL(process.env.JETKVM_URL!).hostname;
  test.skip(!isIPv4(address), "Network reconfiguration requires an IPv4 JETKVM_URL");
  await ensureNoPasswordViaAPI();
  await ensureRpcReady(page, { navigateFirst: true });
  original = (await callJsonRpc(page, "getNetworkSettings")) as NetworkSettings;
  await openNetworkSettings(page, original);
  // Let terminal channel initialization finish before focusing form controls.
  await page.waitForTimeout(1_000);
  const state = (await callJsonRpc(page, "getNetworkState")) as NetworkState;
  const netmask = state.dhcp_lease?.netmask || original.ipv4_static?.netmask;
  expect(netmask, "need the connected subnet mask to preserve device access").toBeTruthy();
  await page.locator('[name="hostname"]').fill("jetkvm-e2e-network");
  await page.locator('[name="ipv4_mode"]').selectOption("static");
  await page.locator('[name="ipv4_static.address"]').fill(address);
  await page.locator('[name="ipv4_static.netmask"]').fill(netmask!);
  await page.locator('[name="ipv4_static.gateway"]').fill("");
  const dnsFields = page.locator('input[name^="ipv4_static.dns."]');
  if (!(await dnsFields.count()))
    await page.getByRole("button", { name: "Add DNS Server" }).click();
  for (const input of await dnsFields.all()) await input.fill("");
  await page.getByRole("button", { name: "Save Settings", exact: true }).first().click();
  await page.getByRole("button", { name: "Apply changes", exact: true }).click();
  await page.waitForTimeout(5_000);
  await ensureRpcReady(page, { navigateFirst: true, timeoutMs: 90_000 });
  const saved = (await callJsonRpc(page, "getNetworkSettings")) as NetworkSettings;
  expect(saved).toMatchObject({
    hostname: "jetkvm-e2e-network",
    ipv4_mode: "static",
    ipv4_static: { address, netmask },
  });
  expect(saved.ipv4_static?.gateway ?? "").toBe("");
  expect(saved.ipv4_static?.dns ?? []).toEqual([]);
  await openNetworkSettings(page, saved);
  await expect(page.locator('[name="hostname"]')).toHaveValue("jetkvm-e2e-network");
  await expect(page.locator('[name="ipv4_static.gateway"]')).toHaveValue("");
  await expect
    .poll(async () => ((await callJsonRpc(page, "getNetworkState")) as NetworkState).hostname)
    .toBe("jetkvm-e2e-network");
});
