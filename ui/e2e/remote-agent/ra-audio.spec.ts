import { test, expect } from "@playwright/test";
import {
  callJsonRpc,
  ensureNoPasswordViaAPI,
  waitForAudioStream,
  waitForWebRTCReady,
} from "../helpers";
import { createRemoteAgent, type AudioDeviceInfo } from "./remote-agent";

const agent = createRemoteAgent();
const USB_ENUMERATION_SETTLE_MS = 3_000;

test.beforeAll(async () => {
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");
  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);
});

test.afterEach(async () => {
  await agent?.stopAudioTone().catch(() => undefined);
});

async function waitForJetKvmAudioDevice(context: string, timeoutMs = 15_000) {
  const deadline = Date.now() + timeoutMs;
  let devices: AudioDeviceInfo[] = [];

  while (Date.now() < deadline) {
    devices = await agent!.getAudioDevices();
    const jetkvmDevice = devices.find(d => d.is_jetkvm);
    if (jetkvmDevice) return jetkvmDevice;
    await new Promise(r => setTimeout(r, 1_000));
  }

  throw new Error(
    `No JetKVM USB ALSA playback device on remote host ${context}: ${JSON.stringify(devices)}`,
  );
}

test("USB audio device remains attached when streaming audio is toggled", async ({ page }) => {
  test.setTimeout(45_000);

  await page.goto("/", { waitUntil: "networkidle" });
  await waitForWebRTCReady(page);

  try {
    await callJsonRpc(page, "setAudioConfig", { params: { enabled: false } });
    await page.waitForTimeout(USB_ENUMERATION_SETTLE_MS);
    await waitForJetKvmAudioDevice("with streaming disabled");

    await callJsonRpc(page, "setAudioConfig", { params: { enabled: true } });
    await page.waitForTimeout(USB_ENUMERATION_SETTLE_MS);
    await waitForJetKvmAudioDevice("after enabling streaming");

    await callJsonRpc(page, "setAudioConfig", { params: { enabled: false } });
    await page.waitForTimeout(USB_ENUMERATION_SETTLE_MS);
    await waitForJetKvmAudioDevice("after disabling streaming");
  } finally {
    await callJsonRpc(page, "setAudioConfig", { params: { enabled: false } }).catch(
      () => undefined,
    );
  }
});

test("audio works end-to-end", async ({ page }) => {
  test.setTimeout(60_000);

  await waitForJetKvmAudioDevice("before enabling streaming");

  // Audio streaming is opt-in via device config (Settings -> Audio -> Enable
  // Audio). The USB audio device class is controlled separately by Settings ->
  // Hardware, so enabling streaming should not force host USB re-enumeration.
  // Connect with audio off, flip the setting via RPC, then reload so the new
  // SDP exchange picks up the freshly-enabled track.
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
