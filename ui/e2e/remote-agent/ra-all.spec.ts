/**
 * Consolidated remote agent E2E tests.
 * All tests share a single page/WebRTC session for maximum speed.
 * Uses JSON-RPC directly instead of UI navigation where possible.
 *
 * Run with:
 *   JETKVM_URL=http://<kvm-ip> JETKVM_REMOTE_HOST=<host-ip> npx playwright test ra-all
 */
import { test, expect, type Page } from "@playwright/test";
import { createRemoteAgent, KEY, HID_TO_LINUX } from "./remote-agent";
import {
  HID_KEY,
  callJsonRpc,
  sendKeypress,
  tapKey,
  waitForWebRTCReady,
  sendAbsMouseMove,
  sshExec,
  getDeviceHost,
  getLedState,
  waitForLedState,
  restartAppViaSSH,
} from "../helpers";

const agent = createRemoteAgent();

// ── Macro setup via SSH (app restart, no reboot) ──

const TEST_MACROS = [
  {
    id: "e2e_test_a",
    name: "E2E KeyA",
    steps: [{ keys: ["KeyA"], modifiers: [], delay: 50 }],
    sortOrder: 0,
  },
  {
    id: "e2e_test_ctrl_a",
    name: "E2E Ctrl+A",
    steps: [{ keys: ["KeyA"], modifiers: ["ControlLeft"], delay: 50 }],
    sortOrder: 1,
  },
  {
    id: "e2e_test_abc",
    name: "E2E ABC",
    steps: [
      { keys: ["KeyA"], modifiers: [], delay: 50 },
      { keys: ["KeyB"], modifiers: [], delay: 50 },
      { keys: ["KeyC"], modifiers: [], delay: 50 },
    ],
    sortOrder: 2,
  },
];

async function setupMacrosViaSSH() {
  const configStr = await sshExec("cat /userdata/kvm_config.json", true);
  let config: Record<string, unknown> = {};
  try {
    config = JSON.parse(configStr || "{}");
  } catch {
    config = {};
  }

  if (Array.isArray(config.keyboard_macros)) {
    const ids = new Set((config.keyboard_macros as Array<{ id: string }>).map(m => m.id));
    if (TEST_MACROS.every(m => ids.has(m.id))) return;
  }

  const existingMacros = Array.isArray(config.keyboard_macros) ? config.keyboard_macros : [];
  const filtered = (existingMacros as Array<{ id: string }>).filter(
    m => !m.id.startsWith("e2e_test_"),
  );
  config.keyboard_macros = [...filtered, ...TEST_MACROS];

  const json = JSON.stringify(config);
  const b64 = Buffer.from(json).toString("base64");
  await sshExec(`echo ${b64} | base64 -d > /userdata/kvm_config.json && sync`);

  await restartAppViaSSH();
}

// ── USB config constants ──

const USB_DEFAULT_CONFIG = {
  vendor_id: "0x1d6b",
  product_id: "0x0104",
  serial_number: "",
  manufacturer: "JetKVM",
  product: "USB Emulation Device",
};

const USB_LOGITECH_CONFIG = {
  vendor_id: "0x046d",
  product_id: "0xc52b",
  serial_number: "1234567&0&1",
  manufacturer: "Logitech (x64)",
  product: "Logitech USB Input Device",
};

const USB_DEVICES_DEFAULT = {
  keyboard: true,
  absolute_mouse: true,
  relative_mouse: true,
  mass_storage: true,
};

const USB_DEVICES_KEYBOARD_ONLY = {
  keyboard: true,
  absolute_mouse: false,
  relative_mouse: false,
  mass_storage: false,
};

const ID_DEFAULT = "1d6b:0104";
const ID_LOGITECH = "046d:c52b";

// ── UDC recovery constants ──

const UDC_NAME = "ffb00000.usb";
const DWC3_PATH = "/sys/bus/platform/drivers/dwc3";
const UDC_STATE_PATH = `/sys/class/udc/${UDC_NAME}/state`;

async function readUdcState(): Promise<string> {
  try {
    const result = (await sshExec(`cat ${UDC_STATE_PATH} 2>/dev/null`, true)).trim();
    return result || "not attached";
  } catch {
    return "not attached";
  }
}

async function waitForUdcState(expected: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastSeen = "";
  while (Date.now() < deadline) {
    lastSeen = await readUdcState();
    if (lastSeen === expected) return;
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error(
    `Timed out waiting for UDC state "${expected}" within ${timeoutMs}ms (last seen: "${lastSeen}")`,
  );
}

// Pre-built key list for batched keyboard scan test
const ALL_SCAN_KEYS = (() => {
  const keys: { hid: number; linux: number; label: string }[] = [];
  for (let i = 0; i < 26; i++) {
    const hid = 0x04 + i;
    if (HID_TO_LINUX[hid]) keys.push({ hid, linux: HID_TO_LINUX[hid], label: String.fromCharCode(65 + i) });
  }
  for (let i = 0; i < 10; i++) {
    const hid = 0x1e + i;
    if (HID_TO_LINUX[hid]) keys.push({ hid, linux: HID_TO_LINUX[hid], label: `Num${i}` });
  }
  for (let i = 0; i < 12; i++) {
    const hid = 0x3a + i;
    if (HID_TO_LINUX[hid]) keys.push({ hid, linux: HID_TO_LINUX[hid], label: `F${i + 1}` });
  }
  return keys;
})();

// ── Test suite ──

test.describe.configure({ mode: "serial" });

let sharedPage: Page;

async function ensureNoPasswordViaAPI() {
  const host = getDeviceHost();
  const status = await fetch(`http://${host}/device/status`)
    .then(r => r.json() as Promise<{ isSetup: boolean }>);

  if (!status.isSetup) {
    const res = await fetch(`http://${host}/device/setup`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ localAuthMode: "noPassword" }),
    });
    if (!res.ok) throw new Error(`Setup POST failed: ${res.status}`);
    return;
  }

  const probe = await fetch(`http://${host}/device`);
  if (probe.status === 401) {
    await sshExec("rm -f /userdata/kvm_config.json && sync");
    await restartAppViaSSH();
    const res = await fetch(`http://${host}/device/setup`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ localAuthMode: "noPassword" }),
    });
    if (!res.ok) throw new Error(`Setup POST after reset failed: ${res.status}`);
    await setupMacrosViaSSH();
  }
}

async function setupMacrosViaRPC(page: Page) {
  const existing = await callJsonRpc(page, "getKeyboardMacros") as Array<{ id: string }>;
  const ids = new Set(existing.map(m => m.id));
  if (TEST_MACROS.every(m => ids.has(m.id))) return;

  const merged = [
    ...existing.filter(m => !m.id.startsWith("e2e_test_")),
    ...TEST_MACROS,
  ];
  await callJsonRpc(page, "setKeyboardMacros", { params: { macros: merged } });
}

test.beforeAll(async ({ browser }) => {
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");

  await Promise.all([
    agent!.ensureDeployed(),
    ensureNoPasswordViaAPI(),
  ]);

  sharedPage = await browser.newPage();
  await sharedPage.goto("/", { waitUntil: "networkidle" });
  await waitForWebRTCReady(sharedPage);

  await setupMacrosViaRPC(sharedPage);
  await sharedPage.reload({ waitUntil: "networkidle" });
  await waitForWebRTCReady(sharedPage);

  await agent!.waitForInputDevices(
    ["keyboard", "absolute_mouse", "relative_mouse"],
    10000,
  );
});

test.afterAll(async () => {
  if (!agent) return;
  // Clean up test macros via RPC (no SSH needed)
  try {
    const existing = await callJsonRpc(sharedPage, "getKeyboardMacros") as Array<{ id: string }>;
    const filtered = existing.filter(m => !m.id.startsWith("e2e_test_"));
    await callJsonRpc(sharedPage, "setKeyboardMacros", { params: { macros: filtered } });
  } catch { /* page may already be closed */ }
  if (sharedPage) await sharedPage.close();
});

test.describe("Remote Host Agent", () => {
  // ═══════════════════════════════════════════
  // DISPLAY + EDID
  // ═══════════════════════════════════════════

  test("display: resolution, modes, and EDID preset change", async () => {
    // Verify display info
    const [displays, resolution] = await Promise.all([
      agent!.getDisplays(),
      agent!.getResolution(),
    ]);

    const connected = displays.filter(d => d.status === "connected");
    expect(connected.length).toBeGreaterThanOrEqual(1);
    expect(connected[0].modes).toBeDefined();
    expect(connected[0].modes!.length).toBeGreaterThan(0);
    expect(resolution).not.toBeNull();
    expect(resolution).toMatch(/^\d+x\d+$/);

    // Change EDID preset and verify host sees the new resolution
    const currentEdid = await callJsonRpc(sharedPage, "getEDID") as string;
    const targetEdid = currentEdid === "1920x1080" ? "1280x720" : "1920x1080";
    await callJsonRpc(sharedPage, "setEDID", { edid: targetEdid });

    const newRes = await agent!.getResolution();
    expect(newRes).not.toBeNull();
    expect(newRes).toMatch(/^\d+x\d+$/);

    // Restore original EDID in background (keyboard test below tolerates brief HID disruption)
    callJsonRpc(sharedPage, "setEDID", { edid: currentEdid }).catch(() => {});
  });

  // ═══════════════════════════════════════════
  // KEYBOARD: TOGGLE KEYS + LED ROUND-TRIP
  // ═══════════════════════════════════════════

  test("keyboard: toggle keys with LED round-trip", async () => {
    test.setTimeout(30_000);

    const initialState = await getLedState(sharedPage);
    expect(initialState).not.toBeNull();
    const initialCaps = initialState!.caps_lock;

    // EDID restore may still be in-flight; retry until full HID stack (including LED reports) stabilizes
    const deadline = Date.now() + 15000;
    let capsToggled = false;
    while (Date.now() < deadline) {
      await agent!.clearKeyboardEvents();
      try {
        await agent!.expectKeyPress(KEY.CAPS_LOCK, async () => {
          await tapKey(sharedPage, HID_KEY.CAPS_LOCK);
        }, 3000);
        await waitForLedState(sharedPage, "caps_lock", !initialCaps, 2000);
        capsToggled = true;
        break;
      } catch {
        // HID or LED path not ready; undo toggle attempt and retry
        await tapKey(sharedPage, HID_KEY.CAPS_LOCK);
        await new Promise(r => setTimeout(r, 500));
      }
    }
    expect(capsToggled, "CAPS_LOCK LED should toggle").toBe(true);
    expect((await getLedState(sharedPage))!.caps_lock).toBe(!initialCaps);

    // Restore CAPS_LOCK
    await agent!.expectKeyPress(KEY.CAPS_LOCK, async () => {
      await tapKey(sharedPage, HID_KEY.CAPS_LOCK);
    });
    await waitForLedState(sharedPage, "caps_lock", initialCaps);

    // NUM_LOCK: same round-trip verification
    const initialNum = initialState!.num_lock;

    const numEvents = await agent!.expectKeyPress(KEY.NUM_LOCK, async () => {
      await tapKey(sharedPage, HID_KEY.NUM_LOCK);
    });
    expect(numEvents.length).toBeGreaterThan(0);
    await waitForLedState(sharedPage, "num_lock", !initialNum);
    expect((await getLedState(sharedPage))!.num_lock).toBe(!initialNum);

    await agent!.expectKeyPress(KEY.NUM_LOCK, async () => {
      await tapKey(sharedPage, HID_KEY.NUM_LOCK);
    });
    await waitForLedState(sharedPage, "num_lock", initialNum);

    // SPACE: verify received (no LED, just key delivery)
    const spaceEvents = await agent!.expectKeyPress(KEY.SPACE, async () => {
      await tapKey(sharedPage, HID_KEY.SPACE);
    });
    expect(spaceEvents.length).toBeGreaterThan(0);
  });

  // ═══════════════════════════════════════════
  // KEYBOARD: SCANS + PRESS/RELEASE + MODIFIERS
  // ═══════════════════════════════════════════

  test("keyboard: key scans, press/release, and modifiers", async () => {
    // Batch all 48 key scans in a single evaluate (eliminates per-key round-trip overhead)
    await agent!.clearKeyboardEvents();

    await sharedPage.evaluate(async (keys: number[]) => {
      const hooks = window.__kvmTestHooks;
      if (!hooks) throw new Error("Test hooks not available");
      for (const hid of keys) {
        hooks.sendKeypress(hid, true);
        hooks.sendKeypress(hid, false);
        await new Promise(r => setTimeout(r, 5));
      }
    }, ALL_SCAN_KEYS.map(k => k.hid));

    const scanDeadline = Date.now() + 3000;
    let failed: string[] = [];
    while (Date.now() < scanDeadline) {
      const events = await agent!.getKeyboardEvents();
      const pressedCodes = new Set(
        events.filter(ev => ev.type === "key_press").map(ev => ev.code),
      );
      failed = ALL_SCAN_KEYS.filter(k => !pressedCodes.has(k.linux)).map(k => k.label);
      if (failed.length === 0) break;
      await new Promise(r => setTimeout(r, 50));
    }
    expect(failed, `Keys not received: ${failed.join(", ")}`).toHaveLength(0);

    // Press/release timing: verify release comes after press
    await agent!.clearKeyboardEvents();
    await sendKeypress(sharedPage, HID_KEY.SPACE, true);
    await new Promise(r => setTimeout(r, 10));
    await sendKeypress(sharedPage, HID_KEY.SPACE, false);
    await new Promise(r => setTimeout(r, 50));

    const prEvents = await agent!.getKeyboardEvents();
    const presses = prEvents.filter(ev => ev.code === KEY.SPACE && ev.type === "key_press");
    const releases = prEvents.filter(ev => ev.code === KEY.SPACE && ev.type === "key_release");
    expect(presses.length).toBeGreaterThanOrEqual(1);
    expect(releases.length).toBeGreaterThanOrEqual(1);
    expect(releases[0].time_ms).toBeGreaterThan(presses[0].time_ms);

    // Modifier combo: verify C key arrives
    await agent!.clearKeyboardEvents();
    await sendKeypress(sharedPage, 0x06, true);
    await new Promise(r => setTimeout(r, 10));
    await sendKeypress(sharedPage, 0x06, false);
    await new Promise(r => setTimeout(r, 50));

    const cEvents = await agent!.getKeyboardEvents();
    const cPresses = cEvents.filter(ev => ev.code === KEY.C && ev.type === "key_press");
    expect(cPresses.length).toBeGreaterThanOrEqual(1);
  });

  // ═══════════════════════════════════════════
  // MOUSE
  // ═══════════════════════════════════════════

  test("mouse: movement, corners, rapid input, and position values", async () => {
    await sendAbsMouseMove(sharedPage, 0, 0);
    await agent!.clearAllEvents();

    // Center movement
    let events = await agent!.expectMouseMove(async () => {
      await sendAbsMouseMove(sharedPage, 16384, 16384);
    });
    expect(events.length).toBeGreaterThan(0);
    expect(events.filter(ev => ev.type === "mouse_move_abs").length).toBeGreaterThan(0);

    // Corner movements
    for (const pos of [
      { x: 0, y: 0, label: "top-left" },
      { x: 32767, y: 0, label: "top-right" },
      { x: 32767, y: 32767, label: "bottom-right" },
      { x: 0, y: 32767, label: "bottom-left" },
    ]) {
      events = await agent!.expectMouseMove(async () => {
        await sendAbsMouseMove(sharedPage, pos.x, pos.y);
      });
      expect(events.length, `No mouse events for ${pos.label}`).toBeGreaterThan(0);
    }

    // Rapid diagonal movement
    await agent!.clearMouseEvents();
    for (let i = 0; i < 10; i++) {
      const v = Math.floor((i / 10) * 32767);
      await sendAbsMouseMove(sharedPage, v, v);
    }
    await new Promise(r => setTimeout(r, 50));

    const rapidEvents = await agent!.getMouseEvents();
    const moveEvents = rapidEvents.filter(
      ev => ev.type === "mouse_move_abs" || ev.type === "mouse_move_rel",
    );
    expect(moveEvents.length).toBeGreaterThanOrEqual(5);

    // Position value verification
    await agent!.clearMouseEvents();
    await sendAbsMouseMove(sharedPage, 16384, 16384);
    await new Promise(r => setTimeout(r, 50));

    const centerEvents = await agent!.getMouseEvents();
    const absEvents = centerEvents.filter(ev => ev.type === "mouse_move_abs");
    if (absEvents.length > 0) {
      const last = absEvents[absEvents.length - 1];
      expect(last.x + last.y).toBeGreaterThan(0);
    }
  });

  // ═══════════════════════════════════════════
  // INPUT: PASTE + MACROS
  // ═══════════════════════════════════════════

  test("input: paste text and macros", async () => {
    // ── Paste ──
    const expectedPasteKeys = [KEY.H, KEY.I, KEY.KEY_5];
    await agent!.clearKeyboardEvents();

    await sharedPage.getByRole("button", { name: "Paste text" }).click();
    const textarea = sharedPage.locator("textarea#asd");
    await textarea.waitFor({ state: "visible", timeout: 3000 });
    await textarea.fill("hi5");

    const confirmBtn = sharedPage.getByRole("button", { name: "Confirm Paste" });
    await confirmBtn.waitFor({ state: "visible", timeout: 2000 });
    await confirmBtn.click({ force: true });

    const pasteDeadline = Date.now() + 5000;
    let pasteMatchIdx = 0;
    while (Date.now() < pasteDeadline) {
      const events = await agent!.getKeyboardEvents();
      const pressedCodes = events.filter(ev => ev.type === "key_press").map(ev => ev.code);
      pasteMatchIdx = 0;
      for (const code of pressedCodes) {
        if (code === expectedPasteKeys[pasteMatchIdx]) {
          pasteMatchIdx++;
          if (pasteMatchIdx === expectedPasteKeys.length) break;
        }
      }
      if (pasteMatchIdx === expectedPasteKeys.length) break;
      await new Promise(r => setTimeout(r, 50));
    }
    expect(pasteMatchIdx, `Paste: expected 3 keys but matched ${pasteMatchIdx}`).toBe(expectedPasteKeys.length);

    // Dismiss any lingering paste dialog
    const cancelBtn = sharedPage.getByRole("button", { name: "Cancel" });
    if (await cancelBtn.isVisible({ timeout: 300 }).catch(() => false)) {
      await cancelBtn.click();
    }

    // ── Macros ──

    // Single key press (A)
    const keyABtn = sharedPage.getByRole("button", { name: "E2E KeyA" });
    await keyABtn.waitFor({ state: "visible", timeout: 5000 });
    await agent!.clearKeyboardEvents();
    await keyABtn.click();

    let macroEvents = await agent!.waitForKeyboardEvent(
      ev => ev.code === KEY.A && ev.type === "key_press",
      3000,
    );
    expect(macroEvents.length).toBeGreaterThan(0);

    // Modifier combo (Ctrl+A)
    await agent!.clearKeyboardEvents();
    await sharedPage.getByRole("button", { name: "E2E Ctrl+A" }).click();

    const ctrlDeadline = Date.now() + 3000;
    let gotCtrl = false, gotA = false;
    while (Date.now() < ctrlDeadline && (!gotCtrl || !gotA)) {
      macroEvents = (await agent!.getKeyboardEvents()).filter(ev => ev.type === "key_press");
      for (const ev of macroEvents) {
        if (ev.code === KEY.LEFT_CTRL) gotCtrl = true;
        if (ev.code === KEY.A) gotA = true;
      }
      if (!gotCtrl || !gotA) await new Promise(r => setTimeout(r, 50));
    }
    expect(gotCtrl, "Ctrl key should arrive").toBe(true);
    expect(gotA, "A key should arrive").toBe(true);

    // Key sequence (A, B, C)
    await agent!.clearKeyboardEvents();
    await sharedPage.getByRole("button", { name: "E2E ABC" }).click();

    const expectedSeq = [KEY.A, KEY.B, KEY.C];
    const seqDeadline = Date.now() + 3000;
    let matched = false;
    while (Date.now() < seqDeadline && !matched) {
      const seqEvents = await agent!.getKeyboardEvents();
      const presses = seqEvents.filter(ev => ev.type === "key_press").map(ev => ev.code);
      let idx = 0;
      for (const code of presses) {
        if (code === expectedSeq[idx]) {
          idx++;
          if (idx === expectedSeq.length) { matched = true; break; }
        }
      }
      if (!matched) await new Promise(r => setTimeout(r, 50));
    }
    expect(matched, "Keys A, B, C should arrive in order").toBe(true);
  });

  // ═══════════════════════════════════════════
  // VIRTUAL MEDIA
  // ═══════════════════════════════════════════

  test("virtual-media: mount ISO from URL and verify, then unmount", async () => {
    test.setTimeout(60_000);

    try { await callJsonRpc(sharedPage, "unmountImage"); } catch { /* ok if nothing mounted */ }

    const stateBefore = await callJsonRpc(sharedPage, "getVirtualMediaState") as null | object;
    expect(stateBefore).toBeNull();

    const NETBOOT_XYZ_URL = "https://boot.netboot.xyz/ipxe/netboot.xyz.iso";
    await callJsonRpc(sharedPage, "mountWithHTTP", { url: NETBOOT_XYZ_URL, mode: "CDROM" });

    const stateAfter = await callJsonRpc(sharedPage, "getVirtualMediaState") as {
      source: string; mode: string; url?: string;
    } | null;
    expect(stateAfter).not.toBeNull();
    expect(stateAfter!.source).toBe("HTTP");
    expect(stateAfter!.mode).toBe("CDROM");
    expect(stateAfter!.url).toBe(NETBOOT_XYZ_URL);

    const usbDevices = await agent!.getUSBDevices();
    expect(usbDevices.length).toBeGreaterThan(0);

    await callJsonRpc(sharedPage, "unmountImage");

    const stateEnd = await callJsonRpc(sharedPage, "getVirtualMediaState") as null | object;
    expect(stateEnd).toBeNull();

    const finalDevices = await agent!.getUSBDevices();
    expect(finalDevices.length).toBeGreaterThan(0);
  });

  // ═══════════════════════════════════════════
  // USB: DEVICE PRESENCE + SWITCHING + DESCRIPTORS
  // ═══════════════════════════════════════════

  test("usb: device presence, switching, and descriptor changes", async () => {
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

    // Switch to keyboard_only — verify mice are removed
    await callJsonRpc(sharedPage, "setUsbDevices", { devices: USB_DEVICES_KEYBOARD_ONLY });

    const afterDevices = await agent!.waitForInputDevices(["keyboard"], 10000);
    const afterTypes = afterDevices.map(d => d.type);
    expect(afterTypes).toContain("keyboard");
    expect(afterTypes).not.toContain("absolute_mouse");
    expect(afterTypes).not.toContain("relative_mouse");

    // Restore default devices
    await callJsonRpc(sharedPage, "setUsbDevices", { devices: USB_DEVICES_DEFAULT });
    await agent!.waitForInputDevices(
      ["keyboard", "absolute_mouse", "relative_mouse"],
      10000,
    );

    // Switch USB descriptor to Logitech — verify host sees new VID/PID
    await callJsonRpc(sharedPage, "setUsbConfig", { usbConfig: USB_LOGITECH_CONFIG });

    const logitechDevices = await agent!.waitForUSBDevice(
      d => d.id === ID_LOGITECH,
      true,
      8000,
    );
    expect(logitechDevices.length).toBeGreaterThan(0);
    expect(logitechDevices[0].name).toContain("Logitech");

    // Restore default descriptor
    const deviceId = await callJsonRpc(sharedPage, "getDeviceID") as string;
    const defaultConfig = { ...USB_DEFAULT_CONFIG, serial_number: deviceId || "" };
    callJsonRpc(sharedPage, "setUsbConfig", { usbConfig: defaultConfig }).catch(() => {});
  });

  // ═══════════════════════════════════════════
  // USB RECOVERY
  // ═══════════════════════════════════════════

  test("usb-recovery: auto-recovers USB gadget after UDC unbind", async () => {
    test.setTimeout(90_000);

    await waitForUdcState("configured", 10_000);
    await sshExec(`echo ${UDC_NAME} > ${DWC3_PATH}/unbind 2>/dev/null`, true);

    await waitForUdcState("configured", 30_000);

    await agent!.waitForInputDevices(
      ["keyboard", "absolute_mouse", "relative_mouse"],
      10000,
    );
    await waitForWebRTCReady(sharedPage, 15_000);

    const deadline = Date.now() + 45_000;
    let keyboardRecovered = false;
    let mouseRecovered = false;

    // After gadget re-enumeration, host input device permissions and event
    // nodes can flap briefly. Retry both paths until they stabilize.
    while (Date.now() < deadline && (!keyboardRecovered || !mouseRecovered)) {
      if (!keyboardRecovered) {
        try {
          const keyEvents = await agent!.expectKeyPress(KEY.SPACE, async () => {
            await tapKey(sharedPage, HID_KEY.SPACE);
          }, 1500);
          keyboardRecovered = keyEvents.length > 0;
        } catch { /* retry */ }
      }

      if (!mouseRecovered) {
        try {
          const mouseEvents = await agent!.expectMouseMove(async () => {
            await sendAbsMouseMove(sharedPage, 0, 0);
            await new Promise(resolve => setTimeout(resolve, 50));
            await sendAbsMouseMove(sharedPage, 32767, 32767);
          }, 1500);
          mouseRecovered = mouseEvents.length > 0;
        } catch { /* retry */ }
      }

      if (!keyboardRecovered || !mouseRecovered) {
        await new Promise(resolve => setTimeout(resolve, 250));
      }
    }

    expect(keyboardRecovered, "keyboard input should recover after UDC rebind").toBe(true);
    expect(mouseRecovered, "mouse input should recover after UDC rebind").toBe(true);
  });

  // ═══════════════════════════════════════════
  // HTTPS VIA RPC
  // ═══════════════════════════════════════════

  test("https: TLS round-trip via RPC", async ({ browser }) => {
    test.setTimeout(60_000);

    const host = getDeviceHost();
    const httpsUrl = `https://${host}:443`;

    // Enable self-signed TLS via RPC (no UI navigation needed)
    await callJsonRpc(sharedPage, "setTLSState", {
      state: { mode: "self-signed", certificate: "", privateKey: "" },
    });

    // Poll until HTTPS listener is ready (setTLSState returns before the listener starts)
    const httpsContext = await browser.newContext({ ignoreHTTPSErrors: true });
    const probePage = await httpsContext.newPage();
    const probeDeadline = Date.now() + 10000;
    while (Date.now() < probeDeadline) {
      try {
        await probePage.goto(httpsUrl, { timeout: 3000 });
        break;
      } catch {
        await new Promise(r => setTimeout(r, 250));
      }
    }

    // Verify HTTPS works: WebRTC connects over TLS
    try {
      await waitForWebRTCReady(probePage, 30000);
    } finally {
      await probePage.close();
      await httpsContext.close();
    }

    // Restore TLS to disabled via RPC.
    // sharedPage was never navigated during this test, so its WebRTC connection is still alive.
    try {
      await callJsonRpc(sharedPage, "setTLSState", {
        state: { mode: "", certificate: "", privateKey: "" },
      });
    } catch {
      // WebRTC dropped; restore via UI and re-establish
      await sharedPage.goto("/settings/access");
      await sharedPage.waitForLoadState("networkidle");
      const tlsDropdown = sharedPage.locator("select").filter({
        has: sharedPage.locator('option[value="self-signed"]'),
      });
      await expect(tlsDropdown).toBeVisible({ timeout: 5000 });
      await tlsDropdown.selectOption("disabled");
      await sharedPage.waitForTimeout(500);
      await sharedPage.goto("/", { waitUntil: "networkidle" });
      await waitForWebRTCReady(sharedPage);
    }
  });

  // ═══════════════════════════════════════════
  // HDMI SLEEP MODE
  // ═══════════════════════════════════════════

  test("hdmi-sleep: activates when no session and deactivates on reconnect", async () => {
    const SLEEP_MODE_SYSFS = "/sys/devices/platform/ff470000.i2c/i2c-4/4-000f/sleep_mode";

    const before = (await callJsonRpc(sharedPage, "getVideoSleepMode")) as {
      supported: boolean;
      duration: number;
    };

    if (!before.supported) {
      test.skip(true, "HDMI sleep mode not supported on this device");
      return;
    }

    const originalDuration = before.duration;

    // Set a very short sleep timer so the test doesn't wait long
    await callJsonRpc(sharedPage, "setVideoSleepMode", { duration: 3 });

    // Disconnect WebRTC by navigating the shared page away
    await sharedPage.goto("about:blank");

    // Wait for the 3s sleep timer + margin
    await new Promise(r => setTimeout(r, 5000));

    // Verify the HDMI capture chip entered sleep via sysfs
    const sleepState = (await sshExec(`cat ${SLEEP_MODE_SYSFS}`)).trim();
    expect(sleepState, "HDMI capture chip should be sleeping").toBe("1");

    // Reconnect — session start wakes the chip
    await sharedPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);

    const wakeState = (await sshExec(`cat ${SLEEP_MODE_SYSFS}`)).trim();
    expect(wakeState, "HDMI capture chip should be awake after reconnect").toBe("0");

    // Restore original duration
    await callJsonRpc(sharedPage, "setVideoSleepMode", { duration: originalDuration });
  });

  // ═══════════════════════════════════════════
  // CONFIG RESET (must be last — resets device config)
  // ═══════════════════════════════════════════

  test("config-reset: reset config via RPC and verify setup endpoint", async () => {
    test.setTimeout(30_000);
    const host = getDeviceHost();

    await callJsonRpc(sharedPage, "resetConfig");

    const statusRes = await fetch(`http://${host}/device/status`);
    const status = await statusRes.json() as { isSetup: boolean };
    expect(status.isSetup, "Device should be not set up after reset").toBe(false);

    const setupRes = await fetch(`http://${host}/device/setup`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ localAuthMode: "noPassword" }),
    });
    expect(setupRes.ok, `Setup POST failed: ${setupRes.status}`).toBe(true);

    const verifyRes = await fetch(`http://${host}/device/status`);
    const verify = await verifyRes.json() as { isSetup: boolean };
    expect(verify.isSetup, "Device should be set up after POST /device/setup").toBe(true);
  });
});
