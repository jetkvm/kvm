import { test, expect, type Page } from "@playwright/test";
import {
  ensureNoPasswordViaAPI,
  rawJsonRpc,
  sendAbsMouseMove,
  tapKey,
  waitForDecodedFrames,
  waitForWebRTCReady,
} from "../helpers";
import { createRemoteAgent, KEY } from "./remote-agent";

const agent = createRemoteAgent();
const RPC_TIMEOUT_MS = 2_000;
const CONNECTION_CYCLES = 6;
const HANDOVER_CYCLES = 5;
const LOAD_ROUNDS = 60;
const PASTE = "abcdefghij";
const PASTE_KEYS = [KEY.A, KEY.B, KEY.C, KEY.D, KEY.E, KEY.F, KEY.G, KEY.H, KEY.I, KEY.J];

// One test gives the entire workload a shared deadline. A failing cycle must
// not be hidden by Playwright retries or the helpers' reconnect/reload loops.
test.describe.configure({ retries: 0, timeout: 240_000 });
test.skip(!agent, "JETKVM_REMOTE_HOST not set");

async function ready(page: Page): Promise<void> {
  await waitForWebRTCReady(page, 10_000);
  await rawJsonRpc(page, "getDeviceID", {}, RPC_TIMEOUT_MS);
  await waitForDecodedFrames(page, 10_000);
}

async function open(page: Page): Promise<void> {
  await page.goto("/", { waitUntil: "domcontentloaded", timeout: 10_000 });
  await ready(page);
}

// Observe both press and release on the USB host, not just a successful send
// in the browser. Shift avoids typing into whichever application has focus.
async function verifyInput(page: Page): Promise<void> {
  await agent!.clearAllEvents();
  await tapKey(page, 0xe1);
  await sendAbsMouseMove(page, 10_000, 10_000, 1);
  await sendAbsMouseMove(page, 20_000, 20_000, 0);
  await expect
    .poll(
      async () => {
        const [keys, mouse] = await Promise.all([
          agent!.getKeyboardEvents(),
          agent!.getMouseEvents(),
        ]);
        return {
          keys: keys.filter(e => e.code === KEY.LEFT_SHIFT).map(e => e.type),
          buttons: mouse.filter(e => e.type === "mouse_button" && e.code === 272).map(e => e.value),
          moved: mouse.some(e => e.type === "mouse_move_abs"),
        };
      },
      { message: "active session must deliver keyboard and mouse input", timeout: 3_000 },
    )
    .toEqual({ keys: ["key_press", "key_release"], buttons: [1, 0], moved: true });
}

async function paste(page: Page): Promise<void> {
  await agent!.clearKeyboardEvents();
  const toggle = page.getByRole("button", { name: "Paste text" });
  await toggle.click();
  await page.locator('textarea[rows="4"]').fill(PASTE);
  const confirm = page.getByRole("button", { name: "Confirm Paste" });
  await confirm.click();
  await expect
    .poll(
      async () =>
        (await agent!.getKeyboardEvents()).filter(e => e.type === "key_press").map(e => e.code),
      { message: "every pasted character must reach the host in order", timeout: 5_000 },
    )
    .toEqual(PASTE_KEYS);
  await expect(confirm).toBeEnabled({ timeout: 3_000 });
  await toggle.click();
}

interface MemorySample {
  phase: string;
  processStart?: number;
  heapBytes?: number;
  residentBytes?: number;
}

// The current Linux firmware exports these metrics without device SSH. Mini
// firmware can still run the workloads if it has no Prometheus endpoint;
// missing telemetry is explicitly reported, never treated as zero memory.
async function sampleMemory(page: Page, phase: string): Promise<MemorySample> {
  const response = await page.request.get("/metrics", { timeout: 3_000 });
  try {
    if ([404, 501].includes(response.status())) return { phase };
    expect(response.ok(), "device metrics request must succeed").toBe(true);
    const body = await response.text();
    const metric = (name: string): number | undefined => {
      const value = body.match(new RegExp(`^${name}\\s+(\\S+)`, "m"))?.[1];
      if (value === undefined) return undefined;
      const parsed = Number(value);
      expect(Number.isFinite(parsed), `${name} must be a finite number`).toBe(true);
      return parsed;
    };
    return {
      phase,
      processStart: metric("process_start_time_seconds"),
      heapBytes: metric("go_memstats_heap_alloc_bytes"),
      residentBytes: metric("process_resident_memory_bytes"),
    };
  } finally {
    await response.dispose();
  }
}

test("bounded connection, workload and handover stress @stress", async ({ context }, testInfo) => {
  // The fixture owns one context and closes any remaining pages on failure.
  context.setDefaultTimeout(5_000);
  const samples: MemorySample[] = [];
  const rpcLatencies: number[] = [];
  const checkpoint = async (page: Page, phase: string) => {
    const sample = await sampleMemory(page, phase);
    samples.push(sample);
    if (samples[0].processStart !== undefined) {
      expect(sample.processStart, "device app must not restart during stress").toBe(
        samples[0].processStart,
      );
    }
  };

  try {
    // Deployment normally reuses the agent already running for this project.
    await agent!.ensureDeployed();
    await ensureNoPasswordViaAPI();
    const primary = await context.newPage();
    await open(primary);
    await verifyInput(primary);
    await primary.waitForTimeout(2_000);
    await checkpoint(primary, "warm single session");
    if (samples[0].processStart === undefined) {
      testInfo.annotations.push({
        type: "telemetry",
        description: "No process_start_time_seconds metric: restart detection unavailable",
      });
    }
    if (samples[0].heapBytes === undefined) {
      testInfo.annotations.push({
        type: "telemetry",
        description: "No heap metric: workload coverage only; memory measurements unavailable",
      });
    }
    await primary.close();

    await test.step(
      `connection churn: ${CONNECTION_CYCLES} complete and ${Math.ceil(CONNECTION_CYCLES / 2)} interrupted setups`,
      async () => {
        for (let cycle = 0; cycle < CONNECTION_CYCLES; cycle++) {
          await test.step(`connection ${cycle + 1}/${CONNECTION_CYCLES}`, async () => {
            const page = await context.newPage();
            await open(page);
            await page.close();
          });
          if (cycle % 2 === 0) {
            const partial = await context.newPage();
            // Hold the answer before applying it: the device has handled the
            // offer, but the browser cannot complete the WebRTC connection.
            await partial.addInitScript(() => {
              const original: (
                this: RTCPeerConnection,
                description: RTCSessionDescriptionInit,
              ) => Promise<void> = RTCPeerConnection.prototype.setRemoteDescription;
              RTCPeerConnection.prototype.setRemoteDescription = function (description) {
                if (description.type === "answer") {
                  document.documentElement.dataset.stressAnswerReceived = "true";
                  return new Promise<void>(() => {});
                }
                return original.call(this, description);
              };
            });
            await partial.goto("/", { waitUntil: "domcontentloaded", timeout: 10_000 });
            await partial.waitForFunction(
              () => document.documentElement.dataset.stressAnswerReceived === "true",
              {},
              { timeout: 5_000 },
            );
            await partial.close();
          }
        }
      },
      { timeout: 55_000 },
    );

    const active = await context.newPage();
    await open(active);
    await verifyInput(active);
    await checkpoint(active, "after connection churn");

    await test.step(
      `mixed load: ${LOAD_ROUNDS} paced rounds of video, mouse, pastes and RPC bursts`,
      async () => {
        // Drive motion in the browser at 25 Hz while RPC and paste assertions
        // run on the test runner. Browser timers stop when the page is closed.
        const timer = await active.evaluate(() => {
          let tick = 0;
          return window.setInterval(() => {
            tick++;
            window.__kvmTestHooks!.sendAbsMouseMove(
              8_000 + ((tick * 173) % 16_000),
              8_000 + ((tick * 317) % 16_000),
              0,
            );
          }, 40);
        });
        try {
          const started = performance.now();
          for (let round = 0; round < LOAD_ROUNDS; round++) {
            await Promise.all([
              ...Array.from({ length: 5 }, async () => {
                const sent = performance.now();
                await rawJsonRpc(active, "getDeviceID", {}, RPC_TIMEOUT_MS);
                // The RPC has its own deadline. Runner timings additionally
                // include browser communication and are diagnostic only.
                rpcLatencies.push(performance.now() - sent);
              }),
              waitForDecodedFrames(active, 3_000),
              ...(round % 10 === 0 ? [paste(active)] : []),
            ]);
            if (round % 10 === 9) {
              expect(
                (await agent!.popMouseEvents()).some(e => e.type === "mouse_move_abs"),
                "mouse movement must keep reaching the host under load",
              ).toBe(true);
            }
            await active.waitForTimeout(
              Math.max(0, started + (round + 1) * 1_000 - performance.now()),
            );
          }
        } finally {
          // Closing the context also stops the timer. A closed page during
          // failure cleanup must not replace the original test error.
          await active.evaluate(timer => window.clearInterval(timer), timer).catch(() => {});
        }
        await verifyInput(active);
        await checkpoint(active, "after mixed load");
      },
      { timeout: 80_000 },
    );

    await test.step(
      `${HANDOVER_CYCLES} consecutive takeovers in both directions`,
      async () => {
        for (let cycle = 0; cycle < HANDOVER_CYCLES; cycle++) {
          await test.step(`handover ${cycle + 1}/${HANDOVER_CYCLES}`, async () => {
            const replacement = await context.newPage();
            await open(replacement);
            const useHere = active.getByRole("button", { name: "Use Here" });
            await expect(useHere).toBeVisible({ timeout: 3_000 });
            await verifyInput(replacement);
            // Reclaim the device while the replacement is still connected,
            // so the return leg exercises a live takeover too.
            await useHere.click();
            await ready(active);
            await verifyInput(active);
            await expect(replacement.getByRole("button", { name: "Use Here" })).toBeVisible({
              timeout: 3_000,
            });
            await replacement.close();
            await checkpoint(active, `after handover ${cycle + 1}`);
          });
        }
      },
      { timeout: 75_000 },
    );

    await ready(active);
    await verifyInput(active);
    await active.waitForTimeout(2_000);
    await checkpoint(active, "recovered single session");
  } finally {
    await testInfo.attach("stress-observations", {
      contentType: "application/json",
      body: JSON.stringify({ samples, rpcLatenciesMs: rpcLatencies }, null, 2),
    });
  }
});
