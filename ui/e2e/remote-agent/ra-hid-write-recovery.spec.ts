/**
 * E2E regression test for issue #1512: a UDC rebind can leave /dev/hidg0
 * openable but non-functional — every keyboard report write times out while
 * the UDC still reports "configured". The app must detect this and escalate
 * to a full gadget reconfigure on its own instead of silently dropping
 * keyboard input until the user manually cycles identifier profiles.
 *
 * The broken state is reproduced deterministically from the host side:
 * unbinding usbhid from the gadget's keyboard interface stops the host from
 * polling the interrupt IN endpoint, so gadget-side keyboard writes hit their
 * write deadline while the gadget stays "configured" — the exact failure
 * signature from the issue (mouse alive, keyboard silently dead).
 *
 * The self-recovery reconfigure forces the host to re-enumerate the gadget,
 * which rebinds usbhid to the fresh interface and restores keyboard input.
 *
 * Run with:
 *   JETKVM_URL=http://<kvm-ip> JETKVM_REMOTE_HOST=<user@host> \
 *     npx playwright test --project=remote-agent ra-hid-write-recovery
 */
import { execSync } from "child_process";
import { test, expect, type Page } from "@playwright/test";
import { HID_KEY, SSH_OPTS, ensureNoPasswordViaAPI, ensureRpcReady, tapKey } from "../helpers";
import { createRemoteAgent, KEY, type RemoteAgent } from "./remote-agent";

const agent = createRemoteAgent();

test.describe.configure({ mode: "serial" });

let page: Page;

/** Run a command on the remote host (the machine the KVM's USB plugs into). */
function remoteHostExec(cmd: string, timeoutMs = 15000): string {
  const target = process.env.JETKVM_REMOTE_HOST;
  if (!target) throw new Error("JETKVM_REMOTE_HOST not set");
  const escaped = cmd.replace(/'/g, "'\\''");
  return execSync(`ssh ${SSH_OPTS} ${target} '${escaped}'`, {
    encoding: "utf8",
    timeout: timeoutMs,
  });
}

/**
 * Find the JetKVM gadget's boot-protocol keyboard interface on the host
 * (HID class 03, protocol 01), e.g. "1-2:1.0".
 */
function findGadgetKeyboardInterface(): string {
  const out = remoteHostExec(
    "for d in /sys/bus/usb/devices/*-*/; do " +
      '[ -f "$d/manufacturer" ] || continue; ' +
      'grep -qi jetkvm "$d/manufacturer" 2>/dev/null || continue; ' +
      'for i in "$d"*:*/; do ' +
      '[ -f "$i/bInterfaceClass" ] || continue; ' +
      '[ "$(cat "$i/bInterfaceClass")" = "03" ] || continue; ' +
      '[ "$(cat "$i/bInterfaceProtocol")" = "01" ] && basename "$i"; ' +
      "done; " +
      "done",
  );
  return out.trim().split("\n")[0] ?? "";
}

/** Keyboard round-trip: tap Space until the host observes it or timeout. */
async function waitForKeyboardRoundTrip(
  ra: RemoteAgent,
  p: Page,
  timeoutMs: number,
  perTryMs = 3000,
) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      return await ra.expectKeyPress(
        KEY.SPACE,
        async () => {
          await tapKey(p, HID_KEY.SPACE);
        },
        perTryMs,
      );
    } catch {
      /* keyboard not (yet) delivering events */
    }
  }
  return [];
}

test.beforeAll(async ({ browser }) => {
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");

  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);

  page = await browser.newPage();
  await page.goto("/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 30000);
});

test.afterAll(async () => {
  if (page) await page.close();
});

test("keyboard self-recovers when HID writes time out while USB stays configured (#1512)", async () => {
  test.setTimeout(240_000);

  // Sanity: the keyboard path works before we break it.
  const before = await waitForKeyboardRoundTrip(agent!, page, 30_000);
  expect(before.length, "keyboard round-trip must work before the test").toBeGreaterThan(0);

  const iface = findGadgetKeyboardInterface();
  expect(iface, "JetKVM keyboard interface not found on remote host").toMatch(/^[\d.-]+:\d+\.\d+$/);

  try {
    // Stop the host from polling the keyboard interrupt endpoint. From the
    // gadget's point of view this is the post-rebind broken state: the UDC
    // stays "configured" but every /dev/hidg0 write times out.
    remoteHostExec(`echo -n "${iface}" | sudo tee /sys/bus/usb/drivers/usbhid/unbind > /dev/null`);

    // Generate keyboard traffic so the gadget accumulates consecutive write
    // timeouts. Old firmware swallows these forever; fixed firmware counts
    // them and escalates to a full gadget reconfigure.
    for (let i = 0; i < 8; i++) {
      await tapKey(page, HID_KEY.SPACE);
      await new Promise(r => setTimeout(r, 250));
    }

    // The reconfigure re-enumerates the gadget on the host, usbhid rebinds
    // to the fresh interface, and key events flow again — without any manual
    // identifier cycling.
    const events = await waitForKeyboardRoundTrip(agent!, page, 120_000);
    expect(
      events.length,
      "keyboard did not self-recover after HID write timeouts (issue #1512)",
    ).toBeGreaterThan(0);
  } finally {
    // Failsafe for the failing (unfixed) case: rebind usbhid so the host
    // keyboard is not left dead. Harmless if recovery already re-enumerated.
    remoteHostExec(
      `echo -n "${iface}" | sudo tee /sys/bus/usb/drivers/usbhid/bind > /dev/null 2>&1 || true`,
    );
  }
});
