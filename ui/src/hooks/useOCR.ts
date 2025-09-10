import { type WorkerOptions } from "tesseract.js";

export type ImageLike = string | HTMLImageElement | HTMLCanvasElement | HTMLVideoElement
  | CanvasRenderingContext2D | File | Blob | OffscreenCanvas;

// tesseract.js is h
async function ocrImage(
  language: string | string[],
  image: ImageLike,
  options?: Partial<WorkerOptions>,
) {
  const tesseract = await import('tesseract.js')
  const createWorker = tesseract.createWorker || tesseract.default.createWorker

  const worker = await createWorker(language, undefined, options)
  const { data: { text } } = await worker.recognize(image)
  await worker.terminate()
  return text
}

export default function useOCR() {
  return {
    ocrImage,
  }
}