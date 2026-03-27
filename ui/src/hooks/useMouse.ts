import { useCallback, useRef, useState } from "react";

import { useJsonRpc } from "./useJsonRpc";
import { useHidRpc } from "./useHidRpc";
import { useMouseStore, useSettingsStore } from "./stores";

const calcDelta = (pos: number) => (Math.abs(pos) < 10 ? pos * 2 : pos);

export interface AbsMouseMoveHandlerProps {
  videoClientWidth: number;
  videoClientHeight: number;
  videoWidth: number;
  videoHeight: number;
}

export default function useMouse() {
  // states
  const { setMousePosition, setMouseMove } = useMouseStore();
  const [blockWheelEvent, setBlockWheelEvent] = useState(false);

  const { mouseMode, scrollThrottling } = useSettingsStore();

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
        if (!videoClientWidth || !videoClientHeight) return;
        if (mouseMode !== "absolute") return;

        // Get the aspect ratios of the video element and the video stream
        const videoElementAspectRatio = videoClientWidth / videoClientHeight;
        const videoStreamAspectRatio = videoWidth / videoHeight;

        // Calculate the effective video display area
        let effectiveWidth = videoClientWidth;
        let effectiveHeight = videoClientHeight;
        let offsetX = 0;
        let offsetY = 0;

        if (videoElementAspectRatio > videoStreamAspectRatio) {
          // Pillarboxing: black bars on the left and right
          effectiveWidth = videoClientHeight * videoStreamAspectRatio;
          offsetX = (videoClientWidth - effectiveWidth) / 2;
        } else if (videoElementAspectRatio < videoStreamAspectRatio) {
          // Letterboxing: black bars on the top and bottom
          effectiveHeight = videoClientWidth / videoStreamAspectRatio;
          offsetY = (videoClientHeight - effectiveHeight) / 2;
        }

        // Clamp mouse position within the effective video boundaries
        const clampedX = Math.min(Math.max(offsetX, e.offsetX), offsetX + effectiveWidth);
        const clampedY = Math.min(Math.max(offsetY, e.offsetY), offsetY + effectiveHeight);

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
      const wheelY = -clampWheel(e.deltaY);
      // Keep X as-is: browser deltaX and HID AC Pan both use positive = right
      const wheelX = clampWheel(e.deltaX);

      if (wheelY === 0 && wheelX === 0) return;

      send("wheelReport", { wheelY, wheelX });

      // Apply blocking delay based of throttling settings
      if (scrollThrottling && !blockWheelEvent) {
        setBlockWheelEvent(true);
        setTimeout(() => setBlockWheelEvent(false), scrollThrottling);
      }
    },
    [send, blockWheelEvent, scrollThrottling],
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
