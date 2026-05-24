import { test, expect, type Page } from "@playwright/test";
import {
  callJsonRpc,
  ensureNoPasswordViaAPI,
  waitForAudioStream,
  waitForWebRTCReady,
} from "../helpers";
import { createRemoteAgent } from "./remote-agent";

const agent = createRemoteAgent();

const MICROPHONE_ENABLED_STORAGE_KEY = "jetkvm.microphone.enabled";

test.beforeAll(async () => {
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");
  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);
});

async function useSineWaveMicrophone(page: Page) {
  await page.addInitScript(() => {
    const mediaDevices = (navigator.mediaDevices ?? {}) as MediaDevices;
    const contexts: AudioContext[] = [];

    Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: mediaDevices });
    Object.defineProperty(mediaDevices, "getUserMedia", {
      configurable: true,
      value: async () => {
        const AudioContextCtor =
          window.AudioContext ??
          (window as Window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
        if (!AudioContextCtor) throw new Error("AudioContext is not available");

        const context = new AudioContextCtor({ sampleRate: 48000 });
        const destination = context.createMediaStreamDestination();
        const oscillator = context.createOscillator();
        const gain = context.createGain();

        oscillator.frequency.value = 997;
        gain.gain.value = 0.03;
        oscillator.connect(gain).connect(destination);
        oscillator.start();
        await context.resume().catch(() => undefined);
        contexts.push(context);
        return destination.stream;
      },
    });
  });
}

test.afterEach(async ({ page }) => {
  await agent?.stopAudioTone().catch(() => undefined);
  await page
    .evaluate(
      enabledKey => window.localStorage.removeItem(enabledKey),
      MICROPHONE_ENABLED_STORAGE_KEY,
    )
    .catch(() => undefined);
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

test("microphone works end-to-end", async ({ page }) => {
  test.setTimeout(60_000);
  await useSineWaveMicrophone(page);

  await page.goto("/", { waitUntil: "networkidle" });
  await waitForWebRTCReady(page);
  await callJsonRpc(page, "setAudioConfig", {
    params: { enabled: true, microphone_enabled: true },
  });

  try {
    await page.evaluate(
      enabledKey => window.localStorage.setItem(enabledKey, "true"),
      MICROPHONE_ENABLED_STORAGE_KEY,
    );

    await page.reload({ waitUntil: "networkidle" });
    await waitForWebRTCReady(page);

    await expect
      .poll(async () => (await agent!.getAudioCaptureDevices()).some(d => d.is_jetkvm), {
        message: "Waiting for JetKVM USB ALSA capture device",
        timeout: 10_000,
        intervals: [250, 500, 1000],
      })
      .toBe(true);

    await page.waitForTimeout(1500);
    const capture = await agent!.captureMicrophoneAudio();
    expect(
      capture.device.is_jetkvm,
      `selected non-JetKVM capture device: ${JSON.stringify(capture)}`,
    ).toBe(true);
    expect(capture.samples).toBeGreaterThan(200_000);
    expect(capture.peak).toBeGreaterThan(3500);
    expect(capture.rms).toBeGreaterThan(1500);
    expect(capture.rms_dbfs).toBeGreaterThan(-27);
    expect(capture.tone_ratio).toBeGreaterThan(0.45);
    expect(capture.zero_crossings).toBeGreaterThan(4500);
    expect(capture.zero_crossings).toBeLessThan(7500);
  } finally {
    await callJsonRpc(page, "setAudioConfig", {
      params: { enabled: false, microphone_enabled: false },
    }).catch(() => undefined);
  }
});
