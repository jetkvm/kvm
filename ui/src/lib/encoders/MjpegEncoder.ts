/**
 * MJPEG encoder implementation using WebWorker and Canvas.
 *
 * This encoder captures video frames to a canvas in a WebWorker, then encodes
 * them as JPEG using Canvas.toBlob(). This approach works in all browsers
 * that support OffscreenCanvas and WebWorkers.
 *
 * @internal This is an internal implementation - use CameraEncoder for the public API.
 */

import { CODEC_BYTES } from "../cameraTransport";
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

  // Error rate limiting
  private errorCount = 0;
  private lastErrorLogTime = 0;
  private static readonly ERROR_LOG_INTERVAL_MS = 1000;

  private running = false;

  constructor(
    mediaStream: MediaStream,
    config: EncoderConfig,
    events: InternalEncoderEvents,
    actualFrameRate: number,
  ) {
    this.mediaStream = mediaStream;
    this.config = config;
    this.events = events;
    this.actualFrameRate = actualFrameRate;
    this.minFrameIntervalMs = (1000 / config.frameRate) * 0.9;
  }

  /**
   * Rate-limited error logging to avoid log spam during error storms.
   */
  private logError(context: string, err: unknown): void {
    this.errorCount++;
    const now = performance.now();
    if (now - this.lastErrorLogTime >= MjpegEncoder.ERROR_LOG_INTERVAL_MS) {
      if (this.errorCount > 1) {
        console.warn(
          `[MjpegEncoder] ${context}:`,
          err,
          `(${this.errorCount} errors in last interval)`,
        );
      } else {
        console.warn(`[MjpegEncoder] ${context}:`, err);
      }
      this.lastErrorLogTime = now;
      this.errorCount = 0;
    }
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
          this.events.onError(new Error(msg.message));
          break;
      }
    };

    this.worker.onerror = error => {
      this.events.onError(new Error(error.message));
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
          this.logError("video.play failed", err);
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
      this.worker.postMessage(
        { type: "frame", bitmap, timestamp: timestamp ?? performance.now() * 1000 },
        [bitmap],
      );
      bitmap = null;
    } catch (err) {
      this.logError("createImageBitmap failed", err);
      bitmap?.close();
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
