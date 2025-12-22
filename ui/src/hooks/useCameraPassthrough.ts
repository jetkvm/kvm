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
        console.log('[CameraPassthrough] Starting...');

        // Create transport first (to receive format negotiation)
        console.log('[CameraPassthrough] Creating transport');
        const transport = await createCameraTransport(baseUrl);
        if (cancelled) {
          transport.close();
          return;
        }
        transportRef.current = transport;

        // Create encoder - start with H.264, may switch to MJPEG based on host request
        // 24fps (cinema rate) to reduce CPU/USB load on JetKVM
        const encoder = await createEncoder('h264', {
          width: 1920,
          height: 1080,
          frameRate: 24, // 24fps cinema rate - reduces CPU/USB load on JetKVM
          bitrate: 3_000_000, // 3 Mbps for H.264 at 24fps
          quality: 0.35, // 35% quality for MJPEG (smaller frames = less CPU/bandwidth)
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
            console.log('[CameraPassthrough] Format request:', format);
            // JetKVM tells us what format UVC host wants
            const currentEncoder = encoderRef.current;
            console.log('[CameraPassthrough] Current encoder:', currentEncoder ? currentEncoder.currentCodec : 'null', 'state:', currentEncoder?.state);

            if (currentEncoder) {
              // Update frame rate to match what UVC host negotiated
              // This is critical for reducing CPU load on the device
              if (format.frameRate && format.frameRate > 0) {
                currentEncoder.setFrameRate(format.frameRate);
              }

              // Apply encoder settings from JetKVM config
              if (format.h264Bitrate && format.h264Bitrate > 0) {
                currentEncoder.setBitrate(format.h264Bitrate);
              }
              if (format.mjpegQuality !== undefined && format.mjpegQuality > 0) {
                currentEncoder.setQuality(format.mjpegQuality);
              }

              // Switch codec if host requests different format
              if (currentEncoder.currentCodec !== format.codec) {
                try {
                  console.log('[CameraPassthrough] Switching codec to:', format.codec);
                  await currentEncoder.switchCodec(format.codec);
                  updateState({ currentCodec: format.codec });
                  console.log('[CameraPassthrough] Codec switched');
                } catch (err) {
                  console.error('[CameraPassthrough] Codec switch error:', err);
                  handleError(err instanceof Error ? err : new Error(String(err)));
                }
              }
            }

            // Start/resume encoding when format request received (UVC started with camera source)
            const enc = encoderRef.current;
            if (enc) {
              if (enc.state === 'idle' || enc.state === 'stopped') {
                // Encoder not running - start it now that UVC is ready
                console.log('[CameraPassthrough] Starting encoder (state was:', enc.state, ')');
                try {
                  await enc.start();
                  updateState({ encoderState: enc.state });
                  console.log('[CameraPassthrough] Encoder started, new state:', enc.state);
                } catch (err) {
                  console.error('[CameraPassthrough] Failed to start encoder:', err);
                  handleError(err instanceof Error ? err : new Error(String(err)));
                }
              } else if (enc.state === 'paused') {
                console.log('[CameraPassthrough] Resuming encoder');
                enc.resume();
                updateState({ encoderState: enc.state });
              }
            }
          },
          onStreamingStopped: () => {
            // Fully stop encoder when UVC host disconnects or source switches to HDMI
            // This releases camera resources (camera LED off, zero CPU usage)
            const enc = encoderRef.current;
            if (enc && (enc.state === 'running' || enc.state === 'paused')) {
              console.log('[CameraPassthrough] Stopping encoder (UVC stopped or switched to HDMI)');
              enc.stop();
              updateState({ encoderState: 'stopped' });
            }
          },
        });

        // Set up encoder event handlers
        let frameLogCount = 0;
        encoder.setEventHandlers({
          onFrame: (frame) => {
            // Log first few frames
            if (frameLogCount < 3) {
              console.log('[CameraPassthrough] Sending frame', frameLogCount, 'codec:', frame.codec, 'size:', frame.data.byteLength);
              frameLogCount++;
            }
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
        console.log('[CameraPassthrough] Connecting transport');
        await transport.connect();
        console.log('[CameraPassthrough] Transport connected');
        if (cancelled) {
          encoder.stop();
          transport.close();
          return;
        }

        // DON'T start encoder yet - wait for format request from UVC host
        // The encoder will be started in onFormatRequest callback when UVC streaming begins
        // This ensures SPS/PPS are sent when the host is ready to receive them
        console.log('[CameraPassthrough] Waiting for UVC format request before starting encoder');

        updateState({
          encoderState: 'idle', // Encoder waiting for format request
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
