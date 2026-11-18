import { test, expect, type Page } from "@playwright/test";

import {
  callJsonRpc,
  ensureLocalAuthMode,
  skipWithoutCapability,
  skipWithoutDeviceShell,
  sshExec,
  waitForWebRTCReady,
} from "./helpers";

interface OsServicesSettings {
  enabled: boolean;
}

interface VideoState {
  osServices?: { connected: boolean };
}

interface VirtualMediaState {
  source: string;
  mode: string;
  size: number;
}

const LINKED_FUNCTIONS = "ls /sys/kernel/config/usb_gadget/jetkvm/configs/c.1";
const USB_FUNCTION = "ffs.os_services";
const SMALLEST_CDROM = 300 * 2048;

async function open(page: Page, path: string) {
  await page.goto(path);
  await page.waitForLoadState("networkidle");
  await waitForWebRTCReady(page);
}

async function start(page: Page) {
  await page.goto("/");
  await page.waitForLoadState("networkidle");
  await ensureLocalAuthMode(page, { mode: "noPassword" });
  await waitForWebRTCReady(page);
  await skipWithoutCapability(page, "os_services");
}

async function settings(page: Page) {
  return (await callJsonRpc(page, "getOsServicesSettings")) as OsServicesSettings;
}

async function setEnabled(page: Page, enabled: boolean) {
  await callJsonRpc(page, "setOsServicesSettings", { settings: { enabled } }, 30_000);
}

test("the OS Services switch adds and removes its USB function @ssh", async ({ page }) => {
  test.setTimeout(120_000);
  await skipWithoutDeviceShell();
  await start(page);
  const original = await settings(page);

  try {
    await setEnabled(page, false);
    expect(await sshExec(LINKED_FUNCTIONS)).not.toContain(USB_FUNCTION);

    await open(page, "/settings/os-services");
    const toggle = page.getByTestId("os-services-enabled");
    const installer = page.getByTestId("os-services-mount-installer");
    await expect(toggle).not.toBeChecked();
    await expect(installer).toHaveCount(0);

    await toggle.check();
    await expect.poll(async () => (await settings(page)).enabled, { timeout: 30_000 }).toBe(true);
    await expect.poll(() => sshExec(LINKED_FUNCTIONS), { timeout: 30_000 }).toContain(USB_FUNCTION);
    await expect(page.getByTestId("os-services-fingerprint-copy")).toBeVisible();

    await expect(toggle).toBeEnabled({ timeout: 30_000 });
    await toggle.uncheck();
    await expect.poll(async () => (await settings(page)).enabled, { timeout: 30_000 }).toBe(false);
    await expect
      .poll(() => sshExec(LINKED_FUNCTIONS), { timeout: 30_000 })
      .not.toContain(USB_FUNCTION);
    await expect(installer).toHaveCount(0);
  } finally {
    await setEnabled(page, original.enabled);
  }
});

test("the OS Services settings page mounts the installer disc as a CD-ROM", async ({ page }) => {
  test.setTimeout(120_000);
  await start(page);
  const original = await settings(page);

  try {
    await callJsonRpc(page, "unmountImage");
    await setEnabled(page, true);
    const state = (await callJsonRpc(page, "getVideoState")) as VideoState;
    test.skip(
      state.osServices?.connected === true,
      "a sender is connected, so the page does not offer the installer",
    );

    await open(page, "/settings/os-services");
    await page.getByTestId("os-services-mount-installer").click();

    await expect
      .poll(async () => (await callJsonRpc(page, "getVirtualMediaState")) as VirtualMediaState, {
        timeout: 30_000,
      })
      .toMatchObject({ source: "Builtin", mode: "CDROM" });
    const mounted = (await callJsonRpc(page, "getVirtualMediaState")) as VirtualMediaState;
    expect(mounted.size).toBeGreaterThanOrEqual(SMALLEST_CDROM);
  } finally {
    await callJsonRpc(page, "unmountImage");
    await setEnabled(page, original.enabled);
  }
});
