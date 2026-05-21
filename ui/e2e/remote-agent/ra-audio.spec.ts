import { test, expect } from "@playwright/test";
import {
  callJsonRpc,
  ensureNoPasswordViaAPI,
  waitForAudioStream,
  waitForWebRTCReady,
} from "../helpers";
import { createRemoteAgent } from "./remote-agent";

const agent = createRemoteAgent();

test.beforeAll(async () => {
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");
  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);
});

test.afterEach(async () => {
  await agent?.stopAudioTone().catch(() => undefined);
});

test("audio works end-to-end", async ({ page }) => {
  test.setTimeout(45_000);

  const devices = await agent!.getAudioDevices();
  test.skip(
    !devices.some(d => d.is_jetkvm),
    `No JetKVM USB ALSA playback device on remote host: ${JSON.stringify(devices)}`,
  );

  // Audio is opt-in via device config (Settings → Audio → Enable Audio).
  // First connect with audio off, flip the setting via RPC, then reload so
  // the new SDP exchange picks up the freshly-enabled track.
  await page.goto("/", { waitUntil: "networkidle" });
  await waitForWebRTCReady(page);
  await callJsonRpc(page, "setAudioConfig", { params: { enabled: true } });

  try {
    await page.reload({ waitUntil: "networkidle" });
    await waitForWebRTCReady(page);
    await waitForAudioStream(page);

    const before = (await page.evaluate(() => window.__kvmTestHooks?.getInboundAudioStats())) ?? {
      bytesReceived: 0,
      packetsReceived: 0,
      totalAudioEnergy: 0,
    };

    const tone = await agent!.startAudioTone();
    expect(tone.is_jetkvm, `selected non-JetKVM playback device: ${JSON.stringify(tone)}`).toBe(
      true,
    );

    await expect
      .poll(
        async () => {
          const stats = await page.evaluate(() => window.__kvmTestHooks?.getInboundAudioStats());
          if (!stats) return false;
          return (
            stats.bytesReceived - before.bytesReceived > 800 &&
            stats.packetsReceived - before.packetsReceived > 10 &&
            stats.totalAudioEnergy - before.totalAudioEnergy > 0.0001
          );
        },
        {
          message: "USB audio energy never reached browser",
          timeout: 12_000,
          intervals: [500, 1000],
        },
      )
      .toBe(true);
  } finally {
    // Restore the default (disabled) so other specs aren't affected.
    await callJsonRpc(page, "setAudioConfig", { params: { enabled: false } }).catch(
      () => undefined,
    );
  }
});
