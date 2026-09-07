import { test, expect, type Page } from "@playwright/test";
import { HID_KEY, callJsonRpc, tapKey, ensureRpcReady } from "../helpers";
import { waitForKeyboardReady, KEY } from "./remote-agent";
import { agent, mountKey, registerSharedSession, remoteHostExec } from "./shared";

test.describe.configure({ mode: "serial" });

let sharedPage: Page;
registerSharedSession(page => (sharedPage = page));

test.describe("Remote Host Agent: virtual media", () => {
  test("virtual-media: mount ISO from URL and verify, then unmount", async () => {
    test.setTimeout(60_000);

    try {
      await callJsonRpc(sharedPage, "unmountImage");
    } catch {
      /* ok if nothing mounted */
    }

    const stateBefore = (await callJsonRpc(sharedPage, "getVirtualMediaState")) as null | object;
    expect(stateBefore).toBeNull();

    const NETBOOT_XYZ_URL = "https://boot.netboot.xyz/ipxe/netboot.xyz.iso";
    await callJsonRpc(sharedPage, "mountWithHTTP", { url: NETBOOT_XYZ_URL, mode: "CDROM" });

    const stateAfter = (await callJsonRpc(sharedPage, "getVirtualMediaState")) as {
      source: string;
      mode: string;
      url?: string;
    } | null;
    expect(stateAfter).not.toBeNull();
    expect(stateAfter!.source).toBe("HTTP");
    expect(stateAfter!.mode).toBe("CDROM");
    expect(stateAfter!.url).toBe(NETBOOT_XYZ_URL);

    const usbDevices = await agent!.getUSBDevices();
    expect(usbDevices.length).toBeGreaterThan(0);

    await callJsonRpc(sharedPage, "unmountImage");

    const stateEnd = (await callJsonRpc(sharedPage, "getVirtualMediaState")) as null | object;
    expect(stateEnd).toBeNull();

    const finalDevices = await agent!.getUSBDevices();
    expect(finalDevices.length).toBeGreaterThan(0);
  });

  test("virtual-media: unavailable URL is rejected without mounting", async () => {
    test.setTimeout(30_000);

    try {
      await callJsonRpc(sharedPage, "unmountImage");
    } catch {
      /* ok if nothing mounted */
    }

    const stateBefore = (await callJsonRpc(sharedPage, "getVirtualMediaState")) as null | object;
    expect(stateBefore).toBeNull();

    const baselineMounts = new Set((await agent!.getMounts()).map(mountKey));

    await expect(
      callJsonRpc(sharedPage, "mountWithHTTP", { url: MISSING_IMAGE_URL, mode: "CDROM" }),
    ).rejects.toThrow(MISSING_IMAGE_ERROR);

    const stateAfter = (await callJsonRpc(sharedPage, "getVirtualMediaState")) as null | object;
    expect(stateAfter).toBeNull();

    await agent!.waitForMount(mount => !baselineMounts.has(mountKey(mount)), false, 5_000);
  });

  test("virtual-media: URL mount form shows unavailable image error", async () => {
    test.setTimeout(30_000);

    try {
      await callJsonRpc(sharedPage, "unmountImage");
    } catch {
      /* ok if nothing mounted */
    }

    const baselineMounts = new Set((await agent!.getMounts()).map(mountKey));

    await sharedPage.getByRole("button", { name: "Virtual Media" }).click();
    await sharedPage.getByRole("button", { name: "Add New Media" }).click();
    await sharedPage.getByRole("button", { name: "Continue" }).click();
    await sharedPage.getByPlaceholder("https://example.com/image.iso").fill(MISSING_IMAGE_URL);
    await sharedPage.getByRole("button", { name: "Mount URL" }).click();

    await expect(sharedPage.getByRole("heading", { name: "Mount Error" })).toBeVisible();
    await expect(sharedPage.getByText(MISSING_IMAGE_ERROR)).toBeVisible();

    const stateAfter = (await callJsonRpc(sharedPage, "getVirtualMediaState")) as null | object;
    expect(stateAfter).toBeNull();

    await agent!.waitForMount(mount => !baselineMounts.has(mountKey(mount)), false, 5_000);

    await sharedPage.getByRole("button", { name: "Close" }).click();
    await ensureRpcReady(sharedPage);
  });

  test("virtual-media: mount ISO as Disk mode preserves keyboard (#560)", async () => {
    test.setTimeout(90_000);

    // Ensure clean state
    try {
      await callJsonRpc(sharedPage, "unmountImage");
    } catch {
      /* ok */
    }

    // Verify keyboard works before mount
    const preEvents = await agent!.expectKeyPress(KEY.SPACE, async () => {
      await tapKey(sharedPage, HID_KEY.SPACE);
    });
    expect(preEvents.length, "keyboard should work before disk mount").toBeGreaterThan(0);

    // Mount as Disk mode — this triggers USB rebind (unlike CDROM which skips it)
    const NETBOOT_XYZ_URL = "https://boot.netboot.xyz/ipxe/netboot.xyz.iso";
    await callJsonRpc(sharedPage, "mountWithHTTP", { url: NETBOOT_XYZ_URL, mode: "Disk" });

    const stateAfter = (await callJsonRpc(sharedPage, "getVirtualMediaState")) as {
      source: string;
      mode: string;
    } | null;
    expect(stateAfter).not.toBeNull();
    expect(stateAfter!.mode).toBe("Disk");

    // Wait for HID devices to re-enumerate after USB rebind
    await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 15000);

    // Verify keyboard works after disk mount (this would fail without the ResetHIDFiles fix)
    const postMountEvents = await waitForKeyboardReady(agent!, sharedPage);
    expect(postMountEvents.length, "keyboard should work after disk mount").toBeGreaterThan(0);

    // Unmount — Disk-mode unmount can hit the EBUSY rebind path plus NBD
    // disconnect drain, which routinely runs past the default 10s RPC timeout.
    await callJsonRpc(sharedPage, "unmountImage", {}, 30_000);
    const stateEnd = (await callJsonRpc(sharedPage, "getVirtualMediaState")) as null | object;
    expect(stateEnd).toBeNull();

    // Wait for HID devices after unmount (unmount also triggers rebind back to CDROM default)
    await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 15000);

    // Verify keyboard works after unmount too
    const postUnmountEvents = await waitForKeyboardReady(agent!, sharedPage);
    expect(postUnmountEvents.length, "keyboard should work after unmount").toBeGreaterThan(0);
  });

  // ═══════════════════════════════════════════
  // VIRTUAL MEDIA: EBUSY UNMOUNT FALLBACK (#834)
  // ═══════════════════════════════════════════

  test("virtual-media: unmount succeeds when host holds device open via EBUSY fallback (#834)", async () => {
    test.setTimeout(120_000);

    // Ensure clean state
    try {
      await callJsonRpc(sharedPage, "unmountImage");
    } catch {
      /* ok if nothing mounted */
    }

    // Verify keyboard works before test
    const preEvents = await waitForKeyboardReady(agent!, sharedPage);
    expect(preEvents.length, "keyboard should work before EBUSY test").toBeGreaterThan(0);

    // Mount ISO as CDROM
    const NETBOOT_XYZ_URL = "https://boot.netboot.xyz/ipxe/netboot.xyz.iso";
    await callJsonRpc(sharedPage, "mountWithHTTP", { url: NETBOOT_XYZ_URL, mode: "CDROM" });

    const vmState = (await callJsonRpc(sharedPage, "getVirtualMediaState")) as {
      source: string;
      mode: string;
    } | null;
    expect(vmState).not.toBeNull();
    expect(vmState!.mode).toBe("CDROM");

    // Wait for the host to enumerate the USB mass storage device
    await new Promise(r => setTimeout(r, 5000));

    // Find the JetKVM CDROM block device on the remote host.
    // lsblk is the most portable way to find USB-attached SCSI optical drives.
    // Match on the transport type (usb) and device type (rom).
    const findBlockDevCmd = `lsblk -Snrpo NAME,TRAN,TYPE 2>/dev/null | awk '$2=="usb" && $3=="rom" {print $1; exit}'`;

    let blockDev = "";
    const devDeadline = Date.now() + 45000;
    while (Date.now() < devDeadline) {
      try {
        blockDev = remoteHostExec(findBlockDevCmd).trim();
        if (blockDev) break;
      } catch {
        /* retry */
      }
      await new Promise(r => setTimeout(r, 1000));
    }
    if (!blockDev) {
      // Some hosts don't enumerate USB mass storage as sr* (missing sr_mod, etc.)
      await callJsonRpc(sharedPage, "unmountImage");
      test.skip(true, "CDROM block device did not appear on remote host (sr_mod not loaded?)");
      return;
    }

    // Lock the CDROM medium — sends PREVENT MEDIUM REMOVAL, which causes the
    // KVM kernel to return EBUSY when clearing the backing file. Lock via
    // eject -i instead of mounting the ISO: a mount reads filesystem data the
    // device streams over HTTP on demand, and a stalled stream leaves an
    // unkillable D-state mount on the host that aborts all later S3 suspends
    // (userspace freeze fails after 20s).
    try {
      remoteHostExec(`sudo eject -i on ${blockDev}`);
    } catch {
      test.skip(true, "Could not lock CDROM medium on remote host");
      return;
    }

    try {
      // Unmount on the KVM side — should hit EBUSY, then fallback rebinds USB
      await callJsonRpc(sharedPage, "unmountImage");

      // Verify virtual media state is cleared
      const stateEnd = (await callJsonRpc(sharedPage, "getVirtualMediaState")) as null | object;
      expect(stateEnd, "Virtual media should be unmounted after EBUSY fallback").toBeNull();

      // Wait for HID devices to re-enumerate after the USB rebind
      await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 15000);

      // Verify keyboard still works after the rebind
      const postEvents = await waitForKeyboardReady(agent!, sharedPage);
      expect(
        postEvents.length,
        "keyboard should work after EBUSY unmount fallback",
      ).toBeGreaterThan(0);
    } finally {
      // Clean up: unlock the medium (may already be gone after the USB rebind)
      try {
        remoteHostExec(`sudo eject -i off ${blockDev} 2>/dev/null`);
      } catch {
        /* best effort */
      }
    }
  });
});
