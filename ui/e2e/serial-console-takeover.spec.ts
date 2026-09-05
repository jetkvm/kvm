import { test, expect, type Browser, type Page } from "@playwright/test";
import {
  callJsonRpc,
  ensureNoPasswordViaAPI,
  ensureRpcReady,
  sshExec,
  waitForWebRTCReady,
} from "./helpers";

// Every session opens a serial data channel and the device makes it the
// console broker's sink. On a takeover the device closes the replaced peer
// connection a second after answering the new one, so the old channel's
// close lands after the new channel became the sink. That close used to
// clear the sink unconditionally and the new session's console went silent.

declare global {
  interface Window {
    __e2eSerial?: { dc: RTCDataChannel; rx: string[] };
  }
}

// Hooks the page's own serial channel so the test can watch what the device
// sends on it without going through the terminal UI.
async function hookSerialChannel(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const create = RTCPeerConnection.prototype.createDataChannel;
    RTCPeerConnection.prototype.createDataChannel = function (label, init) {
      const dc = create.call(this, label, init);
      if (label === "serial") {
        const hook = { dc, rx: [] as string[] };
        dc.addEventListener("message", e => hook.rx.push(String(e.data)));
        window.__e2eSerial = hook;
      }
      return dc;
    };
  });
}

// The device writes only to the channel that owns the sink. With echo on,
// what a session sends comes back to it through the broker, which makes sink
// ownership observable end to end.
async function expectEcho(page: Page, tag: string): Promise<void> {
  const text = `${tag}_${Date.now()}`;
  await page.evaluate(text => {
    const hook = window.__e2eSerial;
    if (!hook || hook.dc.readyState !== "open") throw new Error("serial channel not open");
    hook.dc.send(JSON.stringify({ type: "serial", data: `${text}\n` }));
  }, text);
  await expect
    .poll(
      async () =>
        (await page.evaluate(() => window.__e2eSerial?.rx ?? [])).some(m => m.includes(text)),
      {
        message: `${tag}: echo should arrive on this session's serial channel`,
        timeout: 5_000,
      },
    )
    .toBe(true);
}

const SERIAL_SETTINGS_PATH = "/userdata/serialSettings.json";

// The device's built-in defaults; getSerialSettings errors until a settings
// file exists.
const DEFAULT_SERIAL_SETTINGS = {
  baudRate: 115200,
  dataBits: 8,
  parity: "none",
  stopBits: "1",
  terminator: { label: "LF (\\n)", value: "\n" },
  hideSerialSettings: false,
  enableEcho: false,
  normalizeMode: "names",
  normalizeLineEnd: "keep",
  tabRender: "",
  preserveANSI: true,
  showNLTag: false,
  buttons: [],
};

test.describe("serial console sink across a session takeover", () => {
  let hadSettingsFile = false;
  let settings: Record<string, unknown> = DEFAULT_SERIAL_SETTINGS;

  async function withPage(browser: Browser, fn: (page: Page) => Promise<void>): Promise<void> {
    const page = await browser.newPage();
    try {
      await page.goto("/", { waitUntil: "networkidle" });
      await ensureRpcReady(page);
      await fn(page);
    } finally {
      await page.close();
    }
  }

  test.beforeAll(async ({ browser }) => {
    await ensureNoPasswordViaAPI();
    hadSettingsFile =
      (await sshExec(`[ -f ${SERIAL_SETTINGS_PATH} ] && echo yes || echo no`)).trim() === "yes";
    await withPage(browser, async page => {
      if (hadSettingsFile) {
        settings = (await callJsonRpc(page, "getSerialSettings")) as Record<string, unknown>;
      }
      await callJsonRpc(page, "setSerialSettings", { settings: { ...settings, enableEcho: true } });
    });
  });

  test.afterAll(async ({ browser }) => {
    await withPage(browser, async page => {
      await callJsonRpc(page, "setSerialSettings", { settings });
    });
    if (!hadSettingsFile) await sshExec(`rm -f ${SERIAL_SETTINGS_PATH}`, true);
  });

  test("the new session keeps its serial sink when the replaced session's channel closes", async ({
    browser,
  }) => {
    test.setTimeout(60_000);

    const first = await browser.newPage();
    await hookSerialChannel(first);
    await first.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(first);
    await expectEcho(first, "first");

    const second = await browser.newPage();
    await hookSerialChannel(second);
    await second.goto("/", { waitUntil: "networkidle" });
    await waitForWebRTCReady(second);
    await expectEcho(second, "second");

    // The device closes the first peer connection a second after the
    // takeover. The browser reports the channel closed once that happened.
    await expect
      .poll(() => first.evaluate(() => window.__e2eSerial?.dc.readyState), {
        message: "replaced session's serial channel should close",
        timeout: 15_000,
      })
      .toBe("closed");

    await expectEcho(second, "after_close");

    await first.close();
    await second.close();
  });
});
