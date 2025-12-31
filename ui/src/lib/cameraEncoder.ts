/**
 * Camera Encoder - Public API for H.264 and MJPEG encoding for UVC passthrough.
 *
 * This module provides a unified interface for encoding camera video using either
 * H.264 (via WebCodecs) or MJPEG (via WebWorker). The encoder automatically handles
 * camera initialization, frame capture, and codec switching.
 *
 * Architecture:
 * - CameraEncoder: Public facade managing camera lifecycle and delegating to codec-specific encoders
 * - H264Encoder: Internal implementation using WebCodecs API (./encoders/H264Encoder.ts)
 * - MjpegEncoder: Internal implementation using WebWorker (./encoders/MjpegEncoder.ts)
 */

import {
  H264Encoder,
  MjpegEncoder,
  isH264Supported,
  isVideoEncoderSupported,
  isMjpegSupported,
} from "./encoders";
import type {
  EncoderConfig,
  MutableEncoderConfig,
  EncodedFrame,
  VideoCodec,
} from "./encoders/types";

// Re-export types for public API
export type { VideoCodec, EncoderConfig, EncodedFrame };

export type EncoderState = "idle" | "initializing" | "running" | "paused" | "error" | "stopped";

export interface CameraEncoderEvents {
  onFrame: (frame: EncodedFrame) => void;
  onStateChange: (state: EncoderState) => void;
  onError: (error: Error) => void;
  onStats?: (stats: { fps: number; avgEncodeMs: number; frameSize: number }) => void;
}

/**
 * Default encoder configuration with values optimized for UVC passthrough.
 *
 * - width/height: 1080p is the most common UVC resolution, balancing quality and bandwidth
 * - frameRate: 30fps is widely compatible; higher rates may exceed USB 2.0 bandwidth
 * - bitrate: 9Mbps provides good H.264 quality at 1080p30 without exceeding typical
 *   USB 2.0 bandwidth (~40MB/s shared with other endpoints)
 * - quality: 0.65 MJPEG quality balances file size (~150KB/frame at 1080p) with
 *   acceptable visual quality; higher values increase bandwidth significantly
 * - keyFrameInterval: 1 second ensures fast seek/recovery if frames are dropped,
 *   at the cost of ~10% higher bitrate vs longer intervals
 */
const DEFAULT_CONFIG: EncoderConfig = {
  width: 1920,
  height: 1080,
  frameRate: 30,
  bitrate: 9_000_000,
  quality: 0.65,
  keyFrameInterval: 1,
};

// Re-export capability checks for convenience
export { isVideoEncoderSupported, isH264Supported, isMjpegSupported };

/**
 * Camera Encoder - Unified interface for H.264 and MJPEG encoding.
 *
 * This class manages the camera lifecycle and delegates encoding to the appropriate
 * codec-specific implementation (H264Encoder or MjpegEncoder).
 */
export class CameraEncoder {
  private codec: VideoCodec;
  private config: MutableEncoderConfig;
  private events: Partial<CameraEncoderEvents> = {};
  private _state: EncoderState = "idle";

  // Camera capture
  private mediaStream: MediaStream | null = null;
  private videoTrack: MediaStreamTrack | null = null;

  // Internal encoder (H264Encoder or MjpegEncoder)
  private internalEncoder: H264Encoder | MjpegEncoder | null = null;

  // Stats tracking
  private lastStatsTime = 0;
  private statsFrameCount = 0;
  private lastFrameSize = 0;
  private actualFrameRate = 0;
  private frameCount = 0;

  constructor(codec: VideoCodec, config: Partial<EncoderConfig> = {}) {
    this.codec = codec;

    // Create validated config by clamping values to valid ranges.
    // We build a new object rather than mutating since EncoderConfig is readonly.
    const merged = { ...DEFAULT_CONFIG, ...config };
    this.config = {
      width: Math.max(320, Math.min(3840, merged.width)),
      height: Math.max(240, Math.min(2160, merged.height)),
      frameRate: Math.max(1, Math.min(120, merged.frameRate)),
      bitrate: Math.max(100_000, Math.min(50_000_000, merged.bitrate)),
      quality: Math.max(0.0, Math.min(1.0, merged.quality)),
      keyFrameInterval: merged.keyFrameInterval,
    };
  }

  get frameRate(): number {
    return this.actualFrameRate || this.config.frameRate;
  }

  get state(): EncoderState {
    return this._state;
  }

  get currentCodec(): VideoCodec {
    return this.codec;
  }

  setEventHandlers(events: Partial<CameraEncoderEvents>): void {
    this.events = { ...this.events, ...events };
  }

  private setState(state: EncoderState): void {
    if (this._state !== state) {
      this._state = state;
      this.events.onStateChange?.(state);
    }
  }

  /**
   * Switch codec (will restart encoder to create new resources)
   */
  async switchCodec(newCodec: VideoCodec): Promise<void> {
    if (this.codec === newCodec) return;

    // Always restart when switching codec - need to create new encoder resources
    const wasActive = this._state === "running" || this._state === "paused";
    if (wasActive) {
      this.stop();
    }

    this.codec = newCodec;

    if (wasActive) {
      await this.start();
    }
  }

  /**
   * Update the target frame rate.
   * @returns true if the value was accepted, false if invalid or unchanged
   */
  setFrameRate(fps: number): boolean {
    if (fps < 1 || fps > 120) {
      console.warn(`[CameraEncoder] setFrameRate: invalid fps ${fps} (must be 1-120)`);
      return false;
    }
    if (this.config.frameRate === fps) return true;

    this.config.frameRate = fps;
    this.internalEncoder?.setFrameRate(fps);
    return true;
  }

  /**
   * Update the H.264 bitrate (takes effect on encoder restart).
   * @returns true if the value was accepted, false if invalid or unchanged
   */
  setBitrate(bitrate: number): boolean {
    if (bitrate < 100_000 || bitrate > 50_000_000) {
      console.warn(`[CameraEncoder] setBitrate: invalid bitrate ${bitrate} (must be 100k-50M)`);
      return false;
    }
    if (this.config.bitrate === bitrate) return true;

    this.config.bitrate = bitrate;
    return true;
  }

  /**
   * Update the MJPEG quality (takes effect immediately).
   * @returns true if the value was accepted, false if invalid or unchanged
   */
  setQuality(quality: number): boolean {
    if (quality < 0.0 || quality > 1.0) {
      console.warn(`[CameraEncoder] setQuality: invalid quality ${quality} (must be 0.0-1.0)`);
      return false;
    }
    if (this.config.quality === quality) return true;

    this.config.quality = quality;

    // Update MJPEG encoder immediately if running
    if (this.codec === "mjpeg" && this.internalEncoder instanceof MjpegEncoder) {
      this.internalEncoder.setQuality(quality);
    }
    return true;
  }

  /**
   * Update the capture resolution (requires encoder restart).
   * @returns true if the value was accepted, false if invalid or unchanged
   */
  async setResolution(width: number, height: number): Promise<boolean> {
    if (width < 320 || width > 3840 || height < 240 || height > 2160) {
      console.warn(
        `[CameraEncoder] setResolution: invalid ${width}x${height} (must be 320-3840 x 240-2160)`,
      );
      return false;
    }
    if (this.config.width === width && this.config.height === height) return true;

    this.config.width = width;
    this.config.height = height;

    // Resolution change requires encoder restart
    if (this._state === "running" || this._state === "paused") {
      this.stop();
      await this.start();
    }
    return true;
  }

  /**
   * Update stats and report periodically.
   */
  private updateStats(frameSize: number): void {
    this.frameCount++;
    this.statsFrameCount++;
    this.lastFrameSize = frameSize;

    const now = performance.now();
    if (now - this.lastStatsTime >= 1000) {
      const fps = (this.statsFrameCount / (now - this.lastStatsTime)) * 1000;
      this.events.onStats?.({ fps, avgEncodeMs: 0, frameSize: this.lastFrameSize });
      this.lastStatsTime = now;
      this.statsFrameCount = 0;
    }
  }

  /**
   * Initialize camera and get actual settings.
   */
  private async initCamera(): Promise<{ width: number; height: number }> {
    this.mediaStream = await navigator.mediaDevices.getUserMedia({
      video: {
        width: { ideal: this.config.width },
        height: { ideal: this.config.height },
        frameRate: { ideal: this.config.frameRate, min: 15 },
      },
    });

    const videoTracks = this.mediaStream.getVideoTracks();
    if (videoTracks.length === 0) {
      throw new Error("No video track available");
    }

    this.videoTrack = videoTracks[0];
    const settings = this.videoTrack.getSettings();
    const width = settings.width || this.config.width;
    const height = settings.height || this.config.height;
    this.actualFrameRate = settings.frameRate || this.config.frameRate;

    return { width, height };
  }

  /**
   * Start the appropriate encoder based on current codec.
   */
  private async startEncoder(width: number, height: number): Promise<void> {
    this.lastStatsTime = performance.now();
    this.statsFrameCount = 0;

    const events = {
      onFrame: (frame: EncodedFrame) => {
        this.updateStats(frame.data.byteLength);
        this.events.onFrame?.(frame);
      },
      onError: (error: Error) => {
        this.events.onError?.(error);
        this.setState("error");
      },
    };

    if (this.codec === "h264") {
      if (!this.videoTrack) throw new Error("No video track available");
      this.internalEncoder = new H264Encoder(this.videoTrack, this.config, events);
    } else {
      if (!this.mediaStream) throw new Error("No media stream available");
      this.internalEncoder = new MjpegEncoder(
        this.mediaStream,
        this.config,
        events,
        this.actualFrameRate,
      );
    }

    await this.internalEncoder.start(width, height);
    this.setState("running");
  }

  /**
   * Start capturing and encoding camera video
   */
  async start(): Promise<void> {
    if (this._state === "running") {
      return;
    }

    this.setState("initializing");

    try {
      const { width, height } = await this.initCamera();
      await this.startEncoder(width, height);
    } catch (error) {
      this.setState("error");
      this.events.onError?.(error instanceof Error ? error : new Error(String(error)));
      this.cleanup();
      throw error;
    }
  }

  /**
   * Stop capturing and encoding
   */
  stop(): void {
    this.setState("stopped");
    this.cleanup();
  }

  pause(): void {
    if (this._state !== "running") {
      return;
    }
    this.setState("paused");

    // Stop internal encoder and release camera resources
    // (resume() will reinitialize the camera when called)
    this.internalEncoder?.stop();
    this.internalEncoder = null;

    if (this.mediaStream) {
      this.mediaStream.getTracks().forEach(track => track.stop());
      this.mediaStream = null;
      this.videoTrack = null;
    }
  }

  async resume(): Promise<void> {
    if (this._state !== "paused") {
      return;
    }

    try {
      const { width, height } = await this.initCamera();
      this.forceKeyFrame();
      await this.startEncoder(width, height);
    } catch (error) {
      this.setState("error");
      this.events.onError?.(error instanceof Error ? error : new Error(String(error)));
      throw error;
    }
  }

  forceKeyFrame(): void {
    this.internalEncoder?.forceKeyFrame();
  }

  private cleanup(): void {
    this.internalEncoder?.stop();
    this.internalEncoder = null;

    if (this.mediaStream) {
      this.mediaStream.getTracks().forEach(track => track.stop());
      this.mediaStream = null;
    }

    this.videoTrack = null;
    this.frameCount = 0;
  }
}

/**
 * Create an encoder with automatic codec selection.
 * Prefers H.264 if supported, falls back to MJPEG.
 */
export async function createEncoder(
  preferredCodec: VideoCodec = "h264",
  config?: Partial<EncoderConfig>,
): Promise<CameraEncoder> {
  if (preferredCodec === "h264") {
    const h264Ok = await isH264Supported();
    if (h264Ok) {
      return new CameraEncoder("h264", config);
    }
  }

  if (isMjpegSupported()) {
    return new CameraEncoder("mjpeg", config);
  }

  throw new Error("No supported video codec available");
}
