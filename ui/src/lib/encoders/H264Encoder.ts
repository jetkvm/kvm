/**
 * H.264 encoder implementation using WebCodecs API.
 *
 * This encoder uses the browser's hardware-accelerated H.264 encoder via WebCodecs,
 * producing Annex B format output suitable for UVC gadgets. SPS/PPS parameter sets
 * are automatically extracted and prepended to keyframes.
 *
 * @internal This is an internal implementation - use CameraEncoder for the public API.
 */

import { CODEC_BYTES } from "../cameraTransport";
import { RateLimitedLogger } from "./types";
import type { EncodedFrame, EncoderConfig, InternalEncoderEvents } from "./types";

// Type declarations for APIs not yet in TypeScript lib
declare class MediaStreamTrackProcessor<T> {
  constructor(init: { track: MediaStreamTrack });
  readonly readable: ReadableStream<T>;
}

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
    console.debug("[H264Encoder] H.264 support check failed:", e);
    return false;
  }
}

/**
 * H.264 encoder using WebCodecs API.
 * Produces Annex B format with SPS/PPS prepended to keyframes.
 */
export class H264Encoder {
  private encoder: VideoEncoder | null = null;
  private trackProcessor: MediaStreamTrackProcessor<VideoFrame> | null = null;
  private frameReader: ReadableStreamDefaultReader<VideoFrame> | null = null;

  private config: EncoderConfig;
  private events: InternalEncoderEvents;
  private videoTrack: MediaStreamTrack;

  private frameCount = 0;
  private keyFrameCounter = 0;
  private framesPerKeyFrame: number;
  private lastCaptureTime = 0;
  private minFrameIntervalMs: number;

  // Cached SPS/PPS NAL units (with start codes)
  private spsNalu: Uint8Array | null = null;
  private ppsNalu: Uint8Array | null = null;

  private readonly logger = new RateLimitedLogger("H264Encoder");
  private running = false;

  constructor(videoTrack: MediaStreamTrack, config: EncoderConfig, events: InternalEncoderEvents) {
    this.videoTrack = videoTrack;
    this.config = config;
    this.events = events;
    this.framesPerKeyFrame = Math.round(config.frameRate * config.keyFrameInterval);
    this.minFrameIntervalMs = (1000 / config.frameRate) * 0.9;
  }

  async start(width: number, height: number): Promise<void> {
    if (!isVideoEncoderSupported()) {
      throw new Error("VideoEncoder API not supported");
    }

    this.encoder = new VideoEncoder({
      output: (chunk, metadata) => this.handleChunk(chunk, metadata),
      error: error => {
        this.events.onError(error);
      },
    });

    await this.encoder.configure({
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
    this.running = true;
    this.readFrames();
  }

  stop(): void {
    this.running = false;

    if (this.frameReader) {
      this.frameReader.cancel().catch((err: unknown) => {
        console.debug("[H264Encoder] frameReader.cancel failed:", err);
      });
      this.frameReader = null;
    }

    if (this.encoder && this.encoder.state !== "closed") {
      try {
        this.encoder.close();
      } catch (err) {
        console.debug("[H264Encoder] encoder.close failed:", err);
      }
      this.encoder = null;
    }

    this.trackProcessor = null;
    this.frameCount = 0;
    this.keyFrameCounter = 0;
    this.spsNalu = null;
    this.ppsNalu = null;
  }

  forceKeyFrame(): void {
    this.keyFrameCounter = this.framesPerKeyFrame;
  }

  setFrameRate(fps: number): void {
    this.config.frameRate = fps;
    this.minFrameIntervalMs = (1000 / fps) * 0.9;
    this.framesPerKeyFrame = Math.round(fps * this.config.keyFrameInterval);
  }

  private async readFrames(): Promise<void> {
    if (!this.frameReader) return;

    try {
      while (this.running) {
        const { value: frame, done } = await this.frameReader.read();
        if (done) break;

        if (frame) {
          try {
            const now = performance.now();
            if (now - this.lastCaptureTime >= this.minFrameIntervalMs) {
              this.lastCaptureTime = now;

              if (this.encoder && this.encoder.state === "configured") {
                const isKeyFrame = this.keyFrameCounter >= this.framesPerKeyFrame;
                if (isKeyFrame) this.keyFrameCounter = 0;

                this.encoder.encode(frame, { keyFrame: isKeyFrame });
                this.frameCount++;
                this.keyFrameCounter++;
              }
            }
          } catch (err) {
            this.logger.logError("frame encode error", err);
          } finally {
            frame.close();
          }
        }
      }
    } catch (error) {
      if (this.running) {
        this.events.onError(error instanceof Error ? error : new Error(String(error)));
      }
    }
  }

  /**
   * Extract SPS/PPS from WebCodecs AVCC description for Annex B conversion.
   * WebCodecs returns H.264 in AVCC format (length-prefixed NALUs), but UVC
   * gadgets need Annex B format (start code prefixed). SPS/PPS are required
   * in keyframes for decoders to initialize.
   */
  private extractParameterSets(description: ArrayBuffer): void {
    const data = new Uint8Array(description);
    if (data.length < 7) {
      console.warn("[H264Encoder] Decoder config description too short:", data.length, "bytes");
      return;
    }

    let offset = 5; // Skip AVCC header: version, profile, compat, level, lengthSize

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

  private handleChunk(chunk: EncodedVideoChunk, metadata?: EncodedVideoChunkMetadata): void {
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
      framed[0] = CODEC_BYTES.h264;
      framed.set(this.spsNalu, 1);
      framed.set(this.ppsNalu, 1 + this.spsNalu.length);
      framed.set(chunkData, 1 + this.spsNalu.length + this.ppsNalu.length);
    } else {
      // Regular frame or keyframe with inline SPS/PPS: [codec][chunk]
      framed = new Uint8Array(1 + chunkData.length);
      framed[0] = CODEC_BYTES.h264;
      framed.set(chunkData, 1);
    }

    const frame: EncodedFrame = {
      data: framed.buffer,
      timestamp: chunk.timestamp,
      isKeyFrame,
      codec: "h264",
    };

    this.events.onFrame(frame);
  }
}
