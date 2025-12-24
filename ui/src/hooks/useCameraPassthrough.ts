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

import { useEffect, useRef, useState } from "react";
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
} from "@/lib/cameraTransport";

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
        console.log("[CameraPassthrough] Starting...");

        // Create transport first (to receive format negotiation)
        console.log("[CameraPassthrough] Creating transport");
        const transport = await createCameraTransport(baseUrl);
        if (cancelled) {
          transport.close();
          return;
        }
        transportRef.current = transport;

        // Create encoder - start with H.264, may switch to MJPEG based on host request
        // 60fps target with optimized settings for throughput
        const encoder = await createEncoder("h264", {
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
          onStateChange: transportState => {
            updateState({ transportState });
          },
          onError: handleError,
          onStats: stats => {
            updateState({ stats });
          },
          onFormatRequest: async format => {
            console.log("[CameraPassthrough] Format request:", format);
            // JetKVM tells us what format UVC host wants
            const currentEncoder = encoderRef.current;
            console.log(
              "[CameraPassthrough] Current encoder:",
              currentEncoder ? currentEncoder.currentCodec : "null",
              "state:",
              currentEncoder?.state,
            );

            if (currentEncoder) {
              // IMPORTANT: Apply settings in correct order:
              // 1. Frame rate, bitrate, quality (config changes, no restart)
              // 2. Codec switch (may restart encoder with new codec)
              // 3. Resolution change (may restart encoder with new camera constraints)
              // This ensures all settings are applied before any restart occurs.

              // Use the minimum of UVC-negotiated rate and user's cap
              // This ensures we don't exceed what the host requested OR what the user configured
              let effectiveFrameRate = format.frameRate || 30;
              if (format.frameRateCap && format.frameRateCap > 0) {
                effectiveFrameRate = Math.min(effectiveFrameRate, format.frameRateCap);
                console.log(
                  `[CameraPassthrough] Frame rate: UVC=${format.frameRate}, cap=${format.frameRateCap}, effective=${effectiveFrameRate}`,
                );
              }
              if (effectiveFrameRate > 0) {
                currentEncoder.setFrameRate(effectiveFrameRate);
              }

              // Apply encoder settings from JetKVM config
              if (format.h264Bitrate && format.h264Bitrate > 0) {
                currentEncoder.setBitrate(format.h264Bitrate);
              }
              if (format.mjpegQuality !== undefined && format.mjpegQuality > 0) {
                currentEncoder.setQuality(format.mjpegQuality);
              }

              // Switch codec if host requests different format (may restart encoder)
              if (currentEncoder.currentCodec !== format.codec) {
                try {
                  console.log("[CameraPassthrough] Switching codec to:", format.codec);
                  await currentEncoder.switchCodec(format.codec);
                  updateState({ currentCodec: format.codec });
                  console.log("[CameraPassthrough] Codec switched");
                } catch (err) {
                  console.error("[CameraPassthrough] Codec switch error:", err);
                  handleError(err instanceof Error ? err : new Error(String(err)));
                }
              }

              // Update resolution LAST - this may restart encoder with all settings applied
              if (format.width && format.height && format.width > 0 && format.height > 0) {
                await currentEncoder.setResolution(format.width, format.height);
              }
            }

            // Start/resume encoder when USB host requests video
            const enc = encoderRef.current;
            if (enc && enc.state === "idle") {
              console.log("[CameraPassthrough] Starting encoder (USB host requested video)");
              await enc.start();
              updateState({ encoderState: enc.state });
            } else if (enc && enc.state === "paused") {
              console.log("[CameraPassthrough] Resuming encoder");
              await enc.resume();
              updateState({ encoderState: enc.state });
            } else if (enc && enc.state === "running") {
              console.log("[CameraPassthrough] Format request while running - forcing keyframe");
              enc.forceKeyFrame();
            }
          },
          onStreamingStopped: () => {
            // Pause encoder when UVC host disconnects (saves CPU/power)
            const enc = encoderRef.current;
            if (enc && enc.state === "running") {
              console.log("[CameraPassthrough] Pausing encoder (UVC stopped)");
              enc.pause();
              updateState({ encoderState: "paused" });
            }
          },
        });

        // Set up encoder event handlers
        let frameLogCount = 0;
        encoder.setEventHandlers({
          onFrame: frame => {
            // Log first few frames
            if (frameLogCount < 3) {
              console.log(
                "[CameraPassthrough] Sending frame",
                frameLogCount,
                "codec:",
                frame.codec,
                "size:",
                frame.data.byteLength,
              );
              frameLogCount++;
            }
            // Send encoded frame to JetKVM
            const currentTransport = transportRef.current;
            if (currentTransport) {
              currentTransport.sendFrame(frame.data, frame.timestamp, frame.codec);
            }
          },
          onStateChange: encoderState => {
            updateState({ encoderState });
          },
          onError: handleError,
        });

        // Connect transport
        console.log("[CameraPassthrough] Connecting transport");
        await transport.connect();
        console.log("[CameraPassthrough] Transport connected");
        if (cancelled) {
          encoder.stop();
          transport.close();
          return;
        }

        // Encoder stays idle until USB host requests video (STREAMON)
        // Camera will activate only when onFormatRequest is triggered
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
