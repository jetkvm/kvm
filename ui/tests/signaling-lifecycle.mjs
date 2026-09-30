// Offline browser regression: serves a built UI and intercepts signaling locally.
// No request or peer connection is made to a physical device.
// Run from ui/ after npm run build:device: node tests/signaling-lifecycle.mjs [build dir]
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { resolve, extname } from "node:path";
import { chromium } from "@playwright/test";

const dist = resolve(process.argv[2] || "../static");
const executablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE;
const mime = {
  ".js": "text/javascript",
  ".css": "text/css",
  ".html": "text/html",
  ".svg": "image/svg+xml",
  ".woff2": "font/woff2",
};
const server = createServer(async (req, res) => {
  const path = new URL(req.url, "http://localhost").pathname;
  if (path === "/device/status" || path === "/device") {
    res.setHeader("Content-Type", "application/json");
    res.end(
      JSON.stringify(
        path.endsWith("/status")
          ? { isSetup: true }
          : { authMode: "noPassword", deviceId: "offline-test" },
      ),
    );
    return;
  }
  try {
    const file = path.startsWith("/static/")
      ? resolve(dist, path.slice(8))
      : resolve(dist, "index.html");
    if (!file.startsWith(dist + "/")) throw new Error("invalid path");
    res.setHeader("Content-Type", mime[extname(file)] || "application/octet-stream");
    res.end(await readFile(file));
  } catch {
    res.writeHead(404);
    res.end();
  }
});
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch({
  headless: true,
  ...(executablePath ? { executablePath } : {}),
});
const failures = [];

async function waitFor(predicate, message) {
  const end = Date.now() + 10000;
  while (Date.now() < end) {
    if (await predicate()) return;
    await new Promise(resolve => setTimeout(resolve, 20));
  }
  throw new Error(message);
}

async function fixture({ deferFirstOffer = false, takeoverHub } = {}) {
  const context = await browser.newContext();
  const sockets = [];
  const messages = [];
  await context.addInitScript(
    ({ deferFirstOffer }) => {
      const NativePeer = window.RTCPeerConnection;
      window.probe = { peers: [], releaseOffer: null, localPending: false };
      window.RTCPeerConnection = class extends NativePeer {
        constructor(config) {
          super(config);
          window.probe.peers.push(this);
        }
        async setLocalDescription(description) {
          await super.setLocalDescription(description);
          if (deferFirstOffer && this === window.probe.peers[0]) {
            window.probe.localPending = true;
            await new Promise(resolve => {
              window.probe.releaseOffer = resolve;
            });
          }
        }
      };
    },
    { deferFirstOffer },
  );
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.routeWebSocket("**/webrtc/signaling/client", socket => {
    if (takeoverHub) {
      if (takeoverHub.current) {
        takeoverHub.current.send(JSON.stringify({ type: "other-session-connected" }));
        takeoverHub.current.close({ code: 4001, reason: "another session" });
      }
      takeoverHub.current = socket;
    }
    sockets.push(socket);
    socket.onMessage(raw => {
      if (raw === "ping") {
        socket.send("pong");
        return;
      }
      messages.push({ socket: sockets.indexOf(socket), ...JSON.parse(raw) });
    });
    socket.send(JSON.stringify({ type: "device-metadata", data: { deviceVersion: "1.0.0" } }));
  });
  await page.goto(origin, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => window.probe.peers.length === 1);
  return { context, page, sockets, messages, errors };
}

async function test(name, run) {
  let f;
  try {
    f = await fixture(run.options);
    await run(f);
    assert.deepEqual(f.errors, [], "browser runtime errors");
    console.log(`PASS ${name}`);
  } catch (error) {
    failures.push(name);
    console.error(`FAIL ${name}: ${error.message}`);
  } finally {
    await f?.context.close();
  }
}
try {
  await test("signaling close retires the peer before reconnect", async ({ page, sockets }) => {
    sockets[0].close({ code: 1001, reason: "offline test disconnect" });
    await page.waitForTimeout(100);
    assert.equal(await page.evaluate(() => window.probe.peers[0].connectionState), "closed");
  });
  await test("a signaling close that reconnects does not publish a failed connection", async ({
    page,
    sockets,
  }) => {
    await page.evaluate(() => {
      window.probe.states = [];
      window.probe.watch = setInterval(
        () => window.probe.states.push(window.__kvmTestHooks._getPeerConnectionState?.()),
        5,
      );
    });
    sockets[0].close({ code: 1001, reason: "offline test disconnect" });
    await waitFor(() => sockets.length === 2, "replacement signaling socket missing");
    await page.waitForFunction(() => window.probe.peers.length === 2);
    const states = await page.evaluate(() => {
      clearInterval(window.probe.watch);
      return window.probe.states;
    });
    assert.ok(
      states.includes("connecting"),
      `no reconnect progress: ${[...new Set(states)].join(", ")}`,
    );
    assert.ok(
      !states.includes("closed"),
      `published a failure: ${[...new Set(states)].join(", ")}`,
    );
  });
  const delayedOffer = async ({ page, sockets, messages }) => {
    await page.waitForFunction(() => window.probe.localPending);
    sockets[0].close({ code: 1001, reason: "offline test disconnect" });
    await waitFor(() => sockets.length === 2, "replacement signaling socket missing");
    await page.waitForFunction(() => window.probe.peers.length === 2);
    await waitFor(
      () => messages.some(m => m.socket === 1 && m.type === "offer"),
      "replacement offer missing",
    );
    const offersBefore = messages.filter(m => m.socket === 1 && m.type === "offer").length;
    await page.evaluate(() => window.probe.releaseOffer());
    await page.waitForTimeout(150);
    assert.equal(
      messages.filter(m => m.socket === 1 && m.type === "offer").length,
      offersBefore,
      "retired peer sent its delayed offer into the replacement signaling socket",
    );
  };
  delayedOffer.options = { deferFirstOffer: true };
  await test("late offer cannot enter a replacement session", delayedOffer);
  await test("queued old ICE callback cannot enter a replacement session", async ({
    page,
    sockets,
    messages,
  }) => {
    await page.evaluate(() => {
      window.probe.oldIce = window.probe.peers[0].onicecandidate;
    });
    sockets[0].close({ code: 1001, reason: "offline test disconnect" });
    await waitFor(() => sockets.length === 2, "replacement signaling socket missing");
    await page.waitForFunction(() => window.probe.peers.length === 2);
    await page.evaluate(() =>
      window.probe.oldIce({
        candidate: {
          candidate: "candidate:stale 1 udp 123 127.0.0.1 44239 typ host",
          sdpMid: "0",
          sdpMLineIndex: 0,
        },
      }),
    );
    await page.waitForTimeout(100);
    assert.equal(
      messages.some(m => m.socket === 1 && m.data?.candidate?.includes("candidate:stale")),
      false,
    );
  });
  await test("superseded session stays closed without reconnecting", async ({ page, sockets }) => {
    sockets[0].send(JSON.stringify({ type: "other-session-connected" }));
    sockets[0].close({ code: 4001, reason: "another session" });
    await page.waitForURL("**/other-session");
    await page.waitForTimeout(800);
    assert.equal(sockets.length, 1);
    assert.equal(await page.evaluate(() => window.probe.peers[0].connectionState), "closed");
  });
  const takeoverHub = {};
  const twoClients = async first => {
    const second = await fixture({ takeoverHub });
    try {
      await first.page.waitForURL("**/other-session");
      await first.page.waitForTimeout(800);
      assert.equal(
        first.sockets.length,
        1,
        "displaced browser must not automatically reclaim control",
      );
      assert.equal(
        await first.page.evaluate(() => window.probe.peers[0].connectionState),
        "closed",
      );
      await first.page.getByRole("button", { name: "Use Here", exact: true }).click();
      await waitFor(() => first.sockets.length === 2, "explicit takeover did not reopen signaling");
      await first.page.waitForFunction(() => window.probe.peers.length === 2);
      await second.page.waitForURL("**/other-session");
      await second.page.waitForTimeout(800);
      assert.equal(second.sockets.length, 1, "second browser must not fight the explicit takeover");
      assert.equal(
        await second.page.evaluate(() => window.probe.peers[0].connectionState),
        "closed",
      );
      assert.deepEqual(second.errors, [], "second browser runtime errors");
    } finally {
      await second.context.close();
    }
  };
  twoClients.options = { takeoverHub };
  await test("two browsers transfer ownership only on explicit takeover", twoClients);
} finally {
  await browser.close();
  await new Promise(resolve => server.close(resolve));
}
assert.deepEqual(failures, [], "signaling lifecycle regressions");
