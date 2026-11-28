import { test, expect, type Browser, type Page } from "@playwright/test";

import type { TailscaleStatus } from "../src/hooks/stores";
import {
  callJsonRpc,
  ensureNoPasswordViaAPI,
  ensureRpcReady,
  rebootDeviceViaSSH,
  skipWithoutDeviceShell,
  sshExec,
  waitForWebRTCReady,
} from "./helpers";

const CONTROL_URL = process.env.TAILSCALE_CONTROL_URL ?? "";
const AUTH_KEY = process.env.TAILSCALE_AUTHKEY ?? "";
const LOGIN_ORIGIN = CONTROL_URL ? new URL(CONTROL_URL).origin : "https://login.tailscale.com";

test.describe.configure({ mode: "serial" });

async function openTailscaleCard(page: Page) {
  await ensureRpcReady(page, { navigateFirst: true });
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page.getByRole("link", { name: "Network", exact: true }).click();
  const heading = page.getByRole("heading", { name: "Tailscale", exact: true });
  await expect(heading).toBeVisible({ timeout: 20_000 });
  await heading.scrollIntoViewIfNeeded();
}

function tailscaleBadge(page: Page, text: string) {
  return page
    .getByRole("heading", { name: "Tailscale", exact: true })
    .locator("..")
    .getByText(text, { exact: true });
}

async function tailscaleStatus(page: Page) {
  return (await callJsonRpc(page, "getTailscaleStatus")) as TailscaleStatus;
}

async function skipWithoutBuiltinTailscale(page: Page) {
  await ensureNoPasswordViaAPI();
  await ensureRpcReady(page, { navigateFirst: true });
  const status = await tailscaleStatus(page);
  test.skip(!status.builtin, "the device runs a script-installed Tailscale");
}

async function deviceIceCandidateAddresses(page: Page) {
  return page.evaluate(async () => {
    const stats = await window.__kvmTestHooks!._getPeerConnection!()!.getStats();
    const addresses: string[] = [];
    stats.forEach(report => {
      if (report.type === "remote-candidate" && report.address) addresses.push(report.address);
    });
    return addresses;
  });
}

async function withPage(browser: Browser, baseURL: string, fn: (page: Page) => Promise<void>) {
  const context = await browser.newContext({ baseURL });
  const page = await context.newPage();
  try {
    await fn(page);
  } finally {
    await context.close();
  }
}

test.afterAll(async ({ browser }) => {
  test.setTimeout(120_000);
  await withPage(browser, process.env.JETKVM_URL!, async page => {
    await ensureRpcReady(page, { navigateFirst: true, timeoutMs: 90_000 });
    const status = await tailscaleStatus(page);
    if (!status.builtin) return;
    if (status.running) await callJsonRpc(page, "logoutTailscale", {}, 30_000);
    await callJsonRpc(page, "setTailscaleEnabled", { enabled: false }, 30_000);
    await callJsonRpc(page, "setTailscaleControlURL", { controlURL: "" }, 30_000);
  });
});

test("enabling Tailscale brings up the tunnel interface and offers a login link", async ({
  page,
}) => {
  test.setTimeout(180_000);
  await skipWithoutBuiltinTailscale(page);
  await skipWithoutDeviceShell();

  await callJsonRpc(page, "setTailscaleEnabled", { enabled: false }, 30_000);
  await sshExec("rm -rf /userdata/jetkvm/tailscale");
  await callJsonRpc(page, "setTailscaleControlURL", { controlURL: CONTROL_URL }, 30_000);

  await openTailscaleCard(page);
  const toggle = page.getByRole("checkbox", { name: "Enable Tailscale" });
  await expect(toggle).not.toBeChecked();
  expect((await sshExec("ip link show tailscale0", true)).trim()).toBe("");

  await toggle.click();
  await expect(toggle).toBeChecked({ timeout: 30_000 });

  await expect(tailscaleBadge(page, "Needs Login")).toBeVisible({ timeout: 60_000 });
  const loginLink = page.locator(`a[href^="${LOGIN_ORIGIN}/"]`);
  await expect(loginLink).toBeVisible({ timeout: 60_000 });
  expect(await sshExec("ip link show tailscale0")).toContain("tailscale0");

  await toggle.click();

  await expect(toggle).not.toBeChecked({ timeout: 30_000 });
  await expect(loginLink).toBeHidden();
  await expect
    .poll(async () => (await sshExec("ip link show tailscale0", true)).trim(), {
      timeout: 20_000,
    })
    .toBe("");
});

test("an auth key joins the tailnet, and the UI and WebRTC work over the tailnet address", async ({
  page,
  browser,
}) => {
  test.setTimeout(360_000);
  test.skip(!AUTH_KEY, "set TAILSCALE_AUTHKEY (and TAILSCALE_CONTROL_URL for a custom server)");
  await skipWithoutBuiltinTailscale(page);
  await skipWithoutDeviceShell();

  await callJsonRpc(page, "setTailscaleControlURL", { controlURL: CONTROL_URL }, 30_000);
  await openTailscaleCard(page);
  await page.getByRole("checkbox", { name: "Enable Tailscale" }).click();
  await expect(tailscaleBadge(page, "Needs Login")).toBeVisible({ timeout: 60_000 });

  await page.getByLabel("Auth Key").fill(AUTH_KEY);
  await page.getByRole("button", { name: "Connect", exact: true }).click();
  await expect(tailscaleBadge(page, "Connected")).toBeVisible({ timeout: 60_000 });

  const joined = await tailscaleStatus(page);
  const tailnetIPv4 = joined.self!.tailscaleIPs.find(ip => !ip.includes(":"))!;
  expect(tailnetIPv4).toMatch(/^100\./);
  await expect(page.getByText(tailnetIPv4, { exact: true })).toBeVisible();
  expect(await sshExec("ip -4 addr show dev tailscale0")).toContain(`inet ${tailnetIPv4}/32`);

  await rebootDeviceViaSSH();
  await ensureRpcReady(page, { navigateFirst: true, timeoutMs: 90_000 });
  await expect
    .poll(async () => (await tailscaleStatus(page)).self?.tailscaleIPs ?? [], {
      timeout: 60_000,
    })
    .toContain(tailnetIPv4);
  expect((await tailscaleStatus(page)).running).toBe(true);
  await page.close();

  await withPage(browser, `http://${tailnetIPv4}`, async tailnetPage => {
    await ensureRpcReady(tailnetPage, {
      navigateFirst: true,
      timeoutMs: 90_000,
    });
    await waitForWebRTCReady(tailnetPage);
    expect(await deviceIceCandidateAddresses(tailnetPage)).toContain(tailnetIPv4);
  });

  await withPage(browser, process.env.JETKVM_URL!, async lanPage => {
    await openTailscaleCard(lanPage);
    await lanPage.getByRole("button", { name: "Log out", exact: true }).click();
    await expect(tailscaleBadge(lanPage, "Needs Login")).toBeVisible({ timeout: 60_000 });
    expect((await tailscaleStatus(lanPage)).self?.tailscaleIPs ?? []).toEqual([]);
  });
});
