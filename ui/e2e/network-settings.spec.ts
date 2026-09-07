import { isIPv4 } from "node:net";
import { test, expect } from "@playwright/test";
import { callJsonRpc, ensureNoPasswordViaAPI, ensureRpcReady } from "./helpers";
import type { NetworkSettings, NetworkState } from "../src/hooks/stores";

let original: NetworkSettings | undefined;

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
  await page.goto("/settings/network", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  await page.goto("/settings/network", { waitUntil: "networkidle" });
  // Let terminal channel initialization finish before focusing form controls.
  await page.waitForTimeout(1_000);
  original = (await callJsonRpc(page, "getNetworkSettings")) as NetworkSettings;
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
  await page.goto("/settings/network", { waitUntil: "networkidle" });
  await expect(page.locator('[name="hostname"]')).toHaveValue("jetkvm-e2e-network");
  await expect(page.locator('[name="ipv4_static.gateway"]')).toHaveValue("");
  await expect
    .poll(async () => ((await callJsonRpc(page, "getNetworkState")) as NetworkState).hostname)
    .toBe("jetkvm-e2e-network");
});
