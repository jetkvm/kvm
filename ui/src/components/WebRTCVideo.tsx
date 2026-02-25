import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useResizeObserver } from "usehooks-ts";

import { cx } from "@/cva.config";
import { isWindows } from "@/utils";
import useKeyboard from "@hooks/useKeyboard";
import useMouse from "@hooks/useMouse";
import { useRTCStore, useSettingsStore, useUiStore, useVideoStore } from "@hooks/stores";
import VirtualKeyboard from "@components/VirtualKeyboard";
import Actionbar from "@components/ActionBar";
import MacroBar from "@components/MacroBar";
import InfoBar from "@components/InfoBar";
import {
  HDMIErrorOverlay,
  KeyboardCaptureBar,
  LoadingVideoOverlay,
  NoAutoplayPermissionsOverlay,
  PointerLockBar,
} from "@components/VideoOverlay";
import { keys } from "@/keyboardMappings";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

export default function WebRTCVideo({ hasConnectionIssues }: { hasConnectionIssues: boolean }) {
  // Video and stream related refs and states
  const videoElm = useRef<HTMLVideoElement>(null);
  const fullscreenContainerRef = useRef<HTMLDivElement>(null);
  const { mediaStream, peerConnectionState } = useRTCStore();
  const [isPlaying, setIsPlaying] = useState(false);
  const [isPointerLockActive, setIsPointerLockActive] = useState(false);
  const { setIsKeyboardLockActive } = useUiStore();

  const isPointerLockPossible =
    window.location.protocol === "https:" || window.location.hostname === "localhost";

  // macOS detection for Meta key fix
  const isMacClient = useMemo(() => /Mac|iPhone|iPad|iPod/.test(navigator.userAgent), []);

  // Store hooks
  const settings = useSettingsStore();
  const { handleKeyPress, resetKeyboardState } = useKeyboard();
  const {
    getRelMouseMoveHandler,
    getAbsMouseMoveHandler,
    getMouseWheelHandler,
    resetMousePosition,
  } = useMouse();
  const {
    setClientSize: setVideoClientSize,
    setSize: setVideoSize,
    width: videoWidth,
    height: videoHeight,
    clientWidth: videoClientWidth,
    clientHeight: videoClientHeight,
    hdmiState,
    setVideoElement,
  } = useVideoStore();

  // Video enhancement settings
  const { videoSaturation, videoBrightness, videoContrast } = useSettingsStore();

  // RTC related states
  const { peerConnection } = useRTCStore();

  // HDMI and UI states
  const hdmiError = ["no_lock", "no_signal", "out_of_range"].includes(hdmiState);
  const isVideoLoading = !isPlaying;

  // Video-related
  const handleResize = useCallback(
    ({ width, height }: { width: number | undefined; height: number | undefined }) => {
      if (!videoElm.current) return;
      // Do something with width and height, e.g.:
      setVideoClientSize(width || 0, height || 0);
      setVideoSize(videoElm.current.videoWidth, videoElm.current.videoHeight);
    },
    [setVideoClientSize, setVideoSize],
  );

  // AltGr Fix for Windows Clients
  const altGrSyntheticThresholdMs = 3;
  const isWindowsClient = useMemo(() => isWindows(), []);
  const lastKeyDownRef = useRef<{ hidKey: number; time: number } | null>(null);
  const altGrLoopRef = useRef(false);

  useResizeObserver({
    ref: videoElm as React.RefObject<HTMLElement>,
    onResize: handleResize,
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
    [updateVideoSizeStore],
  );

  // Store video element reference for E2E test hooks
  useEffect(
    function storeVideoElementRef() {
      setVideoElement(videoElm.current);
      return () => setVideoElement(null);
    },
    [setVideoElement],
  );

  // Pointer lock and keyboard lock related
  const isFullscreenEnabled = document.fullscreenEnabled;

  const checkNavigatorPermissions = useCallback(async (permissionName: string) => {
    if (!navigator || !navigator.permissions || !navigator.permissions.query) {
      return false; // if can't query permissions, assume NOT granted
    }

    try {
      const name = permissionName as PermissionName;
      const { state } = await navigator.permissions.query({ name });
      return state === "granted";
    } catch {
      // ignore errors
    }
    return false; // if query fails, assume NOT granted
  }, []);

  const requestPointerLock = useCallback(async () => {
    if (!isPointerLockPossible || videoElm.current === null || document.pointerLockElement) return;

    const isPointerLockGranted = await checkNavigatorPermissions("pointer-lock");

    if (isPointerLockGranted && settings.mouseMode === "relative") {
      try {
        await videoElm.current.requestPointerLock();
      } catch {
        // ignore errors
      }
    }
  }, [checkNavigatorPermissions, isPointerLockPossible, settings.mouseMode]);

  const requestKeyboardLock = useCallback(async () => {
    if (!navigator || !("keyboard" in navigator)) return;

    try {
      // @ts-expect-error - keyboard lock is not supported in all browsers
      await navigator.keyboard.lock();
      console.debug("Keyboard lock acquired");
      setIsKeyboardLockActive(true);
    } catch (e) {
      console.debug("Keyboard lock not available:", e);
    }
  }, [setIsKeyboardLockActive]);

  const releaseKeyboardLock = useCallback(async () => {
    if (!navigator || !("keyboard" in navigator)) return;

    try {
      // @ts-expect-error - keyboard unlock is not supported in all browsers
      navigator.keyboard.unlock();
      console.debug("Keyboard lock released");
    } catch {
      // ignore errors
    }
    setIsKeyboardLockActive(false);
  }, [setIsKeyboardLockActive]);

  useEffect(() => {
    if (!isPointerLockPossible || !videoElm.current) return;

    const handlePointerLockChange = () => {
      if (document.pointerLockElement) {
        notifications.success(m.video_pointer_lock_enabled());
        setIsPointerLockActive(true);
      } else {
        notifications.success(m.video_pointer_lock_disabled());
        setIsPointerLockActive(false);
      }
    };

    const abortController = new AbortController();
    const signal = abortController.signal;

    document.addEventListener("pointerlockchange", handlePointerLockChange, { signal });

    return () => {
      abortController.abort();
    };
  }, [isPointerLockPossible]);

  const requestFullscreen = useCallback(async () => {
    if (!isFullscreenEnabled || !fullscreenContainerRef.current) return;

    await requestPointerLock();

    await fullscreenContainerRef.current.requestFullscreen({
      navigationUI: "show",
    });
    // keyboard.lock() is called in the fullscreenchange handler below,
    // after fullscreen is confirmed active (required by the API)
  }, [isFullscreenEnabled, requestPointerLock]);

  // Handle fullscreen enter/exit: acquire or release keyboard lock accordingly
  useEffect(() => {
    if (!videoElm.current) return;

    const handleFullscreenChange = () => {
      if (document.fullscreenElement) {
        // Entering fullscreen: always acquire keyboard lock
        requestKeyboardLock();
      } else {
        // Exiting fullscreen: re-acquire lock if capture mode is on, otherwise release
        if (settings.keyboardCaptureMode) {
          requestKeyboardLock();
        } else {
          releaseKeyboardLock();
        }
      }
    };

    const abortController = new AbortController();
    document.addEventListener("fullscreenchange", handleFullscreenChange, {
      signal: abortController.signal,
    });

    return () => {
      abortController.abort();
    };
  }, [releaseKeyboardLock, requestKeyboardLock, settings.keyboardCaptureMode]);

  // Sync keyboard lock state with capture mode setting
  useEffect(
    function syncKeyboardCaptureMode() {
      if (settings.keyboardCaptureMode) {
        requestKeyboardLock();
      } else if (!document.fullscreenElement) {
        // Only release if not in fullscreen (fullscreen manages its own lock)
        releaseKeyboardLock();
      }
    },
    [settings.keyboardCaptureMode, requestKeyboardLock, releaseKeyboardLock],
  );

  const absMouseMoveHandler = useMemo(
    () =>
      getAbsMouseMoveHandler({
        videoClientWidth,
        videoClientHeight,
        videoWidth,
        videoHeight,
      }),
    [getAbsMouseMoveHandler, videoClientWidth, videoClientHeight, videoWidth, videoHeight],
  );

  const relMouseMoveHandler = useMemo(() => getRelMouseMoveHandler(), [getRelMouseMoveHandler]);

  const mouseWheelHandler = useMemo(() => getMouseWheelHandler(), [getMouseWheelHandler]);

  function getAdjustedKeyCode(e: KeyboardEvent) {
    const key = e.key;
    let code = e.code;

    if (code == "IntlBackslash" && ["`", "~"].includes(key)) {
      code = "Backquote";
    } else if (code == "Backquote" && ["§", "±"].includes(key)) {
      code = "IntlBackslash";
    }
    // For Japanese 106/109
    else if (code === "IntlYen") {
      code = "Yen";
    } else if (code === "IntlRo") {
      code = "KeyRO";
    } else if (code === "Convert") {
      code = "Henkan";
    } else if (code === "NonConvert") {
      code = "Muhenkan";
    } else if (key === "Shift" && code === "") {
      // Microsoft IME fix
      code = "ShiftRight";
    }

    return code;
  }

  const keyDownHandler = useCallback(
    (e: KeyboardEvent) => {
      e.preventDefault();
      const code = getAdjustedKeyCode(e);
      const hidKey = keys[code];

      if (hidKey === undefined) {
        console.warn(`Key down not mapped: ${code}`);
        return;
      }

      // Detect Windows synthetic AltGr (CtrlLeft then AltRight within ~3ms) and cancel the synthetic Ctrl
      if (isWindowsClient) {
        // Buffer ControlLeft briefly; if no AltRight follows within the threshold, treat it as a real ControlLeft press.
        if (hidKey === keys.ControlLeft) {
          const controlLeftDownTime = e.timeStamp;
          lastKeyDownRef.current = { hidKey, time: controlLeftDownTime };
          setTimeout(() => {
            if (
              lastKeyDownRef.current?.hidKey === keys.ControlLeft &&
              lastKeyDownRef.current.time === controlLeftDownTime
            ) {
              lastKeyDownRef.current = null;
              handleKeyPress(keys.ControlLeft, true);
            }
          }, altGrSyntheticThresholdMs);
          return;
        }

        // If AltRight arrives shortly after ControlLeft, treat the pair as AltGr and cancel the pending ControlLeft.
        if (
          hidKey === keys.AltRight &&
          lastKeyDownRef.current?.hidKey === keys.ControlLeft &&
          e.timeStamp - lastKeyDownRef.current.time <= altGrSyntheticThresholdMs
        ) {
          altGrLoopRef.current = true;
          lastKeyDownRef.current = null;
        }

        // Microsoft IME fix:
        // Effective keydown events are consumed by IME (reported as "Process"),
        // so we handle the full press/release cycle in the keyup handler instead.
        if (["Zenkaku", "Hankaku", "ZenkakuHankaku"].includes(e.key)) {
          return;
        }
      }

      console.debug(`Key down: ${hidKey}`);
      handleKeyPress(hidKey, true);
    },
    [handleKeyPress, isWindowsClient],
  );

  const keyUpHandler = useCallback(
    async (e: KeyboardEvent) => {
      e.preventDefault();
      const code = getAdjustedKeyCode(e);
      const hidKey = keys[code];

      if (hidKey === undefined) {
        console.warn(`Key up not mapped: ${code}`);
        return;
      }

      if (isWindowsClient) {
        // On Windows, handle ControlLeft specially to preserve FIFO semantics with AltGr buffering.
        if (hidKey === keys.ControlLeft) {
          // Synthetic AltGr ControlLeft: never sent a down, swallow the release as well.
          if (altGrLoopRef.current) {
            altGrLoopRef.current = false;
            return;
          }

          // Very fast real Ctrl tap: flush the pending down before the up.
          if (lastKeyDownRef.current?.hidKey === keys.ControlLeft) {
            handleKeyPress(keys.ControlLeft, true);
          }

          lastKeyDownRef.current = null;
        }

        // Microsoft IME fix:
        // Synthesize the missing keydown event to ensure a complete key press cycle.
        if (["Zenkaku", "Hankaku", "ZenkakuHankaku"].includes(e.key)) {
          console.debug(`Synthesizing missed key down for IME key: ${e.key}`);
          handleKeyPress(hidKey, true);
        }
      }

      console.debug(`Key up: ${hidKey}`);
      handleKeyPress(hidKey, false);

      // PiKVM-style fix: When Meta is released on macOS, release all keys to clean up
      // stuck companion keys (Chrome doesn't fire their keyup events)
      // https://bugs.chromium.org/p/chromium/issues/detail?id=28089
      if (isMacClient && (code === "MetaLeft" || code === "MetaRight")) {
        resetKeyboardState();
      }
    },
    [handleKeyPress, isMacClient, isWindowsClient, resetKeyboardState],
  );

  const videoKeyUpHandler = useCallback((e: KeyboardEvent) => {
    if (!videoElm.current) return;

    // In fullscreen mode in chrome & safari, the space key is used to pause/play the video
    // there is no way to prevent this, so we need to simply force play the video when it's paused.
    // Fix only works in chrome based browsers.
    if (e.code === "Space") {
      if (videoElm.current.paused) {
        console.debug("Force playing video");
        videoElm.current.play();
      }
    }
  }, []);

  const addStreamToVideoElm = useCallback(
    (mediaStream: MediaStream) => {
      if (!videoElm.current) return;
      const videoElmRefValue = videoElm.current;
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
      // We set the as early as possible
      addStreamToVideoElm(mediaStream);
    },
    [addStreamToVideoElm, mediaStream],
  );

  // Setup Keyboard Events
  useEffect(
    function setupKeyboardEvents() {
      const abortController = new AbortController();
      const signal = abortController.signal;

      document.addEventListener("keydown", keyDownHandler, { signal });
      document.addEventListener("keyup", keyUpHandler, { signal });

      window.addEventListener("blur", resetKeyboardState, { signal });
      document.addEventListener("visibilitychange", resetKeyboardState, { signal });

      return () => {
        abortController.abort();
      };
    },
    [keyDownHandler, keyUpHandler, resetKeyboardState],
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
    [onVideoPlaying, videoKeyUpHandler],
  );

  // Setup Mouse Events
  useEffect(
    function setMouseModeEventListeners() {
      const videoElmRefValue = videoElm.current;
      if (!videoElmRefValue) return;

      const isRelativeMouseMode = settings.mouseMode === "relative";
      const mouseHandler = isRelativeMouseMode ? relMouseMoveHandler : absMouseMoveHandler;

      const abortController = new AbortController();
      const signal = abortController.signal;

      videoElmRefValue.addEventListener("mousemove", mouseHandler, { signal });
      videoElmRefValue.addEventListener("pointerdown", mouseHandler, { signal });
      videoElmRefValue.addEventListener("pointerup", mouseHandler, { signal });
      videoElmRefValue.addEventListener("wheel", mouseWheelHandler, {
        signal,
        passive: true,
      });

      if (isRelativeMouseMode) {
        videoElmRefValue.addEventListener(
          "click",
          () => {
            if (isPointerLockPossible && !isPointerLockActive && !document.pointerLockElement) {
              requestPointerLock();
            }
          },
          { signal },
        );
      } else {
        // Reset the mouse position when the window is blurred or the document is hidden
        window.addEventListener("blur", resetMousePosition, { signal });
        document.addEventListener("visibilitychange", resetMousePosition, { signal });
      }

      const preventContextMenu = (e: MouseEvent) => e.preventDefault();
      videoElmRefValue.addEventListener("contextmenu", preventContextMenu, { signal });

      return () => {
        abortController.abort();
      };
    },
    [
      isPointerLockActive,
      isPointerLockPossible,
      requestPointerLock,
      absMouseMoveHandler,
      relMouseMoveHandler,
      mouseWheelHandler,
      resetMousePosition,
      settings.mouseMode,
    ],
  );

  const containerRef = useRef<HTMLDivElement>(null);

  const hasNoAutoPlayPermissions = useMemo(() => {
    if (peerConnection?.connectionState !== "connected") return false;
    if (isPlaying) return false;
    if (hdmiError) return false;
    if (videoHeight === 0 || videoWidth === 0) return false;
    return true;
  }, [hdmiError, isPlaying, peerConnection?.connectionState, videoHeight, videoWidth]);

  const showKeyboardCaptureBar = useMemo(() => {
    if (!settings.keyboardCaptureMode) return false;
    if (isVideoLoading) return false;
    if (!isPlaying) return false;
    if (videoHeight === 0 || videoWidth === 0) return false;
    return true;
  }, [settings.keyboardCaptureMode, isPlaying, isVideoLoading, videoHeight, videoWidth]);

  const showPointerLockBar = useMemo(() => {
    if (settings.mouseMode !== "relative") return false;
    if (!isPointerLockPossible) return false;
    if (isPointerLockActive) return false;
    if (isVideoLoading) return false;
    if (!isPlaying) return false;
    if (videoHeight === 0 || videoWidth === 0) return false;
    return true;
  }, [
    isPlaying,
    isPointerLockActive,
    isPointerLockPossible,
    isVideoLoading,
    settings.mouseMode,
    videoHeight,
    videoWidth,
  ]);

  // Conditionally set the filter style so we don't fallback to software rendering if these values are default of 1.0
  const videoStyle = useMemo(() => {
    const isDefault = videoSaturation === 1.0 && videoBrightness === 1.0 && videoContrast === 1.0;
    return isDefault
      ? {} // No filter if all settings are default (1.0)
      : {
          filter: `saturate(${videoSaturation}) brightness(${videoBrightness}) contrast(${videoContrast})`,
        };
  }, [videoSaturation, videoBrightness, videoContrast]);

  return (
    <div className="grid h-full w-full grid-rows-(--grid-layout)">
      <div className="flex min-h-[39.5px] flex-col">
        <div className="flex flex-col">
          <fieldset disabled={peerConnection?.connectionState !== "connected"} className="contents">
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
                  {/* In relative mouse mode and under https, we enable the pointer lock, and to do so we need a bar to show the user to click on the video to enable mouse control */}
                  <PointerLockBar show={showPointerLockBar} />
                  <KeyboardCaptureBar show={showKeyboardCaptureBar && !showPointerLockBar} />
                  <div className="relative mx-4 my-2 flex items-center justify-center overflow-hidden">
                    <div
                      ref={fullscreenContainerRef}
                      className="relative flex h-full w-full items-center justify-center"
                    >
                      <video
                        ref={videoElm}
                        autoPlay
                        controls={false}
                        onPlaying={onVideoPlaying}
                        onPlay={onVideoPlaying}
                        muted
                        playsInline
                        disablePictureInPicture
                        controlsList="nofullscreen"
                        style={videoStyle}
                        className={cx(
                          "max-h-full max-w-full bg-black/50 object-contain transition-all duration-1000 sm:min-h-[384px] sm:min-w-[512px]",
                          {
                            "cursor-none": settings.isCursorHidden,
                            "opacity-0!":
                              isVideoLoading ||
                              hdmiError ||
                              hasConnectionIssues ||
                              peerConnectionState !== "connected",
                            "opacity-60!": showPointerLockBar,
                            "animate-slideUpFade border border-slate-800/30 shadow-xs dark:border-slate-300/20":
                              isPlaying,
                          },
                        )}
                      />
                      {peerConnection?.connectionState == "connected" && !hasConnectionIssues && (
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
