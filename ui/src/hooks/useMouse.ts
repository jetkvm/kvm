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

function getVideoRelativePosition(
  e: MouseEvent,
  videoClientWidth: number,
  videoClientHeight: number,
  videoWidth: number,
  videoHeight: number,
) {
  const target = e.currentTarget;
  if (!(target instanceof HTMLElement)) return null;

  const rect = target.getBoundingClientRect();
  const elementWidth = rect.width || videoClientWidth;
  const elementHeight = rect.height || videoClientHeight;
  if (!elementWidth || !elementHeight) return null;

  const streamWidth = videoWidth || elementWidth;
  const streamHeight = videoHeight || elementHeight;
  if (!streamWidth || !streamHeight) return null;

  const elementAspectRatio = elementWidth / elementHeight;
  const streamAspectRatio = streamWidth / streamHeight;

  let effectiveWidth = elementWidth;
  let effectiveHeight = elementHeight;
  let offsetX = 0;
  let offsetY = 0;

  if (elementAspectRatio > streamAspectRatio) {
    effectiveWidth = elementHeight * streamAspectRatio;
    offsetX = (elementWidth - effectiveWidth) / 2;
  } else if (elementAspectRatio < streamAspectRatio) {
    effectiveHeight = elementWidth / streamAspectRatio;
    offsetY = (elementHeight - effectiveHeight) / 2;
  }

  const pointerX = e.clientX - rect.left;
  const pointerY = e.clientY - rect.top;

  const clampedX = Math.min(Math.max(offsetX, pointerX), offsetX + effectiveWidth);
  const clampedY = Math.min(Math.max(offsetY, pointerY), offsetY + effectiveHeight);

  return {
    x: (clampedX - offsetX) / effectiveWidth,
    y: (clampedY - offsetY) / effectiveHeight,
  };
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
  const { reportAbsMouseEvent, reportRelMouseEvent, reportTouchscreenEvent, rpcHidReady } =
    useHidRpc();
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
    ({ videoClientWidth, videoClientHeight, videoWidth, videoHeight }: AbsMouseMoveHandlerProps) =>
      (e: MouseEvent) => {
        if (!videoClientWidth || !videoClientHeight) return;
        if (mouseMode !== "absolute") return;

        const position = getVideoRelativePosition(
          e,
          videoClientWidth,
          videoClientHeight,
          videoWidth,
          videoHeight,
        );
        if (!position) return;

        // Convert to HID absolute coordinate system (0-32767 range)
        const x = Math.round(position.x * 32767);
        const y = Math.round(position.y * 32767);

        // Send mouse movement
        const { buttons } = e;
        sendAbsMouseMovement(x, y, buttons);
      },
    [mouseMode, sendAbsMouseMovement],
  );

  const getDigitizerMoveHandler = useCallback(
    ({ videoClientWidth, videoClientHeight, videoWidth, videoHeight }: AbsMouseMoveHandlerProps) =>
      (e: MouseEvent) => {
        if (!videoClientWidth || !videoClientHeight) return;
        if (mouseMode !== "digitizer") return;

        const position = getVideoRelativePosition(
          e,
          videoClientWidth,
          videoClientHeight,
          videoWidth,
          videoHeight,
        );
        if (!position) return;

        const x = Math.round(position.x * 32767);
        const y = Math.round(position.y * 32767);
        const touching = e.buttons !== 0;

        if (rpcHidReady) {
          reportTouchscreenEvent(x, y, touching);
        } else {
          send("touchscreenReport", { x, y, touching });
        }
        setMousePosition(x, y);
        lastAbsPos.current = { x, y };
      },
    [mouseMode, reportTouchscreenEvent, rpcHidReady, send, setMousePosition],
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
    if (mouseMode === "digitizer") {
      if (rpcHidReady) {
        reportTouchscreenEvent(lastAbsPos.current.x, lastAbsPos.current.y, false);
      } else {
        send("touchscreenReport", {
          x: lastAbsPos.current.x,
          y: lastAbsPos.current.y,
          touching: false,
        });
      }
      return;
    }

    sendAbsMouseMovement(lastAbsPos.current.x, lastAbsPos.current.y, 0);
  }, [mouseMode, reportTouchscreenEvent, rpcHidReady, send, sendAbsMouseMovement]);

  return {
    getRelMouseMoveHandler,
    getAbsMouseMoveHandler,
    getDigitizerMoveHandler,
    getMouseWheelHandler,
    resetMousePosition,
  };
}
