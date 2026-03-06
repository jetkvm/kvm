import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";

import { useVideoStore, useUiStore } from "@hooks/stores";
import Card from "@components/Card";
import { ConfirmDialog } from "@components/ConfirmDialog";
import TextArea from "@components/TextArea";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

type OcrStatus = "idle" | "selecting" | "processing" | "result";

async function loadTesseract() {
  const { createWorker } = await import("tesseract.js");
  return createWorker;
}

let workerPromise: ReturnType<typeof initWorker> | null = null;
let cleanupTimer: ReturnType<typeof setTimeout> | null = null;

async function initWorker() {
  const createWorker = await loadTesseract();
  return createWorker("eng", 1);
}

async function terminateWorker() {
  if (workerPromise) {
    try {
      const worker = await workerPromise;
      await worker.terminate();
    } catch {
      // Ignore termination errors
    }
    workerPromise = null;
  }
}

function getWorker() {
  if (cleanupTimer) {
    clearTimeout(cleanupTimer);
    cleanupTimer = null;
  }

  if (!workerPromise) {
    workerPromise = initWorker().catch(err => {
      workerPromise = null;
      throw err;
    });
  }

  // Auto-terminate worker after 60 seconds of inactivity
  cleanupTimer = setTimeout(() => {
    terminateWorker();
    cleanupTimer = null;
  }, 60_000);

  return workerPromise;
}

async function performOcr(canvas: HTMLCanvasElement): Promise<string> {
  const worker = await getWorker();
  const { data } = await worker.recognize(canvas, {}, { text: true, blocks: true });

  // Flatten lines from the blocks → paragraphs → lines hierarchy.
  const lines = data.blocks?.flatMap(b => b.paragraphs.flatMap(p => p.lines)) ?? [];

  if (lines.length === 0) return data.text.trim();

  // Estimate average character width from word bounding boxes, then use each
  // line's pixel offset from the left edge to reconstruct indentation that
  // Tesseract strips by default.
  let totalCharWidth = 0;
  let samples = 0;
  for (const line of lines) {
    for (const word of line.words) {
      const len = word.text.trim().length;
      if (len > 0) {
        totalCharWidth += (word.bbox.x1 - word.bbox.x0) / len;
        samples++;
      }
    }
  }

  if (samples === 0) return data.text.trim();

  const charWidth = totalCharWidth / samples;
  const minX = Math.min(...lines.map(l => l.bbox.x0));

  return lines
    .map(line => {
      const indent = Math.round((line.bbox.x0 - minX) / charWidth);
      return " ".repeat(indent) + line.text.trim();
    })
    .join("\n")
    .trim();
}

function captureRegion(videoEl: HTMLVideoElement, rect: Rect): HTMLCanvasElement {
  const canvas = document.createElement("canvas");
  canvas.width = rect.width;
  canvas.height = rect.height;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("Failed to acquire 2D canvas context");
  ctx.drawImage(videoEl, rect.x, rect.y, rect.width, rect.height, 0, 0, rect.width, rect.height);
  return canvas;
}

/**
 * Computes the effective video display area within a container, accounting for
 * object-contain letterboxing/pillarboxing.
 */
function getVideoDisplayRect(
  videoElement: HTMLVideoElement,
  videoWidth: number,
  videoHeight: number,
) {
  const videoRect = videoElement.getBoundingClientRect();
  const elementAspectRatio = videoRect.width / videoRect.height;
  const streamAspectRatio = videoWidth / videoHeight;

  let effectiveWidth = videoRect.width;
  let effectiveHeight = videoRect.height;
  let offsetX = 0;
  let offsetY = 0;

  if (elementAspectRatio > streamAspectRatio) {
    effectiveWidth = videoRect.height * streamAspectRatio;
    offsetX = (videoRect.width - effectiveWidth) / 2;
  } else if (elementAspectRatio < streamAspectRatio) {
    effectiveHeight = videoRect.width / streamAspectRatio;
    offsetY = (videoRect.height - effectiveHeight) / 2;
  }

  return { videoRect, effectiveWidth, effectiveHeight, offsetX, offsetY };
}

/**
 * Outer shell — always mounted. Handles AnimatePresence so the exit animation
 * plays when isOcrMode becomes false. The inner content unmounts naturally,
 * resetting all local state without an effect.
 *
 * Also registers the global keyboard shortcut (Ctrl/Cmd+Shift+O) to toggle
 * OCR mode.
 */
export default function OcrOverlay() {
  const { isOcrMode, setOcrMode } = useUiStore();
  const { width: videoWidth, height: videoHeight } = useVideoStore();

  // Global keyboard shortcut: Ctrl+Shift+O (Cmd+Shift+O on Mac)
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key.toLowerCase() === "o") {
        // Don't toggle if video isn't available
        if (videoWidth === 0 || videoHeight === 0) return;
        e.preventDefault();
        e.stopPropagation();
        setOcrMode(!isOcrMode);
      }
    };

    document.addEventListener("keydown", handleKeyDown, { capture: true });
    return () => document.removeEventListener("keydown", handleKeyDown, { capture: true });
  }, [isOcrMode, setOcrMode, videoWidth, videoHeight]);

  return <AnimatePresence>{isOcrMode && <OcrOverlayContent key="ocr" />}</AnimatePresence>;
}

function OcrOverlayContent() {
  const {
    videoElement,
    containerElement,
    width: videoWidth,
    height: videoHeight,
  } = useVideoStore();
  const { setOcrMode, setDisableVideoFocusTrap } = useUiStore();

  const mountedRef = useRef(true);
  const resultRef = useRef<HTMLTextAreaElement>(null);
  const [status, setStatus] = useState<OcrStatus>("idle");
  const [selectionStart, setSelectionStart] = useState<{ x: number; y: number } | null>(null);
  const [selectionRect, setSelectionRect] = useState<Rect | null>(null);
  const [ocrResult, setOcrResult] = useState<string>("");
  const [isClosing, setIsClosing] = useState(false);

  // Close the ConfirmDialog first (allowing exit animation), then unmount.
  const closeOverlay = useCallback(() => {
    if (status === "processing" || status === "result") {
      setIsClosing(true);
      setSelectionRect(null);
      setSelectionStart(null);
      // Wait for the HeadlessUI Dialog leave transition (200ms) before unmounting
      setTimeout(() => setOcrMode(false), 200);
    } else {
      setOcrMode(false);
    }
  }, [status, setOcrMode]);

  // Track unmount so async OCR callbacks can bail out
  useEffect(() => {
    return () => {
      mountedRef.current = false;
    };
  }, []);

  // Pause the video focus trap when showing a ConfirmDialog (processing or
  // result), so the dialog buttons and textarea can receive focus.
  useEffect(() => {
    if (status === "processing" || status === "result") {
      setDisableVideoFocusTrap(true);
      return () => setDisableVideoFocusTrap(false);
    }
  }, [status, setDisableVideoFocusTrap]);

  // Escape key exits OCR mode (only when no dialog is open — the dialog
  // handles its own Escape via HeadlessUI, which calls closeOverlay)
  useEffect(() => {
    if (status === "processing" || status === "result") return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        setOcrMode(false);
      }
    };

    document.addEventListener("keydown", handleKeyDown, { capture: true });
    return () => document.removeEventListener("keydown", handleKeyDown, { capture: true });
  }, [setOcrMode, status]);

  // Listen for the native copy event (e.g. Cmd+C on selected text) to
  // confirm copy and close the overlay.
  useEffect(() => {
    if (status !== "result") return;

    const handleCopy = () => {
      notifications.success(m.ocr_copied(), { duration: 4000 });
      closeOverlay();
    };

    document.addEventListener("copy", handleCopy);
    return () => document.removeEventListener("copy", handleCopy);
  }, [status, closeOverlay]);

  // Auto-focus and select text when result appears
  useEffect(() => {
    if (status === "result" && resultRef.current) {
      resultRef.current.focus();
      resultRef.current.select();
    }
  }, [status]);

  // Convert client pixel coordinates to native video coordinates,
  // accounting for object-contain letterboxing/pillarboxing.
  const toVideoCoords = useCallback(
    (clientX: number, clientY: number) => {
      if (!videoElement) return { x: 0, y: 0 };

      const { videoRect, effectiveWidth, effectiveHeight, offsetX, offsetY } = getVideoDisplayRect(
        videoElement,
        videoWidth,
        videoHeight,
      );

      const relX = clientX - videoRect.left - offsetX;
      const relY = clientY - videoRect.top - offsetY;

      const scaleX = videoWidth / effectiveWidth;
      const scaleY = videoHeight / effectiveHeight;

      return {
        x: Math.max(0, Math.min(videoWidth, Math.round(relX * scaleX))),
        y: Math.max(0, Math.min(videoHeight, Math.round(relY * scaleY))),
      };
    },
    [videoElement, videoWidth, videoHeight],
  );

  // Extract clientX/clientY from either mouse or touch events
  const getPointerCoords = useCallback((e: React.MouseEvent | React.TouchEvent) => {
    if ("touches" in e) {
      const touch = e.touches[0] || e.changedTouches[0];
      return { clientX: touch.clientX, clientY: touch.clientY };
    }
    return { clientX: e.clientX, clientY: e.clientY };
  }, []);

  const handlePointerDown = useCallback(
    (e: React.MouseEvent | React.TouchEvent) => {
      if (status === "processing" || status === "result") return;
      e.preventDefault();
      e.stopPropagation();

      const { clientX, clientY } = getPointerCoords(e);
      const coords = toVideoCoords(clientX, clientY);
      setSelectionStart(coords);
      setSelectionRect(null);
      setStatus("selecting");
    },
    [status, toVideoCoords, getPointerCoords],
  );

  const handlePointerMove = useCallback(
    (e: React.MouseEvent | React.TouchEvent) => {
      if (status !== "selecting" || !selectionStart) return;
      e.preventDefault();
      e.stopPropagation();

      const { clientX, clientY } = getPointerCoords(e);
      const coords = toVideoCoords(clientX, clientY);
      const x = Math.min(selectionStart.x, coords.x);
      const y = Math.min(selectionStart.y, coords.y);
      const width = Math.abs(coords.x - selectionStart.x);
      const height = Math.abs(coords.y - selectionStart.y);

      setSelectionRect({ x, y, width, height });
    },
    [status, selectionStart, toVideoCoords, getPointerCoords],
  );

  const handlePointerUp = useCallback(
    async (e: React.MouseEvent | React.TouchEvent) => {
      if (status !== "selecting" || !selectionRect || !videoElement) return;
      e.preventDefault();
      e.stopPropagation();

      // Require a minimum selection size (10x10 native pixels)
      if (selectionRect.width < 10 || selectionRect.height < 10) {
        setStatus("idle");
        setSelectionStart(null);
        setSelectionRect(null);
        return;
      }

      setStatus("processing");

      try {
        const canvas = captureRegion(videoElement, selectionRect);
        const text = await performOcr(canvas);
        canvas.width = 0;
        canvas.height = 0;

        if (!mountedRef.current) return;

        if (text) {
          setOcrResult(text);
          setStatus("result");
        } else {
          notifications.error(m.ocr_no_text_detected());
          closeOverlay();
        }
      } catch (err) {
        if (!mountedRef.current) return;
        console.error("OCR failed:", err);
        notifications.error(m.ocr_failed());
        closeOverlay();
      }
    },
    [status, selectionRect, videoElement, closeOverlay],
  );

  // Compute selection rectangle position in CSS pixels relative to the overlay.
  // The overlay covers the full container (`absolute inset-0`) while the video
  // element is centered inside it via flexbox, so we must account for the gap
  // between the container edge and the video element, plus any object-contain
  // letterboxing within the video element itself.
  const selectionStyle = useMemo(() => {
    if (!selectionRect || !videoElement || !containerElement) return undefined;

    const { videoRect, effectiveWidth, effectiveHeight, offsetX, offsetY } = getVideoDisplayRect(
      videoElement,
      videoWidth,
      videoHeight,
    );

    const containerRect = containerElement.getBoundingClientRect();
    const baseX = videoRect.left - containerRect.left + offsetX;
    const baseY = videoRect.top - containerRect.top + offsetY;

    return {
      left: `${baseX + (selectionRect.x / videoWidth) * effectiveWidth}px`,
      top: `${baseY + (selectionRect.y / videoHeight) * effectiveHeight}px`,
      width: `${(selectionRect.width / videoWidth) * effectiveWidth}px`,
      height: `${(selectionRect.height / videoHeight) * effectiveHeight}px`,
    };
  }, [selectionRect, videoElement, containerElement, videoWidth, videoHeight]);

  return (
    <>
      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        exit={{ opacity: 0 }}
        transition={{ duration: 0.15 }}
        className="absolute inset-0 z-10"
        style={{ cursor: status === "result" ? "default" : "crosshair" }}
        onMouseDown={handlePointerDown}
        onMouseMove={handlePointerMove}
        onMouseUp={handlePointerUp}
        onMouseLeave={() => {
          if (status === "selecting") {
            setStatus("idle");
            setSelectionStart(null);
            setSelectionRect(null);
          }
        }}
        onTouchStart={handlePointerDown}
        onTouchMove={handlePointerMove}
        onTouchEnd={handlePointerUp}
      >
        {/* Semi-transparent background */}
        <div className="fixed inset-0 bg-black/20" />

        {/* Instruction text */}
        {status === "idle" && (
          <div className="pointer-events-none absolute inset-x-0 top-4 flex justify-center">
            <div className="rounded-md bg-black/70 px-3 py-1.5 text-xs font-medium text-white">
              {m.ocr_drag_to_select()}
            </div>
          </div>
        )}

        {/* Selection rectangle with size indicator */}
        {selectionRect && selectionStyle && status !== "result" && (
          <div
            className="absolute border-2 border-dashed border-blue-400 bg-blue-400/10"
            style={selectionStyle}
          >
            {selectionRect.width >= 10 && selectionRect.height >= 10 && (
              <Card className="absolute right-0 -bottom-6 w-auto px-1.5 py-0.5 text-[10px] font-medium tabular-nums dark:text-white">
                {selectionRect.width} &times; {selectionRect.height}
              </Card>
            )}
          </div>
        )}
      </motion.div>

      {/* Single dialog for both processing and result states — avoids a
          separate Modal that flickers on fast OCR, and allows the HeadlessUI
          leave transition to play when closing. */}
      <ConfirmDialog
        open={(status === "processing" || status === "result") && !isClosing}
        onClose={closeOverlay}
        title={status === "result" ? m.action_bar_copy_text() : m.ocr_recognizing()}
        description={
          status === "result" ? m.ocr_result_description() : m.ocr_processing_description()
        }
        confirmText={m.ocr_copy_text()}
        isConfirming={status === "processing"}
        onConfirm={() => {
          if (navigator.clipboard?.writeText) {
            navigator.clipboard.writeText(ocrResult).then(() => {
              notifications.success(m.ocr_copied(), { duration: 4000 });
              closeOverlay();
            });
          } else if (resultRef.current) {
            resultRef.current.focus();
            resultRef.current.select();
            document.execCommand("copy");
            // Don't show toast here — the copy event listener handles it
          }
        }}
      >
        {status === "processing" ? (
          <div className="mt-2 space-y-2">
            <div className="h-4 w-full animate-pulse rounded bg-slate-200 dark:bg-slate-700" />
            <div className="h-4 w-3/4 animate-pulse rounded bg-slate-200 dark:bg-slate-700" />
            <div className="h-4 w-5/6 animate-pulse rounded bg-slate-200 dark:bg-slate-700" />
          </div>
        ) : (
          <div className="mt-2">
            <TextArea
              ref={resultRef}
              value={ocrResult}
              readOnly
              rows={Math.min(10, ocrResult.split("\n").length + 1)}
            />
          </div>
        )}
      </ConfirmDialog>
    </>
  );
}
