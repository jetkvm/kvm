import { useCallback, useEffect, useRef, useState } from "react";
import {
  useHidStore,
  useMouseStore,
  useRTCStore,
  useSettingsStore,
  useUiStore,
  useVideoStore,
  useKeyboardMappingsStore,
} from "@/hooks/stores";
import { useResizeObserver } from "@/hooks/useResizeObserver";
import { cx } from "@/cva.config";
import VirtualKeyboard from "@components/VirtualKeyboard";
import Actionbar from "@components/ActionBar";
import InfoBar from "@components/InfoBar";
import useKeyboard from "@/hooks/useKeyboard";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { ConnectionErrorOverlay, HDMIErrorOverlay, LoadingOverlay } from "./VideoOverlay";

// TODO Implement keyboard lock API to resolve #127
// https://developer.chrome.com/docs/capabilities/web-apis/keyboard-lock
// An appropriate error message will need to be displayed in order to alert users to browser compatibility issues.
// This requires TLS, waiting on TLS support.

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

  // Store hooks
  const settings = useSettingsStore();
  const { sendKeyboardEvent, resetKeyboardState } = useKeyboard();
  const setMousePosition = useMouseStore(state => state.setMousePosition);
  const {
    setClientSize: setVideoClientSize,
    setSize: setVideoSize,
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
      const { buttons } = e;

      // Clamp mouse position within the video boundaries
      const currMouseX = Math.min(Math.max(1, e.offsetX), videoClientWidth);
      const currMouseY = Math.min(Math.max(1, e.offsetY), videoClientHeight);

      // Normalize mouse position to 0-32767 range (HID absolute coordinate system)
      const x = Math.round((currMouseX / videoClientWidth) * 32767);
      const y = Math.round((currMouseY / videoClientHeight) * 32767);

      // Send mouse movement
      sendMouseMovement(x, y, buttons);
    },
    [sendMouseMovement, videoClientHeight, videoClientWidth],
  );

  const mouseWheelHandler = useCallback(
    (e: WheelEvent) => {
      if (blockWheelEvent) return;
      e.preventDefault();

      // TODO this should be user controllable
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

      send("wheelReport", { wheelY: invertedScroll });

      // TODO this is making scrolling feel slow and sluggish, also throwing a violation in chrome
      setBlockWheelEvent(true);
      setTimeout(() => setBlockWheelEvent(false), 50);
    },
    [blockWheelEvent, send],
  );

  const resetMousePosition = useCallback(() => {
    sendMouseMovement(0, 0, 0);
  }, [sendMouseMovement]);

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

      // if (document.activeElement?.id !== "videoFocusTrap") {
      //   console.log("KEYUP: Not focusing on the video", document.activeElement);
      //   return;
      // }

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
        activeKeyState.current.clear();
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
                          "outline-50 max-h-full max-w-full rounded-md object-contain transition-all duration-1000",
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
