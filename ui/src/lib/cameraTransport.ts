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
                  if (msg.codec === "stop") {
                    this.events.onStreamingStopped?.();
                  } else if (msg.codec && msg.width && msg.height) {
                    this.events.onFormatRequest?.({
                      codec: msg.codec as "h264" | "mjpeg",
                      width: msg.width,
                      height: msg.height,
                      frameRate: msg.frameRate || 30,
                      frameRateCap: msg.frameRateCap,
                      h264Bitrate: msg.h264Bitrate,
                      mjpegQuality: msg.mjpegQuality,
                    });
                  }
                  break;

                case "error":
                  console.error("[CameraTransport] Server error:", msg.message);
                  this.events.onError?.(new Error(msg.message || "Server error"));
                  break;

                default:
                  break;
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

  // Frames arrive pre-framed with codec byte from encoder - zero-copy send
  sendFrame(frame: ArrayBuffer, _timestamp: number, _codec: VideoCodec): void {
    if (this._state !== "connected" || !this.ws || this.ws.readyState !== WebSocket.OPEN) {
      this._stats.framesDropped++;
      return;
    }

    // Backpressure: drop frames if buffer is too full (4MB threshold)
    if (this.ws.bufferedAmount > 4 * 1024 * 1024) {
      this._stats.framesDropped++;
      if (this._stats.framesDropped % 30 === 1) {
        console.warn(`[CameraTransport] Backpressure, dropped ${this._stats.framesDropped} frames`);
      }
      return;
    }

    // Zero-copy: frame already has codec byte prefix from encoder
    this.ws.send(frame);

    this._stats.framesSent++;
    this._stats.bytesSent += frame.byteLength;

    if (this._stats.framesSent % 30 === 0) {
      this.events.onStats?.(this._stats);
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
  const wsUrl =
    baseUrl
      .replace(/^http:/, "ws:")
      .replace(/^https:/, "wss:")
      .replace(/\/$/, "") + "/api/camera/ws";
  return new WebSocketCameraTransport(wsUrl);
}
