import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";
import type { KeyboardLedState } from "./hid";
import type { VideoStreamDimensions } from "./video";

export async function waitForWebRTCReady(page: Page, timeout = 30000): Promise<void> {
  await expect
    .poll(
      async () => {
        const status = await page.evaluate(() => {
          const hooks = window.__kvmTestHooks;
          if (!hooks) {
            return { hooks: false, webrtc: false, hid: false };
          }
          return {
            hooks: true,
            webrtc: hooks.isWebRTCConnected(),
            hid: hooks.isHidRpcReady(),
          };
        });
        return status.hooks && status.webrtc && status.hid;
      },
      {
        message: "Waiting for WebRTC connection and HID RPC to be ready",
        timeout,
        intervals: [200, 500, 1000],
      },
    )
    .toBe(true);
}

/**
 * Wait until JSON-RPC over WebRTC actually works, verified by a getDeviceID
 * round-trip. Each attempt claims the WebRTC slot ("Use Here"), waits for the
 * connection, and probes RPC; on failure it reloads and retries until the
 * deadline, so a slow reconnect (e.g. after a reboot) recovers instead of
 * flaking like a single fixed-window wait would.
 */
export async function ensureRpcReady(
  page: Page,
  {
    timeoutMs = 60000,
    navigateFirst = false,
  }: { timeoutMs?: number; navigateFirst?: boolean } = {},
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastError: unknown;
  for (let attempt = 0; Date.now() < deadline; attempt++) {
    try {
      // First attempt trusts the caller's existing navigation; retries reload.
      if (attempt > 0 || navigateFirst) {
        await page.goto("/", { waitUntil: "networkidle", timeout: 20000 });
      }
      const useHere = page.getByRole("button", { name: "Use Here" });
      if (await useHere.isVisible({ timeout: 200 }).catch(() => false)) {
        await useHere.click();
        await page.waitForTimeout(1000);
      }
      await waitForWebRTCReady(page, Math.min(15000, Math.max(5000, deadline - Date.now())));
      await rawJsonRpc(page, "getDeviceID", {}, 5000);
      return;
    } catch (err) {
      lastError = err;
      await page.waitForTimeout(1000);
    }
  }
  const detail = lastError instanceof Error ? lastError.message : String(lastError);
  throw new Error(`Page never became RPC-ready within ${timeoutMs}ms: ${detail}`);
}

/** Get the current app version from the /metrics endpoint. */
export async function getCurrentVersion(page: Page): Promise<string | null> {
  return page.evaluate(async () => {
    try {
      const response = await fetch("/metrics");
      if (!response.ok) return null;

      const text = await response.text();
      // Look for promhttp_metric_handler_requests_total or similar app-specific metrics
      // The app version is in the build_info metric, not go_info
      const match = text.match(/build_info.*version="([^"]+)"/);
      if (match) return match[1];

      // Fallback: try to find any version that's not the go version
      const allVersions = Array.from(text.matchAll(/version="([^"]+)"/g));
      for (const m of allVersions) {
        const ver = m[1];
        // Skip go versions
        if (!ver.startsWith("go1.")) {
          return ver;
        }
      }

      return null;
    } catch (error) {
      console.error("Failed to fetch version from /metrics:", error);
      return null;
    }
  });
}

/**
 * Reconnect a page after a device reboot: wait for the device to actually go
 * down and come back (waitBeforeRetry), then retry until RPC works.
 */
export async function reconnectAfterReboot(
  page: Page,
  waitBeforeRetry = 2000,
  timeoutMs = 120_000,
): Promise<void> {
  await page.waitForTimeout(waitBeforeRetry);
  await ensureRpcReady(page, { timeoutMs, navigateFirst: true });
}

// A method the device does not implement answers "Method not found". Tests
// for that feature skip on it instead of failing: the suite also runs against
// older firmware during upgrade testing, and a missing method is a version
// fact, not a regression. Probe with a getter; the call is made for real.
export async function rpcAvailable(page: Page, method: string): Promise<boolean> {
  try {
    await callJsonRpc(page, method, {});
    return true;
  } catch (error) {
    return !/method not found/i.test(String(error));
  }
}

export async function skipWithoutRpc(page: Page, method: string, feature: string): Promise<void> {
  test.skip(!(await rpcAvailable(page, method)), `device has no ${feature} (${method})`);
}

export function getDeviceHost(): string {
  const url = process.env.JETKVM_URL;
  if (!url) {
    throw new Error("JETKVM_URL environment variable is not set");
  }
  return new URL(url).hostname;
}

/**
 * Ensure the device is set up with no-password local auth via the HTTP setup API.
 * No-op if already set up. Used by e2e bootstrap.
 */
export async function ensureNoPasswordViaAPI(): Promise<void> {
  const host = getDeviceHost();
  const status = await fetch(`http://${host}/device/status`).then(
    r => r.json() as Promise<{ isSetup: boolean }>,
  );
  if (status.isSetup) return;

  const res = await fetch(`http://${host}/device/setup`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ localAuthMode: "noPassword" }),
  });
  if (!res.ok) throw new Error(`Setup POST failed: ${res.status}`);
}

export async function waitForDeviceReady(host: string, timeout = 60000): Promise<void> {
  const startTime = Date.now();
  const url = `http://${host}`;

  while (Date.now() - startTime < timeout) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(5000) });
      if (response.ok || response.status === 401 || response.status === 302) {
        // Device is responding (even if it redirects to login)
        return;
      }
    } catch {
      // Device not ready yet, continue waiting
    }
    await new Promise(resolve => setTimeout(resolve, 500));
  }

  throw new Error(`Device at ${host} did not become ready within ${timeout}ms`);
}

async function rawJsonRpc(
  page: Page,
  method: string,
  params: Record<string, unknown>,
  timeoutMs?: number,
): Promise<unknown> {
  return page.evaluate(
    ({ method, params, timeoutMs }) => {
      return new Promise((resolve, reject) => {
        const hooks = window.__kvmTestHooks;
        if (!hooks) return reject(new Error("Test hooks not available"));
        hooks.sendJsonRpc(
          method,
          params,
          (resp: { error?: { message: string; data?: string }; result?: unknown }) => {
            if (resp.error)
              reject(
                new Error(`${resp.error.message}${resp.error.data ? `: ${resp.error.data}` : ""}`),
              );
            else resolve(resp.result);
          },
          timeoutMs,
        );
      });
    },
    { method, params, timeoutMs },
  );
}

const RPC_CHANNEL_DROPPED =
  /RPC data channel not available|Test hooks not available|Execution context was destroyed/;

export async function callJsonRpc(
  page: Page,
  method: string,
  params: Record<string, unknown> = {},
  timeoutMs?: number,
): Promise<unknown> {
  try {
    return await rawJsonRpc(page, method, params, timeoutMs);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    if (!RPC_CHANNEL_DROPPED.test(msg)) throw err;
    await ensureRpcReady(page, { timeoutMs: 20000 });
    return rawJsonRpc(page, method, params, timeoutMs);
  }
}

declare global {
  interface Window {
    __kvmTestHooks?: {
      getKeyboardLedState: () => KeyboardLedState | null;
      getKeysDownState: () => { modifier: number; keys: number[] } | null;
      sendKeypress: (key: number, press: boolean) => void;
      sendAbsMouseMove: (x: number, y: number, buttons: number) => void;
      sendJsonRpc: (
        method: string,
        params: Record<string, unknown>,
        callback: (resp: { error?: { message: string; data?: string }; result?: unknown }) => void,
      ) => void;
      captureVideoRegion: (
        x: number,
        y: number,
        width: number,
        height: number,
      ) => Promise<string | null>;
      captureVideoRegionFingerprint: (
        x: number,
        y: number,
        width: number,
        height: number,
        gridSize?: number,
      ) => number[] | null;
      getVideoStreamDimensions: () => VideoStreamDimensions | null;
      isWebRTCConnected: () => boolean;
      isHidRpcReady: () => boolean;
      isVideoStreamActive: () => boolean;
      sendTerminalCommand: (command: string) => boolean;
      isTerminalReady: () => boolean;
    };
  }
}
