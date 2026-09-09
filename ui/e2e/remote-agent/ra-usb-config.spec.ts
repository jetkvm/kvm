import { test, expect, type Page } from "@playwright/test";
import {
  DWC3_PATH,
  HID_KEY,
  UDC_NAME,
  callJsonRpc,
  waitForUdcState,
  tapKey,
  waitForWebRTCReady,
  waitForVideoDimensions,
  sendAbsMouseMove,
  sshExec,
  restartAppViaSSH,
  skipWithoutDeviceShell,
  rebootDeviceViaSSH,
} from "../helpers";
import { waitForKeyboardReady, KEY } from "./remote-agent";
import {
  ID_DEFAULT,
  ID_LOGITECH,
  USB_DEFAULT_CONFIG,
  USB_DEVICES_DEFAULT,
  USB_DEVICES_KEYBOARD_ONLY,
  USB_LOGITECH_CONFIG,
  agent,
  expectHostStaysAsleep,
  putRemoteHostToSleepForUSBWakeTest,
  recoverRemoteHostAfterUSBWakeTest,
  registerSharedSession,
  remoteHostExec,
  sendBrowserInputThatMustNotWakeHost,
  setUsbConfigAndWait,
  setUsbDevicesAndWait,
  usbReconfigWithRetry,
  waitForRemoteHostTtyACM,
  wakeRemoteHostWithWakeButton,
} from "./shared";

test.describe.configure({ mode: "serial" });

let sharedPage: Page;
registerSharedSession(page => (sharedPage = page));

test.describe("Remote Host Agent: USB gadget", () => {
  // ═══════════════════════════════════════════
  // USB: DEVICE PRESENCE + SWITCHING + DESCRIPTORS
  // ═══════════════════════════════════════════

  test("usb: device presence, switching, and descriptor changes", async () => {
    // Four sequential gadget reconfigs, each allowed up to 45s with rebind retries.
    test.setTimeout(240_000);

    // Verify JetKVM is connected with default devices
    const device = await agent!.expectJetKVMConnected();
    expect(device).toBeDefined();
    expect(device!.name).toContain("JetKVM");
    expect(device!.id).toBe(ID_DEFAULT);

    const devices = await agent!.getJetKVMInputDevices();
    const types = devices.map(d => d.type);
    expect(types).toContain("keyboard");
    expect(types).toContain("absolute_mouse");
    expect(types).toContain("relative_mouse");
    expect(devices.length).toBe(3);

    const afterDevices = await setUsbDevicesAndWait(USB_DEVICES_KEYBOARD_ONLY, ["keyboard"]);
    const afterTypes = afterDevices.map(d => d.type);
    expect(afterTypes).toContain("keyboard");
    expect(afterTypes).not.toContain("absolute_mouse");
    expect(afterTypes).not.toContain("relative_mouse");

    // Restore default devices
    await setUsbDevicesAndWait(USB_DEVICES_DEFAULT, [
      "keyboard",
      "absolute_mouse",
      "relative_mouse",
    ]);

    // Switch USB descriptor to Logitech — verify host sees new VID/PID
    const logitechDevices = await setUsbConfigAndWait(USB_LOGITECH_CONFIG, ID_LOGITECH);
    expect(logitechDevices.length).toBeGreaterThan(0);
    expect(logitechDevices[0].name).toContain("Logitech");

    // Restore default descriptor
    const deviceId = (await callJsonRpc(sharedPage, "getDeviceID")) as string;
    const defaultConfig = { ...USB_DEFAULT_CONFIG, serial_number: deviceId || "" };
    await setUsbConfigAndWait(defaultConfig, ID_DEFAULT);
  });

  // ═══════════════════════════════════════════
  // USB SERIAL CONSOLE (CDC-ACM)
  // ═══════════════════════════════════════════

  test("usb: serial console CDC-ACM toggle creates and removes ttyACM on host @ssh @serial", async () => {
    await skipWithoutDeviceShell();
    test.setTimeout(90_000);

    test.skip(!process.env.JETKVM_REMOTE_HOST, "JETKVM_REMOTE_HOST not set");

    // Verify the host does NOT see a ttyACM device
    const beforeACM = await usbReconfigWithRetry(
      "setUsbDevices",
      { devices: { ...USB_DEVICES_DEFAULT, serial_console: false } },
      attemptMs => waitForRemoteHostTtyACM(false, attemptMs),
      45_000,
    );
    expect(beforeACM).toBe("");

    // Verify the host now sees a ttyACM device
    const afterACM = await usbReconfigWithRetry(
      "setUsbDevices",
      { devices: { ...USB_DEVICES_DEFAULT, serial_console: true } },
      attemptMs => waitForRemoteHostTtyACM(true, attemptMs),
      45_000,
    );
    expect(afterACM).toContain("ttyACM");

    // Verify /dev/ttyGS0 exists on the KVM device
    const afterGS0 = (await sshExec("ls /dev/ttyGS0 2>/dev/null || echo MISSING", true)).trim();
    expect(afterGS0).toBe("/dev/ttyGS0");

    // Verify the host no longer sees a ttyACM device
    const removedACM = await usbReconfigWithRetry(
      "setUsbDevices",
      { devices: { ...USB_DEVICES_DEFAULT, serial_console: false } },
      attemptMs => waitForRemoteHostTtyACM(false, attemptMs),
      45_000,
    );
    expect(removedACM).toBe("");

    // Verify other USB functions still work (keyboard, mouse)
    await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 10000);
  });

  // ═══════════════════════════════════════════
  // USB SERIAL CONSOLE UI
  // ═══════════════════════════════════════════

  test("usb: USB Serial Console terminal sends and receives data via ttyGS0, also after a reconnect @ssh @serial", async () => {
    await skipWithoutDeviceShell();
    // Budget for the ModemManager-probe wait plus typed-string retries.
    test.setTimeout(150_000);

    test.skip(!process.env.JETKVM_REMOTE_HOST, "JETKVM_REMOTE_HOST not set");

    // Enable serial console
    await callJsonRpc(sharedPage, "setUsbDevices", {
      devices: { ...USB_DEVICES_DEFAULT, serial_console: true },
    });
    await new Promise(r => setTimeout(r, 3000));

    // Find the ttyACM device on the remote host
    const ttyACM = await waitForRemoteHostTtyACM(true);
    expect(ttyACM).toContain("ttyACM");

    // ModemManager probes a freshly created ttyACM port and consumes data,
    // racing with our reader. Wait until nothing on the host holds the port.
    await expect
      .poll(() => remoteHostExec(`sudo lsof -t ${ttyACM} 2>/dev/null || true`).trim(), {
        message: `waiting for ${ttyACM} to be free of host processes`,
        timeout: 30_000,
        intervals: [1000],
      })
      .toBe("");

    // Reload the page so the action bar picks up serial_console enabled state
    await sharedPage.reload({ waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);

    // Verify the USB Serial Console button is visible
    const cdcButton = sharedPage.getByRole("button", { name: "USB Serial Console" });
    await expect(cdcButton).toBeVisible({ timeout: 5000 });

    // Click the button to open the terminal
    await cdcButton.click();
    await new Promise(r => setTimeout(r, 1000));

    const cdcPanel = sharedPage
      .getByRole("heading", { name: "USB Serial Console", exact: true })
      .locator("../..");
    const cdcInput = cdcPanel.locator("textarea.xterm-helper-textarea");
    await expect(cdcInput).toBeFocused();

    // Configure the remote serial port and start a background reader
    remoteHostExec(`sudo stty -F ${ttyACM} 9600 raw -echo`);
    remoteHostExec(`sudo bash -c 'nohup cat ${ttyACM} > /tmp/cdcacm_rx.txt 2>/dev/null &'`);
    await new Promise(r => setTimeout(r, 500));

    // Type into the terminal, retrying: right after the button click the
    // terminal's data channel may still be attaching and drop keystrokes.
    await expect(async () => {
      const sentString = `e2e_test_${Date.now()}`;
      await sharedPage.keyboard.type(sentString, { delay: 50 });
      await new Promise(r => setTimeout(r, 2000));
      const received = remoteHostExec("sudo cat /tmp/cdcacm_rx.txt 2>/dev/null || true").trim();
      expect(received).toContain(sentString);
    }).toPass({ timeout: 20_000 });

    // An app restart drops the peer connection and the UI sets up a new one
    // without a reload. The console must follow it: a data channel left over
    // from the old connection is dead and the typed string never arrives.
    await restartAppViaSSH();
    await waitForWebRTCReady(sharedPage);

    // The host may have re-enumerated the gadget, so point a fresh reader at
    // the port. Kill the old one first or two readers split the data.
    try {
      remoteHostExec("sudo pkill -f 'cat /dev/ttyAC[M]'");
    } catch {
      /* no reader running */
    }
    const ttyAfter = await waitForRemoteHostTtyACM(true);
    // Same ModemManager probe wait as above; returns at once when the port
    // did not re-enumerate.
    await expect
      .poll(() => remoteHostExec(`sudo lsof -t ${ttyAfter} 2>/dev/null || true`).trim(), {
        message: `waiting for ${ttyAfter} to be free of host processes`,
        timeout: 30_000,
        intervals: [1000],
      })
      .toBe("");
    remoteHostExec(`sudo stty -F ${ttyAfter} 9600 raw -echo`);
    remoteHostExec(`sudo bash -c 'nohup cat ${ttyAfter} > /tmp/cdcacm_rx.txt 2>/dev/null &'`);

    // Keep the terminal open across reconnect and wait for its input to regain
    // focus, so the typed string reaches the console instead of the host HID.
    await expect(sharedPage.getByRole("button", { name: "Use Here", exact: true })).toBeHidden();
    await cdcInput.focus();
    await expect(cdcInput).toBeFocused();
    await expect(async () => {
      const sentString = `e2e_reconnect_${Date.now()}`;
      await sharedPage.keyboard.type(sentString, { delay: 50 });
      await new Promise(r => setTimeout(r, 1000));
      const received = remoteHostExec("sudo cat /tmp/cdcacm_rx.txt 2>/dev/null || true").trim();
      expect(received).toContain(sentString);
    }).toPass({ timeout: 30_000 });

    // Test receiving data: send from remote host to ttyACM
    const replyString = `reply_${Date.now()}`;
    remoteHostExec(`sudo bash -c 'echo ${replyString} > ${ttyAfter}'`);
    await new Promise(r => setTimeout(r, 2000));

    // Take a screenshot for visual review
    await sharedPage.screenshot({ path: `${process.cwd()}/screenshot.png` });

    // Clean up: kill background cat, remove temp file
    try {
      remoteHostExec("sudo pkill -f cat./dev/ttyACM");
    } catch {
      /* no matching process */
    }
    try {
      remoteHostExec("sudo rm -f /tmp/cdcacm_rx.txt");
    } catch {
      /* ignore */
    }

    // Close the terminal
    await cdcPanel.getByRole("button", { name: "Hide", exact: true }).click();
    await new Promise(r => setTimeout(r, 500));

    // Disable serial console to clean up
    await callJsonRpc(sharedPage, "setUsbDevices", {
      devices: { ...USB_DEVICES_DEFAULT, serial_console: false },
    });
    await new Promise(r => setTimeout(r, 2000));

    // Verify button is gone after disabling
    await sharedPage.reload({ waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);
    await expect(sharedPage.getByRole("button", { name: "USB Serial Console" })).not.toBeVisible({
      timeout: 5000,
    });
  });

  // ═══════════════════════════════════════════
  // USB RECOVERY
  // ═══════════════════════════════════════════

  test("usb-recovery: auto-recovers USB gadget after UDC unbind @ssh", async () => {
    await skipWithoutDeviceShell();
    test.setTimeout(90_000);

    await waitForUdcState("configured", 10_000);
    await sshExec(`echo ${UDC_NAME} > ${DWC3_PATH}/unbind 2>/dev/null`, true);

    try {
      await waitForUdcState("configured", 30_000);

      await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 10000);
      await waitForWebRTCReady(sharedPage, 15_000);

      const deadline = Date.now() + 45_000;
      let keyboardRecovered = false;
      let mouseRecovered = false;

      // After gadget re-enumeration, host input device permissions and event
      // nodes can flap briefly. Retry both paths until they stabilize.
      while (Date.now() < deadline && (!keyboardRecovered || !mouseRecovered)) {
        if (!keyboardRecovered) {
          try {
            const keyEvents = await agent!.expectKeyPress(
              KEY.SPACE,
              async () => {
                await tapKey(sharedPage, HID_KEY.SPACE);
              },
              1500,
            );
            keyboardRecovered = keyEvents.length > 0;
          } catch {
            /* retry */
          }
        }

        if (!mouseRecovered) {
          try {
            const mouseEvents = await agent!.expectMouseMove(async () => {
              await sendAbsMouseMove(sharedPage, 0, 0);
              await new Promise(resolve => setTimeout(resolve, 50));
              await sendAbsMouseMove(sharedPage, 32767, 32767);
            }, 1500);
            mouseRecovered = mouseEvents.length > 0;
          } catch {
            /* retry */
          }
        }

        if (!keyboardRecovered || !mouseRecovered) {
          await new Promise(resolve => setTimeout(resolve, 250));
        }
      }

      expect(keyboardRecovered, "keyboard input should recover after UDC rebind").toBe(true);
      expect(mouseRecovered, "mouse input should recover after UDC rebind").toBe(true);
    } catch (err) {
      // If the test fails, the UDC may still be unbound, leaving the device
      // stuck in a crash loop at initUsbGadget. Re-bind the UDC and reboot
      // to restore a clean state for subsequent tests.
      try {
        await sshExec(`echo ${UDC_NAME} > ${DWC3_PATH}/bind 2>/dev/null`, true);
      } catch {
        /* best effort */
      }
      await rebootDeviceViaSSH();
      await sharedPage.goto("/", { waitUntil: "networkidle" });
      await waitForWebRTCReady(sharedPage);
      throw err;
    }
  });

  // ═══════════════════════════════════════════
  // USB REMOTE WAKEUP: S3 SUSPEND → EXPLICIT WAKE
  // ═══════════════════════════════════════════

  test("usb-wake: browser input on no-video overlay does not wake S3 host", async () => {
    test.setTimeout(180_000);

    await putRemoteHostToSleepForUSBWakeTest(sharedPage, {
      includeRootHubWake: false,
      wakeAfterSeconds: 90,
    });

    try {
      await sendBrowserInputThatMustNotWakeHost(sharedPage);
      await expectHostStaysAsleep(30_000);
    } finally {
      await recoverRemoteHostAfterUSBWakeTest(sharedPage);
    }
  });

  test("usb-wake: no-video Wake button wakes S3 host", async () => {
    test.setTimeout(210_000);

    test.skip(
      !(await agent!.health()),
      "Remote host is already asleep; cannot schedule an RTC fallback",
    );

    await putRemoteHostToSleepForUSBWakeTest(sharedPage, {
      includeRootHubWake: false,
      wakeAfterSeconds: 90,
    });

    let wokeWithButton = false;
    try {
      await wakeRemoteHostWithWakeButton(sharedPage);
      wokeWithButton = true;
    } finally {
      if (!wokeWithButton) {
        await recoverRemoteHostAfterUSBWakeTest(sharedPage);
      }
    }

    await new Promise(r => setTimeout(r, 5000));
    await sharedPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);
    await waitForVideoDimensions(sharedPage, 30000);

    const postEvents = await waitForKeyboardReady(agent!, sharedPage);
    expect(postEvents.length, "keyboard should work after S3 Wake button wake").toBeGreaterThan(0);
  });

  test("usb-wake: S3 suspend and wake via explicit wake RPC", async () => {
    test.setTimeout(180_000);

    await putRemoteHostToSleepForUSBWakeTest(sharedPage, {
      includeRootHubWake: false,
      wakeAfterSeconds: 90,
    });

    await callJsonRpc(sharedPage, "wakeHost");

    // Poll until host wakes up (remote agent responds again)
    const wakeDeadline = Date.now() + 30000;
    let hostUp = false;
    try {
      while (Date.now() < wakeDeadline) {
        if (await agent!.health()) {
          hostUp = true;
          break;
        }
        // Re-send wake signal periodically in case the first was lost
        try {
          await callJsonRpc(sharedPage, "wakeHost");
        } catch {
          /* RPC may fail if WebRTC is reconnecting */
        }
        await new Promise(r => setTimeout(r, 2000));
      }
    } finally {
      if (!hostUp) {
        await recoverRemoteHostAfterUSBWakeTest(sharedPage);
      }
    }
    expect(hostUp, "Host should wake from S3 after explicit wake RPC").toBe(true);

    // Wait for video stream to recover after host resume
    await new Promise(r => setTimeout(r, 5000));

    // Reload page to re-establish clean WebRTC session
    await sharedPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);

    await waitForVideoDimensions(sharedPage, 30000);

    // Verify keyboard works after wake
    const postEvents = await waitForKeyboardReady(agent!, sharedPage);
    expect(postEvents.length, "keyboard should work after S3 wake").toBeGreaterThan(0);
  });

  test("usb-wake: regular HID reports do not wake from S3", async () => {
    test.setTimeout(180_000);

    await putRemoteHostToSleepForUSBWakeTest(sharedPage, {
      includeRootHubWake: false,
      wakeAfterSeconds: 90,
    });

    // Keyboard/mouse HID endpoints have wakeup_on_write=0; only the dedicated
    // wake HID (sent via the wakeHost RPC) can wake the host from S3.
    await callJsonRpc(sharedPage, "relMouseReport", { dx: 8, dy: 0, buttons: 0 });
    await callJsonRpc(sharedPage, "relMouseReport", { dx: 0, dy: 0, buttons: 1 });
    await callJsonRpc(sharedPage, "relMouseReport", { dx: 0, dy: 0, buttons: 0 });
    await callJsonRpc(sharedPage, "keyboardReport", {
      keys: [0x2c, 0, 0, 0, 0, 0],
      modifier: 0,
    });
    await callJsonRpc(sharedPage, "keyboardReport", {
      keys: [0, 0, 0, 0, 0, 0],
      modifier: 0,
    });

    await new Promise(r => setTimeout(r, 5000));
    expect(await agent!.health(), "Host should stay asleep after regular HID reports").toBe(false);

    await callJsonRpc(sharedPage, "wakeHost");

    const wakeDeadline = Date.now() + 30000;
    let hostUp = false;
    try {
      while (Date.now() < wakeDeadline) {
        if (await agent!.health()) {
          hostUp = true;
          break;
        }
        try {
          await callJsonRpc(sharedPage, "wakeHost");
        } catch {
          /* best effort */
        }
        await new Promise(r => setTimeout(r, 2000));
      }
    } finally {
      if (!hostUp) {
        await recoverRemoteHostAfterUSBWakeTest(sharedPage);
      }
    }
    expect(hostUp, "Host should wake from S3 after explicit wake RPC").toBe(true);

    await new Promise(r => setTimeout(r, 5000));
    await sharedPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);
    await waitForVideoDimensions(sharedPage, 30000);

    const postEvents = await waitForKeyboardReady(agent!, sharedPage);
    expect(postEvents.length, "keyboard should work after S3 wake").toBeGreaterThan(0);
  });
});
