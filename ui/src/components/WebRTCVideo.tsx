import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  useDeviceSettingsStore,
  useHidStore,
  useMouseStore,
  useRTCStore,
  useSettingsStore,
  useVideoStore,
  useKeyboardMappingsStore,
} from "@/hooks/stores";
import { useResizeObserver } from "@/hooks/useResizeObserver";
import { cx } from "@/cva.config";
import VirtualKeyboard from "@components/VirtualKeyboard";
import Actionbar from "@components/ActionBar";
import MacroBar from "@/components/MacroBar";
import InfoBar from "@components/InfoBar";
import useKeyboard from "@/hooks/useKeyboard";
import { useJsonRpc } from "@/hooks/useJsonRpc";

import {
  HDMIErrorOverlay,
  LoadingVideoOverlay,
  NoAutoplayPermissionsOverlay,
} from "./VideoOverlay";

// TODO Implement keyboard lock API to resolve #127
// https://developer.chrome.com/docs/capabilities/web-apis/keyboard-lock
// An appropriate error message will need to be displayed in order to alert users to browser compatibility issues.
// This requires TLS, waiting on TLS support.


// TODO Implement keyboard mapping setup in initial JetKVM setup
export default function WebRTCVideo() {
  const [keys, setKeys] = useState(useKeyboardMappingsStore.keys);
  const [chars, setChars] = useState(useKeyboardMappingsStore.chars);
  const [modifiers, setModifiers] = useState(useKeyboardMappingsStore.modifiers);

  // This map is used to maintain consistency between localised key mappings
  const activeKeyState = useRef<Map<string, { mappedKey: string; modifiers: { shift: boolean, altLeft?: boolean, altRight?: boolean}; }>>(new Map());

  useEffect(() => {
    const unsubscribeKeyboardStore = useKeyboardMappingsStore.subscribe(() => {
      setKeys(useKeyboardMappingsStore.keys);
      setChars(useKeyboardMappingsStore.chars);
      setModifiers(useKeyboardMappingsStore.modifiers);
    });
    return unsubscribeKeyboardStore; // Cleanup on unmount
  }, []); 

  // Video and stream related refs and states
  const videoElm = useRef<HTMLVideoElement>(null);
  const mediaStream = useRTCStore(state => state.mediaStream);
  const [isPlaying, setIsPlaying] = useState(false);
  const peerConnectionState = useRTCStore(state => state.peerConnectionState);

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

  // console.log("peerConnection?.connectionState", peerConnection?.connectionState);

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
    if (videoElm.current) updateVideoSizeStore(videoElm.current);
  }, [updateVideoSizeStore]);

  // On mount, get the video size
  useEffect(
    function updateVideoSizeOnMount() {
      if (videoElm.current) updateVideoSizeStore(videoElm.current);
    },
    [setVideoClientSize, updateVideoSizeStore, setVideoSize],
  );

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

      // Send mouse movement
      const { buttons } = e;
      sendRelMouseMovement(e.movementX, e.movementY, buttons);
    },
    [sendRelMouseMovement, settings.mouseMode],
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

      // Determine if the wheel event is from a trackpad or a mouse wheel
      const isTrackpad = Math.abs(e.deltaY) < trackpadThreshold;

      // Apply appropriate sensitivity based on input device
      const scrollSensitivity = isTrackpad ? trackpadSensitivity : mouseSensitivity;

      // Calculate the scroll value
      const scroll = e.deltaY * scrollSensitivity;

      // Apply clamping
      const clampedScroll = Math.max(clampMin, Math.min(clampMax, scroll));

      // Round to the nearest integer
      const roundedScroll = Math.round(clampedScroll);

      // Invert the scroll value to match expected behavior
      const invertedScroll = -roundedScroll;

      send("wheelReport", { wheelY: invertedScroll });

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

  // TODO this needs reworked ot work with mappings
  // Keyboard-related
  const handleModifierKeys = useCallback(
    (e: KeyboardEvent, activeModifiers: number[], mappedKeyModifers: { shift: boolean; altLeft: boolean; altRight: boolean; }) => {
      const { shiftKey, ctrlKey, altKey, metaKey } = e;

      // TODO remove debug logging
      console.log(shiftKey + " " +ctrlKey + " " +altKey + " " +metaKey + " " +mappedKeyModifers.shift + " "+mappedKeyModifers.altLeft + " "+mappedKeyModifers.altRight + " ")

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
              mappedKeyModifers.shift ||
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
              mappedKeyModifers.altLeft ||
              (modifier !== modifiers["AltLeft"]),
          )
          .filter(
            modifier =>
              altKey ||
              mappedKeyModifers.altRight ||
              (modifier !== modifiers["AltRight"])
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
      const code = e.code;
      console.log("MAPPING ENABLED: " + settings.keyboardMappingEnabled)
      var localisedKey = settings.keyboardMappingEnabled ? e.key : code;
      console.log(e);
      console.log("Localised Key: " + localisedKey);

      // if (document.activeElement?.id !== "videoFocusTrap") {hH
      //   console.log("KEYUP: Not focusing on the video", document.activeElement);
      //   return;
      // }
      //
      // console.log(document.activeElement);

      setIsNumLockActive(e.getModifierState("NumLock"));
      setIsCapsLockActive(e.getModifierState("CapsLock"));
      setIsScrollLockActive(e.getModifierState("ScrollLock"));

      /*if (code == "IntlBackslash" && ["`", "~"].includes(key)) {
        code = "Backquote";
      } else if (code == "Backquote" && ["§", "±"].includes(key)) {
        code = "IntlBackslash";
      }*/

      const { key: mappedKey, shift, altLeft, altRight } = chars[localisedKey] ?? { key: code };
      //if (!key) continue; 
      console.log("Mapped Key: " + mappedKey)
      console.log("Current KB Layout:" + useKeyboardMappingsStore.getLayout());
      console.log(chars[localisedKey]);

      console.log("Shift: " + shift + ", altLeft: " + altLeft + ", altRight: " + altRight)
      
      // Add the mapped key to keyState
      activeKeyState.current.set(e.code, { mappedKey, modifiers: {shift, altLeft, altRight}});
      console.log(activeKeyState)

      // Add the key to the active keys
      const newKeys = [...prev.activeKeys, keys[mappedKey]].filter(Boolean);

      // TODO I feel this may not be applying the modifiers correctly, specifically altRight  
      // Add the modifier to the active modifiers
      const newModifiers = handleModifierKeys(e, [
        ...prev.activeModifiers,
        modifiers[code],
        (shift? modifiers['ShiftLeft'] : 0),
        (altLeft? modifiers['AltLeft'] : 0),
        (altRight? modifiers['AltRight'] : 0),],
        {shift: shift, altLeft: altLeft? true : false, altRight: altRight ? true : false}
      );

      // When pressing the meta key + another key, the key will never trigger a keyup
      // event, so we need to clear the keys after a short delay
      // https://bugs.chromium.org/p/chromium/issues/detail?id=28089
      // https://bugzilla.mozilla.org/show_bug.cgi?id=1299553
      if (e.metaKey) {
        setTimeout(() => {
          const prev = useHidStore.getState();
          sendKeyboardEvent([], newModifiers || prev.activeModifiers);
          activeKeyState.current.delete("MetaLeft");
          activeKeyState.current.delete("MetaRight");
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
      chars,
      keys,
      modifiers,
      settings,
    ],
  );

  const keyUpHandler = useCallback(
    (e: KeyboardEvent) => {
      e.preventDefault();
      console.log(e)
      const prev = useHidStore.getState();

      setIsNumLockActive(e.getModifierState("NumLock"));
      setIsCapsLockActive(e.getModifierState("CapsLock"));
      setIsScrollLockActive(e.getModifierState("ScrollLock"));

      // Check if the released key is a modifier (e.g., Shift, Alt, Control)
      const isModifierKey =
      e.code === "ShiftLeft" ||
      e.code === "ShiftRight" ||
      e.code === "AltLeft" ||
      e.code === "AltRight" ||
      e.code === "ControlLeft" ||
      e.code === "ControlRight";

      var newKeys = prev.activeKeys;

      // Handle modifier release
      if (isModifierKey) {
        console.log("ITS A MODIFER")
        // Update all affected keys when this modifier is released
        activeKeyState.current.forEach((value, code) => {
          const { mappedKey, modifiers: mappedModifiers} = value;

          // Remove the released modifier from the modifier bitmask
          //const updatedModifiers = modifiers & ~modifiers[e.code];

          // Recalculate the remapped key based on the updated modifiers
          //const updatedMappedKey = chars[originalKey]?.key || originalKey;

          var removeCurrentKey = false;

          // Shift Handling
          if (mappedModifiers.shift && (e.code === "ShiftLeft" || e.code === "ShiftRight")) {
            activeKeyState.current.delete(code);
            removeCurrentKey = true;
          };
          // Left Alt handling
          if (mappedModifiers.altLeft && e.code === "AltLeft") {
            activeKeyState.current.delete(code);
            removeCurrentKey = true;
          };
          // Right Alt handling
          if (mappedModifiers.altRight && e.code === "AltRight") {
            activeKeyState.current.delete(code);
            removeCurrentKey = true;
          };

          if (removeCurrentKey) {
            newKeys = newKeys
            .filter(k => k !== keys[mappedKey]) // Remove the previously mapped key
            //.concat(keys[updatedMappedKey]) // Add the new remapped key, don't need to do this.
            .filter(Boolean);
          };
        });
        console.log("prev.activemodifers: " + prev.activeModifiers)
        console.log("prev.activemodifers.filtered: " + prev.activeModifiers.filter(k => k !== modifiers[e.code]))
        const newModifiers = handleModifierKeys(
          e,
          prev.activeModifiers.filter(k => k !== modifiers[e.code]),
          {shift: false, altLeft: false, altRight: false}
        );
          console.log("New modifiers in keyup: " + newModifiers)

          // Update the keyState
          /*activeKeyState.current.delete(code);/*.set(code, {
            mappedKey: updatedMappedKey,
            modifiers: updatedModifiers,
            originalKey,
          });*/

          // Remove the modifer key from keyState
          activeKeyState.current.delete(e.code);

          // This is required to filter out the alt keys as well as the modifier.
          newKeys = newKeys
            .filter(k => k !== keys[e.code]) // Remove the previously mapped key
            //.concat(keys[updatedMappedKey]) // Add the new remapped key, don't need to do this.
            .filter(Boolean);

          // Send the updated HID payload
          sendKeyboardEvent([...new Set(newKeys)], [...new Set(newModifiers)]);

        return; // Exit as we've already handled the modifier release
      }

      // Retrieve the mapped key and modifiers from keyState
      const keyInfo = activeKeyState.current.get(e.code);
      if (!keyInfo) return; // Ignore if no record exists

      const { mappedKey, modifiers: modifier } = keyInfo;

      // Remove the key from keyState
      activeKeyState.current.delete(e.code);

      // Filter out the key that was just released
      newKeys = newKeys.filter(k => k !== keys[mappedKey]).filter(Boolean);
      console.log(activeKeyState)

      // Filter out the associated modifier
      //const newModifiers = prev.activeModifiers.filter(k => k !== modifier).filter(Boolean);
      const newModifiers = handleModifierKeys(
        e,
        prev.activeModifiers.filter(k => {
          if (modifier.shift && k == modifiers["ShiftLeft"]) return false;
          if (modifier.altLeft && k == modifiers["AltLeft"]) return false;
          if (modifier.altRight && k == modifiers["AltRight"]) return false;
          return true;
        }),
        {shift: modifier.shift, altLeft: modifier.altLeft? true : false, altRight: modifier.altRight ? true : false}
      );
    /*
      const { key: mappedKey/*, shift, altLeft, altRight*//* } = chars[e.key] ?? { key: e.code };
      //if (!key) continue;
      console.log("Mapped Key: " + mappedKey)
      // Build the modifier bitmask
      /*const modifier =
      (shift ? modifiers["ShiftLeft"] : 0) |
      (altLeft ? modifiers["AltLeft"] : 0) |
      (altRight ? modifiers["AltRight"] : 0); // This is important for a lot of keyboard layouts, right and left alt have different functions*//*

      // Filtering out the key that was just released (keys[e.code])
      const newKeys = prev.activeKeys.filter(k => k !== keys[mappedKey]).filter(Boolean);

      // Filter out the modifier that was just released
      const newModifiers = handleModifierKeys(
        e,
        prev.activeModifiers.filter(k => k !== modifiers[e.code]),
      );
      */

      console.log(e.key);
      sendKeyboardEvent([...new Set(newKeys)], [...new Set(newModifiers)]);
    },
    [
      setIsNumLockActive,
      setIsCapsLockActive,
      setIsScrollLockActive,
      handleModifierKeys,
      sendKeyboardEvent,
      chars,
      keys,
      modifiers,
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
        activeKeyState.current.clear();
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

      const abortController = new AbortController();
      const signal = abortController.signal;

      // We bind to the larger container in relative mode because of delta between the acceleration of the local
      // mouse and the mouse movement of the remote mouse. This simply makes it a bit less painful to use.
      // When we get Pointer Lock support, we can remove this.
      const containerElm = containerRef.current;
      if (!containerElm) return;

      containerElm.addEventListener("mousemove", relMouseMoveHandler, { signal });
      containerElm.addEventListener("pointerdown", relMouseMoveHandler, { signal });
      containerElm.addEventListener("pointerup", relMouseMoveHandler, { signal });

      containerElm.addEventListener("wheel", mouseWheelHandler, {
        signal,
        passive: true,
      });

      const preventContextMenu = (e: MouseEvent) => e.preventDefault();
      containerElm.addEventListener("contextmenu", preventContextMenu, { signal });

      return () => {
        abortController.abort();
      };
    },
    [settings.mouseMode, relMouseMoveHandler, mouseWheelHandler],
  );

  const hasNoAutoPlayPermissions = useMemo(() => {
    if (peerConnection?.connectionState !== "connected") return false;
    if (isPlaying) return false;
    if (hdmiError) return false;
    if (videoHeight === 0 || videoWidth === 0) return false;
    return true;
  }, [peerConnection?.connectionState, isPlaying, hdmiError, videoHeight, videoWidth]);

  return (
    <div className="grid h-full w-full grid-rows-layout">
      <div className="min-h-[39.5px] flex flex-col">
        <div className="flex flex-col">
          <fieldset disabled={peerConnection?.connectionState !== "connected"} className="contents">
            <Actionbar
              requestFullscreen={async () =>
                videoElm.current?.requestFullscreen({
                  navigationUI: "show",
                })
              }
            />
            <MacroBar />
          </fieldset>
        </div>
      </div>

      <div
        ref={containerRef}
        className={cx("h-full overflow-hidden", {
          "cursor-none": settings.mouseMode === "relative" && settings.isCursorHidden,
        })}
      >
        <div className="relative h-full">
          <div
            className={cx(
              "absolute inset-0 bg-blue-50/40 opacity-80 dark:bg-slate-800/40",
              "[background-image:radial-gradient(theme(colors.blue.300)_0.5px,transparent_0.5px),radial-gradient(theme(colors.blue.300)_0.5px,transparent_0.5px)] dark:[background-image:radial-gradient(theme(colors.slate.700)_0.5px,transparent_0.5px),radial-gradient(theme(colors.slate.700)_0.5px,transparent_0.5px)]",
              "[background-position:0_0,10px_10px]",
              "[background-size:20px_20px]",
            )}
          />
          <div className="flex h-full flex-col">
            <div className="relative flex-grow overflow-hidden">
              <div className="flex h-full flex-col">
                <div className="grid flex-grow grid-rows-bodyFooter overflow-hidden">
                  <div className="relative mx-4 my-2 flex items-center justify-center overflow-hidden">
                    <div className="relative flex h-full w-full items-center justify-center">
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
                            "cursor-none":
                              settings.mouseMode === "absolute" &&
                              settings.isCursorHidden,
                            "opacity-0":
                              isVideoLoading ||
                              hdmiError ||
                              peerConnectionState !== "connected",
                            "animate-slideUpFade border border-slate-800/30 opacity-0 shadow dark:border-slate-300/20":
                              isPlaying,
                          },
                        )}
                      />
                      {peerConnection?.connectionState == "connected" && (
                        <div
                          style={{ animationDuration: "500ms" }}
                          className="pointer-events-none absolute inset-0 flex animate-slideUpFade items-center justify-center opacity-0"
                        >
                          <div className="relative h-full max-h-[720px] w-full max-w-[1280px] rounded-md">
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
