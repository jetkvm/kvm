/**
 * Consolidated remote agent E2E tests.
 * All tests share a single page/WebRTC session for maximum speed.
 * Uses JSON-RPC directly instead of UI navigation where possible.
 *
 * Run with:
 *   JETKVM_URL=http://<kvm-ip> JETKVM_REMOTE_HOST=<host-ip> npx playwright test ra-all
 */
import { execSync } from "child_process";
import { test, expect, type Page } from "@playwright/test";
import {
  HID_KEY,
  SSH_OPTS,
  callJsonRpc,
  sendKeypress,
  tapKey,
  waitForWebRTCReady,
  waitForVideoDimensions,
  sendAbsMouseMove,
  sshExec,
  getDeviceHost,
  getLedState,
  waitForLedState,
  restartAppViaSSH,
} from "../helpers";
import {
  createRemoteAgent,
  KEY,
  HID_TO_LINUX,
  type RemoteAgent,
  type MouseEvent as RAMouseEvent,
  type KeyboardEvent as RAKeyboardEvent,
} from "./remote-agent";

/** Run a command on the remote host (the machine whose display is captured by the KVM). */
function remoteHostExec(cmd: string, timeoutMs = 15000): string {
  const target = process.env.JETKVM_REMOTE_HOST;
  if (!target) throw new Error("JETKVM_REMOTE_HOST not set");
  // Use single-quote wrapping. Commands containing single quotes must
  // use the '\'' escape sequence (end quote, literal quote, resume quote).
  const escaped = cmd.replace(/'/g, "'\\''");
  return execSync(`ssh ${SSH_OPTS} ${target} '${escaped}'`, {
    encoding: "utf8",
    timeout: timeoutMs,
  });
}

/**
 * Retry keyboard round-trip (send Space, verify host received it) until success
 * or timeout. Used after USB rebinds where the HID channel needs time to stabilize.
 */
async function waitForKeyboardReady(
  ra: RemoteAgent,
  page: Page,
  timeoutMs = 30000,
  perTryMs = 3000,
): Promise<RAKeyboardEvent[]> {
  const deadline = Date.now() + timeoutMs;
  let events: RAKeyboardEvent[] = [];
  while (Date.now() < deadline) {
    try {
      events = await ra.expectKeyPress(
        KEY.SPACE,
        async () => {
          await tapKey(page, HID_KEY.SPACE);
        },
        perTryMs,
      );
      return events;
    } catch {
      /* HID not ready yet */
    }
  }
  return events;
}

/** Toggle DPMS on the remote host via GNOME ScreenSaver D-Bus API. */
function remoteHostSetDPMS(off: boolean): void {
  remoteHostExec(
    `DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus ` +
      `gdbus call --session --dest org.gnome.ScreenSaver ` +
      `--object-path /org/gnome/ScreenSaver ` +
      `--method org.gnome.ScreenSaver.SetActive ${off ? "true" : "false"}`,
  );
}

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
    const ids = new Set((config.keyboard_macros as { id: string }[]).map(m => m.id));
    if (TEST_MACROS.every(m => ids.has(m.id))) return;
  }

  const existingMacros = Array.isArray(config.keyboard_macros) ? config.keyboard_macros : [];
  const filtered = (existingMacros as { id: string }[]).filter(m => !m.id.startsWith("e2e_test_"));
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

const USB_DEVICES_REL_MOUSE_ONLY = {
  keyboard: true,
  absolute_mouse: false,
  relative_mouse: true,
  mass_storage: true,
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
    if (HID_TO_LINUX[hid])
      keys.push({ hid, linux: HID_TO_LINUX[hid], label: String.fromCharCode(65 + i) });
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
  const status = await fetch(`http://${host}/device/status`).then(
    r => r.json() as Promise<{ isSetup: boolean }>,
  );

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

async function setupMacrosViaRPC(page: Page, retries = 3) {
  for (let attempt = 1; attempt <= retries; attempt++) {
    try {
      const existing = (await callJsonRpc(page, "getKeyboardMacros")) as { id: string }[];
      const ids = new Set(existing.map(m => m.id));
      if (TEST_MACROS.every(m => ids.has(m.id))) return;

      const merged = [...existing.filter(m => !m.id.startsWith("e2e_test_")), ...TEST_MACROS];
      await callJsonRpc(page, "setKeyboardMacros", { params: { macros: merged } });
      return;
    } catch (err) {
      if (attempt === retries) throw err;
      await new Promise(r => setTimeout(r, 2000));
    }
  }
}

async function waitForRpcReady(page: Page, timeoutMs = 30000) {
  const deadline = Date.now() + timeoutMs;
  let reloaded = false;
  while (Date.now() < deadline) {
    const useHereBtn = page.getByRole("button", { name: "Use Here" });
    if (await useHereBtn.isVisible({ timeout: 200 }).catch(() => false)) {
      await useHereBtn.click();
      await new Promise(r => setTimeout(r, 2000));
    }
    try {
      await callJsonRpc(page, "getDeviceID");
      return;
    } catch {
      if (!reloaded && Date.now() > deadline - timeoutMs + 10000) {
        reloaded = true;
        await page.reload({ waitUntil: "networkidle" });
        await waitForWebRTCReady(page);
      }
      await new Promise(r => setTimeout(r, 1000));
    }
  }
  throw new Error(`RPC channel not ready after ${timeoutMs}ms`);
}

test.beforeAll(async ({ browser }) => {
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");

  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);

  sharedPage = await browser.newPage();
  await sharedPage.goto("/", { waitUntil: "networkidle" });

  // If the page redirected to the welcome/setup flow, complete setup and reload
  if (sharedPage.url().includes("/welcome")) {
    const host = getDeviceHost();
    await fetch(`http://${host}/device/setup`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ localAuthMode: "noPassword" }),
    });
    await sharedPage.goto("/", { waitUntil: "networkidle" });
  }

  await waitForWebRTCReady(sharedPage);
  await waitForRpcReady(sharedPage);

  await setupMacrosViaRPC(sharedPage);
  await sharedPage.reload({ waitUntil: "networkidle" });
  await waitForWebRTCReady(sharedPage);
  await waitForRpcReady(sharedPage);

  await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 30000);

  // Wake the remote host display — it may have entered DPMS sleep during
  // the build/deploy phase, preventing the KVM from detecting an HDMI signal.
  try {
    remoteHostSetDPMS(false);
  } catch {
    /* best-effort: not all hosts have GNOME ScreenSaver */
  }

  // Wait for the video stream to be flowing — LED state reports are only
  // reliable once the full WebRTC media pipeline is up.
  // Use a generous timeout: after app restart, the HDMI capture chip may need
  // time to re-detect the signal, especially if the display was in DPMS sleep.
  try {
    await waitForVideoDimensions(sharedPage, 30000);
  } catch {
    // If video dimensions still aren't available, reload the page and retry.
    // This handles the case where the WebRTC session connected before the
    // HDMI signal was detected and the video track never started.
    await sharedPage.reload({ waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);
    await waitForVideoDimensions(sharedPage, 30000);
  }

  // Verify the keyboard HID path works end-to-end before any tests run.
  // After reboot, Init() rebinds the USB gadget and the host needs time
  // to re-enumerate before HID reports are delivered.
  await waitForKeyboardReady(agent!, sharedPage, 15000);
});

test.afterAll(async () => {
  if (!agent) return;
  try {
    const existing = (await callJsonRpc(sharedPage, "getKeyboardMacros")) as { id: string }[];
    const filtered = existing.filter(m => !m.id.startsWith("e2e_test_"));
    await callJsonRpc(sharedPage, "setKeyboardMacros", { params: { macros: filtered } });
  } catch {
    /* page may already be closed */
  }
  if (sharedPage) await sharedPage.close();
});

test.describe("Remote Host Agent", () => {
  // ═══════════════════════════════════════════
  // KEYBOARD: TOGGLE KEYS + LED ROUND-TRIP
  // ═══════════════════════════════════════════

  test("keyboard: toggle keys with LED round-trip", async () => {
    test.setTimeout(30_000);

    const initialState = await getLedState(sharedPage);
    expect(initialState).not.toBeNull();

    // EDID restore may still be in-flight; retry until full HID stack (including LED reports) stabilizes.
    // Re-read caps_lock each iteration so a failed-try + successful-undo doesn't leave us
    // permanently toggling in the wrong direction.
    const deadline = Date.now() + 15000;
    let capsToggled = false;
    let capsBeforeToggle = initialState!.caps_lock;
    while (Date.now() < deadline) {
      capsBeforeToggle = (await getLedState(sharedPage))!.caps_lock;
      await agent!.clearKeyboardEvents();
      try {
        await agent!.expectKeyPress(
          KEY.CAPS_LOCK,
          async () => {
            await tapKey(sharedPage, HID_KEY.CAPS_LOCK);
          },
          3000,
        );
        await waitForLedState(sharedPage, "caps_lock", !capsBeforeToggle, 2000);
        capsToggled = true;
        break;
      } catch {
        await tapKey(sharedPage, HID_KEY.CAPS_LOCK);
        await new Promise(r => setTimeout(r, 500));
      }
    }
    expect(capsToggled, "CAPS_LOCK LED should toggle").toBe(true);
    expect((await getLedState(sharedPage))!.caps_lock).toBe(!capsBeforeToggle);

    // Restore CAPS_LOCK
    await agent!.expectKeyPress(
      KEY.CAPS_LOCK,
      async () => {
        await tapKey(sharedPage, HID_KEY.CAPS_LOCK);
      },
      5000,
    );
    await waitForLedState(sharedPage, "caps_lock", capsBeforeToggle);

    // NUM_LOCK: same round-trip verification
    const initialNum = initialState!.num_lock;

    const numEvents = await agent!.expectKeyPress(
      KEY.NUM_LOCK,
      async () => {
        await tapKey(sharedPage, HID_KEY.NUM_LOCK);
      },
      5000,
    );
    expect(numEvents.length).toBeGreaterThan(0);
    await waitForLedState(sharedPage, "num_lock", !initialNum);
    expect((await getLedState(sharedPage))!.num_lock).toBe(!initialNum);

    await agent!.expectKeyPress(
      KEY.NUM_LOCK,
      async () => {
        await tapKey(sharedPage, HID_KEY.NUM_LOCK);
      },
      5000,
    );
    await waitForLedState(sharedPage, "num_lock", initialNum);

    // SPACE: verify received (no LED, just key delivery)
    const spaceEvents = await agent!.expectKeyPress(
      KEY.SPACE,
      async () => {
        await tapKey(sharedPage, HID_KEY.SPACE);
      },
      5000,
    );
    expect(spaceEvents.length).toBeGreaterThan(0);
  });

  // ═══════════════════════════════════════════
  // DISPLAY + EDID
  // ═══════════════════════════════════════════

  test("display: resolution, modes, and EDID preset change", async () => {
    test.setTimeout(90_000);

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

    const currentEdid = (await callJsonRpc(sharedPage, "getEDID")) as string;
    const targetEdid = currentEdid === "1920x1080" ? "1280x720" : "1920x1080";

    // setEDID may drop the WebSocket/WebRTC connection on some devices,
    // so tolerate RPC timeouts and reconnect afterwards. The remote agent
    // may also briefly become unreachable during display re-negotiation.
    await callJsonRpc(sharedPage, "setEDID", { edid: targetEdid }, 30000).catch(() => {});
    await new Promise(r => setTimeout(r, 5000));
    await sharedPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);
    await waitForRpcReady(sharedPage);

    // Wait for the remote agent to recover and report a resolution.
    // The agent may be briefly unreachable during display re-negotiation.
    let newRes: string | null = null;
    const resDeadline = Date.now() + 15_000;
    while (Date.now() < resDeadline) {
      try {
        newRes = await agent!.getResolution();
        if (newRes && /^\d+x\d+$/.test(newRes)) break;
      } catch {
        /* agent not ready yet */
      }
      await new Promise(r => setTimeout(r, 1000));
    }
    expect(newRes).not.toBeNull();
    expect(newRes).toMatch(/^\d+x\d+$/);

    // Restore original EDID. This triggers USB disconnect/reconnect.
    await callJsonRpc(sharedPage, "setEDID", { edid: currentEdid }, 30000).catch(() => {});
    await new Promise(r => setTimeout(r, 5000));

    await sharedPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);
    await waitForRpcReady(sharedPage);
    await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 15000);

    // Verify keyboard works after EDID changes
    const kbEvents = await waitForKeyboardReady(agent!, sharedPage);
    expect(kbEvents.length, "keyboard should work after EDID restore").toBeGreaterThan(0);
  });

  // ═══════════════════════════════════════════
  // KEYBOARD: SCANS + PRESS/RELEASE + MODIFIERS
  // ═══════════════════════════════════════════

  test("keyboard: key scans, press/release, and modifiers", async () => {
    // Batch all 48 key scans in a single evaluate (eliminates per-key round-trip overhead)
    await agent!.clearKeyboardEvents();

    await sharedPage.evaluate(
      async (keys: number[]) => {
        const hooks = window.__kvmTestHooks;
        if (!hooks) throw new Error("Test hooks not available");
        for (const hid of keys) {
          hooks.sendKeypress(hid, true);
          hooks.sendKeypress(hid, false);
          await new Promise(r => setTimeout(r, 10));
        }
      },
      ALL_SCAN_KEYS.map(k => k.hid),
    );

    const scanDeadline = Date.now() + 5000;
    let failed: string[] = [];
    while (Date.now() < scanDeadline) {
      const events = await agent!.getKeyboardEvents();
      const pressedCodes = new Set(events.filter(ev => ev.type === "key_press").map(ev => ev.code));
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
  // KEYBOARD: MODIFIER AUTO-RELEASE
  // ═══════════════════════════════════════════

  test("keyboard: modifier keys auto-release after timeout", async () => {
    test.setTimeout(15_000);

    const modifiers = [
      { hid: 0xe0, linux: KEY.LEFT_CTRL, label: "LeftCtrl" },
      { hid: 0xe1, linux: KEY.LEFT_SHIFT, label: "LeftShift" },
      { hid: 0xe2, linux: KEY.LEFT_ALT, label: "LeftAlt" },
    ];

    for (const { hid, linux, label } of modifiers) {
      await agent!.clearKeyboardEvents();

      // Call keypressReport directly via JSON-RPC to bypass the browser's
      // keepalive timer, which would otherwise extend the auto-release indefinitely.
      await callJsonRpc(sharedPage, "keypressReport", { key: hid, press: true });
      await new Promise(r => setTimeout(r, 300));

      const events = await agent!.getKeyboardEvents();
      const presses = events.filter(ev => ev.code === linux && ev.type === "key_press");
      const releases = events.filter(ev => ev.code === linux && ev.type === "key_release");

      expect(presses.length, `${label} press should be received`).toBeGreaterThanOrEqual(1);
      expect(releases.length, `${label} should auto-release after timeout`).toBeGreaterThanOrEqual(
        1,
      );
    }
  });

  // ═══════════════════════════════════════════
  // KEYBOARD: KEEPALIVE & AUTO-RELEASE
  // ═══════════════════════════════════════════

  test("keepalive: held key survives beyond 100ms with keepalives", async () => {
    // The device-side auto-release timer fires at 100ms (DefaultAutoReleaseDuration).
    // The browser sends keepalives every 50ms, each extending the timer by up to 100ms
    // (baseExtension = expectedRate + maxLateness = 50ms + 50ms).
    // A key held for 300ms via the browser must NOT auto-release prematurely.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0x04, true);
    await new Promise(r => setTimeout(r, 300));
    await sendKeypress(sharedPage, 0x04, false);
    await new Promise(r => setTimeout(r, 150));

    const events = await agent!.getKeyboardEvents();
    const presses = events.filter(ev => ev.code === KEY.A && ev.type === "key_press");
    const releases = events.filter(ev => ev.code === KEY.A && ev.type === "key_release");

    expect(presses.length, "Key A should have been pressed").toBeGreaterThanOrEqual(1);
    expect(releases.length, "Key A should have exactly one release").toBe(1);

    const holdDuration = releases[0].time_ms - presses[0].time_ms;
    expect(
      holdDuration,
      "Key should be held for at least 250ms (keepalives kept it alive)",
    ).toBeGreaterThanOrEqual(250);
  });

  test("keepalive: auto-release fires at ~100ms without keepalives", async () => {
    // Bypass the browser keepalive by using keypressReport RPC directly.
    // The device schedules auto-release at 100ms (DefaultAutoReleaseDuration).
    // After 300ms the key must have been auto-released.
    await agent!.clearKeyboardEvents();

    await callJsonRpc(sharedPage, "keypressReport", { key: 0x04, press: true });
    await new Promise(r => setTimeout(r, 300));

    const events = await agent!.getKeyboardEvents();
    const presses = events.filter(ev => ev.code === KEY.A && ev.type === "key_press");
    const releases = events.filter(ev => ev.code === KEY.A && ev.type === "key_release");

    expect(presses.length, "Key A should have been pressed").toBeGreaterThanOrEqual(1);
    expect(releases.length, "Key A should have auto-released").toBeGreaterThanOrEqual(1);

    const holdDuration = releases[0].time_ms - presses[0].time_ms;
    // Auto-release fires at 100ms. Allow some slack for scheduling jitter.
    expect(holdDuration, "Auto-release should fire near 100ms").toBeLessThan(200);
  });

  test("keepalive: key auto-releases after window blur, no stuck keys on re-focus", async () => {
    // When the browser loses focus, resetKeyboardState fires: cancels keepalives
    // and sends a zero-key report. The device then auto-releases any held keys.
    // On re-focus, no phantom presses should appear.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0x04, true);
    await new Promise(r => setTimeout(r, 100));

    // Blur triggers resetKeyboardState — cancels keepalives, sends zero-key report
    await sharedPage.evaluate(() => window.dispatchEvent(new Event("blur")));

    // Wait for device-side auto-release (100ms timer + network margin)
    await new Promise(r => setTimeout(r, 400));

    const events = await agent!.getKeyboardEvents();
    const releases = events.filter(ev => ev.code === KEY.A && ev.type === "key_release");
    expect(releases.length, "Key A should auto-release after blur").toBeGreaterThanOrEqual(1);

    // Re-focus and verify no phantom key events
    await agent!.clearKeyboardEvents();
    await sharedPage.evaluate(() => window.dispatchEvent(new Event("focus")));
    await new Promise(r => setTimeout(r, 200));

    const focusEvents = await agent!.getKeyboardEvents();
    const stuckPresses = focusEvents.filter(ev => ev.type === "key_press");
    expect(stuckPresses.length, "No stuck keys after re-focus").toBe(0);
  });

  test("keepalive: arrow key held for 500ms is not prematurely released", async () => {
    // Regression: arrow keys would release intermittently during hold when browser
    // setInterval jitter exceeded the old tolerance window.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0x4f, true); // HID Right Arrow
    await new Promise(r => setTimeout(r, 500));
    await sendKeypress(sharedPage, 0x4f, false);
    await new Promise(r => setTimeout(r, 150));

    const events = await agent!.getKeyboardEvents();
    const presses = events.filter(ev => ev.code === KEY.RIGHT && ev.type === "key_press");
    const releases = events.filter(ev => ev.code === KEY.RIGHT && ev.type === "key_release");

    expect(presses.length, "Right arrow should have been pressed").toBeGreaterThanOrEqual(1);
    expect(releases.length, "Right arrow should have exactly one release").toBe(1);

    const holdDuration = releases[0].time_ms - presses[0].time_ms;
    expect(holdDuration, "Right arrow should be held for at least 400ms").toBeGreaterThanOrEqual(
      400,
    );
  });

  test("keepalive: modifier + key combo does not auto-release modifier", async () => {
    // Hold Shift, hold A, release A, release Shift.
    // Shift must not auto-release independently while A is held.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe1, true); // HID ShiftLeft
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0x04, true); // HID A
    await new Promise(r => setTimeout(r, 200));
    await sendKeypress(sharedPage, 0x04, false);
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0xe1, false);
    await new Promise(r => setTimeout(r, 150));

    const events = await agent!.getKeyboardEvents();
    const shiftReleases = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    const aReleases = events.filter(ev => ev.code === KEY.A && ev.type === "key_release");

    expect(shiftReleases.length, "Shift should have exactly one release").toBe(1);
    expect(aReleases.length, "A should have exactly one release").toBe(1);

    // Shift must be released after A
    expect(shiftReleases[0].time_ms).toBeGreaterThan(aReleases[0].time_ms);
  });

  test("keepalive: modifier held while tapping multiple keys (Shift+10 chars over 10s)", async () => {
    // Reproduces https://github.com/jetkvm/kvm/issues/1386
    // Hold Shift and tap 10 letter keys with ~1s inter-key delays (~10s total).
    // The modifier must remain held throughout all letter presses.
    test.setTimeout(30_000);
    await agent!.clearKeyboardEvents();

    // HID codes for A-J (0x04-0x0D)
    const letters = [0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d];

    await sendKeypress(sharedPage, 0xe1, true); // Shift down
    await new Promise(r => setTimeout(r, 500));

    for (const hid of letters) {
      await sendKeypress(sharedPage, hid, true);
      await new Promise(r => setTimeout(r, 50));
      await sendKeypress(sharedPage, hid, false);
      await new Promise(r => setTimeout(r, 950)); // ~1s between keys
    }

    await sendKeypress(sharedPage, 0xe1, false); // Shift up
    await new Promise(r => setTimeout(r, 300));

    const events = await agent!.getKeyboardEvents();

    const shiftPresses = events.filter(ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_press");
    const shiftReleases = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    expect(shiftPresses.length, "Shift should have exactly 1 press").toBe(1);
    expect(
      shiftReleases.length,
      "Shift should have exactly 1 release (no premature auto-release)",
    ).toBe(1);

    // All 10 letter keys should have been pressed
    const expectedLinux: number[] = [
      KEY.A,
      KEY.B,
      KEY.C,
      KEY.D,
      KEY.E,
      KEY.F,
      KEY.G,
      KEY.H,
      KEY.I,
      KEY.J,
    ];
    for (const code of expectedLinux) {
      const presses = events.filter(ev => ev.code === code && ev.type === "key_press");
      expect(presses.length, `Key ${code} should have been pressed`).toBeGreaterThanOrEqual(1);
    }

    // Shift release must come after the last letter press
    const lastLetterPress = Math.max(
      ...events
        .filter(ev => expectedLinux.includes(ev.code) && ev.type === "key_press")
        .map(ev => ev.time_ms),
    );
    expect(
      shiftReleases[0].time_ms,
      "Shift release must come after all letter key presses",
    ).toBeGreaterThan(lastLetterPress);
  });

  test("keepalive: modifier held with key-repeat simulation", async () => {
    // Simulates browser key-repeat: Shift held, then rapid repeated A presses
    // without releases (mimicking how browsers fire keydown with repeat=true).
    // Key-repeat must not starve the keepalive and cause modifier auto-release.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe1, true); // Shift down
    await new Promise(r => setTimeout(r, 50));

    // Simulate key-repeat: 20 rapid A presses at ~30ms (no releases between)
    await sharedPage.evaluate(async () => {
      const hooks = window.__kvmTestHooks;
      if (!hooks) throw new Error("Test hooks not available");
      for (let i = 0; i < 20; i++) {
        hooks.sendKeypress(0x04, true); // repeated A press
        await new Promise(r => setTimeout(r, 30));
      }
    });

    await sendKeypress(sharedPage, 0x04, false);
    await new Promise(r => setTimeout(r, 200));
    await sendKeypress(sharedPage, 0xe1, false);
    await new Promise(r => setTimeout(r, 200));

    const events = await agent!.getKeyboardEvents();
    const shiftReleases = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    expect(shiftReleases.length, "Shift should have exactly 1 release").toBe(1);
  });

  test("keepalive: modifier held across rapid tap burst", async () => {
    // Hold Shift, tap 20 keys at ~50ms spacing. Shift must survive throughout.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe1, true);
    await new Promise(r => setTimeout(r, 50));

    await sharedPage.evaluate(async () => {
      const hooks = window.__kvmTestHooks;
      if (!hooks) throw new Error("Test hooks not available");
      for (let i = 0; i < 20; i++) {
        const hid = 0x04 + (i % 10); // cycle A-J
        hooks.sendKeypress(hid, true);
        await new Promise(r => setTimeout(r, 20));
        hooks.sendKeypress(hid, false);
        await new Promise(r => setTimeout(r, 30));
      }
    });

    await sendKeypress(sharedPage, 0xe1, false);
    await new Promise(r => setTimeout(r, 200));

    const events = await agent!.getKeyboardEvents();
    const shiftReleases = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    expect(shiftReleases.length, "Shift should have exactly 1 release").toBe(1);
  });

  test("keepalive: multiple simultaneous modifiers + key", async () => {
    // Hold Ctrl+Shift, tap A, release both. Neither modifier should auto-release.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe0, true); // Ctrl
    await sendKeypress(sharedPage, 0xe1, true); // Shift
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0x04, true); // A
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0x04, false);
    await new Promise(r => setTimeout(r, 200));
    await sendKeypress(sharedPage, 0xe1, false);
    await sendKeypress(sharedPage, 0xe0, false);
    await new Promise(r => setTimeout(r, 200));

    const events = await agent!.getKeyboardEvents();
    for (const [code, label] of [
      [KEY.LEFT_CTRL, "Ctrl"],
      [KEY.LEFT_SHIFT, "Shift"],
    ] as const) {
      const releases = events.filter(ev => ev.code === code && ev.type === "key_release");
      expect(releases.length, `${label} should have exactly 1 release`).toBe(1);
    }
    const aPresses = events.filter(ev => ev.code === KEY.A && ev.type === "key_press");
    expect(aPresses.length, "A should have been pressed").toBeGreaterThanOrEqual(1);
  });

  test("keepalive: modifier released before non-modifier (reversed release order)", async () => {
    // Shift down, A down, Shift up (while A still held), A up.
    // Tests that releasing the modifier first doesn't corrupt the key buffer.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe1, true); // Shift
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0x04, true); // A
    await new Promise(r => setTimeout(r, 100));
    await sendKeypress(sharedPage, 0xe1, false); // Shift up first
    await new Promise(r => setTimeout(r, 100));
    await sendKeypress(sharedPage, 0x04, false); // A up
    await new Promise(r => setTimeout(r, 200));

    const events = await agent!.getKeyboardEvents();
    const shiftRelease = events.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    const aRelease = events.filter(ev => ev.code === KEY.A && ev.type === "key_release");

    expect(shiftRelease.length, "Shift should have exactly 1 release").toBe(1);
    expect(aRelease.length, "A should have exactly 1 release").toBe(1);
    // Shift released before A
    expect(shiftRelease[0].time_ms).toBeLessThan(aRelease[0].time_ms);
  });

  test("keepalive: AltGr (AltRight) held while tapping key", async () => {
    // Hold AltRight (AltGr on international layouts), tap A.
    // AltRight must not auto-release prematurely.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0xe6, true); // AltRight
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0x04, true); // A
    await new Promise(r => setTimeout(r, 50));
    await sendKeypress(sharedPage, 0x04, false);
    await new Promise(r => setTimeout(r, 200));
    await sendKeypress(sharedPage, 0xe6, false);
    await new Promise(r => setTimeout(r, 200));

    const events = await agent!.getKeyboardEvents();
    const altReleases = events.filter(ev => ev.code === KEY.RIGHT_ALT && ev.type === "key_release");
    expect(altReleases.length, "AltRight should have exactly 1 release").toBe(1);
  });

  test("keepalive: multiple simultaneous keys held for 300ms", async () => {
    // Hold A + B + C simultaneously, wait 300ms, release all.
    // Each key should get exactly 1 release — tests per-key timer independence.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0x04, true); // A
    await sendKeypress(sharedPage, 0x05, true); // B
    await sendKeypress(sharedPage, 0x06, true); // C
    await new Promise(r => setTimeout(r, 300));
    await sendKeypress(sharedPage, 0x04, false);
    await sendKeypress(sharedPage, 0x05, false);
    await sendKeypress(sharedPage, 0x06, false);
    await new Promise(r => setTimeout(r, 150));

    const events = await agent!.getKeyboardEvents();
    for (const [code, label] of [
      [KEY.A, "A"],
      [KEY.B, "B"],
      [KEY.C, "C"],
    ] as const) {
      const presses = events.filter(ev => ev.code === code && ev.type === "key_press");
      const releases = events.filter(ev => ev.code === code && ev.type === "key_release");
      expect(presses.length, `${label} should have at least 1 press`).toBeGreaterThanOrEqual(1);
      expect(releases.length, `${label} should have exactly 1 release`).toBe(1);
    }
  });

  test("keepalive: rapid press/release cycles produce no phantom releases", async () => {
    // Tap a key 20 times fast (~30ms apart). Should get exactly 20 press + 20 release.
    const TAP_COUNT = 20;
    await agent!.clearKeyboardEvents();

    await sharedPage.evaluate(async (count: number) => {
      const hooks = window.__kvmTestHooks;
      if (!hooks) throw new Error("Test hooks not available");
      for (let i = 0; i < count; i++) {
        hooks.sendKeypress(0x04, true);
        await new Promise(r => setTimeout(r, 10));
        hooks.sendKeypress(0x04, false);
        await new Promise(r => setTimeout(r, 20));
      }
    }, TAP_COUNT);

    await new Promise(r => setTimeout(r, 500));

    const events = await agent!.getKeyboardEvents();
    const presses = events.filter(ev => ev.code === KEY.A && ev.type === "key_press");
    const releases = events.filter(ev => ev.code === KEY.A && ev.type === "key_release");

    expect(presses.length, `Should have ${TAP_COUNT} presses`).toBe(TAP_COUNT);
    expect(releases.length, `Should have ${TAP_COUNT} releases (no phantom releases)`).toBe(
      TAP_COUNT,
    );
  });

  test("keepalive: long hold (2s) stays held with keepalives", async () => {
    // Real-world scenario: holding Backspace to delete a line, or holding an arrow
    // key to scroll through code. The key must stay held for the full 2s.
    // Keepalives arrive every 50ms, each extending the 100ms auto-release timer.
    await agent!.clearKeyboardEvents();

    await sendKeypress(sharedPage, 0x2a, true); // HID Backspace
    await new Promise(r => setTimeout(r, 2000));
    await sendKeypress(sharedPage, 0x2a, false);
    await new Promise(r => setTimeout(r, 150));

    const events = await agent!.getKeyboardEvents();
    const presses = events.filter(ev => ev.code === KEY.BACKSPACE && ev.type === "key_press");
    const releases = events.filter(ev => ev.code === KEY.BACKSPACE && ev.type === "key_release");

    expect(presses.length, "Backspace should have been pressed").toBeGreaterThanOrEqual(1);
    expect(releases.length, "Backspace should have exactly one release").toBe(1);

    const holdDuration = releases[0].time_ms - presses[0].time_ms;
    expect(holdDuration, "Backspace should be held for at least 1800ms").toBeGreaterThanOrEqual(
      1800,
    );
  });

  // ═══════════════════════════════════════════
  // KEYBOARD: KEYS RELEASED ON DISCONNECT
  // ═══════════════════════════════════════════

  test("keyboard: all keys released when WebRTC session disconnects", async ({ browser }) => {
    test.setTimeout(30_000);

    // Opening a new page takes over currentSession (single-session device),
    // kicking sharedPage. We'll reconnect sharedPage at the end.
    const freshPage = await browser.newPage();
    await freshPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(freshPage);

    await agent!.clearKeyboardEvents();

    // Hold down a modifier (LeftShift) and a regular key (Space) without releasing
    await sendKeypress(freshPage, 0xe1, true);
    await new Promise(r => setTimeout(r, 20));
    await sendKeypress(freshPage, HID_KEY.SPACE, true);
    await new Promise(r => setTimeout(r, 50));

    // Verify the host received the presses before we disconnect
    const preEvents = await agent!.getKeyboardEvents();
    const shiftPresses = preEvents.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_press",
    );
    expect(shiftPresses.length, "Host should see LeftShift press").toBeGreaterThanOrEqual(1);

    // Close the page to sever the WebRTC session, triggering the all-keys-up report
    await freshPage.close();
    await new Promise(r => setTimeout(r, 1000));

    // Verify the host received releases for both keys
    const allEvents = await agent!.getKeyboardEvents();
    const shiftReleases = allEvents.filter(
      ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release",
    );
    const spaceReleases = allEvents.filter(
      ev => ev.code === KEY.SPACE && ev.type === "key_release",
    );

    expect(
      shiftReleases.length,
      "Host should see LeftShift release after disconnect",
    ).toBeGreaterThanOrEqual(1);
    expect(
      spaceReleases.length,
      "Host should see Space release after disconnect",
    ).toBeGreaterThanOrEqual(1);

    // Reconnect sharedPage so subsequent tests can use it
    await sharedPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);
    await waitForRpcReady(sharedPage);

    // Verify device-side keys-down state is clear by querying the device
    // directly via JSON-RPC (bypasses Zustand store / hidRpc timing)
    await expect
      .poll(
        async () => {
          const s = (await callJsonRpc(sharedPage, "getKeyDownState")) as {
            modifier: number;
            keys: number[];
          };
          return s.modifier === 0 && s.keys.every((k: number) => k === 0);
        },
        {
          message: "All key slots should be clear after disconnect",
          timeout: 5000,
          intervals: [200, 500],
        },
      )
      .toBe(true);
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

  test("mouse: multi-button combinations (hold left, press right)", async () => {
    const BTN_LEFT = 0x110; // 272
    const BTN_RIGHT = 0x111; // 273

    // Move mouse to a stable position first
    await sendAbsMouseMove(sharedPage, 16384, 16384);
    await new Promise(r => setTimeout(r, 100));

    let successes = 0;
    const attempts = 20;

    for (let i = 0; i < attempts; i++) {
      await agent!.clearMouseEvents();

      // Press left button (buttons bitmask: 1 = left)
      await sendAbsMouseMove(sharedPage, 16384, 16384, 1);
      await new Promise(r => setTimeout(r, 100));

      // While holding left, press right button (buttons bitmask: 3 = left + right)
      await sendAbsMouseMove(sharedPage, 16384, 16384, 3);
      await new Promise(r => setTimeout(r, 100));

      // Release right button, left still held (buttons bitmask: 1 = left)
      await sendAbsMouseMove(sharedPage, 16384, 16384, 1);
      await new Promise(r => setTimeout(r, 100));

      // Release all buttons (buttons bitmask: 0)
      await sendAbsMouseMove(sharedPage, 16384, 16384, 0);
      await new Promise(r => setTimeout(r, 150));

      const events = await agent!.getMouseEvents();
      const buttonEvents = events.filter(ev => ev.type === "mouse_button");

      // We expect to see button events for both left and right buttons
      const leftPress = buttonEvents.find(ev => ev.code === BTN_LEFT && ev.value === 1);
      const rightPress = buttonEvents.find(ev => ev.code === BTN_RIGHT && ev.value === 1);
      const rightRelease = buttonEvents.find(ev => ev.code === BTN_RIGHT && ev.value === 0);
      const leftRelease = buttonEvents.find(ev => ev.code === BTN_LEFT && ev.value === 0);

      if (leftPress && rightPress && rightRelease && leftRelease) {
        successes++;
      }
    }

    // All attempts must succeed — button state changes must be reliably delivered
    expect(successes, `Multi-button succeeded ${successes}/${attempts} times`).toBe(attempts);
  });

  // ═══════════════════════════════════════════
  // MOUSE: BLUR DOES NOT JUMP TO TOP-LEFT (#392)
  // ═══════════════════════════════════════════

  test("mouse: window blur releases buttons without moving cursor (#392)", async () => {
    // Move mouse to center of the video element via a real mousemove event
    // so that useMouse's lastAbsPos is updated through the normal code path.
    const video = sharedPage.locator("video");
    const box = await video.boundingBox();
    expect(box).not.toBeNull();

    const centerX = box!.x + box!.width / 2;
    const centerY = box!.y + box!.height / 2;

    // Move to center — this triggers the real absMouseMoveHandler
    await sharedPage.mouse.move(centerX, centerY);
    await new Promise(r => setTimeout(r, 100));

    // Clear events, then dispatch blur
    await agent!.clearMouseEvents();
    await sharedPage.evaluate(() => window.dispatchEvent(new Event("blur")));
    await new Promise(r => setTimeout(r, 200));

    // Collect any mouse events that were sent on blur
    const events = await agent!.getMouseEvents();
    const absEvents = events.filter(ev => ev.type === "mouse_move_abs");

    // If any abs mouse events were sent, none should be at (0, 0)
    for (const ev of absEvents) {
      expect(
        ev.x > 100 || ev.y > 100,
        `Blur should not move cursor to origin, got (${ev.x}, ${ev.y})`,
      ).toBe(true);
    }
  });

  // ═══════════════════════════════════════════
  // MOUSE: BACK/FORWARD BUTTONS (4 & 5)
  // ═══════════════════════════════════════════

  test("mouse: back and forward buttons via absolute mouse", async () => {
    const BTN_SIDE = 0x113;
    const BTN_EXTRA = 0x114;

    for (const { buttons, btnCode, label } of [
      { buttons: 0x08, btnCode: BTN_SIDE, label: "back (button 4)" },
      { buttons: 0x10, btnCode: BTN_EXTRA, label: "forward (button 5)" },
    ]) {
      await agent!.clearMouseEvents();

      await sendAbsMouseMove(sharedPage, 16384, 16384, buttons);
      await new Promise(r => setTimeout(r, 50));
      await sendAbsMouseMove(sharedPage, 16384, 16384, 0);
      await new Promise(r => setTimeout(r, 50));

      const deadline = Date.now() + 3000;
      let found = false;
      while (Date.now() < deadline) {
        const events = await agent!.getMouseEvents();
        if (events.some(ev => ev.type === "mouse_button" && ev.code === btnCode)) {
          found = true;
          break;
        }
        await new Promise(r => setTimeout(r, 50));
      }
      expect(found, `${label} should be received by host`).toBe(true);
    }
  });

  // ═══════════════════════════════════════════
  // MOUSE: WHEEL SCROLL (VERTICAL + HORIZONTAL)
  // ═══════════════════════════════════════════

  test("mouse: vertical and horizontal wheel scroll", async () => {
    const REL_WHEEL = 0x08;
    const REL_HWHEEL = 0x06;

    // Vertical scroll
    await agent!.clearMouseEvents();
    await callJsonRpc(sharedPage, "wheelReport", { wheelY: 1, wheelX: 0 });
    const vWheel = await agent!.waitForMouseEvent(
      ev => ev.type === "mouse_move_rel" && ev.code === REL_WHEEL,
      3000,
    );
    expect(vWheel.length, "Vertical wheel event should be received").toBeGreaterThan(0);
    expect(vWheel[0].value).not.toBe(0);

    // Horizontal scroll
    await agent!.clearMouseEvents();
    await callJsonRpc(sharedPage, "wheelReport", { wheelY: 0, wheelX: 1 });
    const hWheel = await agent!.waitForMouseEvent(
      ev => ev.type === "mouse_move_rel" && ev.code === REL_HWHEEL,
      3000,
    );
    expect(hWheel.length, "Horizontal wheel event should be received").toBeGreaterThan(0);
    expect(hWheel[0].value).not.toBe(0);

    // Both axes simultaneously
    await agent!.clearMouseEvents();
    await callJsonRpc(sharedPage, "wheelReport", { wheelY: -1, wheelX: 1 });
    const bothV = await agent!.waitForMouseEvent(
      ev => ev.type === "mouse_move_rel" && ev.code === REL_WHEEL,
      3000,
    );
    expect(bothV.length, "Vertical wheel in combined event").toBeGreaterThan(0);
    const bothEvents = await agent!.getMouseEvents();
    const bothH = bothEvents.filter(ev => ev.type === "mouse_move_rel" && ev.code === REL_HWHEEL);
    expect(bothH.length, "Horizontal wheel in combined event").toBeGreaterThan(0);
  });

  test("mouse: wheel scroll works in relative-only mouse mode", async () => {
    test.setTimeout(30_000);
    const REL_WHEEL = 0x08;
    const REL_HWHEEL = 0x06;

    await callJsonRpc(sharedPage, "setUsbDevices", { devices: USB_DEVICES_REL_MOUSE_ONLY });
    await agent!.waitForInputDevices(["keyboard", "relative_mouse"], 10000);

    // After USB device re-enumeration the remote agent needs time to re-open
    // the new /dev/input/event* nodes — poll with retries instead of fixed sleep.
    try {
      // Vertical scroll — retry sending until the agent picks it up
      const vDeadline = Date.now() + 10000;
      let vWheel: RAMouseEvent[] = [];
      while (Date.now() < vDeadline) {
        await agent!.clearMouseEvents();
        await callJsonRpc(sharedPage, "wheelReport", { wheelY: 1, wheelX: 0 });
        try {
          vWheel = await agent!.waitForMouseEvent(
            ev => ev.type === "mouse_move_rel" && ev.code === REL_WHEEL,
            2000,
          );
          break;
        } catch {
          /* agent not ready yet, retry */
        }
      }
      expect(vWheel.length, "Vertical wheel in relative-only mode").toBeGreaterThan(0);
      expect(vWheel[0].value).not.toBe(0);

      // Horizontal scroll
      await agent!.clearMouseEvents();
      await callJsonRpc(sharedPage, "wheelReport", { wheelY: 0, wheelX: 1 });
      const hWheel = await agent!.waitForMouseEvent(
        ev => ev.type === "mouse_move_rel" && ev.code === REL_HWHEEL,
        3000,
      );
      expect(hWheel.length, "Horizontal wheel in relative-only mode").toBeGreaterThan(0);
      expect(hWheel[0].value).not.toBe(0);
    } finally {
      await callJsonRpc(sharedPage, "setUsbDevices", { devices: USB_DEVICES_DEFAULT });
      await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 10000);
    }
  });

  test("mouse: wheel scroll should not produce duplicate events from both HID devices", async () => {
    test.setTimeout(30_000);
    const REL_WHEEL = 0x08;
    const REL_HWHEEL = 0x06;

    await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 10000);

    // Retry until the agent starts receiving wheel events (host may still
    // be re-enumerating HID devices after previous test's USB changes).
    const vDeadline = Date.now() + 10000;
    let vWheelEvents: RAMouseEvent[] = [];
    while (Date.now() < vDeadline) {
      await agent!.clearMouseEvents();
      await callJsonRpc(sharedPage, "wheelReport", { wheelY: 1, wheelX: 0 });
      try {
        await agent!.waitForMouseEvent(
          ev => ev.type === "mouse_move_rel" && ev.code === REL_WHEEL,
          2000,
        );
        break;
      } catch {
        /* agent not ready yet, retry */
      }
    }
    // Allow any duplicate from the second HID device to arrive
    await new Promise(r => setTimeout(r, 500));

    const allEvents = await agent!.getMouseEvents();
    vWheelEvents = allEvents.filter(ev => ev.type === "mouse_move_rel" && ev.code === REL_WHEEL);
    const devices = new Set(vWheelEvents.map(ev => ev.device));
    console.log(
      `Vertical wheel: ${vWheelEvents.length} event(s) from device(s): ${[...devices].join(", ")}`,
    );
    expect(
      vWheelEvents.length,
      `Expected 1 vertical wheel event, got ${vWheelEvents.length} from [${[...devices].join(", ")}]. ` +
        `Duplicate means rpcWheelReport sends to both abs and rel mouse HID devices.`,
    ).toBe(1);

    await agent!.clearMouseEvents();
    await callJsonRpc(sharedPage, "wheelReport", { wheelY: 0, wheelX: 1 });
    await agent!.waitForMouseEvent(
      ev => ev.type === "mouse_move_rel" && ev.code === REL_HWHEEL,
      3000,
    );
    await new Promise(r => setTimeout(r, 500));

    const allHEvents = await agent!.getMouseEvents();
    const hWheelEvents = allHEvents.filter(
      ev => ev.type === "mouse_move_rel" && ev.code === REL_HWHEEL,
    );
    const hDevices = new Set(hWheelEvents.map(ev => ev.device));
    console.log(
      `Horizontal wheel: ${hWheelEvents.length} event(s) from device(s): ${[...hDevices].join(", ")}`,
    );
    expect(
      hWheelEvents.length,
      `Expected 1 horizontal wheel event, got ${hWheelEvents.length} from [${[...hDevices].join(", ")}].`,
    ).toBe(1);
  });

  // ═══════════════════════════════════════════
  // INPUT: MACROS
  // ═══════════════════════════════════════════

  test("input: keyboard macros", async () => {
    test.setTimeout(30_000);

    // Single key press (A) — retry in case the remote agent is still
    // re-opening input devices after the previous USB mode switch.
    const keyABtn = sharedPage.getByRole("button", { name: "E2E KeyA" });
    await keyABtn.waitFor({ state: "visible", timeout: 5000 });

    const macroDeadline = Date.now() + 15000;
    let macroEvents: RAKeyboardEvent[] = [];
    while (Date.now() < macroDeadline) {
      await agent!.clearKeyboardEvents();
      await keyABtn.click();
      try {
        macroEvents = await agent!.waitForKeyboardEvent(
          ev => ev.code === KEY.A && ev.type === "key_press",
          3000,
        );
        break;
      } catch {
        /* agent not ready, retry */
      }
    }
    expect(macroEvents.length).toBeGreaterThan(0);

    // Modifier combo (Ctrl+A)
    await agent!.clearKeyboardEvents();
    await sharedPage.getByRole("button", { name: "E2E Ctrl+A" }).click();

    const ctrlDeadline = Date.now() + 3000;
    let gotCtrl = false,
      gotA = false;
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
          if (idx === expectedSeq.length) {
            matched = true;
            break;
          }
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

    // Unmount
    await callJsonRpc(sharedPage, "unmountImage");
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
    const preEvents = await agent!.expectKeyPress(KEY.SPACE, async () => {
      await tapKey(sharedPage, HID_KEY.SPACE);
    });
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

    // Mount the ISO on the remote host — triggers PREVENT MEDIUM REMOVAL SCSI command,
    // which causes the KVM kernel to return EBUSY when clearing the backing file.
    const mountPoint = "/tmp/jetkvm-e2e-cdrom";
    try {
      remoteHostExec(`sudo mkdir -p ${mountPoint} && sudo mount -o ro ${blockDev} ${mountPoint}`);
    } catch {
      // If ISO mount fails, try eject -i on as fallback to lock the medium
      try {
        remoteHostExec(`sudo eject -i on ${blockDev}`);
      } catch {
        test.skip(true, "Could not lock CDROM medium on remote host");
        return;
      }
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
      // Clean up: unmount on remote host (may already be ejected by USB rebind)
      try {
        remoteHostExec(
          `sudo umount -l ${mountPoint} 2>/dev/null; sudo rmdir ${mountPoint} 2>/dev/null`,
        );
      } catch {
        /* best effort */
      }
      try {
        remoteHostExec(`sudo eject -i off ${blockDev} 2>/dev/null`);
      } catch {
        /* best effort */
      }
    }
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
    await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 10000);

    // Switch USB descriptor to Logitech — verify host sees new VID/PID
    await callJsonRpc(sharedPage, "setUsbConfig", { usbConfig: USB_LOGITECH_CONFIG });

    const logitechDevices = await agent!.waitForUSBDevice(d => d.id === ID_LOGITECH, true, 8000);
    expect(logitechDevices.length).toBeGreaterThan(0);
    expect(logitechDevices[0].name).toContain("Logitech");

    // Restore default descriptor
    const deviceId = (await callJsonRpc(sharedPage, "getDeviceID")) as string;
    const defaultConfig = { ...USB_DEFAULT_CONFIG, serial_number: deviceId || "" };
    callJsonRpc(sharedPage, "setUsbConfig", { usbConfig: defaultConfig }).catch(() => {
      /* ignore */
    });
  });

  // ═══════════════════════════════════════════
  // USB SERIAL CONSOLE (CDC-ACM)
  // ═══════════════════════════════════════════

  test("usb: serial console CDC-ACM toggle creates and removes ttyACM on host", async () => {
    test.setTimeout(30_000);

    test.skip(!process.env.JETKVM_REMOTE_HOST, "JETKVM_REMOTE_HOST not set");

    // Ensure serial console is off initially
    await callJsonRpc(sharedPage, "setUsbDevices", {
      devices: { ...USB_DEVICES_DEFAULT, serial_console: false },
    });
    await new Promise(r => setTimeout(r, 3000));

    // Verify the host does NOT see a ttyACM device
    const beforeACM = remoteHostExec(
      "find /dev -maxdepth 1 -name 'ttyACM*' | head -1 || true",
    ).trim();
    expect(beforeACM).toBe("");

    // Enable serial console
    await callJsonRpc(sharedPage, "setUsbDevices", {
      devices: { ...USB_DEVICES_DEFAULT, serial_console: true },
    });
    await new Promise(r => setTimeout(r, 3000));

    // Verify the host now sees a ttyACM device
    const afterACM = remoteHostExec("find /dev -maxdepth 1 -name 'ttyACM*' | head -1").trim();
    expect(afterACM).toContain("ttyACM");

    // Verify /dev/ttyGS0 exists on the KVM device
    const afterGS0 = (await sshExec("ls /dev/ttyGS0 2>/dev/null || echo MISSING", true)).trim();
    expect(afterGS0).toBe("/dev/ttyGS0");

    // Disable serial console
    await callJsonRpc(sharedPage, "setUsbDevices", {
      devices: { ...USB_DEVICES_DEFAULT, serial_console: false },
    });
    await new Promise(r => setTimeout(r, 3000));

    // Verify the host no longer sees a ttyACM device
    const removedACM = remoteHostExec(
      "find /dev -maxdepth 1 -name 'ttyACM*' | head -1 || true",
    ).trim();
    expect(removedACM).toBe("");

    // Verify other USB functions still work (keyboard, mouse)
    await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 10000);
  });

  // ═══════════════════════════════════════════
  // USB SERIAL CONSOLE UI
  // ═══════════════════════════════════════════

  test("usb: USB Serial Console terminal sends and receives data via ttyGS0", async () => {
    test.setTimeout(60_000);

    test.skip(!process.env.JETKVM_REMOTE_HOST, "JETKVM_REMOTE_HOST not set");

    // Enable serial console
    await callJsonRpc(sharedPage, "setUsbDevices", {
      devices: { ...USB_DEVICES_DEFAULT, serial_console: true },
    });
    await new Promise(r => setTimeout(r, 3000));

    // Find the ttyACM device on the remote host
    const ttyACM = remoteHostExec("find /dev -maxdepth 1 -name 'ttyACM*' | head -1").trim();
    expect(ttyACM).toContain("ttyACM");

    // Reload the page so the action bar picks up serial_console enabled state
    await sharedPage.reload({ waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);

    // Verify the USB Serial Console button is visible
    const cdcButton = sharedPage.getByRole("button", { name: "USB Serial Console" });
    await expect(cdcButton).toBeVisible({ timeout: 5000 });

    // Click the button to open the terminal
    await cdcButton.click();
    await new Promise(r => setTimeout(r, 1000));

    // Configure the remote serial port and start a background reader
    const testString = `e2e_test_${Date.now()}`;
    remoteHostExec(`sudo stty -F ${ttyACM} 9600 raw -echo`);
    remoteHostExec(`sudo bash -c 'nohup cat ${ttyACM} > /tmp/cdcacm_rx.txt 2>/dev/null &'`);
    await new Promise(r => setTimeout(r, 500));

    // Type a string into the USB Serial Console terminal
    await sharedPage.keyboard.type(testString, { delay: 50 });
    await new Promise(r => setTimeout(r, 2000));

    // Read what the remote host received
    const received = remoteHostExec("sudo cat /tmp/cdcacm_rx.txt 2>/dev/null || echo EMPTY").trim();
    expect(received).toContain(testString);

    // Test receiving data: send from remote host to ttyACM
    const replyString = `reply_${Date.now()}`;
    remoteHostExec(`sudo bash -c 'echo ${replyString} > ${ttyACM}'`);
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
    await sharedPage.keyboard.press("Escape");
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

  test("usb-recovery: auto-recovers USB gadget after UDC unbind", async () => {
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
  // VIDEO: NON-ALIGNED RESOLUTION (1366x768) — #699
  // ═══════════════════════════════════════════

  // EDID for a 1366x768 monitor (pixel clock 85.5 MHz, 60 Hz)
  const EDID_1366x768 =
    "00ffffffffffff0028b401000100000001220103802213780aee95a3544c99260f50540000000101010101010101010101010101010166" +
    "2156aa51002030468f350058c21000001e000000fc004a65744b564d20313336367837000000fd00384c1e530a00202020202020200000" +
    "0010002020202020202020202020202000d0";

  test("video: non-aligned resolution 1366x768 produces video frames", async () => {
    test.setTimeout(60_000);

    const originalEdid = (await callJsonRpc(sharedPage, "getEDID")) as string;

    await callJsonRpc(sharedPage, "setEDID", { edid: EDID_1366x768 }, 30000).catch(() => {});
    await new Promise(r => setTimeout(r, 3000));
    await sharedPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);
    await waitForRpcReady(sharedPage);

    try {
      await agent!.waitForResolution("1366x768", 15_000);

      await expect
        .poll(
          async () => {
            const state = (await callJsonRpc(sharedPage, "getVideoState")) as {
              ready: boolean;
              width: number;
              height: number;
            };
            return state;
          },
          {
            message: "Waiting for KVM to report 1366x768",
            timeout: 15_000,
            intervals: [500, 1000],
          },
        )
        .toMatchObject({ ready: true, width: 1366, height: 768 });

      await sharedPage.goto("/", { waitUntil: "networkidle" });
      await waitForWebRTCReady(sharedPage);

      const dims = await waitForVideoDimensions(sharedPage, 15_000);
      expect(dims.width).toBe(1366);
      expect(dims.height).toBe(768);
    } finally {
      await callJsonRpc(sharedPage, "setEDID", { edid: originalEdid }, 30000).catch(() => {
        /* ignore */
      });

      await new Promise(r => setTimeout(r, 3000));

      await sharedPage.goto("/", { waitUntil: "networkidle" });
      await waitForWebRTCReady(sharedPage);
    }
  });

  // ═══════════════════════════════════════════
  // PANEL VISIBILITY: HIDE HEADER BAR / STATUS BAR
  // ═══════════════════════════════════════════

  test("panel-visibility: hide and show header and status bars via appearance settings", async () => {
    const checkboxFor = (label: string) => sharedPage.getByRole("checkbox", { name: label });

    const headerBar = sharedPage.locator('img[alt=""]').first();

    await sharedPage.evaluate(() => {
      const stored = localStorage.getItem("settings");
      if (stored) {
        const parsed = JSON.parse(stored);
        if (parsed.state) {
          delete parsed.state.hideHeaderBar;
          delete parsed.state.hideStatusBar;
          delete parsed.state.showHeaderBar;
          delete parsed.state.showStatusBar;
          localStorage.setItem("settings", JSON.stringify(parsed));
        }
      }
    });

    await sharedPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);
    await expect(headerBar).toBeVisible({ timeout: 5000 });

    await sharedPage.goto("/settings/appearance", { waitUntil: "networkidle" });
    await checkboxFor("Hide header bar").check();

    await sharedPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);
    await expect(headerBar).not.toBeVisible({ timeout: 5000 });

    await sharedPage.goto("/settings/appearance", { waitUntil: "networkidle" });
    await checkboxFor("Hide status bar").check();

    await sharedPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);
    await expect(headerBar).not.toBeVisible({ timeout: 5000 });
    await expect(sharedPage.getByText("Caps Lock").first()).not.toBeVisible({ timeout: 5000 });

    await sharedPage.goto("/settings/appearance", { waitUntil: "networkidle" });
    await checkboxFor("Hide header bar").uncheck();
    await checkboxFor("Hide status bar").uncheck();

    await sharedPage.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(sharedPage);
    await expect(headerBar).toBeVisible({ timeout: 5000 });
  });

  // ═══════════════════════════════════════════
  // HDMI SLEEP WAKE: SIGNAL RE-DETECTION AFTER DPMS OFF→ON
  // ═══════════════════════════════════════════

  test("hdmi-sleep-wake: re-detects signal after DPMS off→on with chip asleep", async () => {
    test.setTimeout(120_000);

    const SLEEP_MODE_SYSFS = "/sys/devices/platform/ff470000.i2c/i2c-4/4-000f/sleep_mode";

    const sleepInfo = (await callJsonRpc(sharedPage, "getVideoSleepMode")) as {
      supported: boolean;
      duration: number;
    };

    if (!sleepInfo.supported) {
      test.skip(true, "HDMI sleep mode not supported on this device");
      return;
    }

    const originalDuration = sleepInfo.duration;

    try {
      // Set a short sleep timer (3s) so the chip enters sleep quickly
      await callJsonRpc(sharedPage, "setVideoSleepMode", { duration: 3 });

      // Disconnect WebRTC so there are no active sessions → sleep timer starts
      await sharedPage.goto("about:blank");

      // Wait for sleep timer + margin
      await new Promise(r => setTimeout(r, 6000));

      // Verify chip entered sleep mode
      const sleepState = (await sshExec(`cat ${SLEEP_MODE_SYSFS}`)).trim();
      expect(sleepState, "Capture chip should be in sleep mode").toBe("1");

      // Toggle DPMS off on the remote host (simulates host GPU cutting signal)
      remoteHostSetDPMS(true);

      // Wait for the GPU to fully cut the TMDS clock
      await new Promise(r => setTimeout(r, 3000));

      // Bring the display back on
      remoteHostSetDPMS(false);

      // Wait for host display to stabilize
      await new Promise(r => setTimeout(r, 3000));

      // Reconnect — this triggers VideoStart() which must wake the chip and re-lock
      await sharedPage.goto("/", { waitUntil: "networkidle" });
      await waitForWebRTCReady(sharedPage);

      // Verify the chip woke up
      const wakeState = (await sshExec(`cat ${SLEEP_MODE_SYSFS}`)).trim();
      expect(wakeState, "Capture chip should be awake after reconnect").toBe("0");

      // Verify video state shows a valid signal (no error)
      const videoState = (await callJsonRpc(sharedPage, "getVideoState")) as {
        ready: boolean;
        error?: string;
        width: number;
        height: number;
      };
      expect(videoState.ready, `Video should be ready but got error: ${videoState.error}`).toBe(
        true,
      );
      expect(videoState.width).toBeGreaterThan(0);
      expect(videoState.height).toBeGreaterThan(0);
    } finally {
      // Always restore DPMS and sleep duration, even if test fails
      try {
        remoteHostSetDPMS(false);
      } catch {
        // best effort
      }

      // Reconnect if needed to restore sleep duration via RPC
      if (sharedPage.url() === "about:blank") {
        await sharedPage.goto("/", { waitUntil: "networkidle" });
        await waitForWebRTCReady(sharedPage);
      }
      await callJsonRpc(sharedPage, "setVideoSleepMode", { duration: originalDuration });
    }
  });

  // ═══════════════════════════════════════════
  // FACTORY RESET (must be last — erases all user data and reboots)
  // ═══════════════════════════════════════════

  test("factory-reset: reset device via RPC and verify setup endpoint after reboot", async () => {
    test.setTimeout(120_000);
    const host = getDeviceHost();

    await callJsonRpc(sharedPage, "factoryReset");

    // First, wait for the device to go DOWN (become unreachable).
    // Without this, we may poll /device/status before the reboot starts
    // and get the stale pre-reset isSetup=true response.
    const waitForDeviceDown = async (timeout: number) => {
      const start = Date.now();
      while (Date.now() - start < timeout) {
        try {
          await fetch(`http://${host}/device/status`, {
            signal: AbortSignal.timeout(2000),
          });
          // Still reachable — keep waiting
        } catch {
          return; // Device is down
        }
        await new Promise(r => setTimeout(r, 500));
      }
      throw new Error(`Device did not go down within ${timeout}ms`);
    };

    const waitForDeviceUp = async (timeout: number) => {
      const start = Date.now();
      while (Date.now() - start < timeout) {
        try {
          const res = await fetch(`http://${host}/device/status`, {
            signal: AbortSignal.timeout(2000),
          });
          if (res.ok) return (await res.json()) as { isSetup: boolean };
        } catch {
          // Device is still rebooting
        }
        await new Promise(r => setTimeout(r, 2000));
      }
      throw new Error(`Device did not come back within ${timeout}ms`);
    };

    await waitForDeviceDown(30_000);
    const status = await waitForDeviceUp(90_000);
    expect(status.isSetup, "Device should be not set up after factory reset").toBe(false);

    const setupRes = await fetch(`http://${host}/device/setup`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ localAuthMode: "noPassword" }),
    });
    expect(setupRes.ok, `Setup POST failed: ${setupRes.status}`).toBe(true);

    const verifyRes = await fetch(`http://${host}/device/status`);
    const verify = (await verifyRes.json()) as { isSetup: boolean };
    expect(verify.isSetup, "Device should be set up after POST /device/setup").toBe(true);

    // Restore SSH key so subsequent test runs can SSH into the device.
    const context = await sharedPage
      .context()
      .browser()!
      .newContext({
        baseURL: `http://${host}`,
      });
    const freshPage = await context.newPage();
    try {
      await freshPage.goto("/");
      await freshPage.waitForLoadState("networkidle");
      await waitForWebRTCReady(freshPage);

      const fs = await import("fs");
      const os = await import("os");
      const path = await import("path");
      const sshPubKeyPath = path.join(os.homedir(), ".ssh", "id_ed25519.pub");
      let sshKey: string;
      try {
        sshKey = fs.readFileSync(sshPubKeyPath, "utf-8").trim();
      } catch {
        const rsaPath = path.join(os.homedir(), ".ssh", "id_rsa.pub");
        sshKey = fs.readFileSync(rsaPath, "utf-8").trim();
      }

      await callJsonRpc(freshPage, "setSSHKeyState", { sshKey });
    } finally {
      await freshPage.close();
      await context.close();
    }
  });
});
