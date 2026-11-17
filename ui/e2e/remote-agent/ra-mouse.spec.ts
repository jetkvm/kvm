import { test, expect, type Page } from "@playwright/test";
import { callJsonRpc, sendAbsMouseMove } from "../helpers";
import { type MouseEvent as RAMouseEvent } from "./remote-agent";
import {
  USB_DEVICES_DEFAULT,
  USB_DEVICES_REL_MOUSE_ONLY,
  agent,
  registerSharedSession,
  setUsbDevicesAndWait,
} from "./shared";

test.describe.configure({ mode: "serial" });

let sharedPage: Page;
registerSharedSession(page => (sharedPage = page));

test.describe("Remote Host Agent: mouse", () => {
  // ═══════════════════════════════════════════
  // MOUSE
  // ═══════════════════════════════════════════

  test("mouse: movement, corners, rapid input, and position values", async () => {
    // Default 60s can be eaten by priming retries; keep headroom for the rest.
    test.setTimeout(90_000);

    // A previous test can leave the host pointer at the center. Wait until at
    // least one priming move is visible before clearing events; otherwise the
    // first asserted center move can be a no-op and produce no Linux input event.
    // Re-send on timeout — a batch sent right after session churn can be lost.
    await expect(async () => {
      await agent!.expectMouseMove(async () => {
        await sendAbsMouseMove(sharedPage, 0, 0);
        await sendAbsMouseMove(sharedPage, 32767, 32767);
        await sendAbsMouseMove(sharedPage, 0, 0);
      }, 3000);
    }).toPass({ timeout: 20_000 });
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
    test.setTimeout(90_000);
    const REL_WHEEL = 0x08;
    const REL_HWHEEL = 0x06;

    await callJsonRpc(sharedPage, "setUsbDevices", { devices: USB_DEVICES_REL_MOUSE_ONLY });
    await agent!.waitForInputDevices(["keyboard", "relative_mouse"], 15000);

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

      // Horizontal scroll can hit the same post-re-enumeration race as vertical.
      const hDeadline = Date.now() + 10000;
      let hWheel: RAMouseEvent[] = [];
      while (Date.now() < hDeadline) {
        await agent!.clearMouseEvents();
        await callJsonRpc(sharedPage, "wheelReport", { wheelY: 0, wheelX: 1 });
        try {
          hWheel = await agent!.waitForMouseEvent(
            ev => ev.type === "mouse_move_rel" && ev.code === REL_HWHEEL,
            2000,
          );
          break;
        } catch {
          /* agent not ready yet, retry */
        }
      }
      expect(hWheel.length, "Horizontal wheel in relative-only mode").toBeGreaterThan(0);
      expect(hWheel[0].value).not.toBe(0);
    } finally {
      // Host xHCI re-enumeration regularly exceeds 10s; use the 45s default.
      await setUsbDevicesAndWait(USB_DEVICES_DEFAULT, [
        "keyboard",
        "absolute_mouse",
        "relative_mouse",
      ]);
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
});
