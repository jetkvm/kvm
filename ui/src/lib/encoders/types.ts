/**
 * Shared types for encoder implementations.
 * @internal
 */

export type VideoCodec = "h264" | "mjpeg";

export interface EncoderConfig {
  width: number;
  height: number;
  frameRate: number;
  bitrate: number; // For H.264
  quality: number; // For MJPEG (0.0 - 1.0)
  keyFrameInterval: number; // For H.264, in seconds
}

export interface EncodedFrame {
  data: ArrayBuffer;
  timestamp: number;
  isKeyFrame: boolean;
  codec: VideoCodec;
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
