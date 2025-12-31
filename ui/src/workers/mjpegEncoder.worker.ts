/**
 * MJPEG Encoder WebWorker
 *
 * Offloads MJPEG encoding from the main thread for 60fps performance.
 * Uses OffscreenCanvas.convertToBlob() which is the most efficient
 * JPEG encoding method available in browsers.
 */

interface EncoderConfig {
  width: number;
  height: number;
  quality: number;
  codecByte?: number; // Prepended to each frame for transport (0x02 for MJPEG)
}

interface StartMessage {
  type: "start";
  config: EncoderConfig;
}

interface FrameMessage {
  type: "frame";
  bitmap: ImageBitmap;
  timestamp: number;
}

interface StopMessage {
  type: "stop";
}

interface SetQualityMessage {
  type: "setQuality";
  quality: number;
}

type WorkerMessage = StartMessage | FrameMessage | StopMessage | SetQualityMessage;

interface EncodedFrameMessage {
  type: "frame";
  data: ArrayBuffer;
  timestamp: number;
}

interface ErrorMessage {
  type: "error";
  message: string;
}

interface ReadyMessage {
  type: "ready";
}

interface StoppedMessage {
  type: "stopped";
}

type WorkerResponse = EncodedFrameMessage | ErrorMessage | ReadyMessage | StoppedMessage;

let canvas: OffscreenCanvas | null = null;
let ctx: OffscreenCanvasRenderingContext2D | null = null;
let config: EncoderConfig = { width: 1920, height: 1080, quality: 0.7 };
let isRunning = false;

// Error rate limiting to avoid spamming main thread
let errorCount = 0;
let lastErrorTime = 0;
const ERROR_LOG_INTERVAL_MS = 1000;

function reportError(err: unknown): void {
  errorCount++;
  const now = performance.now();
  if (now - lastErrorTime >= ERROR_LOG_INTERVAL_MS) {
    const message = err instanceof Error ? err.message : String(err);
    const fullMessage =
      errorCount > 1 ? `${message} (${errorCount} errors in last interval)` : message;
    self.postMessage({ type: "error", message: fullMessage } as ErrorMessage);
    lastErrorTime = now;
    errorCount = 0;
  }
}

self.onmessage = async (event: MessageEvent<WorkerMessage>) => {
  const msg = event.data;

  switch (msg.type) {
    case "start":
      await handleStart(msg.config);
      break;

    case "frame":
      await handleFrame(msg.bitmap, msg.timestamp);
      break;

    case "stop":
      handleStop();
      break;

    case "setQuality":
      handleSetQuality(msg.quality);
      break;

    default:
      // Unknown message types are silently ignored for forward compatibility
      break;
  }
};

async function handleStart(newConfig: EncoderConfig): Promise<void> {
  try {
    config = newConfig;

    // Create OffscreenCanvas for encoding
    canvas = new OffscreenCanvas(config.width, config.height);
    ctx = canvas.getContext("2d", {
      alpha: false, // No alpha channel needed for MJPEG
      desynchronized: true, // Hint for lower latency
    });

    if (!ctx) {
      throw new Error("Failed to get 2D context");
    }

    isRunning = true;

    const response: ReadyMessage = { type: "ready" };
    self.postMessage(response);
  } catch (error) {
    const response: ErrorMessage = {
      type: "error",
      message: error instanceof Error ? error.message : String(error),
    };
    self.postMessage(response);
  }
}

async function handleFrame(bitmap: ImageBitmap, timestamp: number): Promise<void> {
  if (!isRunning || !canvas || !ctx) {
    bitmap.close();
    return;
  }

  try {
    // Scale bitmap to canvas dimensions
    ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);

    const blob = await canvas.convertToBlob({
      type: "image/jpeg",
      quality: config.quality,
    });

    const jpegData = await blob.arrayBuffer();

    // Prepend codec byte if configured (avoids copy in transport)
    let data: ArrayBuffer;
    if (config.codecByte !== undefined) {
      // Create framed buffer: [codecByte][jpegData...]
      const jpegBytes = new Uint8Array(jpegData);
      const framed = new Uint8Array(1 + jpegBytes.length);
      framed[0] = config.codecByte;
      framed.set(jpegBytes, 1);
      data = framed.buffer;
    } else {
      data = jpegData;
    }

    const response: EncodedFrameMessage = { type: "frame", data, timestamp };
    self.postMessage(response, { transfer: [data] });
  } catch (err) {
    reportError(err);
  } finally {
    // Always close the bitmap to free GPU memory
    bitmap.close();
  }
}

function handleStop(): void {
  isRunning = false;
  canvas = null;
  ctx = null;

  const response: StoppedMessage = { type: "stopped" };
  self.postMessage(response);
}

function handleSetQuality(quality: number): void {
  if (quality >= 0.0 && quality <= 1.0) {
    config.quality = quality;
  } else {
    reportError(new Error(`Invalid quality value: ${quality} (must be 0.0-1.0)`));
  }
}

export type { WorkerMessage, WorkerResponse, EncoderConfig };
