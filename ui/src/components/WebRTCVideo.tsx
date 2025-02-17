import { useCallback, useEffect, useRef, useState } from "react";
import {
  useHidStore,
  useMouseStore,
  useRTCStore,
  useSettingsStore,
  useUiStore,
  useVideoStore,
} from "@/hooks/stores";
import { keys, modifiers } from "@/keyboardMappings";
import { useResizeObserver } from "@/hooks/useResizeObserver";
import { cx } from "@/cva.config";
import VirtualKeyboard from "@components/VirtualKeyboard";
import Actionbar from "@components/ActionBar";
import InfoBar from "@components/InfoBar";
import useKeyboard from "@/hooks/useKeyboard";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { ConnectionErrorOverlay, HDMIErrorOverlay, LoadingOverlay } from "./VideoOverlay";

export default function WebRTCVideo() {
  // Video and stream related refs and states
  const videoElm = useRef<HTMLVideoElement>(null);
  const mediaStream = useRTCStore(state => state.mediaStream);
  const [isPlaying, setIsPlaying] = useState(false);

  // Store hooks
  const settings = useSettingsStore();
  const { sendKeyboardEvent, resetKeyboardState } = useKeyboard();
  const setMousePosition = useMouseStore(state => state.setMousePosition);
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
  const peerConnectionState = useRTCStore(state => state.peerConnectionState);

  // HDMI and UI states
  const hdmiState = useVideoStore(state => state.hdmiState);
  const hdmiError = ["no_lock", "no_signal", "out_of_range"].includes(hdmiState);
  const isLoading = !hdmiError && !isPlaying;
  const isConnectionError = ["error", "failed", "disconnected"].includes(
    peerConnectionState || "",
  );

  // Keyboard related states
  const { setIsNumLockActive, setIsCapsLockActive, setIsScrollLockActive } =
    useHidStore();

  // Misc states and hooks
  const [blockWheelEvent, setBlockWheelEvent] = useState(false);
  const [send] = useJsonRpc();

  // Video-related
  useResizeObserver({
    ref: videoElm,
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
    videoElm.current && updateVideoSizeStore(videoElm.current);
  }, [updateVideoSizeStore]);

  // On mount, get the video size
  useEffect(
    function updateVideoSizeOnMount() {
      videoElm.current && updateVideoSizeStore(videoElm.current);
    },
    [setVideoClientSize, updateVideoSizeStore, setVideoSize],
  );

  // Mouse-related
  const sendMouseMovement = useCallback(
    (x: number, y: number, buttons: number) => {
      send("absMouseReport", { x, y, buttons });

      // We set that for the debug info bar
      setMousePosition(x, y);
    },
    [send, setMousePosition],
  );

  const mouseMoveHandler = useCallback(
    (e: MouseEvent) => {
      if (!videoClientWidth || !videoClientHeight) return;
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
      sendMouseMovement(x, y, buttons);
    },
    [sendMouseMovement, videoClientHeight, videoClientWidth, videoWidth, videoHeight],
  );

  const mouseWheelHandler = useCallback(
    (e: WheelEvent) => {
      if (blockWheelEvent) return;
      e.preventDefault();

      // Define a scaling factor to adjust scrolling sensitivity
      const scrollSensitivity = 0.8; // Adjust this value to change scroll speed

      // Calculate the scroll value
      const scroll = e.deltaY * scrollSensitivity;

      // Clamp the scroll value to a reasonable range (e.g., -15 to 15)
      const clampedScroll = Math.max(-4, Math.min(4, scroll));

      // Round to the nearest integer
      const roundedScroll = Math.round(clampedScroll);

      // Invert the scroll value to match expected behavior
      const invertedScroll = -roundedScroll;

      console.log("wheelReport", { wheelY: invertedScroll });
      send("wheelReport", { wheelY: invertedScroll });

      setBlockWheelEvent(true);
      setTimeout(() => setBlockWheelEvent(false), 50);
    },
    [blockWheelEvent, send],
  );

  const resetMousePosition = useCallback(() => {
    sendMouseMovement(0, 0, 0);
  }, [sendMouseMovement]);

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
          // If altKey is false, filter out 0x04 (AltLeft) and 0x40 (AltRight)
          .filter(
            modifier =>
              altKey ||
              (modifier !== modifiers["AltLeft"] && modifier !== modifiers["AltRight"]),
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
      console.log(document.activeElement);

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

      // if (document.activeElement?.id !== "videoFocusTrap") {
      //   console.log("KEYUP: Not focusing on the video", document.activeElement);
      //   return;
      // }

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

  // Effect hooks
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

  useEffect(
    function setupVideoEventListeners() {
      let videoElmRefValue = null;
      if (!videoElm.current) return;
      videoElmRefValue = videoElm.current;
      const abortController = new AbortController();
      const signal = abortController.signal;

      videoElmRefValue.addEventListener("mousemove", mouseMoveHandler, { signal });
      videoElmRefValue.addEventListener("pointerdown", mouseMoveHandler, { signal });
      videoElmRefValue.addEventListener("pointerup", mouseMoveHandler, { signal });

      videoElmRefValue.addEventListener("wheel", mouseWheelHandler, { signal });
      videoElmRefValue.addEventListener(
        "contextmenu",
        (e: MouseEvent) => e.preventDefault(),
        { signal },
      );
      videoElmRefValue.addEventListener("playing", onVideoPlaying, { signal });

      const local = resetMousePosition;
      window.addEventListener("blur", local, { signal });
      document.addEventListener("visibilitychange", local, { signal });

      return () => {
        if (videoElmRefValue) abortController.abort();
      };
    },
    [mouseMoveHandler, resetMousePosition, onVideoPlaying, mouseWheelHandler],
  );

  useEffect(
    function updateVideoStream() {
      if (!mediaStream) return;
      if (!videoElm.current) return;
      if (peerConnection?.iceConnectionState !== "connected") return;

      setTimeout(() => {
        if (videoElm?.current) {
          videoElm.current.srcObject = mediaStream;
        }
      }, 0);
      updateVideoSizeStore(videoElm.current);
    },
    [
      setVideoClientSize,
      setVideoSize,
      mediaStream,
      updateVideoSizeStore,
      peerConnection?.iceConnectionState,
    ],
  );

  // Focus trap management
  const setDisableVideoFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const sidebarView = useUiStore(state => state.sidebarView);
  useEffect(() => {
    setTimeout(function () {
      if (["connection-stats", "system"].includes(sidebarView ?? "")) {
        // Reset keyboard state. Incase the user is pressing a key while enabling the sidebar
        sendKeyboardEvent([], []);
        setDisableVideoFocusTrap(true);

        // For some reason, the focus trap is not disabled immediately
        // so we need to blur the active element
        // (document.activeElement as HTMLElement)?.blur();
        console.log("Just disabled focus trap");
      } else {
        setDisableVideoFocusTrap(false);
      }
    }, 300);
  }, [sendKeyboardEvent, setDisableVideoFocusTrap, sidebarView]);

  return (
    <div className="grid w-full h-full grid-rows-layout">
      <div className="min-h-[39.5px]">
        <fieldset disabled={peerConnectionState !== "connected"}>
          <Actionbar
            requestFullscreen={async () =>
              videoElm.current?.requestFullscreen({
                navigationUI: "show",
              })
            }
          />
        </fieldset>
      </div>

      <div className="h-full overflow-hidden">
        <div className="relative h-full">
          <div
            className={cx(
              "absolute inset-0 bg-blue-50/40 dark:bg-slate-800/40 opacity-80",
              "[background-image:radial-gradient(theme(colors.blue.300)_0.5px,transparent_0.5px),radial-gradient(theme(colors.blue.300)_0.5px,transparent_0.5px)] dark:[background-image:radial-gradient(theme(colors.slate.700)_0.5px,transparent_0.5px),radial-gradient(theme(colors.slate.700)_0.5px,transparent_0.5px)]",
              "[background-position:0_0,10px_10px]",
              "[background-size:20px_20px]",
            )}
          />
          <div className="flex flex-col h-full">
            <div className="relative flex-grow overflow-hidden">
              <div className="flex flex-col h-full">
                <div className="grid flex-grow overflow-hidden grid-rows-bodyFooter">
                  <div className="relative flex items-center justify-center mx-4 my-2 overflow-hidden">
                    <div className="relative flex items-center justify-center w-full h-full">
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
                          "outline-50 max-h-full max-w-full object-contain transition-all duration-1000",
                          {
                            "cursor-none": settings.isCursorHidden,
                            "opacity-0": isLoading || isConnectionError || hdmiError,
                            "animate-slideUpFade border border-slate-800/30 dark:border-slate-300/20 opacity-0 shadow":
                              isPlaying,
                          },
                        )}
                      />
                      <div
                        style={{ animationDuration: "500ms" }}
                        className="absolute inset-0 flex items-center justify-center opacity-0 pointer-events-none animate-slideUpFade"
                      >
                        <div className="relative h-full max-h-[720px] w-full max-w-[1280px] rounded-md">
                          <LoadingOverlay show={isLoading} />
                          <ConnectionErrorOverlay show={isConnectionError} />
                          <HDMIErrorOverlay show={hdmiError} hdmiState={hdmiState} />
                        </div>
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
