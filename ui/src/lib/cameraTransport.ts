/**
 * WebSocket transport for sending camera frames to JetKVM.
 *
 * Protocol:
 * - Server sends JSON messages: format requests, acks, errors
 * - Client sends binary frames: [codec:u8][frame_data...]
 */

import type { VideoCodec } from "./cameraEncoder";

export type TransportState = "disconnected" | "connecting" | "connected" | "error";

export interface CameraTransportStats {
  framesSent: number;
  bytesSent: number;
  framesDropped: number;
  avgLatencyMs: number;
}

export interface FormatRequest {
  codec: VideoCodec;
  width: number;
  height: number;
  frameRate: number; // Negotiated frame rate from UVC host (e.g., 30 or 60)
  frameRateCap?: number; // User's configured frame rate cap (browser uses min of both)
  h264Bitrate?: number; // H.264 bitrate in bps (from config)
  mjpegQuality?: number; // MJPEG quality 0.0-1.0 (from config)
}

export interface CameraTransportEvents {
  onStateChange: (state: TransportState) => void;
  onError: (error: Error) => void;
  onStats: (stats: CameraTransportStats) => void;
  /** Called when JetKVM requests a specific video format */
  onFormatRequest: (format: FormatRequest) => void;
  /** Called when UVC streaming stops (host disconnected) */
  onStreamingStopped: () => void;
}

export interface CameraTransport {
  /** Current connection state */
  readonly state: TransportState;

  /** Transport statistics */
  readonly stats: CameraTransportStats;

  /** Connect to the server */
  connect(): Promise<void>;

  /** Send a video frame with codec type */
  sendFrame(frame: ArrayBuffer, timestamp: number, codec: VideoCodec): void;

  /** Close the connection */
  close(): void;

  /** Set event handlers */
  setEventHandlers(events: Partial<CameraTransportEvents>): void;
}

export class WebSocketCameraTransport implements CameraTransport {
  private ws: WebSocket | null = null;
  private _state: TransportState = "disconnected";
  private _stats: CameraTransportStats = {
    framesSent: 0,
    bytesSent: 0,
    framesDropped: 0,
    avgLatencyMs: 0,
  };
  private events: Partial<CameraTransportEvents> = {};
  private url: string;
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 3;

  // Reusable send buffer to minimize GC pressure (resized as needed)
  private sendBuffer: Uint8Array = new Uint8Array(2 * 1024 * 1024); // 2MB initial (handles large MJPEG frames)

  constructor(url: string) {
    this.url = url;
  }

  get state(): TransportState {
    return this._state;
  }

  get stats(): CameraTransportStats {
    return { ...this._stats };
  }

  setEventHandlers(events: Partial<CameraTransportEvents>): void {
    this.events = { ...this.events, ...events };
  }

  private setState(state: TransportState): void {
    if (this._state !== state) {
      this._state = state;
      this.events.onStateChange?.(state);
    }
  }

  async connect(): Promise<void> {
    if (this._state === "connected" || this._state === "connecting") {
      return;
    }

    this.setState("connecting");

    return new Promise((resolve, reject) => {
      try {
        this.ws = new WebSocket(this.url);
        this.ws.binaryType = "arraybuffer";

        this.ws.onopen = () => {
          this.reconnectAttempts = 0;
          this.setState("connected");
          resolve();
        };

        this.ws.onclose = event => {
          this.setState("disconnected");

          // Attempt reconnect if not a clean close
          if (event.code !== 1000 && this.reconnectAttempts < this.maxReconnectAttempts) {
            this.reconnectAttempts++;
            setTimeout(() => this.connect(), 1000 * this.reconnectAttempts);
          }
        };

        this.ws.onerror = event => {
          const error = new Error("WebSocket error");
          console.error("[CameraTransport] WebSocket error:", event);
          this.events.onError?.(error);

          if (this._state === "connecting") {
            this.setState("error");
            reject(error);
          }
        };

        this.ws.onmessage = event => {
          // Handle server messages (JSON control messages)
          if (typeof event.data === "string") {
            try {
              const msg = JSON.parse(event.data);

              switch (msg.type) {
                case "format":
                  // Format negotiation from JetKVM
                  // UVC host has requested a specific format, or "stop" when disconnected
                  if (msg.codec === "stop") {
                    console.log("[CameraTransport] UVC streaming stopped");
                    this.events.onStreamingStopped?.();
                  } else if (msg.codec && msg.width && msg.height) {
                    // Log format with frame rate and encoder settings for debugging
                    console.log(
                      `[CameraTransport] Format request: ${msg.codec} ${msg.width}x${msg.height}@${msg.frameRate || 30}fps (cap: ${msg.frameRateCap || "none"}), h264Bitrate=${msg.h264Bitrate}, mjpegQuality=${msg.mjpegQuality}`,
                    );
                    this.events.onFormatRequest?.({
                      codec: msg.codec as "h264" | "mjpeg",
                      width: msg.width,
                      height: msg.height,
                      frameRate: msg.frameRate || 30, // UVC-negotiated rate, default to 30fps
                      frameRateCap: msg.frameRateCap, // User's configured cap
                      h264Bitrate: msg.h264Bitrate, // H.264 bitrate in bps
                      mjpegQuality: msg.mjpegQuality, // MJPEG quality 0.0-1.0
                    });
                  }
                  break;

                case "error":
                  // Server error
                  console.error("[CameraTransport] Server error:", msg.message);
                  this.events.onError?.(new Error(msg.message || "Server error"));
                  break;

                default:
                  console.log("[CameraTransport] Unknown message type:", msg.type);
              }
            } catch {
              // Ignore non-JSON messages
            }
          }
        };
      } catch (error) {
        this.setState("error");
        reject(error);
      }
    });
  }

  // Codec byte constants matching Go server
  private static readonly CODEC_H264 = 0x01;
  private static readonly CODEC_MJPEG = 0x02;

  sendFrame(frame: ArrayBuffer, _timestamp: number, codec: VideoCodec): void {
    if (this._state !== "connected" || !this.ws) {
      this._stats.framesDropped++;
      return;
    }

    if (this.ws.readyState !== WebSocket.OPEN) {
      this._stats.framesDropped++;
      return;
    }

    // Check if buffer is getting too full (backpressure)
    // Drop frames early to prevent buffer from growing too large
    // 4MB threshold allows several MJPEG frames at high quality for transient network hiccups
    if (this.ws.bufferedAmount > 4 * 1024 * 1024) {
      // 4MB buffer threshold
      this._stats.framesDropped++;
      // Only log every 30 dropped frames to avoid console spam
      if (this._stats.framesDropped % 30 === 1) {
        console.warn(
          `[CameraTransport] Buffer backpressure, dropped ${this._stats.framesDropped} frames`,
        );
      }
      return;
    }

    try {
      // Create frame header: 1-byte codec (timestamp not used by server)
      const headerSize = 1;
      const totalSize = headerSize + frame.byteLength;

      // Resize buffer if needed (rare, only for large frames)
      if (this.sendBuffer.length < totalSize) {
        // Round up to next 64KB boundary to avoid frequent resizing
        const newSize = Math.ceil(totalSize / (64 * 1024)) * (64 * 1024);
        this.sendBuffer = new Uint8Array(newSize);
      }

      // Write codec byte
      this.sendBuffer[0] =
        codec === "h264"
          ? WebSocketCameraTransport.CODEC_H264
          : WebSocketCameraTransport.CODEC_MJPEG;

      // Copy frame data into reusable buffer
      this.sendBuffer.set(new Uint8Array(frame), headerSize);

      // Send only the portion we need (subarray doesn't allocate)
      this.ws.send(this.sendBuffer.subarray(0, totalSize));

      this._stats.framesSent++;
      this._stats.bytesSent += totalSize;

      // Periodically report stats
      if (this._stats.framesSent % 30 === 0) {
        this.events.onStats?.(this.stats);
      }
    } catch {
      this._stats.framesDropped++;
    }
  }

  close(): void {
    if (this.ws) {
      this.ws.close(1000, "Client closing");
      this.ws = null;
    }
    this.setState("disconnected");
  }
}

export function createCameraTransport(baseUrl: string): CameraTransport {
  console.log("[CameraTransport] Creating transport with baseUrl:", baseUrl);
  const wsUrl =
    baseUrl
      .replace(/^http:/, "ws:")
      .replace(/^https:/, "wss:")
      .replace(/\/$/, "") + "/api/camera/ws";
  console.log("[CameraTransport] WebSocket URL:", wsUrl);
  return new WebSocketCameraTransport(wsUrl);
}
