// Shared browser hook contract for the UI and hardware E2E tests.
import type { KeyboardLedState, KeysDownState } from "@/hooks/stores";

/** Internal handlers set by React components (prefixed with _ to indicate internal use) */
interface TestHooksInternal {
  _handleKeyPress?: (key: number, press: boolean) => void;
  _pauseKeepAlive?: (ms: number) => void;
  _handleAbsMouseMove?: (x: number, y: number, buttons: number) => void;
  _getKeyboardLedState?: () => KeyboardLedState;
  _getKeysDownState?: () => KeysDownState;
  _getPeerConnectionState?: () => RTCPeerConnectionState | null;
  _getRpcHidProtocolVersion?: () => number | null;
  _getMediaStream?: () => MediaStream | null;
  _getHdmiState?: () => string;
  _getVideoElement?: () => HTMLVideoElement | null;
  _getKvmTerminal?: () => RTCDataChannel | null;
  _getRpcDataChannel?: () => RTCDataChannel | null;
  _getPeerConnection?: () => RTCPeerConnection | null;
}

export interface KvmTestHooks extends TestHooksInternal {
  getKeyboardLedState: () => KeyboardLedState | null;
  getKeysDownState: () => KeysDownState | null;
  sendKeypress: (key: number, press: boolean) => void;
  /**
   * Test-only: pause keypress keepalives while preserving held keys.
   */
  pauseKeepAlive: (ms: number) => void;
  sendAbsMouseMove: (x: number, y: number, buttons: number) => void;
  sendJsonRpc: (
    method: string,
    params: Record<string, unknown>,
    callback: (resp: { error?: { message: string; data?: string }; result?: unknown }) => void,
    timeoutMs?: number,
  ) => void;
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
  isAudioStreamActive: () => boolean;
  getInboundVideoStats: () => Promise<{
    bytesReceived: number;
    timestamp: number;
    jitterBufferDelay: number;
    jitterBufferEmittedCount: number;
    framesPerSecond: number;
    framesDecoded: number;
    framesDropped: number;
    totalDecodeTime: number;
    freezeCount: number;
    totalFreezesDuration: number;
    codecMimeType: string;
  } | null>;
  getInboundAudioStats: () => Promise<{
    bytesReceived: number;
    packetsReceived: number;
    timestamp: number;
    audioLevel: number;
    totalAudioEnergy: number;
    totalSamplesDuration: number;
    codecMimeType: string;
  } | null>;
}

declare global {
  interface Window {
    __kvmTestHooks?: KvmTestHooks;
  }
}
