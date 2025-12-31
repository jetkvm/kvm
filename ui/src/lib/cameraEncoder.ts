/**
 * Camera Encoder - H.264 (WebCodecs) and MJPEG (WebWorker) encoding for UVC passthrough.
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
  frameRate: 30,
  bitrate: 9_000_000,
  quality: 0.65,
  keyFrameInterval: 1,
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
  } catch (e) {
    console.debug("[CameraEncoder] H.264 support check failed:", e);
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
  private spsNalu: Uint8Array | null = null; // Cached SPS NAL unit (with start code)
  private ppsNalu: Uint8Array | null = null; // Cached PPS NAL unit (with start code)

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

  // Error rate limiting
  private errorCount = 0;
  private lastErrorLogTime = 0;
  private static readonly ERROR_LOG_INTERVAL_MS = 1000;

  constructor(codec: VideoCodec, config: Partial<EncoderConfig> = {}) {
    this.codec = codec;
    this.config = { ...DEFAULT_CONFIG, ...config };

    // Validate and clamp config values to valid ranges
    this.config.width = Math.max(320, Math.min(3840, this.config.width));
    this.config.height = Math.max(240, Math.min(2160, this.config.height));
    this.config.frameRate = Math.max(1, Math.min(120, this.config.frameRate));
    this.config.bitrate = Math.max(100_000, Math.min(50_000_000, this.config.bitrate));
    this.config.quality = Math.max(0.0, Math.min(1.0, this.config.quality));

    this.framesPerKeyFrame = this.config.frameRate * this.config.keyFrameInterval;
    // Calculate minimum frame interval for rate limiting (allow 10% tolerance)
    this.minFrameIntervalMs = (1000 / this.config.frameRate) * 0.9;
  }

  /**
   * Rate-limited error logging to avoid log spam during error storms.
   */
  private logError(context: string, err: unknown): void {
    this.errorCount++;
    const now = performance.now();
    if (now - this.lastErrorLogTime >= CameraEncoder.ERROR_LOG_INTERVAL_MS) {
      if (this.errorCount > 1) {
        console.warn(
          `[CameraEncoder] ${context}:`,
          err,
          `(${this.errorCount} errors in last interval)`,
        );
      } else {
        console.warn(`[CameraEncoder] ${context}:`, err);
      }
      this.lastErrorLogTime = now;
      this.errorCount = 0;
    }
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
   * Update the target frame rate.
   * @returns true if the value was accepted, false if invalid or unchanged
   */
  setFrameRate(fps: number): boolean {
    if (fps < 1 || fps > 120) {
      console.warn(`[CameraEncoder] setFrameRate: invalid fps ${fps} (must be 1-120)`);
      return false;
    }
    if (this.config.frameRate === fps) return true; // Already set

    this.config.frameRate = fps;
    this.minFrameIntervalMs = (1000 / fps) * 0.9;
    this.framesPerKeyFrame = Math.round(fps * this.config.keyFrameInterval);
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
    if (this.config.bitrate === bitrate) return true; // Already set

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
    if (this.config.quality === quality) return true; // Already set

    this.config.quality = quality;

    // Update worker immediately if MJPEG is running
    if (this.codec === "mjpeg" && this.mjpegWorker) {
      this.mjpegWorker.postMessage({ type: "setQuality", quality });
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
    if (this.config.width === width && this.config.height === height) return true; // Already set

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
   * Returns { width, height } of the actual camera resolution.
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
    this.framesPerKeyFrame = Math.round(this.actualFrameRate * this.config.keyFrameInterval);

    return { width, height };
  }

  /**
   * Start the appropriate encoder based on current codec.
   */
  private async startEncoder(width: number, height: number): Promise<void> {
    this.lastStatsTime = performance.now();
    this.statsFrameCount = 0;
    this.setState("running");

    if (this.codec === "h264") {
      await this.startH264Encoder(width, height);
    } else {
      await this.startMjpegEncoder(width, height);
    }
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
    this.keyFrameCounter = this.framesPerKeyFrame;
  }

  private cleanup(): void {
    if (this.frameReader) {
      this.frameReader.cancel().catch((err: unknown) => {
        console.debug("[CameraEncoder] frameReader.cancel failed:", err);
      });
      this.frameReader = null;
    }

    if (this.h264Encoder && this.h264Encoder.state !== "closed") {
      try {
        this.h264Encoder.close();
      } catch (err) {
        console.debug("[CameraEncoder] h264Encoder.close failed:", err);
      }
      this.h264Encoder = null;
    }

    this.trackProcessor = null;

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

    if (this.mediaStream) {
      this.mediaStream.getTracks().forEach(track => track.stop());
      this.mediaStream = null;
    }

    this.videoTrack = null;
    this.frameCount = 0;
    this.keyFrameCounter = 0;
    this.spsNalu = null;
    this.ppsNalu = null;
  }

  private async startH264Encoder(width: number, height: number): Promise<void> {
    if (!isVideoEncoderSupported()) {
      throw new Error("VideoEncoder API not supported");
    }

    this.h264Encoder = new VideoEncoder({
      output: (chunk, metadata) => this.handleH264Chunk(chunk, metadata),
      error: error => {
        this.events.onError?.(error);
        this.setState("error");
      },
    });

    await this.h264Encoder.configure({
      codec: "avc1.640032",
      width,
      height,
      bitrate: this.config.bitrate,
      framerate: this.config.frameRate,
      latencyMode: "realtime",
      avc: { format: "annexb" },
    });

    // @ts-expect-error - MediaStreamTrackProcessor not in TS types yet
    this.trackProcessor = new MediaStreamTrackProcessor({ track: this.videoTrack });
    this.frameReader = this.trackProcessor.readable.getReader();
    this.readH264Frames();
  }

  private async readH264Frames(): Promise<void> {
    if (!this.frameReader || (this._state !== "running" && this._state !== "paused")) {
      return;
    }

    try {
      while (this._state === "running" || this._state === "paused") {
        if (this._state === "paused") {
          await new Promise(resolve => setTimeout(resolve, 100));
          continue;
        }

        const { value: frame, done } = await this.frameReader.read();
        if (done) break;

        if (frame) {
          try {
            const now = performance.now();
            if (now - this.lastCaptureTime >= this.minFrameIntervalMs) {
              this.lastCaptureTime = now;

              if (this.h264Encoder && this.h264Encoder.state === "configured") {
                const isKeyFrame = this.keyFrameCounter >= this.framesPerKeyFrame;
                if (isKeyFrame) this.keyFrameCounter = 0;

                this.h264Encoder.encode(frame, { keyFrame: isKeyFrame });
                this.frameCount++;
                this.keyFrameCounter++;
              }
            }
          } catch (err) {
            this.logError("frame encode error", err);
          } finally {
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

  private extractParameterSets(description: ArrayBuffer): void {
    const data = new Uint8Array(description);
    if (data.length < 7) return;

    let offset = 5; // Skip version, profile, compat, level, lengthSize

    // Extract SPS
    const numSps = data[offset] & 0x1f;
    offset++;
    if (numSps > 0 && offset + 2 <= data.length) {
      const spsLen = (data[offset] << 8) | data[offset + 1];
      offset += 2;
      if (offset + spsLen <= data.length) {
        this.spsNalu = new Uint8Array(4 + spsLen);
        this.spsNalu.set([0x00, 0x00, 0x00, 0x01], 0);
        this.spsNalu.set(data.subarray(offset, offset + spsLen), 4);
        offset += spsLen;
      }
    }

    // Skip any additional SPS entries
    for (let i = 1; i < numSps && offset + 2 <= data.length; i++) {
      const len = (data[offset] << 8) | data[offset + 1];
      offset += 2 + len;
    }

    // Extract PPS
    if (offset < data.length) {
      const numPps = data[offset] & 0x1f;
      offset++;
      if (numPps > 0 && offset + 2 <= data.length) {
        const ppsLen = (data[offset] << 8) | data[offset + 1];
        offset += 2;
        if (offset + ppsLen <= data.length) {
          this.ppsNalu = new Uint8Array(4 + ppsLen);
          this.ppsNalu.set([0x00, 0x00, 0x00, 0x01], 0);
          this.ppsNalu.set(data.subarray(offset, offset + ppsLen), 4);
        }
      }
    }
  }

  private hasSpsInStream(data: Uint8Array): boolean {
    if (data.length < 5) return false;
    // Check 4-byte start code: 00 00 00 01 [NAL]
    if (data[0] === 0 && data[1] === 0 && data[2] === 0 && data[3] === 1) {
      return (data[4] & 0x1f) === 7;
    }
    // Check 3-byte start code: 00 00 01 [NAL]
    if (data[0] === 0 && data[1] === 0 && data[2] === 1) {
      return (data[3] & 0x1f) === 7;
    }
    return false;
  }

  private static readonly CODEC_H264 = 0x01;

  private handleH264Chunk(chunk: EncodedVideoChunk, metadata?: EncodedVideoChunkMetadata): void {
    if (metadata?.decoderConfig?.description) {
      this.extractParameterSets(metadata.decoderConfig.description as ArrayBuffer);
    }

    const isKeyFrame = chunk.type === "key";
    const chunkData = new Uint8Array(chunk.byteLength);
    chunk.copyTo(chunkData);

    let framed: Uint8Array;

    // Build frame with codec byte prefix for zero-copy transport
    if (isKeyFrame && !this.hasSpsInStream(chunkData) && this.spsNalu && this.ppsNalu) {
      // Keyframe needs SPS/PPS prepended: [codec][SPS][PPS][chunk]
      const totalLen = 1 + this.spsNalu.length + this.ppsNalu.length + chunkData.length;
      framed = new Uint8Array(totalLen);
      framed[0] = CameraEncoder.CODEC_H264;
      framed.set(this.spsNalu, 1);
      framed.set(this.ppsNalu, 1 + this.spsNalu.length);
      framed.set(chunkData, 1 + this.spsNalu.length + this.ppsNalu.length);
    } else {
      // Regular frame or keyframe with inline SPS/PPS: [codec][chunk]
      framed = new Uint8Array(1 + chunkData.length);
      framed[0] = CameraEncoder.CODEC_H264;
      framed.set(chunkData, 1);
    }

    this.updateStats(framed.byteLength);

    this.events.onFrame?.({
      data: framed.buffer,
      timestamp: chunk.timestamp,
      isKeyFrame,
      codec: "h264",
    });
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

    // Initialize worker with config (codecByte enables zero-copy transport)
    this.mjpegWorker.postMessage({
      type: "start",
      config: {
        width,
        height,
        quality: this.config.quality,
        codecByte: 0x02, // MJPEG codec byte for transport
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

  private captureWithVideoFrameCallback(): void {
    if (this._state !== "running" || !this.videoElement || !hasRequestVideoFrameCallback()) {
      return;
    }

    if (this.videoElement.paused) {
      this.videoElement
        .play()
        .then(() => {
          if (this._state === "running") this.captureWithVideoFrameCallback();
        })
        .catch((err: unknown) => {
          this.logError("video.play failed", err);
          this.setState("error");
          this.events.onError?.(err instanceof Error ? err : new Error(String(err)));
        });
      return;
    }

    this.videoFrameCallbackId = this.videoElement.requestVideoFrameCallback((now, metadata) => {
      if (now - this.lastCaptureTime >= this.minFrameIntervalMs) {
        this.lastCaptureTime = now;
        this.captureFrame(metadata.presentationTime);
      }
      if (this._state === "running") this.captureWithVideoFrameCallback();
    });
  }

  private async captureFrame(timestamp?: number): Promise<void> {
    if (this._state !== "running" || !this.videoElement || !this.mjpegWorker) return;

    let bitmap: ImageBitmap | null = null;
    try {
      bitmap = await createImageBitmap(this.videoElement);
      this.mjpegWorker.postMessage(
        { type: "frame", bitmap, timestamp: timestamp ?? performance.now() * 1000 },
        [bitmap],
      );
      bitmap = null;
    } catch (err) {
      this.logError("createImageBitmap failed", err);
      bitmap?.close();
    }
  }

  private handleMjpegFrame(data: ArrayBuffer, timestamp: number): void {
    this.updateStats(data.byteLength);

    this.events.onFrame?.({
      data,
      timestamp: Math.floor(timestamp),
      isKeyFrame: true,
      codec: "mjpeg",
    });
  }
}

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
