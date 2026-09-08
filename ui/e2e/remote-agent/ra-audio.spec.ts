import {
  captureHardwareState,
  configureTestUSB,
  restoreHardwareState,
  type HardwareState,
} from "../helpers/hardware-state";
import { test, expect } from "@playwright/test";
import {
  callJsonRpc,
  skipWithoutRpc,
  ensureNoPasswordViaAPI,
  waitForAudioStream,
  waitForWebRTCReady,
} from "../helpers";
import { createRemoteAgent, type AudioDeviceInfo } from "./remote-agent";

const agent = createRemoteAgent();
const USB_ENUMERATION_SETTLE_MS = 3_000;
let originalHardware: HardwareState | undefined;

test.beforeAll(async ({ browser }) => {
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");
  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);
  const page = await browser.newPage();
  try {
    await page.goto("/");
    await waitForWebRTCReady(page);
    originalHardware = await captureHardwareState(page);
    await configureTestUSB(page, originalHardware, true);
  } finally {
    await page.close();
  }
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

test("USB audio device remains attached when streaming audio is toggled @audio", async ({
  page,
}) => {
  test.setTimeout(45_000);

  await page.goto("/", { waitUntil: "networkidle" });
  await waitForWebRTCReady(page);
  await skipWithoutRpc(page, "getAudioConfig", "audio");

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

test("audio works end-to-end @audio", async ({ page }) => {
  test.setTimeout(60_000);

  await page.goto("/", { waitUntil: "networkidle" });
  await waitForWebRTCReady(page);
  await skipWithoutRpc(page, "getAudioConfig", "audio");

  await waitForJetKvmAudioDevice("before enabling streaming");

  // Audio streaming is opt-in via device config (Settings -> Audio -> Enable
  // Audio). The USB audio device class is controlled separately by Settings ->
  // Hardware, so enabling streaming should not force host USB re-enumeration.
  // Connect with audio off, flip the setting via RPC, then reload so the new
  // SDP exchange picks up the freshly-enabled track.
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

test.afterAll(async ({ browser }) => {
  if (!originalHardware) return;
  const page = await browser.newPage();
  try {
    await restoreHardwareState(page, originalHardware);
  } finally {
    await page.close();
  }
});

declare global {
  interface Window {
    __e2eAudioProbe?: {
      context: AudioContext;
      source: MediaStreamAudioSourceNode;
      analyser: AnalyserNode;
    };
  }
}

test("USB audio delivers a sustained 997 Hz tone alongside video and HID @audio", async ({
  page,
}, info) => {
  test.setTimeout(100_000);
  await page.goto("/");
  await waitForWebRTCReady(page);
  await skipWithoutRpc(page, "getAudioConfig", "audio");
  try {
    await callJsonRpc(page, "setAudioConfig", { params: { enabled: true } });
    await page.reload();
    await waitForWebRTCReady(page);
    await waitForAudioStream(page);
    await agent!.startAudioTone();
    await page.mouse.click(5, 5);
    await page.evaluate(async () => {
      const tracks = window.__kvmTestHooks?._getMediaStream?.()?.getAudioTracks();
      if (!tracks?.length) throw new Error("No browser audio track");
      const context = new AudioContext({ sampleRate: 48000 });
      const source = context.createMediaStreamSource(new MediaStream(tracks));
      const analyser = context.createAnalyser();
      analyser.fftSize = 4096;
      source.connect(analyser);
      window.__e2eAudioProbe = { context, source, analyser };
      await context.resume();
    });
    const measure = () =>
      page.evaluate(() => {
        const { context, analyser } = window.__e2eAudioProbe!;
        const wave = new Float32Array(analyser.fftSize);
        const bins = new Float32Array(analyser.frequencyBinCount);
        analyser.getFloatTimeDomainData(wave);
        analyser.getFloatFrequencyData(bins);
        let peak = 1;
        for (let i = 2; i < bins.length; i++) if (bins[i] > bins[peak]) peak = i;
        return {
          rms: Math.sqrt(wave.reduce((sum, value) => sum + value * value, 0) / wave.length),
          hz: (peak * context.sampleRate) / analyser.fftSize,
        };
      });
    await expect.poll(async () => (await measure()).rms, { timeout: 12_000 }).toBeGreaterThan(0.01);
    const samples = [];
    let before = await page.evaluate(() => window.__kvmTestHooks?.getInboundAudioStats());
    for (let i = 0; i < 6; i++) {
      await page.waitForTimeout(5000);
      const sample = await measure();
      expect(sample.rms).toBeGreaterThan(0.01);
      expect(Math.abs(sample.hz - 997)).toBeLessThan(30);
      const after = await page.evaluate(() => window.__kvmTestHooks?.getInboundAudioStats());
      expect(after!.packetsReceived).toBeGreaterThan(before!.packetsReceived);
      expect(after!.totalAudioEnergy).toBeGreaterThan(before!.totalAudioEnergy);
      expect(await page.evaluate(() => window.__kvmTestHooks?.isVideoStreamActive())).toBe(true);
      samples.push({ ...sample, ...after });
      before = after;
    }
    const { waitForKeyboardReady } = await import("./remote-agent");
    expect((await waitForKeyboardReady(agent!, page, 15_000)).length).toBeGreaterThan(0);
    await info.attach("audio-samples", {
      body: JSON.stringify(samples),
      contentType: "application/json",
    });
  } finally {
    try {
      await page.evaluate(async () => {
        const probe = window.__e2eAudioProbe;
        if (probe) {
          probe.source.disconnect();
          await probe.context.close();
          delete window.__e2eAudioProbe;
        }
      });
    } finally {
      await callJsonRpc(page, "setAudioConfig", { params: { enabled: false } });
    }
  }
});
