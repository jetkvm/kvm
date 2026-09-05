import { execSync } from "child_process";
import { test, expect, type Page } from "@playwright/test";
import {
  HID_KEY,
  SSH_OPTS,
  ensureNoPasswordViaAPI,
  ensureRpcReady,
  getLedState,
  restartAppViaSSH,
  sendAbsMouseMove,
  sendKeypress,
  tapKey,
  waitForLedState,
  waitForWebRTCReady,
} from "../helpers";
import { KEY, createRemoteAgent, waitForKeyboardReady } from "./remote-agent";

// An app restart keeps the previous instance's gadget bound when nothing in
// the configuration changed, so the host never sees a disconnect. These tests
// pin the two things the new process has to do on its own in that case:
// release inputs the old process left pressed, and carry over the host's LED
// state, which the host only sends again after an enumeration.

const agent = createRemoteAgent();

test.describe.configure({ mode: "serial" });

let page: Page;

function remoteHostExec(cmd: string, timeoutMs = 15_000): string {
  const target = process.env.JETKVM_REMOTE_HOST;
  if (!target) throw new Error("JETKVM_REMOTE_HOST not set");
  const escaped = cmd.replace(/'/g, "'\\''");
  return execSync(`ssh ${SSH_OPTS} ${target} '${escaped}'`, {
    encoding: "utf8",
    timeout: timeoutMs,
  });
}

// The host assigns a new device number on every enumeration. An unchanged
// number across the restart proves the gadget was adopted, so whatever the
// host observed afterwards came from the new process, not from a re-plug.
function gadgetDeviceNumber(): number {
  const out = remoteHostExec(
    "for d in /sys/bus/usb/devices/*-*/; do " +
      '[ -f "$d/manufacturer" ] || continue; ' +
      'grep -qi jetkvm "$d/manufacturer" 2>/dev/null || continue; ' +
      'cat "$d/devnum"; break; ' +
      "done",
  ).trim();
  const devnum = parseInt(out, 10);
  expect(devnum, "gadget not found on the remote host").toBeGreaterThan(0);
  return devnum;
}

async function reconnect(): Promise<void> {
  await page.goto("/", { waitUntil: "networkidle" });
  await waitForWebRTCReady(page);
  await ensureRpcReady(page);
}

test.beforeAll(async ({ browser }) => {
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");
  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);

  page = await browser.newPage();
  await reconnect();
  await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 30_000);
});

test.afterAll(async () => {
  if (page) await page.close();
});

test("app restart releases keys the previous process left held", async () => {
  test.setTimeout(90_000);

  expect(
    (await waitForKeyboardReady(agent!, page, 30_000)).length,
    "keyboard must work before the test",
  ).toBeGreaterThan(0);

  await agent!.clearKeyboardEvents();
  await sendKeypress(page, HID_KEY.LEFT_SHIFT, true);
  await sendKeypress(page, HID_KEY.SPACE, true);

  await expect
    .poll(
      async () => {
        const events = await agent!.getKeyboardEvents();
        return (
          events.some(ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_press") &&
          events.some(ev => ev.code === KEY.SPACE && ev.type === "key_press")
        );
      },
      { message: "host should see both keys pressed", timeout: 5_000 },
    )
    .toBe(true);

  const before = gadgetDeviceNumber();

  // SIGTERM: the old process exits without releasing anything.
  await restartAppViaSSH();

  await expect
    .poll(
      async () => {
        const events = await agent!.getKeyboardEvents();
        return (
          events.some(ev => ev.code === KEY.LEFT_SHIFT && ev.type === "key_release") &&
          events.some(ev => ev.code === KEY.SPACE && ev.type === "key_release")
        );
      },
      { message: "new process should release both keys on the host", timeout: 15_000 },
    )
    .toBe(true);

  expect(gadgetDeviceNumber(), "gadget must not re-enumerate on app restart").toBe(before);

  await reconnect();
});

test("app restart keeps the host's keyboard LED state", async () => {
  test.setTimeout(90_000);

  expect(
    (await waitForKeyboardReady(agent!, page, 30_000)).length,
    "keyboard must work before the test",
  ).toBeGreaterThan(0);

  const initial = await getLedState(page);
  expect(initial, "LED state should be available").not.toBeNull();
  const toggled = !initial!.caps_lock;

  await tapKey(page, HID_KEY.CAPS_LOCK);
  await waitForLedState(page, "caps_lock", toggled);

  const before = gadgetDeviceNumber();

  try {
    await restartAppViaSSH();
    await reconnect();

    expect(gadgetDeviceNumber(), "gadget must not re-enumerate on app restart").toBe(before);

    // Nothing toggled Caps Lock since the restart and the host sent no new
    // LED report, so this is the state the new process carried over.
    await waitForLedState(page, "caps_lock", toggled, 10_000);
  } finally {
    await tapKey(page, HID_KEY.CAPS_LOCK);
    await waitForLedState(page, "caps_lock", initial!.caps_lock).catch(() => {});
  }
});

test("app restart releases an absolute mouse button once", async () => {
  test.setTimeout(120_000);

  const press = { x: 16384, y: 16384 };
  const away = { x: 8192, y: 8192 };

  await agent!.clearMouseEvents();
  await sendAbsMouseMove(page, press.x, press.y, 1);
  await expect
    .poll(
      async () =>
        (await agent!.getMouseEvents()).some(ev => ev.type === "mouse_button" && ev.value === 1),
      { message: "host should see the button pressed", timeout: 5_000 },
    )
    .toBe(true);

  const before = gadgetDeviceNumber();
  await restartAppViaSSH();

  await expect
    .poll(
      async () =>
        (await agent!.getMouseEvents()).some(ev => ev.type === "mouse_button" && ev.value === 0),
      { message: "new process should release the button on the host", timeout: 15_000 },
    )
    .toBe(true);
  expect(gadgetDeviceNumber(), "gadget must not re-enumerate on app restart").toBe(before);

  // Park the cursor elsewhere. A release replayed at the press position on
  // the next restart would show up on the host as a move back to it.
  await reconnect();
  await agent!.expectMouseMove(() => sendAbsMouseMove(page, away.x, away.y));
  await agent!.clearMouseEvents();

  await restartAppViaSSH();
  await new Promise(r => setTimeout(r, 3_000));
  expect(await agent!.getMouseEvents(), "second restart must not replay the release").toEqual([]);
  expect(gadgetDeviceNumber(), "gadget must not re-enumerate on app restart").toBe(before);

  await reconnect();
});
