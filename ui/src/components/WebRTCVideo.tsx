import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useResizeObserver } from "usehooks-ts";

import {
  useDeviceSettingsStore,
  useHidStore,
  useMouseStore,
  useRTCStore,
  useSettingsStore,
  useUiStore,
  useVideoStore,
} from "@/hooks/stores";
import { keys, modifiers } from "@/keyboardMappings";
import { cx } from "@/cva.config";
import VirtualKeyboard from "@components/VirtualKeyboard";
import Actionbar from "@components/ActionBar";
import MacroBar from "@/components/MacroBar";
import InfoBar from "@components/InfoBar";
import useKeyboard from "@/hooks/useKeyboard";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";

import {
  HDMIErrorOverlay,
  LoadingVideoOverlay,
  NoAutoplayPermissionsOverlay,
  PointerLockBar,
} from "./VideoOverlay";

export default function WebRTCVideo() {
  // Video and stream related refs and states
  const videoElm = useRef<HTMLVideoElement>(null);
  const mediaStream = useRTCStore(state => state.mediaStream);
  const [isPlaying, setIsPlaying] = useState(false);
  const peerConnectionState = useRTCStore(state => state.peerConnectionState);
  const [isPointerLockActive, setIsPointerLockActive] = useState(false);
  // Store hooks
  const settings = useSettingsStore();
  const { sendKeyboardEvent, resetKeyboardState } = useKeyboard();
  const setMousePosition = useMouseStore(state => state.setMousePosition);
  const setMouseMove = useMouseStore(state => state.setMouseMove);
  const {
    setClientSize: setVideoClientSize,
    setSize: setVideoSize,
    width: videoWidth,
    height: videoHeight,
    clientWidth: videoClientWidth,
    clientHeight: videoClientHeight,
  } = useVideoStore();

  // RTC related states
  const peerConnection = useRTCStore(state => state.peerConnection);

  // HDMI and UI states
  const hdmiState = useVideoStore(state => state.hdmiState);
  const hdmiError = ["no_lock", "no_signal", "out_of_range"].includes(hdmiState);
  const isVideoLoading = !isPlaying;

  // Keyboard related states
  const { setIsNumLockActive, setIsCapsLockActive, setIsScrollLockActive } =
    useHidStore();

  // Misc states and hooks
  const [blockWheelEvent, setBlockWheelEvent] = useState(false);
  const disableVideoFocusTrap = useUiStore(state => state.disableVideoFocusTrap);
  const [send] = useJsonRpc();

  // Video-related
  useResizeObserver({
    ref: videoElm as React.RefObject<HTMLElement>,
    onResize: ({ width, height }) => {
      // This is actually client size, not videoSize
      if (width && height) {
        if (!videoElm.current) return;
        setVideoClientSize(width, height);
        setVideoSize(videoElm.current.videoWidth, videoElm.current.videoHeight);
      }
    },
  });

  const updateVideoSizeStore = useCallback(
    (videoElm: HTMLVideoElement) => {
      setVideoClientSize(videoElm.clientWidth, videoElm.clientHeight);
      setVideoSize(videoElm.videoWidth, videoElm.videoHeight);
    },
    [setVideoClientSize, setVideoSize],
  );

  const onVideoPlaying = useCallback(() => {
    setIsPlaying(true);
    if (videoElm.current) updateVideoSizeStore(videoElm.current);
  }, [updateVideoSizeStore]);

  // On mount, get the video size
  useEffect(
    function updateVideoSizeOnMount() {
      if (videoElm.current) updateVideoSizeStore(videoElm.current);
    },
    [setVideoClientSize, updateVideoSizeStore, setVideoSize],
  );

  // Pointer lock and keyboard lock related
  const isPointerLockPossible = window.location.protocol === "https:";

  const checkNavigatorPermissions = useCallback(async (permissionName: string) => {
    const name = permissionName as PermissionName;
    const { state } = await navigator.permissions.query({ name });
    return state === "granted";
  }, []);

  const requestPointerLock = useCallback(async () => {
    if (document.pointerLockElement) return;

    const isPointerLockGranted = await checkNavigatorPermissions("pointer-lock");
    if (isPointerLockGranted && settings.mouseMode === "relative") {
      videoElm.current?.requestPointerLock();
    }
  }, [checkNavigatorPermissions, settings.mouseMode]);

  useEffect(() => {
    if (!isPointerLockPossible || !videoElm.current) return;

    const handlePointerLockChange = () => {
      if (document.pointerLockElement) {
        notifications.success("Pointer lock Enabled, hold escape to exit");
        setIsPointerLockActive(true);
      } else {
        notifications.success("Pointer lock disabled");
        setIsPointerLockActive(false);
      }
    };

    const abortController = new AbortController();
    const signal = abortController.signal;

    document.addEventListener("pointerlockchange", handlePointerLockChange, { signal });

    return () => {
      abortController.abort();
    };
  }, [isPointerLockPossible, videoElm]);

  const requestFullscreen = useCallback(async () => {
    videoElm.current?.requestFullscreen({
      navigationUI: "show",
    });

    // we do not care about pointer lock if it's for fullscreen
    await requestPointerLock();

    const isKeyboardLockGranted = await checkNavigatorPermissions("keyboard-lock");
    if (isKeyboardLockGranted) {
      if ("keyboard" in navigator) {
        // @ts-expect-error - keyboard lock is not supported in all browsers
        await navigator.keyboard.lock();
      }
    }
  }, [requestPointerLock, checkNavigatorPermissions]);

  // Mouse-related
  const calcDelta = (pos: number) => (Math.abs(pos) < 10 ? pos * 2 : pos);
  const sendRelMouseMovement = useCallback(
    (x: number, y: number, buttons: number) => {
      if (settings.mouseMode !== "relative") return;
      // if we ignore the event, double-click will not work
      // if (x === 0 && y === 0 && buttons === 0) return;
      send("relMouseReport", { dx: calcDelta(x), dy: calcDelta(y), buttons });
      setMouseMove({ x, y, buttons });
    },
    [send, setMouseMove, settings.mouseMode],
  );

  const relMouseMoveHandler = useCallback(
    (e: MouseEvent) => {
      if (settings.mouseMode !== "relative") return;
      if (isPointerLockActive === false && isPointerLockPossible === true) return;

      // Send mouse movement
      const { buttons } = e;
      sendRelMouseMovement(e.movementX, e.movementY, buttons);
    },
    [
      isPointerLockActive,
      isPointerLockPossible,
      sendRelMouseMovement,
      settings.mouseMode,
    ],
  );

  const sendAbsMouseMovement = useCallback(
    (x: number, y: number, buttons: number) => {
      if (settings.mouseMode !== "absolute") return;
      send("absMouseReport", { x, y, buttons });
      // We set that for the debug info bar
      setMousePosition(x, y);
    },
    [send, setMousePosition, settings.mouseMode],
  );

  const absMouseMoveHandler = useCallback(
    (e: MouseEvent) => {
      if (!videoClientWidth || !videoClientHeight) return;
      if (settings.mouseMode !== "absolute") return;

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
    [
      sendAbsMouseMovement,
      videoClientHeight,
      videoClientWidth,
      videoWidth,
      videoHeight,
      settings.mouseMode,
    ],
  );

  const trackpadSensitivity = useDeviceSettingsStore(state => state.trackpadSensitivity);
  const mouseSensitivity = useDeviceSettingsStore(state => state.mouseSensitivity);
  const clampMin = useDeviceSettingsStore(state => state.clampMin);
  const clampMax = useDeviceSettingsStore(state => state.clampMax);
  const blockDelay = useDeviceSettingsStore(state => state.blockDelay);
  const trackpadThreshold = useDeviceSettingsStore(state => state.trackpadThreshold);

  const mouseWheelHandler = useCallback(
    (e: WheelEvent) => {
      if (blockWheelEvent) return;

      // Determine if the wheel event is an accel scroll value
      const isAccel = Math.abs(e.deltaY) >= 100;

      // Calculate the accel scroll value
      const accelScrollValue = e.deltaY / 100;

      // Calculate the no accel scroll value
      const noAccelScrollValue = e.deltaY > 0 ? 1 : (e.deltaY < 0 ? -1 : 0);

      // Get scroll value
      const scrollValue = isAccel ? accelScrollValue : noAccelScrollValue;

      // Apply clamping (i.e. min and max mouse wheel hardware value)
      const clampedScrollValue = Math.max(-127, Math.min(127, scrollValue));

      // Invert the clamped scroll value to match expected behavior
      const invertedScrollValue = -clampedScrollValue;

      send("wheelReport", { wheelY : invertedScrollValue });

      // Apply blocking delay
      setBlockWheelEvent(true);
      setTimeout(() => setBlockWheelEvent(false), blockDelay);
    },
    [
      blockDelay,
      blockWheelEvent,
      clampMax,
      clampMin,
      mouseSensitivity,
      send,
      trackpadSensitivity,
      trackpadThreshold,
    ],
  );

  const resetMousePosition = useCallback(() => {
    sendAbsMouseMovement(0, 0, 0);
  }, [sendAbsMouseMovement]);

  // Keyboard-related
  const handleModifierKeys = useCallback(
    (e: KeyboardEvent, activeModifiers: number[]) => {
      const { shiftKey, ctrlKey, altKey, metaKey } = e;

      const filteredModifiers = activeModifiers.filter(Boolean);

      // Example: activeModifiers = [0x01, 0x02, 0x04, 0x08]
      // Assuming 0x01 = ControlLeft, 0x02 = ShiftLeft, 0x04 = AltLeft, 0x08 = MetaLeft
      return (
        filteredModifiers
          // Shift: Keep if Shift is pressed or if the key isn't a Shift key
          // Example: If shiftKey is true, keep all modifiers
          // If shiftKey is false, filter out 0x02 (ShiftLeft) and 0x20 (ShiftRight)
          .filter(
            modifier =>
              shiftKey ||
              (modifier !== modifiers["ShiftLeft"] &&
                modifier !== modifiers["ShiftRight"]),
          )
          // Ctrl: Keep if Ctrl is pressed or if the key isn't a Ctrl key
          // Example: If ctrlKey is true, keep all modifiers
          // If ctrlKey is false, filter out 0x01 (ControlLeft) and 0x10 (ControlRight)
          .filter(
            modifier =>
              ctrlKey ||
              (modifier !== modifiers["ControlLeft"] &&
                modifier !== modifiers["ControlRight"]),
          )
          // Alt: Keep if Alt is pressed or if the key isn't an Alt key
          // Example: If altKey is true, keep all modifiers
          // If altKey is false, filter out 0x04 (AltLeft)
          //
          // But intentionally do not filter out 0x40 (AltRight) to accomodate
          // Alt Gr (Alt Graph) as a modifier. Oddly, Alt Gr does not declare
          // itself to be an altKey. For example, the KeyboardEvent for
          // Alt Gr + 2 has the following structure:
          // - altKey: false
          // - code:   "Digit2"
          // - type:   [ "keydown" | "keyup" ]
          //
          // For context, filteredModifiers aims to keep track which modifiers
          // are being pressed on the physical keyboard at any point in time.
          // There is logic in the keyUpHandler and keyDownHandler to add and
          // remove 0x40 (AltRight) from the list of new modifiers.
          //
          // But relying on the two handlers alone to track the state of the
          // modifier bears the risk that the key up event for Alt Gr could
          // get lost while the browser window is temporarily out of focus,
          // which means the Alt Gr key state would then be "stuck". At this
          // point, we would need to rely on the user to press Alt Gr again
          // to properly release the state of that modifier.
          .filter(
            modifier =>
              altKey ||
              (modifier !== modifiers["AltLeft"]),
          )
          // Meta: Keep if Meta is pressed or if the key isn't a Meta key
          // Example: If metaKey is true, keep all modifiers
          // If metaKey is false, filter out 0x08 (MetaLeft) and 0x80 (MetaRight)
          .filter(
            modifier =>
              metaKey ||
              (modifier !== modifiers["MetaLeft"] && modifier !== modifiers["MetaRight"]),
          )
      );
    },
    [],
  );

  const keyDownHandler = useCallback(
    async (e: KeyboardEvent) => {
      e.preventDefault();
      const prev = useHidStore.getState();
      let code = e.code;
      const key = e.key;

      // if (document.activeElement?.id !== "videoFocusTrap") {
      //   console.log("KEYUP: Not focusing on the video", document.activeElement);
      //   return;
      // }

      // console.log(document.activeElement);

      setIsNumLockActive(e.getModifierState("NumLock"));
      setIsCapsLockActive(e.getModifierState("CapsLock"));
      setIsScrollLockActive(e.getModifierState("ScrollLock"));

      if (code == "IntlBackslash" && ["`", "~"].includes(key)) {
        code = "Backquote";
      } else if (code == "Backquote" && ["§", "±"].includes(key)) {
        code = "IntlBackslash";
      }

      // Add the key to the active keys
      const newKeys = [...prev.activeKeys, keys[code]].filter(Boolean);

      // Add the modifier to the active modifiers
      const newModifiers = handleModifierKeys(e, [
        ...prev.activeModifiers,
        modifiers[code],
      ]);

      // When pressing the meta key + another key, the key will never trigger a keyup
      // event, so we need to clear the keys after a short delay
      // https://bugs.chromium.org/p/chromium/issues/detail?id=28089
      // https://bugzilla.mozilla.org/show_bug.cgi?id=1299553
      if (e.metaKey) {
        setTimeout(() => {
          const prev = useHidStore.getState();
          sendKeyboardEvent([], newModifiers || prev.activeModifiers);
        }, 10);
      }

      sendKeyboardEvent([...new Set(newKeys)], [...new Set(newModifiers)]);
    },
    [
      setIsNumLockActive,
      setIsCapsLockActive,
      setIsScrollLockActive,
      handleModifierKeys,
      sendKeyboardEvent,
    ],
  );

  const keyUpHandler = useCallback(
    (e: KeyboardEvent) => {
      e.preventDefault();
      const prev = useHidStore.getState();

      setIsNumLockActive(e.getModifierState("NumLock"));
      setIsCapsLockActive(e.getModifierState("CapsLock"));
      setIsScrollLockActive(e.getModifierState("ScrollLock"));

      // Filtering out the key that was just released (keys[e.code])
      const newKeys = prev.activeKeys.filter(k => k !== keys[e.code]).filter(Boolean);

      // Filter out the modifier that was just released
      const newModifiers = handleModifierKeys(
        e,
        prev.activeModifiers.filter(k => k !== modifiers[e.code]),
      );

      sendKeyboardEvent([...new Set(newKeys)], [...new Set(newModifiers)]);
    },
    [
      setIsNumLockActive,
      setIsCapsLockActive,
      setIsScrollLockActive,
      handleModifierKeys,
      sendKeyboardEvent,
    ],
  );

  const videoKeyUpHandler = useCallback((e: KeyboardEvent) => {
    // In fullscreen mode in chrome & safari, the space key is used to pause/play the video
    // there is no way to prevent this, so we need to simply force play the video when it's paused.
    // Fix only works in chrome based browsers.
    if (e.code === "Space") {
      if (videoElm.current?.paused == true) {
        console.log("Force playing video");
        videoElm.current?.play();
      }
    }
  }, []);

  const addStreamToVideoElm = useCallback(
    (mediaStream: MediaStream) => {
      if (!videoElm.current) return;
      const videoElmRefValue = videoElm.current;
      // console.log("Adding stream to video element", videoElmRefValue);
      videoElmRefValue.srcObject = mediaStream;
      updateVideoSizeStore(videoElmRefValue);
    },
    [updateVideoSizeStore],
  );

  useEffect(
    function updateVideoStreamOnNewTrack() {
      if (!peerConnection) return;
      const abortController = new AbortController();
      const signal = abortController.signal;

      peerConnection.addEventListener(
        "track",
        (e: RTCTrackEvent) => {
          // console.log("Adding stream to video element");
          addStreamToVideoElm(e.streams[0]);
        },
        { signal },
      );

      return () => {
        abortController.abort();
      };
    },
    [addStreamToVideoElm, peerConnection],
  );

  useEffect(
    function updateVideoStream() {
      if (!mediaStream) return;
      console.log("Updating video stream from mediaStream");
      // We set the as early as possible
      addStreamToVideoElm(mediaStream);
    },
    [
      setVideoClientSize,
      mediaStream,
      updateVideoSizeStore,
      peerConnection,
      addStreamToVideoElm,
    ],
  );

  // Setup Keyboard Events
  useEffect(
    function setupKeyboardEvents() {
      const abortController = new AbortController();
      const signal = abortController.signal;

      document.addEventListener("keydown", keyDownHandler, { signal });
      document.addEventListener("keyup", keyUpHandler, { signal });

      // eslint-disable-next-line @typescript-eslint/ban-ts-comment
      // @ts-expect-error
      window.clearKeys = () => sendKeyboardEvent([], []);
      window.addEventListener("blur", resetKeyboardState, { signal });
      document.addEventListener("visibilitychange", resetKeyboardState, { signal });

      return () => {
        abortController.abort();
      };
    },
    [keyDownHandler, keyUpHandler, resetKeyboardState, sendKeyboardEvent],
  );

  // Setup Video Event Listeners
  useEffect(
    function setupVideoEventListeners() {
      const videoElmRefValue = videoElm.current;
      if (!videoElmRefValue) return;

      const abortController = new AbortController();
      const signal = abortController.signal;

      // To prevent the video from being paused when the user presses a space in fullscreen mode
      videoElmRefValue.addEventListener("keyup", videoKeyUpHandler, { signal });

      // We need to know when the video is playing to update state and video size
      videoElmRefValue.addEventListener("playing", onVideoPlaying, { signal });

      return () => {
        abortController.abort();
      };
    },
    [
      absMouseMoveHandler,
      resetMousePosition,
      onVideoPlaying,
      mouseWheelHandler,
      videoKeyUpHandler,
    ],
  );

  // Setup Absolute Mouse Events
  useEffect(
    function setAbsoluteMouseModeEventListeners() {
      const videoElmRefValue = videoElm.current;
      if (!videoElmRefValue) return;

      if (settings.mouseMode !== "absolute") return;

      const abortController = new AbortController();
      const signal = abortController.signal;

      videoElmRefValue.addEventListener("mousemove", absMouseMoveHandler, { signal });
      videoElmRefValue.addEventListener("pointerdown", absMouseMoveHandler, { signal });
      videoElmRefValue.addEventListener("pointerup", absMouseMoveHandler, { signal });
      videoElmRefValue.addEventListener("wheel", mouseWheelHandler, {
        signal,
        passive: true,
      });

      // Reset the mouse position when the window is blurred or the document is hidden
      const local = resetMousePosition;
      window.addEventListener("blur", local, { signal });
      document.addEventListener("visibilitychange", local, { signal });
      const preventContextMenu = (e: MouseEvent) => e.preventDefault();
      videoElmRefValue.addEventListener("contextmenu", preventContextMenu, { signal });

      return () => {
        abortController.abort();
      };
    },
    [absMouseMoveHandler, mouseWheelHandler, resetMousePosition, settings.mouseMode],
  );

  // Setup Relative Mouse Events
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(
    function setupRelativeMouseEventListeners() {
      if (settings.mouseMode !== "relative") return;
      // Relative mouse mode should only be active if the pointer lock is active and Pointer Lock is possible

      const videoElmRefValue = videoElm.current;
      if (!videoElmRefValue) return;

      const abortController = new AbortController();
      const signal = abortController.signal;

      videoElmRefValue.addEventListener("mousemove", relMouseMoveHandler, { signal });
      videoElmRefValue.addEventListener("pointerdown", relMouseMoveHandler, { signal });
      videoElmRefValue.addEventListener("pointerup", relMouseMoveHandler, { signal });
      videoElmRefValue.addEventListener(
        "click",
        () => {
          if (isPointerLockPossible && !document.pointerLockElement) {
            requestPointerLock();
          }
        },
        { signal },
      );
      videoElmRefValue.addEventListener("wheel", mouseWheelHandler, {
        signal,
        passive: true,
      });

      const preventContextMenu = (e: MouseEvent) => e.preventDefault();
      videoElmRefValue.addEventListener("contextmenu", preventContextMenu, { signal });

      return () => {
        abortController.abort();
      };
    },
    [
      settings.mouseMode,
      relMouseMoveHandler,
      mouseWheelHandler,
      disableVideoFocusTrap,
      requestPointerLock,
      isPointerLockPossible,
      isPointerLockActive,
    ],
  );

  const hasNoAutoPlayPermissions = useMemo(() => {
    if (peerConnection?.connectionState !== "connected") return false;
    if (isPlaying) return false;
    if (hdmiError) return false;
    if (videoHeight === 0 || videoWidth === 0) return false;
    return true;
  }, [peerConnection?.connectionState, isPlaying, hdmiError, videoHeight, videoWidth]);

  const showPointerLockBar = useMemo(() => {
    if (settings.mouseMode !== "relative") return false;
    if (!isPointerLockPossible) return false;
    if (isPointerLockActive) return false;
    if (isVideoLoading) return false;
    if (!isPlaying) return false;
    if (videoHeight === 0 || videoWidth === 0) return false;
    return true;
  }, [
    settings.mouseMode,
    isPointerLockPossible,
    isPointerLockActive,
    isVideoLoading,
    isPlaying,
    videoHeight,
    videoWidth,
  ]);

  return (
    <div className="grid h-full w-full grid-rows-(--grid-layout)">
      <div className="flex min-h-[39.5px] flex-col">
        <div className="flex flex-col">
          <fieldset
            disabled={peerConnection?.connectionState !== "connected"}
            className="contents"
          >
            <Actionbar requestFullscreen={requestFullscreen} />
            <MacroBar />
          </fieldset>
        </div>
      </div>

      <div ref={containerRef} className="h-full overflow-hidden">
        <div className="relative h-full">
          <div
            className={cx(
              "absolute inset-0 -z-0 bg-blue-50/40 opacity-80 dark:bg-slate-800/40",
              "bg-[radial-gradient(var(--color-blue-300)_0.5px,transparent_0.5px),radial-gradient(var(--color-blue-300)_0.5px,transparent_0.5px)] dark:bg-[radial-gradient(var(--color-slate-700)_0.5px,transparent_0.5px),radial-gradient(var(--color-slate-700)_0.5px,transparent_0.5px)]",
              "bg-position-[0_0,10px_10px]",
              "bg-size-[20px_20px]",
            )}
          />
          <div className="flex h-full flex-col">
            <div className="relative grow overflow-hidden">
              <div className="flex h-full flex-col">
                <div className="grid grow grid-rows-(--grid-bodyFooter) overflow-hidden">
                  <div className="relative mx-4 my-2 flex items-center justify-center overflow-hidden">
                    <div className="relative flex h-full w-full items-center justify-center">
                      <div className="relative inline-block">
                        {/* In relative mouse mode and under https, we enable the pointer lock, and to do so we need a bar to show the user to click on the video to enable mouse control */}
                        <PointerLockBar show={showPointerLockBar} />
                        <video
                          ref={videoElm}
                          autoPlay={true}
                          controls={false}
                          onPlaying={onVideoPlaying}
                          onPlay={onVideoPlaying}
                          muted={true}
                          playsInline
                          disablePictureInPicture
                          controlsList="nofullscreen"
                          className={cx(
                            "z-30 max-h-full min-h-[384px] min-w-[512px] max-w-full bg-black/50 object-contain transition-all duration-1000",
                            {
                              "cursor-none": settings.isCursorHidden,
                              "opacity-0":
                                isVideoLoading ||
                                hdmiError ||
                                peerConnectionState !== "connected",
                              "opacity-60!": showPointerLockBar,
                              "animate-slideUpFade border border-slate-800/30 shadow-xs dark:border-slate-300/20":
                                isPlaying,
                            },
                          )}
                        />
                        {peerConnection?.connectionState == "connected" && (
                          <div
                            style={{ animationDuration: "500ms" }}
                            className="pointer-events-none absolute inset-0 flex animate-slideUpFade items-center justify-center"
                          >
                            <div className="relative h-full w-full rounded-md">
                              <LoadingVideoOverlay show={isVideoLoading} />
                              <HDMIErrorOverlay show={hdmiError} hdmiState={hdmiState} />
                              <NoAutoplayPermissionsOverlay
                                show={hasNoAutoPlayPermissions}
                                onPlayClick={() => {
                                  videoElm.current?.play();
                                }}
                              />
                            </div>
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                  <VirtualKeyboard />
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
      <div>
        <InfoBar />
      </div>
    </div>
  );
}
