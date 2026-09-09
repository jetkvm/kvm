// Session, host helpers and constants shared by the remote-agent specs.
// Every spec registers one page and WebRTC session per file through
// registerSharedSession() and runs its tests against it in serial mode.
import { execSync } from "child_process";
import { test, expect, type Page } from "@playwright/test";
import {
  SSH_OPTS,
  callJsonRpc,
  waitForWebRTCReady,
  ensureRpcReady,
  waitForVideoDimensions,
  sshExec,
  getDeviceHost,
  restartAppViaSSH,
  semverGte,
} from "../helpers";
import {
  createRemoteAgent,
  waitForKeyboardReady,
  HID_TO_LINUX,
  type MountInfo,
} from "./remote-agent";

let sharedPage: Page;

/** Run a command on the remote host (the machine whose display is captured by the KVM). */
export function remoteHostExec(cmd: string, timeoutMs = 15000): string {
  const target = process.env.JETKVM_REMOTE_HOST;
  if (!target) throw new Error("JETKVM_REMOTE_HOST not set");
  // Use single-quote wrapping. Commands containing single quotes must
  // use the '\'' escape sequence (end quote, literal quote, resume quote).
  const escaped = cmd.replace(/'/g, "'\\''");
  let lastError: unknown;

  for (let attempt = 1; attempt <= 3; attempt++) {
    try {
      return execSync(`ssh ${SSH_OPTS} ${target} '${escaped}'`, {
        encoding: "utf8",
        timeout: timeoutMs,
      });
    } catch (error) {
      lastError = error;
      const msg = error instanceof Error ? error.message : String(error);
      const isTransient =
        msg.includes("Connection reset") ||
        msg.includes("Connection refused") ||
        msg.includes("Connection timed out") ||
        msg.includes("No route to host") ||
        msg.includes("ETIMEDOUT") ||
        msg.includes("timed out");

      if (!isTransient || attempt === 3) break;
      execSync("sleep 2");
    }
  }

  throw lastError;
}

export function getRemoteHostTtyACM(): string {
  return remoteHostExec('sh -lc "ls -1 /dev/ttyACM* 2>/dev/null | head -1 || true"', 5000).trim();
}

export async function waitForRemoteHostTtyACM(
  shouldExist: boolean,
  timeoutMs = 15000,
  intervalMs = 500,
): Promise<string> {
  const deadline = Date.now() + timeoutMs;

  while (Date.now() < deadline) {
    const ttyACM = getRemoteHostTtyACM();
    if (shouldExist ? ttyACM.includes("ttyACM") : ttyACM === "") {
      return ttyACM;
    }
    await new Promise(r => setTimeout(r, intervalMs));
  }

  const ttyACM = getRemoteHostTtyACM();
  if (shouldExist ? ttyACM.includes("ttyACM") : ttyACM === "") {
    return ttyACM;
  }

  throw new Error(
    shouldExist
      ? `ttyACM device did not appear on remote host within ${timeoutMs}ms`
      : `ttyACM device did not disappear from remote host within ${timeoutMs}ms`,
  );
}

/** Toggle DPMS on the remote host via GNOME ScreenSaver D-Bus API. */
export function remoteHostSetDPMS(off: boolean): void {
  remoteHostExec(
    `DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus ` +
      `gdbus call --session --dest org.gnome.ScreenSaver ` +
      `--object-path /org/gnome/ScreenSaver ` +
      `--method org.gnome.ScreenSaver.SetActive ${off ? "true" : "false"}`,
  );
}

export function remoteHostSupportsS3(): { supported: boolean; reason?: string } {
  let memSleep: string;
  try {
    memSleep = remoteHostExec("cat /sys/power/mem_sleep").trim();
  } catch {
    return { supported: false, reason: "Cannot read /sys/power/mem_sleep on remote host" };
  }

  if (!memSleep.includes("deep") && !memSleep.includes("[mem]")) {
    return { supported: false, reason: `S3 deep sleep not available (mem_sleep: ${memSleep})` };
  }

  return { supported: true };
}

export function mountKey(mount: MountInfo): string {
  return `${mount.device}|${mount.mount_point}`;
}

export function enableRemoteHostUSBWake(options: { includeRootHub?: boolean } = {}): void {
  // Keep parent/root hub wake disabled by default so the tests exercise the
  // JetKVM wake-capable HID function instead of generic USB bus activity.
  const enableParentHub = options.includeRootHub
    ? 'p=$(dirname "$(readlink -f "$d")")/power/wakeup; [ -f "$p" ] && echo enabled | sudo tee "$p" > /dev/null; '
    : "";

  remoteHostExec(
    "found=0; " +
      "for d in /sys/bus/usb/devices/*/; do " +
      '[ -f "$d/power/wakeup" ] && echo disabled | sudo tee "$d/power/wakeup" > /dev/null || true; ' +
      "done; " +
      "for d in /sys/bus/usb/devices/*/; do " +
      'if [ -f "$d/power/wakeup" ] && cat "$d/product" "$d/manufacturer" 2>/dev/null | grep -q JetKVM; then ' +
      'echo enabled | sudo tee "$d/power/wakeup" > /dev/null; ' +
      enableParentHub +
      "found=1; " +
      "fi; done; " +
      '[ "$found" -eq 1 ]',
  );
}

export function suspendRemoteHost(options: { wakeAfterSeconds?: number } = {}): void {
  const suspendCommand = options.wakeAfterSeconds
    ? `sleep 0.5 && rtcwake -m mem -s ${options.wakeAfterSeconds}`
    : "sleep 0.5 && echo mem > /sys/power/state";

  remoteHostExec(`sudo sh -c 'nohup sh -c "${suspendCommand}" >/dev/null 2>&1 &'`, 5000);
}

export async function waitForHostAsleep(timeoutMs = 15000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (!(await agent!.health())) {
      return;
    }
    await new Promise(r => setTimeout(r, 1000));
  }

  throw new Error(`Host did not enter sleep within ${timeoutMs}ms`);
}

export async function waitForHostAwake(timeoutMs = 60000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await agent!.health()) {
      return;
    }
    await new Promise(r => setTimeout(r, 1000));
  }

  throw new Error(`Host did not wake within ${timeoutMs}ms`);
}

export async function expectHostStaysAsleep(durationMs: number, intervalMs = 2000): Promise<void> {
  const deadline = Date.now() + durationMs;
  while (Date.now() < deadline) {
    expect(await agent!.health(), "Host woke while it should have stayed asleep").toBe(false);
    await new Promise(r => setTimeout(r, intervalMs));
  }
}

export async function waitForNoSignalOverlay(page: Page, timeoutMs = 20000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const videoState = (await callJsonRpc(page, "getVideoState")) as { error?: string };
      if (videoState.error === "no_signal") {
        return;
      }
    } catch {
      /* WebRTC may still be settling after host suspend */
    }
    await new Promise(r => setTimeout(r, 1000));
  }

  throw new Error(`Video state did not become no_signal within ${timeoutMs}ms`);
}

export async function putRemoteHostToSleepForUSBWakeTest(
  page: Page,
  options: { includeRootHubWake?: boolean; wakeAfterSeconds?: number } = {},
): Promise<void> {
  const s3 = remoteHostSupportsS3();
  test.skip(!s3.supported, s3.reason ?? "S3 sleep not supported");

  if (options.wakeAfterSeconds) {
    try {
      remoteHostExec("command -v rtcwake >/dev/null");
    } catch {
      test.skip(true, "rtcwake is not available on remote host");
    }
  }

  const localVersion = (await callJsonRpc(page, "getLocalVersion")) as {
    appVersion: string;
    systemVersion: string;
  };
  test.skip(
    !semverGte(localVersion.systemVersion, "0.2.8"),
    `S3 wake requires system >= 0.2.8 (got ${localVersion.systemVersion})`,
  );

  enableRemoteHostUSBWake({ includeRootHub: options.includeRootHubWake });
  await waitForVideoDimensions(page, 10000);
  expect(await agent!.health()).toBe(true);

  let suspendScheduled = false;
  try {
    try {
      suspendRemoteHost({ wakeAfterSeconds: options.wakeAfterSeconds });
      suspendScheduled = true;
    } catch {
      /* SSH may drop during suspend, that's expected */
      suspendScheduled = true;
    }

    await waitForHostAsleep();
    await waitForNoSignalOverlay(page);
  } catch (error) {
    if (suspendScheduled && options.wakeAfterSeconds) {
      await recoverRemoteHostAfterUSBWakeTest(page, options.wakeAfterSeconds * 1000 + 30000);
    }
    throw error;
  }
}

export async function recoverRemoteHostAfterUSBWakeTest(
  page: Page,
  timeoutMs = 120000,
): Promise<void> {
  await waitForHostAwake(timeoutMs);
  await page.goto("/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  await waitForVideoDimensions(page, 30000);
}

export async function wakeRemoteHostWithWakeButton(page: Page, timeoutMs = 45000): Promise<void> {
  await waitForNoSignalOverlay(page);
  const wakeButton = page.getByRole("button", { name: /^try to wake$/i }).first();
  await expect(wakeButton).toBeVisible({ timeout: 10000 });
  await wakeButton.click();

  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await agent!.health()) {
      return;
    }
    await new Promise(r => setTimeout(r, 1000));
  }

  throw new Error(`Host did not wake within ${timeoutMs}ms after clicking Try to wake`);
}

export async function sendBrowserInputThatMustNotWakeHost(page: Page): Promise<void> {
  await page.bringToFront();
  await page.locator("body").click({ position: { x: 10, y: 10 }, force: true });

  const video = page.locator("video").first();
  const box = await video.boundingBox();
  const videoX = box ? box.x + Math.max(12, box.width * 0.12) : 40;
  const videoY = box ? box.y + Math.max(12, box.height * 0.12) : 40;

  for (let i = 0; i < 8; i++) {
    await page.mouse.move(videoX + i * 7, videoY + i * 5);
    await page.mouse.down();
    await page.mouse.up();
    await page.mouse.wheel(i % 2 === 0 ? 120 : -120, i % 2 === 0 ? 0 : 120);
    await page.keyboard.press("Space");
    await page.keyboard.press("KeyA");
    await page.keyboard.press("Escape");

    await page.evaluate(() => {
      window.dispatchEvent(new Event("blur"));
      document.dispatchEvent(new Event("visibilitychange"));
      window.dispatchEvent(new Event("focus"));
    });

    await new Promise(r => setTimeout(r, 500));
  }
}

export const agent = createRemoteAgent();

export const TEST_MACROS = [
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

export async function setupMacrosViaSSH() {
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

export const USB_DEFAULT_CONFIG = {
  vendor_id: "0x1d6b",
  product_id: "0x0104",
  serial_number: "",
  manufacturer: "JetKVM",
  product: "USB Emulation Device",
};

export const USB_LOGITECH_CONFIG = {
  vendor_id: "0x046d",
  product_id: "0xc52b",
  serial_number: "1234567&0&1",
  manufacturer: "Logitech (x64)",
  product: "Logitech USB Input Device",
};

export const USB_DEVICES_DEFAULT = {
  keyboard: true,
  absolute_mouse: true,
  relative_mouse: true,
  mass_storage: true,
  audio: true,
};

export const USB_DEVICES_KEYBOARD_ONLY = {
  keyboard: true,
  absolute_mouse: false,
  relative_mouse: false,
  mass_storage: false,
};

export const USB_DEVICES_REL_MOUSE_ONLY = {
  keyboard: true,
  absolute_mouse: false,
  relative_mouse: true,
  mass_storage: true,
};

export const ID_DEFAULT = "1d6b:0104";

export const ID_LOGITECH = "046d:c52b";

export function isJsonRpcTimeout(err: unknown, method: string): boolean {
  return err instanceof Error && err.message.includes(`RPC timeout for ${method}`);
}

export async function sendUsbReconfigRpc(
  method: "setUsbDevices" | "setUsbConfig",
  params: Record<string, unknown>,
): Promise<void> {
  try {
    await callJsonRpc(sharedPage, method, params);
  } catch (err) {
    if (!isJsonRpcTimeout(err, method)) throw err;
  }
}

// When the host xHCI misses a gadget re-enumeration, waiting longer never
// converges — re-send the RPC each round (it always unbind+rebinds the UDC,
// which re-kicks host enumeration).
export async function usbReconfigWithRetry<T>(
  method: "setUsbDevices" | "setUsbConfig",
  params: Record<string, unknown>,
  waitForState: (attemptMs: number) => Promise<T>,
  timeoutMs: number,
): Promise<T> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    await sendUsbReconfigRpc(method, params);
    const attemptMs = Math.max(Math.min(15_000, deadline - Date.now()), 1_000);
    try {
      return await waitForState(attemptMs);
    } catch (err) {
      if (Date.now() >= deadline) throw err;
    }
  }
}

export async function setUsbDevicesAndWait(
  devices: typeof USB_DEVICES_DEFAULT,
  expectedTypes: string[],
  timeoutMs = 45_000,
) {
  return usbReconfigWithRetry(
    "setUsbDevices",
    { devices },
    attemptMs => agent!.waitForInputDevices(expectedTypes, attemptMs),
    timeoutMs,
  );
}

export async function setUsbConfigAndWait(
  usbConfig: typeof USB_DEFAULT_CONFIG,
  expectedId: string,
  timeoutMs = 45_000,
) {
  return usbReconfigWithRetry(
    "setUsbConfig",
    { usbConfig },
    attemptMs => agent!.waitForUSBDevice(d => d.id === expectedId, true, attemptMs),
    timeoutMs,
  );
}

// Pre-built key list for batched keyboard scan test
export const ALL_SCAN_KEYS = (() => {
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

export async function ensureNoPasswordViaAPI() {
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

export async function setupMacrosViaRPC(page: Page, retries = 3) {
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

export function registerSharedSession(onPage: (page: Page) => void): void {
  test.beforeAll(async ({ browser }) => {
    test.skip(!agent, "JETKVM_REMOTE_HOST not set");

    await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);

    sharedPage = await browser.newPage();
    onPage(sharedPage);
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

    await ensureRpcReady(sharedPage);

    await setupMacrosViaRPC(sharedPage);
    await sharedPage.reload({ waitUntil: "networkidle" });
    await ensureRpcReady(sharedPage);

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

  // Snapshot /userdata/jetkvm/last.log into the failing test's output dir before
  // any subsequent test reboots the device (RkLunch's `> last.log` at boot wipes
  // the log, and /oem is read-only so we can't change that). This makes the
  // capture race-free regardless of what later tests do.
  // Empty fixture destructure is required by Playwright; `_` would fail the
  // runtime "destructuring pattern" check.
  // oxlint-disable-next-line no-empty-pattern
  test.afterEach(async ({}, testInfo) => {
    if (!agent) return;
    if (testInfo.status === testInfo.expectedStatus) return;
    const log = await sshExec("cat /userdata/jetkvm/last.log", true);
    try {
      await testInfo.attach("device-last.log", { body: log, contentType: "text/plain" });
    } catch {
      // attach can throw if the worker is already tearing down; sshExec(_, true) won't.
    }
  });
}
