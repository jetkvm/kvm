import { useCallback, useRef, useState } from "react";

import { useJsonRpc } from "./useJsonRpc";
import { useHidRpc } from "./useHidRpc";
import { useMouseStore, useSettingsStore } from "./stores";
import { isAndroidTouchscreenMode } from "@/utils/androidController";

const calcDelta = (pos: number) => (Math.abs(pos) < 10 ? pos * 2 : pos);

export { isAndroidTouchscreenMode };

export interface AbsMouseMoveHandlerProps {
  videoClientWidth: number;
  videoClientHeight: number;
  videoWidth: number;
  videoHeight: number;
}

const getVideoHidCoordinates = (
  e: MouseEvent,
  { videoClientWidth, videoClientHeight, videoWidth, videoHeight }: AbsMouseMoveHandlerProps,
) => {
  if (!videoClientWidth || !videoClientHeight) return;

  const videoElementAspectRatio = videoClientWidth / videoClientHeight;
  const videoStreamAspectRatio = videoWidth / videoHeight;

  let effectiveWidth = videoClientWidth;
  let effectiveHeight = videoClientHeight;
  let offsetX = 0;
  let offsetY = 0;

  if (videoElementAspectRatio > videoStreamAspectRatio) {
    effectiveWidth = videoClientHeight * videoStreamAspectRatio;
    offsetX = (videoClientWidth - effectiveWidth) / 2;
  } else if (videoElementAspectRatio < videoStreamAspectRatio) {
    effectiveHeight = videoClientWidth / videoStreamAspectRatio;
    offsetY = (videoClientHeight - effectiveHeight) / 2;
  }

  const clampedX = Math.min(Math.max(offsetX, e.offsetX), offsetX + effectiveWidth);
  const clampedY = Math.min(Math.max(offsetY, e.offsetY), offsetY + effectiveHeight);

  const relativeX = (clampedX - offsetX) / effectiveWidth;
  const relativeY = (clampedY - offsetY) / effectiveHeight;

  return {
    x: Math.round(relativeX * 32767),
    y: Math.round(relativeY * 32767),
  };
};

export default function useMouse() {
  // states
  const { setMousePosition, setMouseMove } = useMouseStore();
  const [blockWheelEvent, setBlockWheelEvent] = useState(false);

  const { mouseMode, scrollThrottling, invertScroll } = useSettingsStore();

  // Track last absolute mouse position for resetMousePosition
  const lastAbsPos = useRef({ x: 0, y: 0 });

  // RPC hooks
  const { send } = useJsonRpc();
  const {
    reportAbsMouseEvent,
    reportRelMouseEvent,
    reportWheelEvent,
    reportTouchscreenEvent,
    rpcHidReady,
  } = useHidRpc();
  // Mouse-related

  const sendRelMouseMovement = useCallback(
    (x: number, y: number, buttons: number) => {
      if (mouseMode !== "relative") return;
      // if we ignore the event, double-click will not work
      // if (x === 0 && y === 0 && buttons === 0) return;
      const dx = calcDelta(x);
      const dy = calcDelta(y);
      if (rpcHidReady) {
        reportRelMouseEvent(dx, dy, buttons);
      } else {
        // kept for backward compatibility
        send("relMouseReport", { dx, dy, buttons });
      }
      setMouseMove({ x, y, buttons });
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
      if (rpcHidReady) {
        reportAbsMouseEvent(x, y, buttons);
      } else {
        // kept for backward compatibility
        send("absMouseReport", { x, y, buttons });
      }
      // We set that for the debug info bar
      setMousePosition(x, y);
      lastAbsPos.current = { x, y };
    },
    [send, reportAbsMouseEvent, setMousePosition, mouseMode, rpcHidReady],
  );

  const getAbsMouseMoveHandler = useCallback(
    ({ videoClientWidth, videoClientHeight, videoWidth, videoHeight }: AbsMouseMoveHandlerProps) =>
      (e: MouseEvent) => {
        if (mouseMode !== "absolute") return;

        const coords = getVideoHidCoordinates(e, {
          videoClientWidth,
          videoClientHeight,
          videoWidth,
          videoHeight,
        });
        if (!coords) return;

        // Send mouse movement
        const { buttons } = e;
        sendAbsMouseMovement(coords.x, coords.y, buttons);
      },
    [mouseMode, sendAbsMouseMovement],
  );

  const getTouchscreenMoveHandler = useCallback(
    ({ videoClientWidth, videoClientHeight, videoWidth, videoHeight }: AbsMouseMoveHandlerProps) =>
      (e: MouseEvent) => {
        if (!isAndroidTouchscreenMode()) return;

        const coords = getVideoHidCoordinates(e, {
          videoClientWidth,
          videoClientHeight,
          videoWidth,
          videoHeight,
        });
        if (!coords) return;

        const touching = e.buttons !== 0;

        if (rpcHidReady) {
          reportTouchscreenEvent(coords.x, coords.y, touching);
        } else {
          send("touchscreenReport", { x: coords.x, y: coords.y, touching });
        }
        setMousePosition(coords.x, coords.y);
        lastAbsPos.current = coords;
      },
    [reportTouchscreenEvent, rpcHidReady, send, setMousePosition],
  );

  // Wheel events stay as HID wheel reports in Android touchscreen mode. Android
  // then scrolls only focused/scrollable content, instead of treating wheel
  // input as synthetic swipe gestures such as launcher/app-drawer pulls.
  const getMouseWheelHandler = useCallback(
    () => (e: WheelEvent) => {
      if (scrollThrottling && blockWheelEvent) {
        return;
      }

      const clampWheel = (delta: number): number => {
        const isAccel = Math.abs(delta) >= 100;
        const scrollValue = isAccel ? Math.round(delta / 100) : Math.sign(delta);
        return Math.max(-127, Math.min(127, scrollValue));
      };

      // Negate Y: browser deltaY positive = scroll down, HID Wheel positive = scroll up
      const wheelY = (invertScroll ? 1 : -1) * clampWheel(e.deltaY);
      // X conventions already match (positive = right), but macOS Natural Scrolling
      // inverts both axes at OS level, so we negate X to counteract when inverted
      const wheelX = (invertScroll ? -1 : 1) * clampWheel(e.deltaX);

      if (wheelY === 0 && wheelX === 0) return;

      if (isAndroidTouchscreenMode()) {
        e.preventDefault();
      }

      if (rpcHidReady) {
        reportWheelEvent(wheelY, wheelX);
      } else {
        send("wheelReport", { wheelY, wheelX });
      }

      // Apply blocking delay based of throttling settings
      if (scrollThrottling && !blockWheelEvent) {
        setBlockWheelEvent(true);
        setTimeout(() => setBlockWheelEvent(false), scrollThrottling);
      }
    },
    [send, blockWheelEvent, scrollThrottling, invertScroll, rpcHidReady, reportWheelEvent],
  );

  const resetMousePosition = useCallback(() => {
    sendAbsMouseMovement(lastAbsPos.current.x, lastAbsPos.current.y, 0);
  }, [sendAbsMouseMovement]);

  return {
    getRelMouseMoveHandler,
    getAbsMouseMoveHandler,
    getTouchscreenMoveHandler,
    getMouseWheelHandler,
    resetMousePosition,
  };
}
