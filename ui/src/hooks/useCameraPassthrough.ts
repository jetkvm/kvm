/**
 * Camera Passthrough Hook
 *
 * Manages the complete camera passthrough flow:
 * 1. WebSocket transport to JetKVM
 * 2. Camera capture and encoding (H.264 or MJPEG based on UVC host request)
 * 3. Format negotiation with JetKVM
 *
 * This replaces the WebRTC-based camera passthrough with a zero-overhead
 * WebSocket approach that sends raw encoded frames directly to UVC.
 */

import { useEffect, useRef, useState } from 'react';
import { CameraEncoder, createEncoder, type VideoCodec, type EncoderState } from '@/lib/cameraEncoder';
import { createCameraTransport, type CameraTransport, type TransportState, type CameraTransportStats } from '@/lib/cameraTransport';

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
    encoderState: 'idle',
    transportState: 'disconnected',
    currentCodec: 'h264',
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
      encoderState: 'idle',
      transportState: 'disconnected',
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

        // Create transport first (to receive format negotiation)
        const transport = await createCameraTransport(baseUrl);
        if (cancelled) {
          transport.close();
          return;
        }
        transportRef.current = transport;

        // Create encoder - start with H.264, may switch to MJPEG based on host request
        // 60fps target with optimized settings for throughput
        const encoder = await createEncoder('h264', {
          width: 1920,
          height: 1080,
          frameRate: 60, // 60fps target for smooth video
          bitrate: 8_000_000, // 8 Mbps for H.264 at 60fps
          quality: 0.65, // 65% quality for MJPEG (balance size/quality)
        });
        if (cancelled) {
          encoder.stop();
          transport.close();
          return;
        }
        encoderRef.current = encoder;

        // Set up transport event handlers
        transport.setEventHandlers({
          onStateChange: (transportState) => {
            updateState({ transportState });
          },
          onError: handleError,
          onStats: (stats) => {
            updateState({ stats });
          },
          onFormatRequest: async (format) => {
            // JetKVM tells us what format UVC host wants
            // Switch codec if host requests different format
            const currentEncoder = encoderRef.current;
            if (currentEncoder && currentEncoder.currentCodec !== format.codec) {
              try {
                await currentEncoder.switchCodec(format.codec);
                updateState({ currentCodec: format.codec });
              } catch (err) {
                handleError(err instanceof Error ? err : new Error(String(err)));
              }
            }
            // Resume encoding when format request received (UVC started)
            const enc = encoderRef.current;
            if (enc && enc.state === 'paused') {
              enc.resume();
            }
          },
          onStreamingStopped: () => {
            // Pause encoder when UVC host disconnects (saves CPU/power)
            const enc = encoderRef.current;
            if (enc && enc.state === 'running') {
              enc.pause();
              updateState({ encoderState: 'paused' });
            }
          },
        });

        // Set up encoder event handlers
        encoder.setEventHandlers({
          onFrame: (frame) => {
            // Send encoded frame to JetKVM
            const currentTransport = transportRef.current;
            if (currentTransport) {
              currentTransport.sendFrame(frame.data, frame.timestamp, frame.codec);
            }
          },
          onStateChange: (encoderState) => {
            updateState({ encoderState });
          },
          onError: handleError,
        });

        // Connect transport
        await transport.connect();
        if (cancelled) {
          encoder.stop();
          transport.close();
          return;
        }

        // Start encoder (captures camera and begins encoding)
        await encoder.start();
        if (cancelled) {
          encoder.stop();
          transport.close();
          return;
        }

        updateState({
          encoderState: encoder.state,
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
