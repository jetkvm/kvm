/**
 * Camera Encoder Web Worker
 *
 * Handles JPEG encoding off the main thread for better performance.
 * Uses OffscreenCanvas for hardware-accelerated encoding.
 */

interface EncodeRequest {
  type: 'encode';
  bitmap: ImageBitmap;
  quality: number;
}

interface InitRequest {
  type: 'init';
  width: number;
  height: number;
}

type WorkerRequest = EncodeRequest | InitRequest;

let canvas: OffscreenCanvas | null = null;
let ctx: OffscreenCanvasRenderingContext2D | null = null;

self.onmessage = async (e: MessageEvent<WorkerRequest>) => {
  const { data } = e;

  if (data.type === 'init') {
    canvas = new OffscreenCanvas(data.width, data.height);
    // Optimize canvas context for JPEG encoding:
    // - alpha: false - JPEG has no alpha channel, skip alpha processing
    ctx = canvas.getContext('2d', { alpha: false });
    self.postMessage({ type: 'ready' });
    return;
  }

  if (data.type === 'encode') {
    if (!canvas || !ctx) {
      self.postMessage({ type: 'error', error: 'Worker not initialized' });
      return;
    }

    try {
      // Resize canvas if bitmap dimensions changed
      if (canvas.width !== data.bitmap.width || canvas.height !== data.bitmap.height) {
        canvas.width = data.bitmap.width;
        canvas.height = data.bitmap.height;
      }

      // Draw bitmap to canvas (very fast, hardware accelerated)
      ctx.drawImage(data.bitmap, 0, 0);

      // Close the bitmap to free memory
      data.bitmap.close();

      // Encode to JPEG using native encoder (hardware accelerated on most browsers)
      const blob = await canvas.convertToBlob({
        type: 'image/jpeg',
        quality: data.quality
      });

      // Convert to ArrayBuffer for transfer
      const buffer = await blob.arrayBuffer();

      // Send back as transferable (zero-copy)
      self.postMessage({ type: 'encoded', buffer }, { transfer: [buffer] });
    } catch (error) {
      self.postMessage({ type: 'error', error: String(error) });
    }
  }
};

export {}; // Make this a module
