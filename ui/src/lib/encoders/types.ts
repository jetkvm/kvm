/**
 * Shared types for encoder implementations.
 * @internal
 */

/**
 * Video codec identifier.
 * Values must match camera.VideoCodec constants in internal/camera/manager.go
 * (CodecH264 = "h264", CodecMJPEG = "mjpeg").
 */
export type VideoCodec = "h264" | "mjpeg";

/**
 * Encoder configuration parameters (immutable).
 * All fields are readonly to prevent accidental mutation after construction.
 * Validation and clamping is performed in CameraEncoder constructor.
 */
export interface EncoderConfig {
  readonly width: number;
  readonly height: number;
  readonly frameRate: number;
  /** H.264 target bitrate in bits per second */
  readonly bitrate: number;
  /** MJPEG quality factor (0.0 - 1.0) */
  readonly quality: number;
  /** H.264 keyframe interval in seconds */
  readonly keyFrameInterval: number;
}

/**
 * Mutable version of EncoderConfig for internal use in CameraEncoder.
 * Used when config values need to be updated via setFrameRate/setBitrate/etc.
 * @internal
 */
export type MutableEncoderConfig = {
  -readonly [K in keyof EncoderConfig]: EncoderConfig[K];
};

/**
 * Encoded video frame with metadata.
 * All fields are readonly as frames are immutable once created.
 */
export interface EncodedFrame {
  /** Encoded frame data with codec byte prefix for wire transport */
  readonly data: ArrayBuffer;
  /** Frame presentation timestamp in microseconds */
  readonly timestamp: number;
  /** True for keyframes (always true for MJPEG) */
  readonly isKeyFrame: boolean;
  /** Codec used to encode this frame */
  readonly codec: VideoCodec;
}

/**
 * Internal encoder event handlers.
 * Used by H264Encoder and MjpegEncoder implementations.
 */
export interface InternalEncoderEvents {
  onFrame: (frame: EncodedFrame) => void;
  onError: (error: Error) => void;
}

/**
 * Interface for internal encoder implementations.
 * Both H264Encoder and MjpegEncoder implement this interface.
 */
export interface InternalEncoder {
  start(width: number, height: number): Promise<void>;
  stop(): void;
  forceKeyFrame(): void;
  setFrameRate(fps: number): void;
}

/**
 * Rate-limited error logger to prevent log spam during error storms.
 * Accumulates error counts and logs at most once per interval.
 *
 * @internal Used by H264Encoder and MjpegEncoder
 */
export class RateLimitedLogger {
  private errorCount = 0;
  private lastLogTime = 0;
  private readonly intervalMs: number;
  private readonly prefix: string;

  constructor(prefix: string, intervalMs = 1000) {
    if (intervalMs <= 0) {
      throw new Error(`RateLimitedLogger: intervalMs must be positive, got ${intervalMs}`);
    }
    this.prefix = prefix;
    this.intervalMs = intervalMs;
  }

  /**
   * Log an error with rate limiting.
   * Multiple errors within the interval are accumulated and reported together.
   */
  logError(context: string, err: unknown): void {
    this.errorCount++;
    const now = performance.now();
    if (now - this.lastLogTime >= this.intervalMs) {
      if (this.errorCount > 1) {
        console.warn(
          `[${this.prefix}] ${context}:`,
          err,
          `(${this.errorCount} errors in last interval)`,
        );
      } else {
        console.warn(`[${this.prefix}] ${context}:`, err);
      }
      this.lastLogTime = now;
      this.errorCount = 0;
    }
  }

  /** Reset error counters (e.g., on stop) */
  reset(): void {
    this.errorCount = 0;
    this.lastLogTime = 0;
  }
}
