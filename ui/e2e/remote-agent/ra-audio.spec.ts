import { test, expect, type Page, type TestInfo } from "@playwright/test";
import * as fs from "fs";
import {
  callJsonRpc,
  getDeviceHost,
  restartAppViaSSH,
  sshExec,
  waitForWebRTCReady,
} from "../helpers";
import { createRemoteAgent } from "./remote-agent";

const agent = createRemoteAgent();

async function ensureNoPasswordViaAPI() {
  const host = getDeviceHost();
  const status = await fetch(`http://${host}/device/status`).then(
    r => r.json() as Promise<{ isSetup: boolean }>,
  );

  if (!status.isSetup) {
    const res = await fetch(`http://${host}/device/setup`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ localAuthMode: "noPassword" }),
    });
    if (!res.ok) throw new Error(`Setup POST failed: ${res.status}`);
    return;
  }

  const probe = await fetch(`http://${host}/device`);
  if (probe.status !== 401) return;

  await sshExec("rm -f /userdata/kvm_config.json && sync");
  await restartAppViaSSH();
  const res = await fetch(`http://${host}/device/setup`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ localAuthMode: "noPassword" }),
  });
  if (!res.ok) throw new Error(`Setup POST after reset failed: ${res.status}`);
}

async function openReadyPage(page: Page) {
  await page.goto("/", { waitUntil: "networkidle" });

  if (page.url().includes("/welcome")) {
    const host = getDeviceHost();
    await fetch(`http://${host}/device/setup`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ localAuthMode: "noPassword" }),
    });
    await page.goto("/", { waitUntil: "networkidle" });
  }

  await waitForWebRTCReady(page);
}

async function attachAudioDiagnostics(testInfo: TestInfo, page: Page): Promise<void> {
  const sections: string[] = [];

  const browserStats = await page
    .evaluate(() => ({
      active: window.__kvmTestHooks?.isAudioStreamActive(),
      stats: window.__kvmTestHooks?.getInboundAudioStats(),
    }))
    .catch(error => ({ error: error instanceof Error ? error.message : String(error) }));
  sections.push(`browser:\n${JSON.stringify(browserStats, null, 2)}`);

  const audioDevices = agent
    ? await agent.getAudioDevices().catch(error => ({
        error: error instanceof Error ? error.message : String(error),
      }))
    : { error: "remote agent unavailable" };
  sections.push(`remote audio devices:\n${JSON.stringify(audioDevices, null, 2)}`);

  const deviceSnapshot = await sshExec(
    [
      "echo '=== date ==='",
      "date",
      "echo '=== uptime ==='",
      "uptime",
      "echo '=== jetkvm_app pid ==='",
      "pidof jetkvm_app || true",
      "echo '=== jetkvm_app process ==='",
      "ps -w | grep jetkvm_app | grep -v grep || true",
      "echo '=== snd nodes ==='",
      "ls -l /dev/snd 2>/dev/null || true",
      "echo '=== proc asound ==='",
      "cat /proc/asound/cards 2>/dev/null || true",
      "cat /proc/asound/pcm 2>/dev/null || true",
      "echo '=== recent app log ==='",
      "tail -300 /userdata/jetkvm/last.log 2>/dev/null || true",
      "echo '=== recent dmesg ==='",
      "dmesg | tail -120",
    ].join("; "),
    true,
  );
  sections.push(`device snapshot:\n${deviceSnapshot}`);

  const diagnosticsPath = testInfo.outputPath("audio-usb-diagnostics.txt");
  fs.writeFileSync(diagnosticsPath, sections.join("\n\n"));
  await testInfo.attach("audio-usb-diagnostics.txt", {
    path: diagnosticsPath,
    contentType: "text/plain",
  });
}

async function expectRemoteToneReachesBrowser(page: Page, testInfo: TestInfo) {
  const audioDevices = await agent!.getAudioDevices();
  test.skip(
    !audioDevices.some(device => device.is_jetkvm),
    `No JetKVM USB ALSA playback device found: ${JSON.stringify(audioDevices)}`,
  );

  await openReadyPage(page);
  const originalLogLevel = (await callJsonRpc(page, "getDefaultLogLevel")) as string;
  let diagnosticsAttached = false;
  const attachDiagnosticsOnce = async () => {
    if (diagnosticsAttached) return;
    diagnosticsAttached = true;
    await attachAudioDiagnostics(testInfo, page);
  };

  try {
    await callJsonRpc(page, "setDefaultLogLevel", { level: "INFO" });

    await expect
      .poll(() => page.evaluate(() => window.__kvmTestHooks?.isAudioStreamActive()), {
        message: "Waiting for browser audio track",
        timeout: 10_000,
        intervals: [250, 500],
      })
      .toBe(true);

    const before = (await page.evaluate(() => window.__kvmTestHooks?.getInboundAudioStats())) ?? {
      bytesReceived: 0,
      packetsReceived: 0,
      totalAudioEnergy: 0,
      codecMimeType: "",
    };

    const toneDevice = await agent!.startAudioTone();
    expect(
      toneDevice.is_jetkvm,
      `Remote agent selected non-JetKVM audio device: ${JSON.stringify(toneDevice)}`,
    ).toBe(true);

    try {
      await expect
        .poll(
          async () => {
            const stats = await page.evaluate(() => window.__kvmTestHooks?.getInboundAudioStats());
            if (!stats) return false;
            const bytesDelta = stats.bytesReceived - before.bytesReceived;
            const packetsDelta = stats.packetsReceived - before.packetsReceived;
            const energyDelta = stats.totalAudioEnergy - before.totalAudioEnergy;
            const codec = stats.codecMimeType.toLowerCase();
            return (
              bytesDelta > 800 &&
              packetsDelta > 10 &&
              energyDelta > 0.0001 &&
              codec.includes("g722")
            );
          },
          {
            message: "Waiting for captured USB G.722 audio energy",
            timeout: 12_000,
            intervals: [500, 1000],
          },
        )
        .toBe(true);
    } catch (error) {
      await attachDiagnosticsOnce();
      throw error;
    } finally {
      await agent!.stopAudioTone();
    }
  } catch (error) {
    await attachDiagnosticsOnce();
    throw error;
  } finally {
    await callJsonRpc(page, "setDefaultLogLevel", { level: originalLogLevel }).catch(() => {
      /* ignore */
    });
  }
}

test.beforeAll(async () => {
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");
  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);
});

test.afterEach(async () => {
  if (!agent) return;
  await agent.stopAudioTone().catch(() => {
    /* ignore */
  });
});

test("audio: USB remote tone reaches browser WebRTC track as G.722", async ({ page }, testInfo) => {
  test.setTimeout(35_000);
  await expectRemoteToneReachesBrowser(page, testInfo);
});
