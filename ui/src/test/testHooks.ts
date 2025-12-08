/**
 * E2E Test Hooks
 *
 * This module exposes test hooks on window.__kvmTestHooks for Playwright E2E tests.
 * The hooks are only active when the page has window.__E2E_TEST__ set to true.
 *
 * Usage in tests:
 *   await page.evaluate(() => window.__E2E_TEST__ = true);
 *   await page.goto('/devices/local');
 *   const ledState = await page.evaluate(() => window.__kvmTestHooks?.getKeyboardLedState());
 */

import { KeyboardLedState, KeysDownState } from "@/hooks/stores";

export interface KvmTestHooks {
  /** Get current keyboard LED state (caps lock, num lock, etc.) */
  getKeyboardLedState: () => KeyboardLedState | null;

  /** Get current keys down state */
  getKeysDownState: () => KeysDownState | null;

  /** Send a keypress event (key: USB HID keycode, press: true=down, false=up) */
  sendKeypress: (key: number, press: boolean) => void;

  /** Check if WebRTC peer connection is connected */
  isWebRTCConnected: () => boolean;

  /** Check if HID RPC channel is ready */
  isHidRpcReady: () => boolean;
}

declare global {
  interface Window {
    __E2E_TEST__?: boolean;
    __kvmTestHooks?: KvmTestHooks;
    __kvmTestHooksInternal?: {
      handleKeyPress?: (key: number, press: boolean) => void;
      getKeyboardLedState?: () => KeyboardLedState;
      getKeysDownState?: () => KeysDownState;
      getPeerConnectionState?: () => RTCPeerConnectionState | null;
      getRpcHidProtocolVersion?: () => number | null;
    };
  }
}

/**
 * Initialize test hooks on the window object.
 * Call this early in the app lifecycle.
 */
export function initTestHooks(): void {
  if (typeof window === "undefined") return;

  // Initialize internal hooks storage
  window.__kvmTestHooksInternal = {};

  // Expose the public API
  window.__kvmTestHooks = {
    getKeyboardLedState: () => {
      return window.__kvmTestHooksInternal?.getKeyboardLedState?.() ?? null;
    },

    getKeysDownState: () => {
      return window.__kvmTestHooksInternal?.getKeysDownState?.() ?? null;
    },

    sendKeypress: (key: number, press: boolean) => {
      const handler = window.__kvmTestHooksInternal?.handleKeyPress;
      if (handler) {
        handler(key, press);
      } else {
        console.warn("[E2E] sendKeypress called but no handler registered");
      }
    },

    isWebRTCConnected: () => {
      const state = window.__kvmTestHooksInternal?.getPeerConnectionState?.();
      return state === "connected";
    },

    isHidRpcReady: () => {
      const version = window.__kvmTestHooksInternal?.getRpcHidProtocolVersion?.();
      return version !== null && version !== undefined;
    },
  };

  console.log("[E2E] Test hooks initialized");
}

/**
 * Register the keyboard handler from useKeyboard hook.
 * Call this from the device route component.
 */
export function registerKeyboardHandler(
  handleKeyPress: (key: number, press: boolean) => void,
): void {
  if (window.__kvmTestHooksInternal) {
    window.__kvmTestHooksInternal.handleKeyPress = handleKeyPress;
  }
}

/**
 * Register store getters for LED and keys state.
 * Call this from the device route component.
 */
export function registerHidStoreGetters(
  getKeyboardLedState: () => KeyboardLedState,
  getKeysDownState: () => KeysDownState,
): void {
  if (window.__kvmTestHooksInternal) {
    window.__kvmTestHooksInternal.getKeyboardLedState = getKeyboardLedState;
    window.__kvmTestHooksInternal.getKeysDownState = getKeysDownState;
  }
}

/**
 * Register RTC store getters for connection state.
 * Call this from the device route component.
 */
export function registerRTCStoreGetters(
  getPeerConnectionState: () => RTCPeerConnectionState | null,
  getRpcHidProtocolVersion: () => number | null,
): void {
  if (window.__kvmTestHooksInternal) {
    window.__kvmTestHooksInternal.getPeerConnectionState = getPeerConnectionState;
    window.__kvmTestHooksInternal.getRpcHidProtocolVersion = getRpcHidProtocolVersion;
  }
}

/**
 * Cleanup test hooks when component unmounts.
 */
export function cleanupTestHooks(): void {
  if (window.__kvmTestHooksInternal) {
    window.__kvmTestHooksInternal.handleKeyPress = undefined;
    window.__kvmTestHooksInternal.getKeyboardLedState = undefined;
    window.__kvmTestHooksInternal.getKeysDownState = undefined;
    window.__kvmTestHooksInternal.getPeerConnectionState = undefined;
    window.__kvmTestHooksInternal.getRpcHidProtocolVersion = undefined;
  }
}
