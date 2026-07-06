import { useCallback, useRef, useState } from "react";

import { useJsonRpc } from "./useJsonRpc";
import { useHidRpc } from "./useHidRpc";
import { useMouseStore, useSettingsStore } from "./stores";

const calcDelta = (pos: number) => (Math.abs(pos) < 10 ? pos * 2 : pos);

export interface AbsMouseMoveHandlerProps {
  videoWidth: number;
  videoHeight: number;
}

export default function useMouse() {
  // states
  const { setMousePosition, setMouseMove } = useMouseStore();
  const [blockWheelEvent, setBlockWheelEvent] = useState(false);

  const { mouseMode, scrollThrottling, invertScroll } = useSettingsStore();

  // Track last absolute mouse position for resetMousePosition
  const lastAbsPos = useRef({ x: 0, y: 0 });

  // RPC hooks
  const { send } = useJsonRpc();
  const { reportAbsMouseEvent, reportRelMouseEvent, rpcHidReady } = useHidRpc();
  // Mouse-related

  const sendRelMouseMovement = useCallback(
    (x: number, y: number, buttons: number) => {
      if (mouseMode !== "relative") return;
      // if we ignore the event, double-click will not work
      // if (x === 0 && y === 0 && buttons === 0) return;
      const dx = calcDelta(x);
      const dy = calcDelta(y);
      // Keep L/R/M/X1/X2; drop pen-eraser and any future high bits.
      const b = buttons & 0x1f;
      if (rpcHidReady) {
        reportRelMouseEvent(dx, dy, b);
      } else {
        // kept for backward compatibility
        send("relMouseReport", { dx, dy, buttons: b });
      }
      setMouseMove({ x, y, buttons: b });
    },
    [send, reportRelMouseEvent, setMouseMove, mouseMode, rpcHidReady],
  );

  const getRelMouseMoveHandler = useCallback(
    () => (e: MouseEvent) => {
      if (mouseMode !== "relative") return;

      // Send mouse movement
      const { buttons } = e;
      sendRelMouseMovement(e.movementX, e.movementY, buttons);
    },
    [sendRelMouseMovement, mouseMode],
  );

  const sendAbsMouseMovement = useCallback(
    (x: number, y: number, buttons: number) => {
      if (mouseMode !== "absolute") return;
      // Keep L/R/M/X1/X2; drop pen-eraser and any future high bits.
      const b = buttons & 0x1f;
      if (rpcHidReady) {
        reportAbsMouseEvent(x, y, b);
      } else {
        // kept for backward compatibility
        send("absMouseReport", { x, y, buttons: b });
      }
      // We set that for the debug info bar
      setMousePosition(x, y);
      lastAbsPos.current = { x, y };
    },
    [send, reportAbsMouseEvent, setMousePosition, mouseMode, rpcHidReady],
  );

  const getAbsMouseMoveHandler = useCallback(
    ({ videoWidth, videoHeight }: AbsMouseMoveHandlerProps) =>
      (e: PointerEvent | MouseEvent) => {
        if (mouseMode !== "absolute") return;
        if (!videoWidth || !videoHeight) return;

        // Ignore secondary touch points so multi-touch doesn't fight over
        // the single absolute pointer.
        if ("isPrimary" in e && !e.isPrimary) return;

        // Use clientX/Y against the element's bounding rect instead of
        // offsetX/Y: offsetX/Y is unreliable for touch-synthesized events on
        // iPadOS/WebKit (computed against the page rather than the target),
        // which caused a large position offset for touch clients.
        const target = e.currentTarget as HTMLElement | null;
        if (!target) return;
        const rect = target.getBoundingClientRect();
        if (!rect.width || !rect.height) return;

        const localX = e.clientX - rect.left;
        const localY = e.clientY - rect.top;

        // Get the aspect ratios of the video element and the video stream
        const videoElementAspectRatio = rect.width / rect.height;
        const videoStreamAspectRatio = videoWidth / videoHeight;

        // Calculate the effective video display area (object-contain)
        let effectiveWidth = rect.width;
        let effectiveHeight = rect.height;
        let offsetX = 0;
        let offsetY = 0;

        if (videoElementAspectRatio > videoStreamAspectRatio) {
          // Pillarboxing: black bars on the left and right
          effectiveWidth = rect.height * videoStreamAspectRatio;
          offsetX = (rect.width - effectiveWidth) / 2;
        } else if (videoElementAspectRatio < videoStreamAspectRatio) {
          // Letterboxing: black bars on the top and bottom
          effectiveHeight = rect.width / videoStreamAspectRatio;
          offsetY = (rect.height - effectiveHeight) / 2;
        }

        // Clamp mouse position within the effective video boundaries
        const clampedX = Math.min(Math.max(offsetX, localX), offsetX + effectiveWidth);
        const clampedY = Math.min(Math.max(offsetY, localY), offsetY + effectiveHeight);

        // Map clamped mouse position to the video stream's coordinate system
        const relativeX = (clampedX - offsetX) / effectiveWidth;
        const relativeY = (clampedY - offsetY) / effectiveHeight;

        // Convert to HID absolute coordinate system (0-32767 range)
        const x = Math.round(relativeX * 32767);
        const y = Math.round(relativeY * 32767);

        // Send mouse movement
        const { buttons } = e;
        sendAbsMouseMovement(x, y, buttons);
      },
    [mouseMode, sendAbsMouseMovement],
  );

  const getMouseWheelHandler = useCallback(
    () => (e: WheelEvent) => {
      if (scrollThrottling && blockWheelEvent) {
        return;
      }

      const clampWheel = (delta: number): number => {
        const isAccel = Math.abs(delta) >= 100;
        const scrollValue = isAccel ? delta / 100 : Math.sign(delta);
        return Math.max(-127, Math.min(127, scrollValue));
      };

      // Negate Y: browser deltaY positive = scroll down, HID Wheel positive = scroll up
      const wheelY = (invertScroll ? 1 : -1) * clampWheel(e.deltaY);
      // X conventions already match (positive = right), but macOS Natural Scrolling
      // inverts both axes at OS level, so we negate X to counteract when inverted
      const wheelX = (invertScroll ? -1 : 1) * clampWheel(e.deltaX);

      if (wheelY === 0 && wheelX === 0) return;

      send("wheelReport", { wheelY, wheelX });

      // Apply blocking delay based of throttling settings
      if (scrollThrottling && !blockWheelEvent) {
        setBlockWheelEvent(true);
        setTimeout(() => setBlockWheelEvent(false), scrollThrottling);
      }
    },
    [send, blockWheelEvent, scrollThrottling, invertScroll],
  );

  const resetMousePosition = useCallback(() => {
    sendAbsMouseMovement(lastAbsPos.current.x, lastAbsPos.current.y, 0);
  }, [sendAbsMouseMovement]);

  return {
    getRelMouseMoveHandler,
    getAbsMouseMoveHandler,
    getMouseWheelHandler,
    resetMousePosition,
  };
}
