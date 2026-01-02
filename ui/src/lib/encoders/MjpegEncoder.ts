/**
 * MJPEG encoder implementation using WebWorker and OffscreenCanvas.
 *
 * This encoder captures video frames to a canvas in a WebWorker, then encodes
 * them as JPEG using OffscreenCanvas.convertToBlob(). This approach works in
 * all browsers that support OffscreenCanvas and WebWorkers.
 *
 * ## WebWorker Message Protocol
 *
 * Main thread → Worker:
 * - `{ type: "start", config: { width, height, quality, codecByte? } }` - Initialize encoder
 * - `{ type: "frame", bitmap: ImageBitmap, timestamp: number }` - Encode frame (bitmap transferred)
 * - `{ type: "setQuality", quality: number }` - Update JPEG quality (0.0-1.0)
 * - `{ type: "stop" }` - Stop encoder and release resources
 *
 * Worker → Main thread:
 * - `{ type: "ready" }` - Encoder initialized successfully
 * - `{ type: "frame", data: ArrayBuffer, timestamp: number }` - Encoded JPEG (transferred)
 * - `{ type: "error", message: string }` - Error occurred (rate-limited)
 * - `{ type: "stopped" }` - Encoder stopped
 *
 * See `../workers/mjpegEncoder.worker.ts` for the worker implementation.
 *
 * @internal This is an internal implementation - use CameraEncoder for the public API.
 */

import { CODEC_BYTES } from "../cameraTransport";
import { RateLimitedLogger } from "./types";
import type { EncodedFrame, EncoderConfig, InternalEncoderEvents } from "./types";

// requestVideoFrameCallback types
interface VideoFrameCallbackMetadata {
  presentationTime: DOMHighResTimeStamp;
  expectedDisplayTime: DOMHighResTimeStamp;
  width: number;
  height: number;
  mediaTime: number;
  presentedFrames: number;
  processingDuration?: number;
}

declare global {
  interface HTMLVideoElement {
    requestVideoFrameCallback(
      callback: (now: DOMHighResTimeStamp, metadata: VideoFrameCallbackMetadata) => void,
    ): number;
    cancelVideoFrameCallback(handle: number): void;
  }
}

/**
 * Check if MJPEG encoding is supported (OffscreenCanvas + Worker)
 */
export function isMjpegSupported(): boolean {
  return typeof OffscreenCanvas !== "undefined" && typeof Worker !== "undefined";
}

/**
 * Check if requestVideoFrameCallback is supported (better than rAF for video)
 */
export function hasRequestVideoFrameCallback(): boolean {
  return (
    typeof HTMLVideoElement !== "undefined" &&
    "requestVideoFrameCallback" in HTMLVideoElement.prototype
  );
}

/**
 * MJPEG encoder using WebWorker for off-main-thread encoding.
 */
export class MjpegEncoder {
  private worker: Worker | null = null;
  private videoElement: HTMLVideoElement | null = null;
  private videoFrameCallbackId: number | null = null;
  private frameInterval: number | null = null;

  private config: EncoderConfig;
  private events: InternalEncoderEvents;
  private mediaStream: MediaStream;

  private lastCaptureTime = 0;
  private minFrameIntervalMs: number;
  private actualFrameRate: number;

  private readonly logger = new RateLimitedLogger("MjpegEncoder");
  private running = false;

  // Consecutive capture error tracking for escalation
  private consecutiveCaptureErrors = 0;
  private static readonly MAX_CONSECUTIVE_ERRORS = 10;

  constructor(
    mediaStream: MediaStream,
    config: EncoderConfig,
    events: InternalEncoderEvents,
    actualFrameRate: number,
  ) {
    this.mediaStream = mediaStream;
    this.config = { ...config }; // Clone to prevent external mutation
    this.events = events;
    this.actualFrameRate = actualFrameRate;
    this.minFrameIntervalMs = (1000 / config.frameRate) * 0.9;
  }

  async start(width: number, height: number): Promise<void> {
    if (!isMjpegSupported()) {
      throw new Error("WebWorker or OffscreenCanvas not supported");
    }

    // Create WebWorker for MJPEG encoding
    this.worker = new Worker(new URL("../../workers/mjpegEncoder.worker.ts", import.meta.url), {
      type: "module",
    });

    // Handle messages from worker
    this.worker.onmessage = event => {
      const msg = event.data;
      switch (msg.type) {
        case "frame":
          this.handleFrame(msg.data, msg.timestamp);
          break;
        case "error":
          this.events.onError(new Error(`MJPEG worker: ${msg.message}`));
          break;
      }
    };

    this.worker.onerror = error => {
      const location = error.filename ? ` (${error.filename}:${error.lineno})` : "";
      this.events.onError(new Error(`MJPEG worker error: ${error.message}${location}`));
    };

    // Initialize worker with config (codecByte enables zero-copy transport)
    this.worker.postMessage({
      type: "start",
      config: {
        width,
        height,
        quality: this.config.quality,
        codecByte: CODEC_BYTES.mjpeg,
      },
    });

    // Create video element to receive camera stream
    this.videoElement = document.createElement("video");
    this.videoElement.srcObject = this.mediaStream;
    this.videoElement.muted = true;
    this.videoElement.playsInline = true;

    await this.videoElement.play();

    this.running = true;
    this.startCapture();
  }

  stop(): void {
    this.running = false;

    if (this.videoFrameCallbackId !== null && this.videoElement && hasRequestVideoFrameCallback()) {
      this.videoElement.cancelVideoFrameCallback(this.videoFrameCallbackId);
      this.videoFrameCallbackId = null;
    }

    if (this.frameInterval !== null) {
      clearInterval(this.frameInterval);
      this.frameInterval = null;
    }

    if (this.worker) {
      this.worker.postMessage({ type: "stop" });
      this.worker.terminate();
      this.worker = null;
    }

    if (this.videoElement) {
      this.videoElement.srcObject = null;
      this.videoElement = null;
    }

    this.logger.reset();
  }

  forceKeyFrame(): void {
    // MJPEG frames are all keyframes - nothing to do
  }

  setFrameRate(fps: number): void {
    this.config.frameRate = fps;
    this.minFrameIntervalMs = (1000 / fps) * 0.9;
  }

  setQuality(quality: number): void {
    this.config.quality = quality;
    if (this.worker) {
      this.worker.postMessage({ type: "setQuality", quality });
    }
  }

  /**
   * Start frame capture using the best available method
   */
  private startCapture(): void {
    if (!this.videoElement || !this.worker) return;

    // Prefer requestVideoFrameCallback for frame-accurate capture synced to video
    if (hasRequestVideoFrameCallback()) {
      this.captureWithVideoFrameCallback();
    } else {
      // Fallback to setInterval for browsers without rVFC
      const frameIntervalMs = 1000 / this.actualFrameRate;
      this.frameInterval = window.setInterval(() => {
        this.captureFrame();
      }, frameIntervalMs);
    }
  }

  private captureWithVideoFrameCallback(): void {
    if (!this.running || !this.videoElement || !hasRequestVideoFrameCallback()) {
      return;
    }

    if (this.videoElement.paused) {
      this.videoElement
        .play()
        .then(() => {
          if (this.running) this.captureWithVideoFrameCallback();
        })
        .catch((err: unknown) => {
          this.logger.logError("video.play failed", err);
          this.events.onError(err instanceof Error ? err : new Error(String(err)));
        });
      return;
    }

    this.videoFrameCallbackId = this.videoElement.requestVideoFrameCallback((now, metadata) => {
      if (now - this.lastCaptureTime >= this.minFrameIntervalMs) {
        this.lastCaptureTime = now;
        this.captureFrame(metadata.presentationTime);
      }
      if (this.running) this.captureWithVideoFrameCallback();
    });
  }

  private async captureFrame(timestamp?: number): Promise<void> {
    if (!this.running || !this.videoElement || !this.worker) return;

    let bitmap: ImageBitmap | null = null;
    try {
      bitmap = await createImageBitmap(this.videoElement);
      this.consecutiveCaptureErrors = 0; // Reset on success
      this.worker.postMessage(
        { type: "frame", bitmap, timestamp: timestamp ?? performance.now() * 1000 },
        [bitmap],
      );
      bitmap = null;
    } catch (err) {
      this.consecutiveCaptureErrors++;
      this.logger.logError("createImageBitmap failed", err);
      bitmap?.close();

      // Escalate to error handler after too many consecutive failures
      if (this.consecutiveCaptureErrors >= MjpegEncoder.MAX_CONSECUTIVE_ERRORS) {
        this.events.onError(
          new Error(`Frame capture failed ${this.consecutiveCaptureErrors} times consecutively`),
        );
      }
    }
  }

  private handleFrame(data: ArrayBuffer, timestamp: number): void {
    const frame: EncodedFrame = {
      data,
      timestamp: Math.floor(timestamp),
      isKeyFrame: true, // All MJPEG frames are keyframes
      codec: "mjpeg",
    };

    this.events.onFrame(frame);
  }
}
