import { execSync } from "child_process";
import { test, expect, type Page } from "@playwright/test";
import {
  DWC3_PATH,
  SSH_OPTS,
  UDC_NAME,
  UDC_STATE_PATH,
  ensureNoPasswordViaAPI,
  ensureRpcReady,
  sshExec,
  waitForUdcState,
} from "../helpers";
import { createRemoteAgent, waitForKeyboardReady } from "./remote-agent";

const agent = createRemoteAgent();

const KICK_SETTLE_MS = 15_000;
const OBSERVE_MS = 60_000;
const SAMPLE_INTERVAL_MS = 5_000;
const MAX_STEADY_DENTRY_GROWTH = 40;
const DENTRIES_PER_REBIND = 16;
const MAX_WINDOW_REBINDS = 3;

async function deviceRebindCount(): Promise<number> {
  const out = await sshExec(
    'grep -c "rebinding USB gadget" /userdata/jetkvm/last.log 2>/dev/null || echo 0',
    true,
  );
  return parseInt(out, 10) || 0;
}

test.describe.configure({ mode: "serial" });

let page: Page;
let portDir = "";
let hostIsRoot = false;

function remoteHostExec(cmd: string, timeoutMs = 15_000): string {
  const target = process.env.JETKVM_REMOTE_HOST;
  if (!target) throw new Error("JETKVM_REMOTE_HOST not set");
  const escaped = cmd.replace(/'/g, "'\\''");
  return execSync(`ssh ${SSH_OPTS} ${target} '${escaped}'`, {
    encoding: "utf8",
    timeout: timeoutMs,
  });
}

function findGadgetPortDir(): string {
  return remoteHostExec(
    "for d in /sys/bus/usb/devices/*-*/; do " +
      '[ -f "$d/manufacturer" ] || continue; ' +
      'grep -qi jetkvm "$d/manufacturer" 2>/dev/null || continue; ' +
      'dev=$(basename "$d"); ' +
      'case "$dev" in ' +
      '*.*) hub=${dev%.*}; port=${dev##*.}; echo "/sys/bus/usb/devices/$hub:1.0/$hub-port$port";; ' +
      '*) bus=${dev%-*}; port=${dev#*-}; echo "/sys/bus/usb/devices/usb$bus/$bus-0:1.0/usb$bus-port$port";; ' +
      "esac; break; " +
      "done",
  ).trim();
}

function writePortDisable(value: number) {
  const write = hostIsRoot
    ? `echo ${value} > ${portDir}/disable`
    : `echo ${value} | sudo -n tee ${portDir}/disable > /dev/null`;
  remoteHostExec(`${write} 2>/dev/null || true`, 10_000);
}

test.beforeAll(async ({ browser }) => {
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");
  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);

  hostIsRoot = remoteHostExec("id -u").trim() === "0";

  page = await browser.newPage();
  await page.goto("/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 30_000);
});

test.afterAll(async () => {
  if (portDir) {
    try {
      writePortDisable(0);
    } catch {}
  }
  await sshExec(`echo ${UDC_NAME} > ${DWC3_PATH}/bind 2>/dev/null`, true);
  if (page) await page.close();
});

test("idle sessionless gadget neither leaks nor rebind-loops, and recovers when the host returns (#1540, #128)", async () => {
  test.setTimeout(300_000);

  expect(
    (await waitForKeyboardReady(agent!, page, 30_000)).length,
    "keyboard must work before the test",
  ).toBeGreaterThan(0);

  portDir = findGadgetPortDir();
  expect(portDir, "gadget hub port not found on remote host").toMatch(/port\d+$/);

  let sessionless = 0;
  let sampleCount = 0;
  let midpoint = 0;
  let final = 0;
  let midpointRebinds = 0;
  let finalRebinds = 0;
  try {
    writePortDisable(1);
    await sshExec(`echo ${UDC_NAME} > ${DWC3_PATH}/unbind 2>/dev/null`, true);
    await new Promise(r => setTimeout(r, KICK_SETTLE_MS));

    sampleCount = Math.floor(OBSERVE_MS / SAMPLE_INTERVAL_MS);
    for (let i = 0; i < sampleCount; i++) {
      await new Promise(r => setTimeout(r, SAMPLE_INTERVAL_MS));
      const state = (await sshExec(`cat ${UDC_STATE_PATH} 2>/dev/null`, true)).trim();
      if (state !== "configured") sessionless++;
      if (i === Math.floor(sampleCount / 2) - 1) {
        await sshExec("sync; echo 3 > /proc/sys/vm/drop_caches", true);
        midpoint = parseInt(await sshExec("cut -f1 /proc/sys/fs/dentry-state"), 10);
        midpointRebinds = await deviceRebindCount();
      }
    }

    await sshExec("sync; echo 3 > /proc/sys/vm/drop_caches", true);
    final = parseInt(await sshExec("cut -f1 /proc/sys/fs/dentry-state"), 10);
    finalRebinds = await deviceRebindCount();
  } finally {
    writePortDisable(0);
  }

  await waitForUdcState("configured", 60_000);
  await page.goto("/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  expect(
    (await waitForKeyboardReady(agent!, page, 60_000)).length,
    "keyboard must recover after the host returns",
  ).toBeGreaterThan(0);

  test.skip(
    sessionless < Math.ceil(sampleCount * 0.7),
    `host port hold did not stick (${sessionless}/${sampleCount} sessionless samples); this host re-enables the port on gadget reconnect`,
  );

  const windowRebinds = Math.max(0, finalRebinds - midpointRebinds);
  expect(
    windowRebinds,
    `device rebound ${windowRebinds} times in 30s of steady idle; the recovery loop is hammering again`,
  ).toBeLessThanOrEqual(MAX_WINDOW_REBINDS);

  expect(
    final - midpoint,
    `dentries grew by ${final - midpoint} in steady state (${windowRebinds} rebinds, allowance ${
      MAX_STEADY_DENTRY_GROWTH + DENTRIES_PER_REBIND * windowRebinds
    }); the recovery loop is leaking again`,
  ).toBeLessThanOrEqual(MAX_STEADY_DENTRY_GROWTH + DENTRIES_PER_REBIND * windowRebinds);
});
