import type { Frame, Page, TestInfo } from "@playwright/test";
import type { RemoteAgent } from "./remote-agent";

// Test-only capture. Keep the original events, including full host snapshots
// before a test clears them, so a failing assertion cannot discard evidence.
function installBrowserCapture() {
  const w = window as {
    __keyboardCapture?: {
      active: boolean;
      records: unknown[];
      dropped: number;
    };
  };
  if (w.__keyboardCapture) {
    w.__keyboardCapture.active = true;
    w.__keyboardCapture.records = [];
    w.__keyboardCapture.dropped = 0;
    return;
  }
  const capture = (w.__keyboardCapture = { active: true, records: [] as unknown[], dropped: 0 });
  const record = (kind: string, data: unknown) => {
    if (!capture.active) return;
    if (capture.records.length >= 10000) {
      capture.dropped++;
      return;
    }
    capture.records.push({ kind, time: Date.now(), monotonic: performance.now(), data });
  };
  const bytes = (data: ArrayBuffer | ArrayBufferView) =>
    Array.from(
      data instanceof ArrayBuffer
        ? new Uint8Array(data)
        : new Uint8Array(data.buffer, data.byteOffset, data.byteLength),
    );
  const watched = new WeakSet<RTCDataChannel>();
  const send = RTCDataChannel.prototype.send;
  RTCDataChannel.prototype.send = function (data: Parameters<typeof send>[0]) {
    if (!this.label.startsWith("hidrpc")) return send.call(this, data);
    if (!watched.has(this)) {
      watched.add(this);
      this.addEventListener("message", event => {
        const value = bytes(event.data);
        if ([0x01, 0x32, 0x33, 0x34].includes(value[0])) {
          record("receive", { channel: this.label, bytes: value });
        }
      });
      this.addEventListener("close", () => record("close", { channel: this.label }));
    }
    const value = bytes(data as ArrayBufferView);
    const keyboard = [0x01, 0x02, 0x05, 0x07, 0x08, 0x09].includes(value[0]);
    try {
      const result = send.call(this, data);
      if (keyboard) record("send", { channel: this.label, bytes: value });
      return result;
    } catch (error) {
      if (keyboard)
        record("send-error", { channel: this.label, bytes: value, error: String(error) });
      throw error;
    }
  };
  window.addEventListener("pagehide", () => {
    if (capture.active)
      console.debug(
        "[keyboard-capture]" +
          JSON.stringify({
            capture,
            url: location.href,
            documentStarted: performance.timeOrigin,
          }),
      );
  });
}

const initialized = new WeakSet<Page>();

export async function captureKeyboard(page: Page, agent: RemoteAgent) {
  const documents: unknown[] = [];
  const navigations: { time: number; url: string }[] = [];
  const onNavigation = (frame: Frame) => {
    if (frame === frame.page().mainFrame()) {
      navigations.push({ time: Date.now(), url: frame.url() });
    }
  };
  const onConsole = (message: import("@playwright/test").ConsoleMessage) => {
    const text = message.text();
    if (text.startsWith("[keyboard-capture]")) {
      documents.push(JSON.parse(text.slice("[keyboard-capture]".length)));
    }
  };
  const pages = new Set<Page>();
  const watchPage = async (target: Page) => {
    if (pages.has(target)) return;
    pages.add(target);
    target.on("console", onConsole);
    target.on("framenavigated", onNavigation);
    onNavigation(target.mainFrame());
    if (!initialized.has(target)) {
      await target.addInitScript(installBrowserCapture);
      initialized.add(target);
    }
    await target.evaluate(installBrowserCapture);
  };
  const stopPage = async (target: Page) => {
    const snapshot = await target
      .evaluate(() => {
        const capture = (window as { __keyboardCapture?: { active: boolean } }).__keyboardCapture;
        if (capture) capture.active = false;
        return { capture, url: location.href, documentStarted: performance.timeOrigin };
      })
      .catch(error => ({ error: String(error), url: target.url() }));
    documents.push(snapshot);
    target.off("console", onConsole);
    target.off("framenavigated", onNavigation);
    pages.delete(target);
  };
  await watchPage(page);

  // Restore the original methods; invoke them with the agent below.
  // oxlint-disable unbound-method
  const get = agent.getKeyboardEvents;
  const clear = agent.clearKeyboardEvents;
  // oxlint-enable unbound-method
  const host: unknown[] = [];
  agent.getKeyboardEvents = async signal => {
    const events = await get.call(agent, signal);
    host.push({ time: Date.now(), events });
    return events;
  };
  agent.clearKeyboardEvents = async () => {
    // The pop endpoint returns and clears under one lock; a GET followed by
    // DELETE would discard events arriving between the two requests.
    const events = await agent.popKeyboardEvents();
    host.push({ time: Date.now(), events, cleared: true });
  };

  const finish = async (testInfo: TestInfo) => {
    agent.getKeyboardEvents = get;
    agent.clearKeyboardEvents = clear;
    const retain =
      testInfo.status !== testInfo.expectedStatus || !!process.env.JETKVM_CAPTURE_KEYBOARD;
    for (const target of pages) await stopPage(target);
    if (!retain) return;
    try {
      host.push({ time: Date.now(), events: await get.call(agent, AbortSignal.timeout(2000)) });
    } catch (error) {
      host.push({ time: Date.now(), error: String(error) });
    }
    await testInfo.attach("keyboard-delivery", {
      body: JSON.stringify({ documents, navigations, host }),
      contentType: "application/json",
    });
  };
  return Object.assign(finish, { watchPage, stopPage });
}
