/**
 * Camera passthrough hook - manages WebSocket transport and encoding for UVC.
 */

import React, { useEffect, useRef, useState } from "react";
import {
  CameraEncoder,
  createEncoder,
  type VideoCodec,
  type EncoderState,
} from "@/lib/cameraEncoder";
import {
  createCameraTransport,
  type CameraTransport,
  type TransportState,
  type CameraTransportStats,
  type FormatRequest,
} from "@/lib/cameraTransport";

/** Returns true if encoder state allows starting */
function isStartableState(state: EncoderState): boolean {
  return state === "idle" || state === "stopped" || state === "error";
}

/**
 * Applies format settings to encoder (frame rate, bitrate, quality).
 * Does not restart encoder - these are hot-configurable settings.
 */
function applyEncoderSettings(encoder: CameraEncoder, format: FormatRequest): number {
  let effectiveFrameRate = format.frameRate || 30;
  if (format.frameRateCap && format.frameRateCap >= 1) {
    effectiveFrameRate = Math.min(effectiveFrameRate, format.frameRateCap);
  }
  if (effectiveFrameRate > 0) {
    encoder.setFrameRate(effectiveFrameRate);
  }
  if (format.h264Bitrate && format.h264Bitrate > 0) {
    encoder.setBitrate(format.h264Bitrate);
  }
  if (format.mjpegQuality !== undefined && format.mjpegQuality > 0) {
    encoder.setQuality(format.mjpegQuality);
  }
  return effectiveFrameRate;
}

/**
 * Handles format change request from UVC host.
 * Updates encoder settings, switches codec if needed, and starts/resumes encoder.
 */
async function handleFormatRequest(
  format: FormatRequest,
  encoderRef: React.RefObject<CameraEncoder | null>,
  updateState: (updates: Partial<CameraPassthroughState>) => void,
  handleError: (error: Error) => void,
): Promise<void> {
  const currentEncoder = encoderRef.current;
  if (!currentEncoder) return;

  // Apply hot-configurable settings first (no restart)
  applyEncoderSettings(currentEncoder, format);

  // Switch codec if needed (may restart encoder)
  if (currentEncoder.currentCodec !== format.codec) {
    try {
      await currentEncoder.switchCodec(format.codec);
      updateState({ currentCodec: format.codec });
    } catch (err) {
      handleError(err instanceof Error ? err : new Error(String(err)));
      return;
    }
  }

  // Update resolution last (may restart encoder)
  if (format.width && format.height && format.width > 0 && format.height > 0) {
    try {
      const success = await currentEncoder.setResolution(format.width, format.height);
      if (!success) {
        handleError(new Error(`Failed to set resolution to ${format.width}x${format.height}`));
        return;
      }
    } catch (err) {
      handleError(err instanceof Error ? err : new Error(String(err)));
      return;
    }
  }

  // Start/resume encoder when USB host requests video
  const currentState = currentEncoder.state;
  try {
    if (isStartableState(currentState)) {
      await currentEncoder.start();
      updateState({ encoderState: currentEncoder.state });
    } else if (currentState === "paused") {
      await currentEncoder.resume();
      updateState({ encoderState: currentEncoder.state });
    } else if (currentState === "running") {
      currentEncoder.forceKeyFrame();
    }
  } catch (err) {
    handleError(err instanceof Error ? err : new Error(String(err)));
  }
}

export interface CameraPassthroughState {
  encoderState: EncoderState;
  transportState: TransportState;
  currentCodec: VideoCodec;
  stats: CameraTransportStats;
  error: Error | null;
}

export interface UseCameraPassthroughOptions {
  baseUrl: string;
  enabled: boolean;
  onError?: (error: Error) => void;
  onStateChange?: (state: CameraPassthroughState) => void;
}

export function useCameraPassthrough(options: UseCameraPassthroughOptions) {
  const { baseUrl, enabled, onError, onStateChange } = options;

  const [state, setState] = useState<CameraPassthroughState>({
    encoderState: "idle",
    transportState: "disconnected",
    currentCodec: "h264",
    stats: { framesSent: 0, bytesSent: 0, framesDropped: 0, avgLatencyMs: 0 },
    error: null,
  });

  const encoderRef = useRef<CameraEncoder | null>(null);
  const transportRef = useRef<CameraTransport | null>(null);
  const isRunningRef = useRef(false);

  // Use refs for callbacks to avoid effect dependency changes
  const onErrorRef = useRef(onError);
  const onStateChangeRef = useRef(onStateChange);
  onErrorRef.current = onError;
  onStateChangeRef.current = onStateChange;

  const updateState = (updates: Partial<CameraPassthroughState>) => {
    setState(prev => {
      const newState = { ...prev, ...updates };
      onStateChangeRef.current?.(newState);
      return newState;
    });
  };

  const handleError = (error: Error) => {
    updateState({ error });
    onErrorRef.current?.(error);
  };

  const cleanup = () => {
    isRunningRef.current = false;

    if (encoderRef.current) {
      encoderRef.current.stop();
      encoderRef.current = null;
    }

    if (transportRef.current) {
      transportRef.current.close();
      transportRef.current = null;
    }

    updateState({
      encoderState: "idle",
      transportState: "disconnected",
      error: null,
    });
  };

  useEffect(() => {
    if (!enabled) {
      cleanup();
      return;
    }

    // Prevent double initialization in StrictMode
    if (isRunningRef.current) {
      return;
    }
    isRunningRef.current = true;

    let cancelled = false;

    const start = async () => {
      try {
        const transport = await createCameraTransport(baseUrl);
        if (cancelled) {
          transport.close();
          return;
        }
        transportRef.current = transport;

        // Initial encoder config - will be overwritten by format request from backend
        // Using maximum capability values; actual settings come from user config
        const encoder = await createEncoder("h264", {
          width: 1920,
          height: 1080,
          frameRate: 30,
          bitrate: 3_000_000,
          quality: 0.35,
        });
        if (cancelled) {
          encoder.stop();
          transport.close();
          return;
        }
        encoderRef.current = encoder;

        // Set up transport event handlers
        transport.setEventHandlers({
          onStateChange: transportState => {
            updateState({ transportState });
          },
          onError: handleError,
          onStats: stats => {
            updateState({ stats });
          },
          onFormatRequest: format => handleFormatRequest(format, encoderRef, updateState, handleError),
          onStreamingStopped: () => {
            const enc = encoderRef.current;
            if (enc && enc.state === "running") {
              enc.pause();
              updateState({ encoderState: "paused" });
            }
          },
        });

        encoder.setEventHandlers({
          onFrame: frame => {
            transportRef.current?.sendFrame(frame.data, frame.timestamp, frame.codec);
          },
          onStateChange: encoderState => {
            updateState({ encoderState });
          },
          onError: handleError,
        });

        await transport.connect();
        if (cancelled) {
          encoder.stop();
          transport.close();
          return;
        }

        updateState({
          encoderState: "idle",
          transportState: transport.state,
          currentCodec: encoder.currentCodec,
        });
      } catch (err) {
        if (!cancelled) {
          handleError(err instanceof Error ? err : new Error(String(err)));
          cleanup();
        }
      }
    };

    start();

    return () => {
      cancelled = true;
      cleanup();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, baseUrl]);

  return {
    ...state,
    cleanup,
  };
}
