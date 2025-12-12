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

  /** Send absolute mouse move (x, y in 0-32767 range, buttons bitmask) */
  sendAbsMouseMove: (x: number, y: number, buttons: number) => void;

  /** Capture a region of the video frame as base64 PNG */
  captureVideoRegion: (x: number, y: number, width: number, height: number) => Promise<string | null>;

  /**
   * Capture a small fingerprint of a region of the video frame.
   * Returns a downsampled grayscale grid (length = gridSize * gridSize).
   * This is much less flaky than comparing whole PNGs.
   */
  captureVideoRegionFingerprint: (
    x: number,
    y: number,
    width: number,
    height: number,
    gridSize?: number,
  ) => number[] | null;

  /** Get the video stream's natural dimensions */
  getVideoStreamDimensions: () => { width: number; height: number } | null;

  /** Check if WebRTC peer connection is connected */
  isWebRTCConnected: () => boolean;

  /** Check if HID RPC channel is ready */
  isHidRpcReady: () => boolean;

  /** Check if video stream is active */
  isVideoStreamActive: () => boolean;
}

/** Internal handler storage type */
interface TestHooksInternal {
  handleKeyPress?: (key: number, press: boolean) => void;
  handleAbsMouseMove?: (x: number, y: number, buttons: number) => void;
  getKeyboardLedState?: () => KeyboardLedState;
  getKeysDownState?: () => KeysDownState;
  getPeerConnectionState?: () => RTCPeerConnectionState | null;
  getRpcHidProtocolVersion?: () => number | null;
  getMediaStream?: () => MediaStream | null;
  getHdmiState?: () => string;
  getVideoElement?: () => HTMLVideoElement | null;
}

declare global {
  interface Window {
    __E2E_TEST__?: boolean;
    __kvmTestHooks?: KvmTestHooks;
    __kvmTestHooksInternal?: TestHooksInternal;
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

    sendAbsMouseMove: (x: number, y: number, buttons: number) => {
      const handler = window.__kvmTestHooksInternal?.handleAbsMouseMove;
      if (handler) {
        handler(x, y, buttons);
      } else {
        console.warn("[E2E] sendAbsMouseMove called but no handler registered");
      }
    },

    captureVideoRegion: async (x: number, y: number, width: number, height: number): Promise<string | null> => {
      const videoElement = window.__kvmTestHooksInternal?.getVideoElement?.();
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

      // Draw the specified region of the video onto the canvas
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
      const videoElement = window.__kvmTestHooksInternal?.getVideoElement?.();
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
              // simple luma approximation
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
      const videoElement = window.__kvmTestHooksInternal?.getVideoElement?.();
      if (!videoElement || !videoElement.videoWidth || !videoElement.videoHeight) {
        return null;
      }
      return { width: videoElement.videoWidth, height: videoElement.videoHeight };
    },

    isVideoStreamActive: () => {
      const hdmiState = window.__kvmTestHooksInternal?.getHdmiState?.();
      if (hdmiState !== "ready") return false;

      const stream = window.__kvmTestHooksInternal?.getMediaStream?.();
      if (!stream) return false;
      const videoTracks = stream.getVideoTracks();
      return videoTracks.length > 0 && videoTracks[0].readyState === "live";
    },
  };

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
}): void {
  if (window.__kvmTestHooksInternal) {
    Object.assign(window.__kvmTestHooksInternal, handlers);
  }
}

/**
 * Cleanup test hooks when component unmounts.
 */
export function cleanupTestHooks(): void {
  window.__kvmTestHooksInternal = {};
}
