/**
 * E2E Test Hooks
 *
 * This module exposes test hooks on window.__kvmTestHooks for Playwright E2E tests.
 *
 * Usage in tests:
 *   await page.goto('/');
 *   const ledState = await page.evaluate(() => window.__kvmTestHooks?.getKeyboardLedState());
 */

import { KeyboardLedState, KeysDownState } from "@/hooks/stores";

/** Internal handlers set by React components (prefixed with _ to indicate internal use) */
interface TestHooksInternal {
  _handleKeyPress?: (key: number, press: boolean) => void;
  _handleAbsMouseMove?: (x: number, y: number, buttons: number) => void;
  _getKeyboardLedState?: () => KeyboardLedState;
  _getKeysDownState?: () => KeysDownState;
  _getPeerConnectionState?: () => RTCPeerConnectionState | null;
  _getRpcHidProtocolVersion?: () => number | null;
  _getMediaStream?: () => MediaStream | null;
  _getHdmiState?: () => string;
  _getVideoElement?: () => HTMLVideoElement | null;
  _getKvmTerminal?: () => RTCDataChannel | null;
}

export interface KvmTestHooks extends TestHooksInternal {
  getKeyboardLedState: () => KeyboardLedState | null;
  getKeysDownState: () => KeysDownState | null;
  sendKeypress: (key: number, press: boolean) => void;
  sendAbsMouseMove: (x: number, y: number, buttons: number) => void;
  sendTerminalCommand: (command: string) => boolean;
  isTerminalReady: () => boolean;
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
  getVideoStreamDimensions: () => { width: number; height: number } | null;
  isWebRTCConnected: () => boolean;
  isHidRpcReady: () => boolean;
  isVideoStreamActive: () => boolean;
}

declare global {
  interface Window {
    __kvmTestHooks?: KvmTestHooks;
  }
}

/**
 * Initialize test hooks on the window object.
 * Call this early in the app lifecycle.
 */
export function initTestHooks(): void {
  if (typeof window === "undefined") return;

  const hooks: KvmTestHooks = {
    getKeyboardLedState: () => hooks._getKeyboardLedState?.() ?? null,

    getKeysDownState: () => hooks._getKeysDownState?.() ?? null,

    sendKeypress: (key: number, press: boolean) => {
      if (hooks._handleKeyPress) {
        hooks._handleKeyPress(key, press);
      } else {
        console.warn("[E2E] sendKeypress called but no handler registered");
      }
    },

    sendAbsMouseMove: (x: number, y: number, buttons: number) => {
      if (hooks._handleAbsMouseMove) {
        hooks._handleAbsMouseMove(x, y, buttons);
      } else {
        console.warn("[E2E] sendAbsMouseMove called but no handler registered");
      }
    },

    sendTerminalCommand: (command: string) => {
      const terminal = hooks._getKvmTerminal?.();
      if (terminal && terminal.readyState === "open") {
        terminal.send(command + "\n");
        return true;
      }
      return false;
    },

    isTerminalReady: () => {
      const terminal = hooks._getKvmTerminal?.();
      return terminal?.readyState === "open";
    },

    isWebRTCConnected: () => hooks._getPeerConnectionState?.() === "connected",

    isHidRpcReady: () => {
      const version = hooks._getRpcHidProtocolVersion?.();
      return version !== null && version !== undefined;
    },

    captureVideoRegion: async (
      x: number,
      y: number,
      width: number,
      height: number,
    ): Promise<string | null> => {
      const videoElement = hooks._getVideoElement?.();
      if (!videoElement) {
        console.warn("[E2E] captureVideoRegion called but no video element available");
        return null;
      }

      const canvas = document.createElement("canvas");
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext("2d");
      if (!ctx) {
        console.warn("[E2E] captureVideoRegion: failed to get 2d context");
        return null;
      }

      ctx.drawImage(videoElement, x, y, width, height, 0, 0, width, height);
      return canvas.toDataURL("image/png");
    },

    captureVideoRegionFingerprint: (
      x: number,
      y: number,
      width: number,
      height: number,
      gridSize = 8,
    ): number[] | null => {
      const videoElement = hooks._getVideoElement?.();
      if (!videoElement) {
        console.warn("[E2E] captureVideoRegionFingerprint called but no video element available");
        return null;
      }

      const canvas = document.createElement("canvas");
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext("2d");
      if (!ctx) {
        console.warn("[E2E] captureVideoRegionFingerprint: failed to get 2d context");
        return null;
      }

      ctx.drawImage(videoElement, x, y, width, height, 0, 0, width, height);

      const imageData = ctx.getImageData(0, 0, width, height).data;
      const fp: number[] = [];

      const cellW = Math.max(1, Math.floor(width / gridSize));
      const cellH = Math.max(1, Math.floor(height / gridSize));

      for (let gy = 0; gy < gridSize; gy++) {
        for (let gx = 0; gx < gridSize; gx++) {
          const startX = gx * cellW;
          const startY = gy * cellH;
          const endX = gx === gridSize - 1 ? width : Math.min(width, startX + cellW);
          const endY = gy === gridSize - 1 ? height : Math.min(height, startY + cellH);

          let sum = 0;
          let count = 0;

          for (let py = startY; py < endY; py++) {
            for (let px = startX; px < endX; px++) {
              const idx = (py * width + px) * 4;
              const r = imageData[idx];
              const g = imageData[idx + 1];
              const b = imageData[idx + 2];
              sum += (r * 3 + g * 4 + b) >> 3;
              count++;
            }
          }

          fp.push(count > 0 ? Math.round(sum / count) : 0);
        }
      }

      return fp;
    },

    getVideoStreamDimensions: () => {
      const videoElement = hooks._getVideoElement?.();
      if (!videoElement || !videoElement.videoWidth || !videoElement.videoHeight) {
        return null;
      }
      return { width: videoElement.videoWidth, height: videoElement.videoHeight };
    },

    isVideoStreamActive: () => {
      const hdmiState = hooks._getHdmiState?.();
      if (hdmiState !== "ready") return false;

      const stream = hooks._getMediaStream?.();
      if (!stream) return false;
      const videoTracks = stream.getVideoTracks();
      return videoTracks.length > 0 && videoTracks[0].readyState === "live";
    },
  };

  window.__kvmTestHooks = hooks;
  console.log("[E2E] Test hooks initialized");
}

/**
 * Register all test handlers at once.
 * Call this from the device route component.
 */
export function registerTestHandlers(handlers: {
  handleKeyPress: (key: number, press: boolean) => void;
  handleAbsMouseMove: (x: number, y: number, buttons: number) => void;
  getKeyboardLedState: () => KeyboardLedState;
  getKeysDownState: () => KeysDownState;
  getPeerConnectionState: () => RTCPeerConnectionState | null;
  getRpcHidProtocolVersion: () => number | null;
  getMediaStream: () => MediaStream | null;
  getHdmiState: () => string;
  getVideoElement: () => HTMLVideoElement | null;
  getKvmTerminal: () => RTCDataChannel | null;
}): void {
  if (!window.__kvmTestHooks) return;

  window.__kvmTestHooks._handleKeyPress = handlers.handleKeyPress;
  window.__kvmTestHooks._handleAbsMouseMove = handlers.handleAbsMouseMove;
  window.__kvmTestHooks._getKeyboardLedState = handlers.getKeyboardLedState;
  window.__kvmTestHooks._getKeysDownState = handlers.getKeysDownState;
  window.__kvmTestHooks._getPeerConnectionState = handlers.getPeerConnectionState;
  window.__kvmTestHooks._getRpcHidProtocolVersion = handlers.getRpcHidProtocolVersion;
  window.__kvmTestHooks._getMediaStream = handlers.getMediaStream;
  window.__kvmTestHooks._getHdmiState = handlers.getHdmiState;
  window.__kvmTestHooks._getVideoElement = handlers.getVideoElement;
  window.__kvmTestHooks._getKvmTerminal = handlers.getKvmTerminal;
}

/**
 * Cleanup test hooks when component unmounts.
 */
export function cleanupTestHooks(): void {
  if (!window.__kvmTestHooks) return;

  window.__kvmTestHooks._handleKeyPress = undefined;
  window.__kvmTestHooks._handleAbsMouseMove = undefined;
  window.__kvmTestHooks._getKeyboardLedState = undefined;
  window.__kvmTestHooks._getKeysDownState = undefined;
  window.__kvmTestHooks._getPeerConnectionState = undefined;
  window.__kvmTestHooks._getRpcHidProtocolVersion = undefined;
  window.__kvmTestHooks._getMediaStream = undefined;
  window.__kvmTestHooks._getHdmiState = undefined;
  window.__kvmTestHooks._getVideoElement = undefined;
  window.__kvmTestHooks._getKvmTerminal = undefined;
}
