/**
 * Camera Encoder - Unified H.264 and MJPEG encoding for 60fps
 *
 * Supports two encoding modes based on UVC host negotiation:
 * - H.264: Uses WebCodecs VideoEncoder (hardware accelerated)
 * - MJPEG: Uses WebWorker + OffscreenCanvas (parallel encoding)
 *
 * Key optimizations for 60fps:
 * - MJPEG encoding runs in a dedicated WebWorker (doesn't block main thread)
 * - Uses requestVideoFrameCallback for frame-accurate capture timing
 * - ImageBitmap transfer to worker (zero-copy GPU texture sharing)
 * - Double-buffering to pipeline capture and encode
 */

// Type declarations for APIs not yet in TypeScript lib
declare class MediaStreamTrackProcessor<T> {
  constructor(init: { track: MediaStreamTrack });
  readonly readable: ReadableStream<T>;
}

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

export type EncoderState = "idle" | "initializing" | "running" | "paused" | "error" | "stopped";

export interface CameraEncoderEvents {
  onFrame: (frame: EncodedFrame) => void;
  onStateChange: (state: EncoderState) => void;
  onError: (error: Error) => void;
  onStats?: (stats: { fps: number; avgEncodeMs: number; frameSize: number }) => void;
}

const DEFAULT_CONFIG: EncoderConfig = {
  width: 1920,
  height: 1080,
  frameRate: 30, // 30fps max for camera passthrough
  bitrate: 9_000_000, // 9 Mbps to match MJPEG 35% bandwidth (~75KB × 15fps)
  quality: 0.65, // 65% quality for MJPEG (balance size/quality)
  keyFrameInterval: 1, // keyframe every 1s (balance latency/efficiency)
};

/**
 * Check if WebCodecs VideoEncoder is supported
 */
export function isVideoEncoderSupported(): boolean {
  return typeof VideoEncoder !== "undefined";
}

/**
 * Check if H.264 encoding is supported via WebCodecs
 */
export async function isH264Supported(): Promise<boolean> {
  if (!isVideoEncoderSupported()) {
    return false;
  }

  try {
    const support = await VideoEncoder.isConfigSupported({
      codec: "avc1.640032", // H.264 High Profile Level 5.0 (supports 1080p60)
      width: 1920,
      height: 1080,
      bitrate: 8_000_000,
      framerate: 60,
    });
    return support.supported === true;
  } catch {
    return false;
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
 * Camera Encoder - Supports both H.264 and MJPEG at 60fps
 */
export class CameraEncoder {
  private codec: VideoCodec;
  private config: EncoderConfig;
  private events: Partial<CameraEncoderEvents> = {};
  private _state: EncoderState = "idle";

  // Camera capture
  private mediaStream: MediaStream | null = null;
  private videoTrack: MediaStreamTrack | null = null;

  // H.264 encoding (WebCodecs)
  private h264Encoder: VideoEncoder | null = null;
  private trackProcessor: MediaStreamTrackProcessor<VideoFrame> | null = null;
  private frameReader: ReadableStreamDefaultReader<VideoFrame> | null = null;
  private frameCount = 0;
  private keyFrameCounter = 0;
  private framesPerKeyFrame: number;

  // MJPEG encoding (WebWorker)
  private mjpegWorker: Worker | null = null;
  private videoElement: HTMLVideoElement | null = null;
  private videoFrameCallbackId: number | null = null;
  private mjpegFrameInterval: number | null = null;
  private lastCaptureTime = 0; // For frame rate throttling
  private minFrameIntervalMs = 0; // Minimum time between frames

  // Stats
  private lastStatsTime = 0;
  private statsFrameCount = 0;
  private lastFrameSize = 0;
  private actualFrameRate = 0;

  constructor(codec: VideoCodec, config: Partial<EncoderConfig> = {}) {
    this.codec = codec;
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.framesPerKeyFrame = this.config.frameRate * this.config.keyFrameInterval;
    // Calculate minimum frame interval for rate limiting (allow 10% tolerance)
    this.minFrameIntervalMs = (1000 / this.config.frameRate) * 0.9;
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
    // (H.264 and MJPEG have completely different resources: WebCodecs vs WebWorker)
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
   * Update the target frame rate (takes effect immediately for MJPEG, on next start for H.264)
   */
  setFrameRate(fps: number): void {
    if (fps < 1 || fps > 120) {
      console.warn(`[CameraEncoder] Invalid frame rate ${fps}, ignoring`);
      return;
    }

    const oldFps = this.config.frameRate;
    if (oldFps === fps) return;

    console.log(`[CameraEncoder] Updating frame rate: ${oldFps}fps -> ${fps}fps`);
    this.config.frameRate = fps;
    // Recalculate minimum frame interval for rate limiting
    this.minFrameIntervalMs = (1000 / fps) * 0.9;
    // Update keyframe interval if H.264
    this.framesPerKeyFrame = Math.round(fps * this.config.keyFrameInterval);
  }

  /**
   * Update the H.264 bitrate (takes effect on next keyframe or encoder restart)
   * @param bitrate Bitrate in bits per second (e.g., 3_000_000 for 3 Mbps)
   */
  setBitrate(bitrate: number): void {
    if (bitrate < 100_000 || bitrate > 50_000_000) {
      console.warn(`[CameraEncoder] Invalid bitrate ${bitrate}, ignoring`);
      return;
    }

    if (this.config.bitrate === bitrate) return;

    console.log(
      `[CameraEncoder] Updating bitrate: ${this.config.bitrate / 1_000_000}Mbps -> ${bitrate / 1_000_000}Mbps`,
    );
    this.config.bitrate = bitrate;
    // Note: H.264 encoder will use new bitrate on next restart/reconfigure
  }

  /**
   * Update the MJPEG quality (takes effect immediately)
   * @param quality Quality value 0.0-1.0 (0.35 = 35% quality)
   */
  setQuality(quality: number): void {
    if (quality < 0.0 || quality > 1.0) {
      console.warn(`[CameraEncoder] Invalid quality ${quality}, ignoring`);
      return;
    }

    if (this.config.quality === quality) return;

    console.log(
      `[CameraEncoder] Updating MJPEG quality: ${Math.round(this.config.quality * 100)}% -> ${Math.round(quality * 100)}%`,
    );
    this.config.quality = quality;

    // Update worker immediately if MJPEG is running
    if (this.codec === "mjpeg" && this.mjpegWorker) {
      this.mjpegWorker.postMessage({
        type: "setQuality",
        quality,
      });
    }
  }

  /**
   * Update the capture resolution (requires encoder restart)
   * @param width New width
   * @param height New height
   */
  async setResolution(width: number, height: number): Promise<void> {
    if (width < 320 || width > 3840 || height < 240 || height > 2160) {
      console.warn(`[CameraEncoder] Invalid resolution ${width}x${height}, ignoring`);
      return;
    }

    if (this.config.width === width && this.config.height === height) return;

    console.log(
      `[CameraEncoder] Updating resolution: ${this.config.width}x${this.config.height} -> ${width}x${height}`,
    );
    this.config.width = width;
    this.config.height = height;

    // Resolution change requires encoder restart to re-request camera with new constraints
    // Must handle both running AND paused states (camera constraints need to change)
    const wasActive = this._state === "running" || this._state === "paused";
    if (wasActive) {
      console.log("[CameraEncoder] Restarting encoder for resolution change");
      this.stop();
      await this.start();
    }
  }

  /**
   * Start capturing and encoding camera video
   */
  async start(): Promise<void> {
    if (this._state === "running") {
      console.log("[CameraEncoder] Already running");
      return;
    }

    this.setState("initializing");

    const targetFrameRate = this.config.frameRate;
    console.log(`[CameraEncoder] Starting with codec: ${this.codec} at ${targetFrameRate}fps`);

    try {
      // Request camera access with configured frame rate
      console.log("[CameraEncoder] Requesting camera access...");
      this.mediaStream = await navigator.mediaDevices.getUserMedia({
        video: {
          width: { ideal: this.config.width },
          height: { ideal: this.config.height },
          frameRate: { ideal: targetFrameRate, min: 15 },
        },
      });
      console.log("[CameraEncoder] Got camera access");

      const videoTracks = this.mediaStream.getVideoTracks();
      if (videoTracks.length === 0) {
        throw new Error("No video track available");
      }

      this.videoTrack = videoTracks[0];

      // Get actual track settings - use webcam's actual framerate for best performance
      const settings = this.videoTrack.getSettings();
      const actualWidth = settings.width || this.config.width;
      const actualHeight = settings.height || this.config.height;
      this.actualFrameRate = settings.frameRate || this.config.frameRate;

      // Update keyframe interval based on actual framerate
      this.framesPerKeyFrame = Math.round(this.actualFrameRate * this.config.keyFrameInterval);

      // Set state to running BEFORE starting encoder so capture callbacks work
      this.lastStatsTime = performance.now();
      this.statsFrameCount = 0;
      this.setState("running");

      if (this.codec === "h264") {
        console.log("[CameraEncoder] Starting H.264 encoder");
        await this.startH264Encoder(actualWidth, actualHeight);
      } else {
        console.log("[CameraEncoder] Starting MJPEG encoder");
        await this.startMjpegEncoder(actualWidth, actualHeight);
      }

      console.log("[CameraEncoder] Encoder running");
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

    if (this.videoFrameCallbackId !== null && this.videoElement && hasRequestVideoFrameCallback()) {
      this.videoElement.cancelVideoFrameCallback(this.videoFrameCallbackId);
      this.videoFrameCallbackId = null;
    }
    if (this.mjpegFrameInterval !== null) {
      clearInterval(this.mjpegFrameInterval);
      this.mjpegFrameInterval = null;
    }

    if (this.mediaStream) {
      this.mediaStream.getTracks().forEach(track => track.stop());
      this.mediaStream = null;
      this.videoTrack = null;
    }

    if (this.videoElement) {
      this.videoElement.srcObject = null;
    }

    this.trackProcessor = null;
    this.frameReader = null;
  }

  async resume(): Promise<void> {
    if (this._state !== "paused") {
      return;
    }

    try {
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
      const actualWidth = settings.width || this.config.width;
      const actualHeight = settings.height || this.config.height;
      this.actualFrameRate = settings.frameRate || this.config.frameRate;
      this.framesPerKeyFrame = Math.round(this.actualFrameRate * this.config.keyFrameInterval);

      this.forceKeyFrame();

      this.lastStatsTime = performance.now();
      this.statsFrameCount = 0;
      this.setState("running");

      if (this.codec === "h264") {
        await this.startH264Encoder(actualWidth, actualHeight);
      } else {
        await this.startMjpegEncoder(actualWidth, actualHeight);
      }
    } catch (error) {
      this.setState("error");
      this.events.onError?.(error instanceof Error ? error : new Error(String(error)));
      throw error;
    }
  }

  /**
   * Force the next H.264 frame to be a keyframe (IDR with SPS/PPS).
   * Call this when UVC streaming starts so the decoder can immediately start.
   */
  forceKeyFrame(): void {
    // Reset keyframe counter to force immediate keyframe on next encode
    this.keyFrameCounter = this.framesPerKeyFrame;
    console.log("[CameraEncoder] Forcing keyframe on next encode");
  }

  private cleanup(): void {
    // Stop H.264 encoder
    if (this.frameReader) {
      this.frameReader.cancel().catch(() => {
        // Ignore cancel errors during cleanup
      });
      this.frameReader = null;
    }

    if (this.h264Encoder && this.h264Encoder.state !== "closed") {
      try {
        this.h264Encoder.close();
      } catch {
        // Ignore close errors
      }
      this.h264Encoder = null;
    }

    this.trackProcessor = null;

    // Stop MJPEG worker
    if (this.videoFrameCallbackId !== null && this.videoElement && hasRequestVideoFrameCallback()) {
      this.videoElement.cancelVideoFrameCallback(this.videoFrameCallbackId);
      this.videoFrameCallbackId = null;
    }

    if (this.mjpegFrameInterval !== null) {
      clearInterval(this.mjpegFrameInterval);
      this.mjpegFrameInterval = null;
    }

    if (this.mjpegWorker) {
      this.mjpegWorker.postMessage({ type: "stop" });
      this.mjpegWorker.terminate();
      this.mjpegWorker = null;
    }

    if (this.videoElement) {
      this.videoElement.srcObject = null;
      this.videoElement = null;
    }

    // Stop media stream
    if (this.mediaStream) {
      this.mediaStream.getTracks().forEach(track => track.stop());
      this.mediaStream = null;
    }

    this.videoTrack = null;
    this.frameCount = 0;
    this.keyFrameCounter = 0;
  }

  // ==================== H.264 Encoding (WebCodecs) ====================

  private async startH264Encoder(width: number, height: number): Promise<void> {
    if (!isVideoEncoderSupported()) {
      throw new Error("VideoEncoder API not supported");
    }

    // Create H.264 encoder
    this.h264Encoder = new VideoEncoder({
      output: (chunk, metadata) => this.handleH264Chunk(chunk, metadata),
      error: error => {
        this.events.onError?.(error);
        this.setState("error");
      },
    });

    // Configure encoder for H.264 Annex B format (required for UVC)
    // Use High Profile Level 5.0 for 1080p60 support
    // Level encoding: 0x32 = 50 (Level 5.0), supports up to 4096x2304@30fps or 1920x1080@60fps
    await this.h264Encoder.configure({
      codec: "avc1.640032", // High Profile, Level 5.0
      width,
      height,
      bitrate: this.config.bitrate,
      framerate: this.config.frameRate,
      latencyMode: "realtime",
      avc: {
        format: "annexb", // Annex B format with start codes
      },
    });

    // Create track processor to get VideoFrames
    // @ts-expect-error - MediaStreamTrackProcessor not in TS types yet
    this.trackProcessor = new MediaStreamTrackProcessor({ track: this.videoTrack });
    this.frameReader = this.trackProcessor.readable.getReader();

    // Start reading frames
    this.readH264Frames();
  }

  private async readH264Frames(): Promise<void> {
    if (!this.frameReader || (this._state !== "running" && this._state !== "paused")) {
      return;
    }

    try {
      while (this._state === "running" || this._state === "paused") {
        // If paused, wait briefly and check again
        if (this._state === "paused") {
          await new Promise(resolve => setTimeout(resolve, 100));
          continue;
        }

        const { value: frame, done } = await this.frameReader.read();

        if (done) {
          break;
        }

        if (frame) {
          try {
            // Throttle to configured frame rate (same as MJPEG path)
            const now = performance.now();
            const timeSinceLastCapture = now - this.lastCaptureTime;

            if (timeSinceLastCapture >= this.minFrameIntervalMs) {
              this.lastCaptureTime = now;

              if (this.h264Encoder && this.h264Encoder.state === "configured") {
                const isKeyFrame = this.keyFrameCounter >= this.framesPerKeyFrame;
                if (isKeyFrame) {
                  this.keyFrameCounter = 0;
                }

                this.h264Encoder.encode(frame, { keyFrame: isKeyFrame });
                this.frameCount++;
                this.keyFrameCounter++;
              }
            }
            // else: skip this frame (frame rate throttling)
          } catch {
            // Ignore transient encode errors
          } finally {
            // Always close the frame to prevent memory leaks
            frame.close();
          }
        }
      }
    } catch (error) {
      if (this._state === "running") {
        this.events.onError?.(error instanceof Error ? error : new Error(String(error)));
      }
    }
  }

  private handleH264Chunk(chunk: EncodedVideoChunk, _metadata?: EncodedVideoChunkMetadata): void {
    // Skip frames that are too small - likely incomplete from encoder warm-up
    // A valid 1080p H.264 IDR frame should be at least 5KB
    // P-frames can be very small (100-200 bytes) for static scenes - don't filter them
    const minKeyFrameSize = 5000;

    if (chunk.type === "key" && chunk.byteLength < minKeyFrameSize) {
      console.log(
        `[CameraEncoder] Skipping small key frame: ${chunk.byteLength} bytes (min: ${minKeyFrameSize})`,
      );
      return;
    }

    const data = new ArrayBuffer(chunk.byteLength);
    chunk.copyTo(data);

    const encodedFrame: EncodedFrame = {
      data,
      timestamp: chunk.timestamp,
      isKeyFrame: chunk.type === "key",
      codec: "h264",
    };

    this.events.onFrame?.(encodedFrame);
  }

  // ==================== MJPEG Encoding (WebWorker) ====================

  private async startMjpegEncoder(width: number, height: number): Promise<void> {
    if (!isMjpegSupported()) {
      throw new Error("WebWorker or OffscreenCanvas not supported");
    }

    // Create WebWorker for MJPEG encoding
    this.mjpegWorker = new Worker(new URL("../workers/mjpegEncoder.worker.ts", import.meta.url), {
      type: "module",
    });

    // Handle messages from worker
    this.mjpegWorker.onmessage = event => {
      const msg = event.data;
      switch (msg.type) {
        case "frame":
          this.handleMjpegFrame(msg.data, msg.timestamp);
          break;
        case "error":
          this.events.onError?.(new Error(msg.message));
          break;
      }
    };

    this.mjpegWorker.onerror = error => {
      this.events.onError?.(new Error(error.message));
    };

    // Initialize worker with config
    this.mjpegWorker.postMessage({
      type: "start",
      config: {
        width,
        height,
        quality: this.config.quality,
      },
    });

    // Create video element to receive camera stream
    this.videoElement = document.createElement("video");
    this.videoElement.srcObject = this.mediaStream;
    this.videoElement.muted = true;
    this.videoElement.playsInline = true;

    await this.videoElement.play();

    // Start frame capture
    this.startMjpegCapture();
  }

  /**
   * Start MJPEG frame capture using the best available method
   */
  private startMjpegCapture(): void {
    if (!this.videoElement || !this.mjpegWorker) return;

    // Prefer requestVideoFrameCallback for frame-accurate capture synced to video
    if (hasRequestVideoFrameCallback()) {
      this.captureWithVideoFrameCallback();
    } else {
      // Fallback to setInterval for browsers without rVFC
      const frameInterval = 1000 / this.actualFrameRate;
      this.mjpegFrameInterval = window.setInterval(() => {
        this.captureFrame();
      }, frameInterval);
    }
  }

  /**
   * Frame-accurate capture using requestVideoFrameCallback
   * Throttled to configured frame rate to avoid overwhelming the WebSocket
   */
  private captureWithVideoFrameCallback(): void {
    if (this._state !== "running" || !this.videoElement || !hasRequestVideoFrameCallback()) {
      return;
    }

    // Ensure video is playing - requestVideoFrameCallback won't fire for paused video
    if (this.videoElement.paused) {
      console.log("[CameraEncoder] Video paused in capture callback, restarting");
      this.videoElement
        .play()
        .then(() => {
          // Retry after play starts
          if (this._state === "running") {
            this.captureWithVideoFrameCallback();
          }
        })
        .catch(err => {
          console.warn("[CameraEncoder] Failed to restart video:", err);
        });
      return;
    }

    this.videoFrameCallbackId = this.videoElement.requestVideoFrameCallback((now, metadata) => {
      // Throttle to configured frame rate
      const timeSinceLastCapture = now - this.lastCaptureTime;
      if (timeSinceLastCapture >= this.minFrameIntervalMs) {
        this.lastCaptureTime = now;
        // Capture this frame
        this.captureFrame(metadata.presentationTime);
      }

      // Schedule next frame callback
      if (this._state === "running") {
        this.captureWithVideoFrameCallback();
      }
    });
  }

  /**
   * Capture a single frame and send to worker for encoding
   */
  private async captureFrame(timestamp?: number): Promise<void> {
    if (this._state !== "running" || !this.videoElement || !this.mjpegWorker) {
      return;
    }

    // Log first few captures
    if (this.frameCount < 3) {
      console.log("[CameraEncoder] Capturing frame", this.frameCount);
    }

    let bitmap: ImageBitmap | null = null;
    try {
      // Create ImageBitmap from video frame (fast GPU operation)
      // This is transferable and avoids copying pixel data
      bitmap = await createImageBitmap(this.videoElement);

      // Send to worker for encoding (transfer ownership for zero-copy)
      this.mjpegWorker.postMessage(
        {
          type: "frame",
          bitmap,
          timestamp: timestamp ?? performance.now() * 1000, // microseconds
        },
        [bitmap], // Transfer ownership - bitmap is now neutered on this side
      );
      bitmap = null; // Ownership transferred, don't close on this side
    } catch {
      // Close bitmap if transfer failed (prevents memory leak)
      if (bitmap) {
        bitmap.close();
      }
    }
  }

  /**
   * Handle encoded MJPEG frame from worker
   */
  private handleMjpegFrame(data: ArrayBuffer, timestamp: number): void {
    this.frameCount++;
    this.statsFrameCount++;
    this.lastFrameSize = data.byteLength;

    // Log first few frames
    if (this.frameCount <= 3) {
      console.log(
        "[CameraEncoder] MJPEG frame from worker:",
        this.frameCount,
        "size:",
        data.byteLength,
      );
    }

    // Report stats every second via callback
    const now = performance.now();
    if (now - this.lastStatsTime >= 1000) {
      const fps = (this.statsFrameCount / (now - this.lastStatsTime)) * 1000;
      this.events.onStats?.({ fps, avgEncodeMs: 0, frameSize: this.lastFrameSize });
      this.lastStatsTime = now;
      this.statsFrameCount = 0;
    }

    const encodedFrame: EncodedFrame = {
      data,
      timestamp: Math.floor(timestamp),
      isKeyFrame: true,
      codec: "mjpeg",
    };

    this.events.onFrame?.(encodedFrame);
  }
}

/**
 * Create encoder with the best supported codec
 */
export async function createEncoder(
  preferredCodec: VideoCodec = "h264",
  config?: Partial<EncoderConfig>,
): Promise<CameraEncoder> {
  // Check if preferred codec is supported
  if (preferredCodec === "h264") {
    const h264Ok = await isH264Supported();
    if (h264Ok) {
      return new CameraEncoder("h264", config);
    }
    // H.264 not supported, fall back to MJPEG
  }

  if (isMjpegSupported()) {
    return new CameraEncoder("mjpeg", config);
  }

  throw new Error("No supported video codec available");
}
