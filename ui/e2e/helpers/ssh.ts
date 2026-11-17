import * as fs from "fs";
import { exec } from "child_process";
import { promisify } from "util";
import { test } from "@playwright/test";
import { getDeviceHost, waitForDeviceReady } from "./device";

const execAsync = promisify(exec);

export const SSH_OPTS = [
  "-o UserKnownHostsFile=/dev/null",
  "-o StrictHostKeyChecking=no",
  "-o LogLevel=ERROR",
  "-o ConnectTimeout=30",
  "-o ServerAliveInterval=5",
  "-o ServerAliveCountMax=3",
].join(" ");

const SSH_MAX_RETRIES = 3;

const SSH_RETRY_BASE_DELAY_MS = 2000;

const SSH_COMMAND_TIMEOUT_MS = 15000;

const REMOTE_APP_PATH = "/userdata/jetkvm/bin/jetkvm_app";

const REMOTE_DEBUG_APP_PATH = "/userdata/jetkvm/bin/jetkvm_app_debug";

function escapeForSingleQuotedShell(cmd: string): string {
  return cmd.replace(/'/g, "'\\''");
}

function shellSingleQuote(value: string): string {
  return `'${escapeForSingleQuotedShell(value)}'`;
}

// Shell access to the device is optional: a unit with developer mode off has
// no SSH. Set JETKVM_DEVICE_SSH=0 to declare that up front; otherwise one probe
// per worker decides. Tests that need the shell skip instead of failing.
let deviceShellProbe: Promise<boolean> | null = null;

export function deviceShellAvailable(): Promise<boolean> {
  if (process.env.JETKVM_DEVICE_SSH === "0") return Promise.resolve(false);
  deviceShellProbe ??= execAsync(
    `ssh ${SSH_OPTS} -o ConnectTimeout=5 root@${getDeviceHost()} true`,
    { timeout: 15000 },
  ).then(
    () => true,
    () => false,
  );
  return deviceShellProbe;
}

export async function skipWithoutDeviceShell(): Promise<void> {
  test.skip(!(await deviceShellAvailable()), "needs shell access to the device");
}

export async function sshExec(cmd: string, ignoreErrors = false): Promise<string> {
  if (!(await deviceShellAvailable())) {
    if (ignoreErrors) return "";
    throw new Error("device shell not available; set JETKVM_DEVICE_SSH=1 or check ssh access");
  }
  const host = getDeviceHost();
  const escapedCmd = escapeForSingleQuotedShell(cmd);
  const sshCmd = `ssh ${SSH_OPTS} root@${host} '${escapedCmd}'`;

  for (let attempt = 1; attempt <= SSH_MAX_RETRIES; attempt++) {
    try {
      const { stdout } = await execAsync(sshCmd, {
        timeout: SSH_COMMAND_TIMEOUT_MS,
      });
      return stdout;
    } catch (error) {
      if (ignoreErrors) return "";

      const msg = error instanceof Error ? error.message : String(error);
      const isTransient =
        msg.includes("Connection reset") ||
        msg.includes("Connection refused") ||
        msg.includes("Connection timed out") ||
        msg.includes("No route to host") ||
        msg.includes("ETIMEDOUT") ||
        msg.includes("timed out");

      if (isTransient && attempt < SSH_MAX_RETRIES) {
        const delay = SSH_RETRY_BASE_DELAY_MS * attempt;
        console.log(
          `[ssh] Attempt ${attempt}/${SSH_MAX_RETRIES} failed (${msg.split("\n")[0]}), retrying in ${delay}ms...`,
        );
        await new Promise(r => setTimeout(r, delay));
        continue;
      }
      throw error;
    }
  }
  throw new Error("sshExec: unreachable");
}

export async function resetConfigViaSSH(): Promise<void> {
  await sshExec("rm -f /userdata/kvm_config.json");
  await sshExec("sync");
}

export const UDC_NAME = "ffb00000.usb";

export const DWC3_PATH = "/sys/bus/platform/drivers/dwc3";

export const UDC_STATE_PATH = `/sys/class/udc/${UDC_NAME}/state`;

async function readUdcState(): Promise<string> {
  try {
    const result = (await sshExec(`cat ${UDC_STATE_PATH} 2>/dev/null`, true)).trim();
    return result || "not attached";
  } catch {
    return "not attached";
  }
}

export async function waitForUdcState(expected: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastSeen = "";
  while (Date.now() < deadline) {
    lastSeen = await readUdcState();
    if (lastSeen === expected) return;
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error(
    `Timed out waiting for UDC state "${expected}" within ${timeoutMs}ms (last seen: "${lastSeen}")`,
  );
}

export interface SSHDevState {
  sshKey: string;
  devModeEnabled: boolean;
}

export async function saveSSHDevState(): Promise<SSHDevState> {
  const sshKey = await sshExec("cat /userdata/dropbear/.ssh/authorized_keys 2>/dev/null", true);
  const devMode = await sshExec(
    "test -f /userdata/jetkvm/devmode.enable && echo 1 || echo 0",
    true,
  );
  return { sshKey: sshKey.trim(), devModeEnabled: devMode.trim() === "1" };
}

export async function restoreSSHDevState(state: SSHDevState): Promise<void> {
  if (state.sshKey) {
    await sshExec("mkdir -p /userdata/dropbear/.ssh && chmod 700 /userdata/dropbear/.ssh");
    const b64 = Buffer.from(state.sshKey).toString("base64");
    await sshExec(
      `echo ${b64} | base64 -d > /userdata/dropbear/.ssh/authorized_keys && chmod 600 /userdata/dropbear/.ssh/authorized_keys`,
    );
  }
  if (state.devModeEnabled) {
    await sshExec("mkdir -p /userdata/jetkvm && touch /userdata/jetkvm/devmode.enable");
  }
}

async function getRestartAppPathViaSSH(): Promise<string> {
  const configuredPath = process.env.E2E_REMOTE_APP_PATH;
  if (configuredPath) return configuredPath;

  const runningDebug = await sshExec(
    `for exe in /proc/[0-9]*/exe; do ` +
      `target=$(readlink "$exe" 2>/dev/null || true); ` +
      `case "$target" in */jetkvm_app_debug) echo 1; exit 0;; esac; ` +
      `done`,
    true,
  );
  return runningDebug.trim() === "1" ? REMOTE_DEBUG_APP_PATH : REMOTE_APP_PATH;
}

export async function restartAppViaSSH(opts: { beforeStart?: string } = {}): Promise<void> {
  const appPath = await getRestartAppPathViaSSH();
  await sshExec("killall jetkvm_app jetkvm_app_debug", true);
  await new Promise(r => setTimeout(r, 500));
  // Runs on the device while no app is up, for tests that stage the state a
  // crashed process would leave behind.
  if (opts.beforeStart) await sshExec(opts.beforeStart);
  // Rotate last.log into last.log.prev before respawning so a later teardown
  // can still recover the previous session's output if a subsequent restart
  // truncates the live log. Combined into one SSH call to save a round-trip.
  await sshExec(
    "[ -s /userdata/jetkvm/last.log ] && mv /userdata/jetkvm/last.log /userdata/jetkvm/last.log.prev; " +
      `setsid env LD_LIBRARY_PATH=/oem/usr/lib:/oem/lib ${shellSingleQuote(appPath)} > /userdata/jetkvm/last.log 2>&1 &`,
    true,
  );
  await new Promise(r => setTimeout(r, 1000));
  await waitForDeviceReady(getDeviceHost(), 15000);
}

export async function rebootDeviceViaSSH(waitForReady = true): Promise<void> {
  const host = getDeviceHost();

  // SSH connection may be terminated by the reboot, which is expected
  await sshExec("reboot", true);

  if (waitForReady) {
    await new Promise(resolve => setTimeout(resolve, 2000));
    await waitForDeviceReady(host, 60000);
    await new Promise(resolve => setTimeout(resolve, 500));
  }
}

export async function deployBinaryToDevice(binaryPath: string): Promise<void> {
  if (!fs.existsSync(binaryPath)) {
    throw new Error(`Binary not found: ${binaryPath}`);
  }

  const host = getDeviceHost();
  const sshCmd = `ssh ${SSH_OPTS} root@${host} "cat > /userdata/jetkvm/jetkvm_app.update"`;
  await execAsync(`${sshCmd} < "${binaryPath}"`);
}
