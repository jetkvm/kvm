/**
 * Client for the JetKVM Remote Agent running on the target host.
 * Provides direct verification of HID events, USB devices, and mounts
 * instead of indirect methods like screenshot diffing or LED polling.
 */

import { execSync } from "child_process";
import * as fs from "fs";
import * as path from "path";
import { fileURLToPath } from "url";

export interface KeyboardEvent {
  time_ms: number;
  type: "key_press" | "key_release" | "key_repeat";
  code: number;
  value: number;
  device: string;
}

export interface MouseEvent {
  time_ms: number;
  type: "mouse_move_rel" | "mouse_move_abs" | "mouse_button";
  code: number;
  value: number;
  x: number;
  y: number;
  device: string;
}

export interface USBDevice {
  bus: string;
  device: string;
  id: string;
  name: string;
}

export interface MountInfo {
  device: string;
  mount_point: string;
  fs_type: string;
  options: string;
}

export interface InputDeviceInfo {
  name: string;
  handler: string;
  path: string;
  type: "keyboard" | "absolute_mouse" | "relative_mouse" | "unknown";
  is_jetkvm: boolean;
}

export interface DisplayInfo {
  connector: string;
  status: "connected" | "disconnected";
  resolution?: string;
  modes?: string[];
}

// Linux evdev key codes (matching input-event-codes.h)
export const KEY = {
  ESC: 1,
  KEY_1: 2,
  KEY_2: 3,
  KEY_3: 4,
  KEY_4: 5,
  KEY_5: 6,
  KEY_6: 7,
  KEY_7: 8,
  KEY_8: 9,
  KEY_9: 10,
  KEY_0: 11,
  MINUS: 12,
  EQUAL: 13,
  Q: 16,
  W: 17,
  E: 18,
  R: 19,
  T: 20,
  Y: 21,
  U: 22,
  I: 23,
  O: 24,
  P: 25,
  LEFT_BRACE: 26,
  RIGHT_BRACE: 27,
  A: 30,
  S: 31,
  D: 32,
  F: 33,
  G: 34,
  H: 35,
  J: 36,
  K: 37,
  L: 38,
  SEMICOLON: 39,
  APOSTROPHE: 40,
  Z: 44,
  X: 45,
  C: 46,
  V: 47,
  B: 48,
  N: 49,
  M: 50,
  COMMA: 51,
  DOT: 52,
  SLASH: 53,
  ENTER: 28,
  SPACE: 57,
  BACKSPACE: 14,
  TAB: 15,
  CAPS_LOCK: 58,
  NUM_LOCK: 69,
  SCROLL_LOCK: 70,
  LEFT_CTRL: 29,
  LEFT_SHIFT: 42,
  LEFT_ALT: 56,
  LEFT_META: 125,
  RIGHT_CTRL: 97,
  RIGHT_SHIFT: 54,
  RIGHT_ALT: 100,
  UP: 103,
  DOWN: 108,
  LEFT: 105,
  RIGHT: 106,
  F1: 59,
  F2: 60,
  F3: 61,
  F4: 62,
  F5: 63,
  F6: 64,
  F7: 65,
  F8: 66,
  F9: 67,
  F10: 68,
  F11: 87,
  F12: 88,
  DELETE: 111,
  HOME: 102,
  END: 107,
  PAGE_UP: 104,
  PAGE_DOWN: 109,
  INSERT: 110,
} as const;

/**
 * Maps HID usage IDs (sent by JetKVM frontend) to Linux evdev key codes
 * (reported by the remote agent). Only includes commonly tested keys.
 */
export const HID_TO_LINUX: Record<number, number> = {
  // Letters (HID 0x04-0x1D → Linux KEY_A-KEY_Z)
  0x04: KEY.A,
  0x05: KEY.B,
  0x06: KEY.C,
  0x07: KEY.D,
  0x08: KEY.E,
  0x09: KEY.F,
  0x0a: KEY.G,
  0x0b: KEY.H,
  0x0c: KEY.I,
  0x0d: KEY.J,
  0x0e: KEY.K,
  0x0f: KEY.L,
  0x10: KEY.M,
  0x11: KEY.N,
  0x12: KEY.O,
  0x13: KEY.P,
  0x14: KEY.Q,
  0x15: KEY.R,
  0x16: KEY.S,
  0x17: KEY.T,
  0x18: KEY.U,
  0x19: KEY.V,
  0x1a: KEY.W,
  0x1b: KEY.X,
  0x1c: KEY.Y,
  0x1d: KEY.Z,
  // Numbers (HID 0x1E-0x27)
  0x1e: KEY.KEY_1,
  0x1f: KEY.KEY_2,
  0x20: KEY.KEY_3,
  0x21: KEY.KEY_4,
  0x22: KEY.KEY_5,
  0x23: KEY.KEY_6,
  0x24: KEY.KEY_7,
  0x25: KEY.KEY_8,
  0x26: KEY.KEY_9,
  0x27: KEY.KEY_0,
  // Special keys
  0x28: KEY.ENTER,
  0x29: KEY.ESC,
  0x2a: KEY.BACKSPACE,
  0x2b: KEY.TAB,
  0x2c: KEY.SPACE,
  // Modifiers and toggles
  0x39: KEY.CAPS_LOCK,
  0x53: KEY.NUM_LOCK,
  0x47: KEY.SCROLL_LOCK,
  // F-keys (HID 0x3A-0x45)
  0x3a: KEY.F1,
  0x3b: KEY.F2,
  0x3c: KEY.F3,
  0x3d: KEY.F4,
  0x3e: KEY.F5,
  0x3f: KEY.F6,
  0x40: KEY.F7,
  0x41: KEY.F8,
  0x42: KEY.F9,
  0x43: KEY.F10,
  0x44: KEY.F11,
  0x45: KEY.F12,
  // Navigation
  0x4f: KEY.RIGHT,
  0x50: KEY.LEFT,
  0x51: KEY.DOWN,
  0x52: KEY.UP,
  0x4a: KEY.HOME,
  0x4d: KEY.END,
  0x4b: KEY.PAGE_UP,
  0x4e: KEY.PAGE_DOWN,
  0x49: KEY.INSERT,
  0x4c: KEY.DELETE,
};

export class RemoteAgent {
  private baseUrl: string;

  constructor(host: string, port = 9182) {
    this.baseUrl = `http://${host}:${port}`;
  }

  private async get<T>(path: string): Promise<T> {
    const res = await fetch(`${this.baseUrl}${path}`);
    if (!res.ok) throw new Error(`Remote agent ${path}: ${res.status}`);
    return res.json() as Promise<T>;
  }

  private async del<T>(path: string): Promise<T> {
    const res = await fetch(`${this.baseUrl}${path}`, { method: "DELETE" });
    if (!res.ok) throw new Error(`Remote agent DELETE ${path}: ${res.status}`);
    return res.json() as Promise<T>;
  }

  /** Check if the agent is running. */
  async health(): Promise<boolean> {
    try {
      const res = await this.get<{ status: string }>("/health");
      return res.status === "ok";
    } catch {
      return false;
    }
  }

  // ── Event APIs ──

  async getKeyboardEvents(): Promise<KeyboardEvent[]> {
    return this.get<KeyboardEvent[]>("/events/keyboard");
  }

  async getMouseEvents(): Promise<MouseEvent[]> {
    return this.get<MouseEvent[]>("/events/mouse");
  }

  async popKeyboardEvents(): Promise<KeyboardEvent[]> {
    return this.get<KeyboardEvent[]>("/events/keyboard/pop");
  }

  async popMouseEvents(): Promise<MouseEvent[]> {
    return this.get<MouseEvent[]>("/events/mouse/pop");
  }

  async clearKeyboardEvents(): Promise<void> {
    await this.del("/events/keyboard");
  }

  async clearMouseEvents(): Promise<void> {
    await this.del("/events/mouse");
  }

  async clearAllEvents(): Promise<void> {
    await this.get("/events/clear");
  }

  // ── Device/system APIs ──

  async getUSBDevices(): Promise<USBDevice[]> {
    return this.get<USBDevice[]>("/usb/devices");
  }

  async getMounts(): Promise<MountInfo[]> {
    return this.get<MountInfo[]>("/mounts");
  }

  async getInputDevices(): Promise<InputDeviceInfo[]> {
    return this.get<InputDeviceInfo[]>("/input/devices");
  }

  async getDisplays(): Promise<DisplayInfo[]> {
    return this.get<DisplayInfo[]>("/display");
  }

  // ── High-level verification helpers ──

  /**
   * Verify a key was pressed on the remote host.
   * Clears events, calls `action`, then checks for the expected Linux key code.
   */
  async expectKeyPress(
    keyCode: number,
    action: () => Promise<void>,
    timeoutMs = 3000,
  ): Promise<KeyboardEvent[]> {
    await this.clearKeyboardEvents();
    await action();
    return this.waitForKeyboardEvent(
      ev => ev.code === keyCode && ev.type === "key_press",
      timeoutMs,
    );
  }

  /**
   * Verify a sequence of keys were pressed (in order).
   * Returns all matched key_press events.
   */
  async expectKeySequence(
    keyCodes: number[],
    action: () => Promise<void>,
    timeoutMs = 5000,
  ): Promise<KeyboardEvent[]> {
    await this.clearKeyboardEvents();
    await action();

    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      const events = await this.getKeyboardEvents();
      const presses = events.filter(ev => ev.type === "key_press");
      const pressedCodes = presses.map(ev => ev.code);

      // Check if all expected keys appeared (in order)
      let matchIdx = 0;
      for (const code of pressedCodes) {
        if (code === keyCodes[matchIdx]) {
          matchIdx++;
          if (matchIdx === keyCodes.length) {
            return presses;
          }
        }
      }
      await sleep(100);
    }
    throw new Error(`Timed out waiting for key sequence [${keyCodes.join(", ")}] (${timeoutMs}ms)`);
  }

  /**
   * Verify mouse movement occurred on the remote host.
   */
  async expectMouseMove(action: () => Promise<void>, timeoutMs = 3000): Promise<MouseEvent[]> {
    await this.clearMouseEvents();
    await action();
    return this.waitForMouseEvent(
      ev => ev.type === "mouse_move_abs" || ev.type === "mouse_move_rel",
      timeoutMs,
    );
  }

  /**
   * Verify a mouse button was clicked.
   */
  async expectMouseButton(action: () => Promise<void>, timeoutMs = 3000): Promise<MouseEvent[]> {
    await this.clearMouseEvents();
    await action();
    return this.waitForMouseEvent(ev => ev.type === "mouse_button", timeoutMs);
  }

  /**
   * Verify JetKVM is connected as a USB device on the remote host.
   */
  async expectJetKVMConnected(): Promise<USBDevice | undefined> {
    const devices = await this.getUSBDevices();
    return devices.find(d => d.name.includes("JetKVM") || d.id === "1d6b:0104");
  }

  /**
   * Get JetKVM input devices currently registered on the host.
   */
  async getJetKVMInputDevices(): Promise<InputDeviceInfo[]> {
    const devices = await this.getInputDevices();
    return devices.filter(d => d.is_jetkvm);
  }

  /**
   * Get the current display resolution from the connected monitor.
   */
  async getResolution(): Promise<string | null> {
    const displays = await this.getDisplays();
    const connected = displays.find(d => d.status === "connected");
    return connected?.resolution ?? null;
  }

  // ── Wait helpers ──

  async waitForKeyboardEvent(
    predicate: (ev: KeyboardEvent) => boolean,
    timeoutMs = 3000,
  ): Promise<KeyboardEvent[]> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      const events = await this.getKeyboardEvents();
      const matches = events.filter(predicate);
      if (matches.length > 0) return matches;
      await sleep(100);
    }
    throw new Error(`Timed out waiting for keyboard event (${timeoutMs}ms)`);
  }

  async waitForMouseEvent(
    predicate: (ev: MouseEvent) => boolean,
    timeoutMs = 3000,
  ): Promise<MouseEvent[]> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      const events = await this.getMouseEvents();
      const matches = events.filter(predicate);
      if (matches.length > 0) return matches;
      await sleep(100);
    }
    throw new Error(`Timed out waiting for mouse event (${timeoutMs}ms)`);
  }

  async waitForUSBDevice(
    predicate: (d: USBDevice) => boolean,
    present: boolean,
    timeoutMs = 10000,
  ): Promise<USBDevice[]> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      const devices = await this.getUSBDevices();
      const matches = devices.filter(predicate);
      if (present && matches.length > 0) return matches;
      if (!present && matches.length === 0) return [];
      await sleep(250);
    }
    throw new Error(
      `Timed out waiting for USB device to ${present ? "appear" : "disappear"} (${timeoutMs}ms)`,
    );
  }

  async waitForMount(
    predicate: (m: MountInfo) => boolean,
    present: boolean,
    timeoutMs = 10000,
  ): Promise<MountInfo[]> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      const mounts = await this.getMounts();
      const matches = mounts.filter(predicate);
      if (present && matches.length > 0) return matches;
      if (!present && matches.length === 0) return [];
      await sleep(500);
    }
    throw new Error(
      `Timed out waiting for mount to ${present ? "appear" : "disappear"} (${timeoutMs}ms)`,
    );
  }

  /**
   * Wait for JetKVM input devices to have specific types registered.
   * Useful for verifying USB preset changes (e.g., keyboard_only removes mouse devices).
   */
  async waitForInputDevices(
    expectedTypes: string[],
    timeoutMs = 10000,
  ): Promise<InputDeviceInfo[]> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      const devices = await this.getJetKVMInputDevices();
      const types = devices.map(d => d.type).sort();
      const expected = [...expectedTypes].sort();
      if (JSON.stringify(types) === JSON.stringify(expected)) return devices;
      await sleep(200);
    }
    throw new Error(
      `Timed out waiting for input device types [${expectedTypes.join(", ")}] (${timeoutMs}ms)`,
    );
  }

  /**
   * Wait for display resolution to change.
   */
  async waitForResolution(expected: string, timeoutMs = 15000): Promise<string> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      const res = await this.getResolution();
      if (res === expected) return res;
      await sleep(500);
    }
    throw new Error(`Timed out waiting for resolution ${expected} (${timeoutMs}ms)`);
  }

  /**
   * Ensure the remote agent is running on the target host.
   * Always rebuilds from source when it has changed and redeploys.
   */
  async ensureDeployed(sshTarget?: string): Promise<void> {
    const host = this.baseUrl.replace(/^https?:\/\//, "").replace(/:\d+$/, "");
    const port = parseInt(this.baseUrl.replace(/.*:/, ""), 10);
    const target = sshTarget ?? process.env.JETKVM_REMOTE_HOST ?? `tony@${host}`;

    const thisDir = path.dirname(fileURLToPath(import.meta.url));
    const agentDir = path.resolve(thisDir, "..", "..", "..", "e2e", "remote-agent");
    const binary = path.join(agentDir, "remote-agent");
    const goSource = path.join(agentDir, "main.go");

    if (!fs.existsSync(goSource)) {
      throw new Error(
        `Remote agent source not found at ${goSource}. ` +
          `Restore it with: git checkout e2e-remote-host-agent -- e2e/remote-agent/`,
      );
    }

    // Rebuild if source is newer than binary (or binary doesn't exist)
    const needsBuild =
      !fs.existsSync(binary) || fs.statSync(goSource).mtimeMs > fs.statSync(binary).mtimeMs;

    if (needsBuild) {
      console.log("[remote-agent] Building remote-agent binary...");
      execSync("GOOS=linux GOARCH=amd64 go build -o remote-agent .", {
        cwd: agentDir,
        stdio: "inherit",
      });
    }

    // Skip deploy only when the binary hasn't changed and the agent is already running
    if (!needsBuild && (await this.health())) return;

    const sshOpts =
      "-o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -o ConnectTimeout=30 -o ServerAliveInterval=5 -o ServerAliveCountMax=3";

    console.log(`[remote-agent] Deploying to ${target}...`);
    execSync(`scp ${sshOpts} "${binary}" ${target}:/tmp/remote-agent`, { stdio: "inherit" });

    console.log(`[remote-agent] Starting on port ${port}...`);
    execSync(
      `ssh ${sshOpts} ${target} 'pkill -x remote-agent 2>/dev/null; sleep 0.5; PORT=${port} nohup /tmp/remote-agent </dev/null >/tmp/remote-agent.log 2>&1 & sleep 0.5'`,
      { stdio: "inherit" },
    );

    await sleep(1500);

    if (!(await this.health())) {
      let logs = "";
      try {
        logs = execSync(`ssh ${sshOpts} ${target} "cat /tmp/remote-agent.log"`, {
          encoding: "utf8",
        });
      } catch {
        /* best effort */
      }
      throw new Error(`Remote agent failed to start on ${host}:${port}.\nLogs:\n${logs}`);
    }
    console.log(`[remote-agent] Agent running at ${this.baseUrl}`);
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Create a RemoteAgent from the JETKVM_REMOTE_HOST env var.
 * Falls back to null if not configured (tests can skip gracefully).
 */
export function createRemoteAgent(): RemoteAgent | null {
  const raw = process.env.JETKVM_REMOTE_HOST;
  if (!raw) return null;
  const host = raw.includes("@") ? raw.split("@").pop()! : raw;
  const port = parseInt(process.env.JETKVM_REMOTE_PORT || "9182", 10);
  return new RemoteAgent(host, port);
}
