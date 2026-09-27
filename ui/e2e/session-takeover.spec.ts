import { test, expect, type Browser, type Page } from "@playwright/test";

import { ensureNoPasswordViaAPI, ensureRpcReady, rawJsonRpc, waitForWebRTCReady } from "./helpers";

// A second page takes the one WebRTC session over. The first page must show
// the other-session screen and stay there without opening signaling again,
// and the second must keep its session, until the first selects Use Here.

interface Observed {
  signalingOpens: number;
  signalingCloses: [number, number][];
  peers: number;
}

const HOLD_MS = 30_000;
// The JetKVM closes the old peer 1 s after the takeover, and the browser can
// take several more seconds to see it; after this the old page must be down.
const OLD_PEER_GRACE_MS = 10_000;

// Two tabs of one browser, as a user opens them. The init script runs in each.
async function observedPages(browser: Browser): Promise<[Page, Page]> {
  const context = await browser.newContext({ baseURL: process.env.JETKVM_URL });
  await context.addInitScript(() => {
    const seen = { signalingOpens: 0, signalingCloses: [], peers: 0 } as Observed;
    (window as unknown as { __takeover: Observed }).__takeover = seen;
    const NativeWebSocket = window.WebSocket;
    window.WebSocket = class extends NativeWebSocket {
      constructor(url: string | URL, protocols?: string | string[]) {
        super(url, protocols);
        if (String(url).includes("/webrtc/signaling")) {
          seen.signalingOpens++;
          this.addEventListener("close", event =>
            seen.signalingCloses.push([Date.now(), event.code]),
          );
        }
      }
    };
    const NativePeer = window.RTCPeerConnection;
    window.RTCPeerConnection = class extends NativePeer {
      constructor(config?: RTCConfiguration) {
        super(config);
        seen.peers++;
      }
    };
  });
  return [await context.newPage(), await context.newPage()];
}

const observed = (page: Page) =>
  page.evaluate(() => (window as unknown as { __takeover: Observed }).__takeover);

// A page that never connects still gets observed, so the failure shows why.
async function waitForSession(page: Page, label: string) {
  await waitForWebRTCReady(page).catch(() =>
    console.log(`[takeover] ${label} had no WebRTC session within 30 s`),
  );
}

const useHere = (page: Page) => page.getByRole("button", { name: "Use Here" });

/** Watch both pages for `ms`: `idle` must show Use Here, `active` must stay
 * connected, and neither may open signaling or create a peer again. */
async function expectSettled(idle: Page, active: Page, ms: number, label: string) {
  const idleBefore = await observed(idle);
  const activeBefore = await observed(active);
  const samples: {
    t: number;
    idleUseHere: boolean;
    idleConnected: boolean;
    activeConnected: boolean;
  }[] = [];
  const start = Date.now();
  while (Date.now() - start < ms) {
    samples.push({
      t: Date.now() - start,
      idleUseHere: await useHere(idle).isVisible(),
      idleConnected: await idle.evaluate(
        () => window.__kvmTestHooks?.isWebRTCConnected?.() ?? false,
      ),
      activeConnected: await active.evaluate(
        () => window.__kvmTestHooks?.isWebRTCConnected?.() ?? false,
      ),
    });
    await idle.waitForTimeout(1_000);
  }
  const idleAfter = await observed(idle);
  const activeAfter = await observed(active);
  const summary = {
    label,
    idleSignalingOpens: idleAfter.signalingOpens - idleBefore.signalingOpens,
    idlePeers: idleAfter.peers - idleBefore.peers,
    activeSignalingOpens: activeAfter.signalingOpens - activeBefore.signalingOpens,
    activePeers: activeAfter.peers - activeBefore.peers,
    idleWithoutUseHere: samples.filter(s => !s.idleUseHere).length,
    activeDisconnected: samples.filter(s => !s.activeConnected).length,
    idleConnected: samples.filter(s => s.idleConnected && s.t >= OLD_PEER_GRACE_MS).length,
    samples: samples.length,
    idleCloseCodes: idleAfter.signalingCloses.map(([, code]) => code),
    activeCloseCodes: activeAfter.signalingCloses.map(([, code]) => code),
  };
  console.log(`[takeover] ${JSON.stringify(summary)}`);
  expect(summary, label).toMatchObject({
    idleSignalingOpens: 0,
    idlePeers: 0,
    activeSignalingOpens: 0,
    activePeers: 0,
    idleWithoutUseHere: 0,
    activeDisconnected: 0,
    idleConnected: 0,
  });
}

test.describe("session takeover", () => {
  test.beforeAll(async () => {
    await ensureNoPasswordViaAPI();
  });

  test("a live session is taken over and handed back", async ({ browser }) => {
    test.setTimeout(3 * HOLD_MS + 90_000);
    const [first, second] = await observedPages(browser);
    try {
      await first.goto("/", { waitUntil: "domcontentloaded" });
      await ensureRpcReady(first);

      await second.goto("/", { waitUntil: "domcontentloaded" });
      await waitForSession(second, "second page");
      await useHere(first)
        .waitFor({ timeout: 5_000 })
        .catch(() => undefined);
      await expectSettled(first, second, HOLD_MS, "second took over");
      await rawJsonRpc(second, "getDeviceID", {}, 5_000);

      await useHere(first).click();
      await waitForWebRTCReady(first);
      await useHere(second)
        .waitFor({ timeout: 5_000 })
        .catch(() => undefined);
      await expectSettled(second, first, HOLD_MS, "first used here");
      await rawJsonRpc(first, "getDeviceID", {}, 5_000);
    } finally {
      await first.context().close();
    }
  });
});
