import { useCallback, useEffect, useRef, useState } from "react";
import Keyboard from "react-simple-keyboard";
import { ChevronDownIcon } from "@heroicons/react/16/solid";
import { motion, AnimatePresence } from "framer-motion";

import Card from "@components/Card";
// eslint-disable-next-line import/order
import { Button } from "@components/Button";

import "react-simple-keyboard/build/css/index.css";

import { useHidStore, useUiStore, useKeyboardMappingsStore } from "@/hooks/stores";
import { cx } from "@/cva.config";
import { keyDisplayMap } from "@/keyboardMappings/KeyboardLayouts";
import useKeyboard from "@/hooks/useKeyboard";
import DetachIconRaw from "@/assets/detach-icon.svg";
import AttachIconRaw from "@/assets/attach-icon.svg";

export const DetachIcon = ({ className }: { className?: string }) => {
  return <img src={DetachIconRaw} alt="Detach Icon" className={className} />;
};

const AttachIcon = ({ className }: { className?: string }) => {
  return <img src={AttachIconRaw} alt="Attach Icon" className={className} />;
};

function KeyboardWrapper() {
    const [keys, setKeys] = useState(useKeyboardMappingsStore.keys);
    const [chars, setChars] = useState(useKeyboardMappingsStore.chars);
    const [modifiers, setModifiers] = useState(useKeyboardMappingsStore.modifiers);
  
    useEffect(() => {
      const unsubscribeKeyboardStore = useKeyboardMappingsStore.subscribe(() => {
        setKeys(useKeyboardMappingsStore.keys);
        setChars(useKeyboardMappingsStore.chars);
        setModifiers(useKeyboardMappingsStore.modifiers);
        setMappingsEnabled(useKeyboardMappingsStore.getMappingState());
      });
      return unsubscribeKeyboardStore; // Cleanup on unmount
    }, []); 
  
    const [layoutName, setLayoutName] = useState("default");
    const [mappingsEnabled, setMappingsEnabled] = useState(useKeyboardMappingsStore.getMappingState());
  
    useEffect(() => {
      if (mappingsEnabled) {
        if (layoutName == "default" ) {
          setLayoutName("mappedLower")
        }
        if (layoutName == "shift") {
          setLayoutName("mappedUpper")
        }
      } else {
        if (layoutName == "mappedLower") {
          setLayoutName("default")
        }
        if (layoutName == "mappedUpper") {
          setLayoutName("shift")
        }
      }
    }, [mappingsEnabled, layoutName]);

  const keyboardRef = useRef<HTMLDivElement>(null);
  const showAttachedVirtualKeyboard = useUiStore(
    state => state.isAttachedVirtualKeyboardVisible,
  );
  const setShowAttachedVirtualKeyboard = useUiStore(
    state => state.setAttachedVirtualKeyboardVisibility,
  );

  const { sendKeyboardEvent, resetKeyboardState } = useKeyboard();

  const [isDragging, setIsDragging] = useState(false);
  const [position, setPosition] = useState({ x: 0, y: 0 });
  const [newPosition, setNewPosition] = useState({ x: 0, y: 0 });
  const isCapsLockActive = useHidStore(state => state.isCapsLockActive);
  const setIsCapsLockActive = useHidStore(state => state.setIsCapsLockActive);

  const startDrag = useCallback((e: MouseEvent | TouchEvent) => {
    if (!keyboardRef.current) return;
    if (e instanceof TouchEvent && e.touches.length > 1) return;
    setIsDragging(true);

    const clientX = e instanceof TouchEvent ? e.touches[0].clientX : e.clientX;
    const clientY = e instanceof TouchEvent ? e.touches[0].clientY : e.clientY;

    const rect = keyboardRef.current.getBoundingClientRect();
    setPosition({
      x: clientX - rect.left,
      y: clientY - rect.top,
    });
  }, []);

  const onDrag = useCallback(
    (e: MouseEvent | TouchEvent) => {
      if (!keyboardRef.current) return;
      if (isDragging) {
        const clientX = e instanceof TouchEvent ? e.touches[0].clientX : e.clientX;
        const clientY = e instanceof TouchEvent ? e.touches[0].clientY : e.clientY;

        const newX = clientX - position.x;
        const newY = clientY - position.y;

        const rect = keyboardRef.current.getBoundingClientRect();
        const maxX = window.innerWidth - rect.width;
        const maxY = window.innerHeight - rect.height;

        setNewPosition({
          x: Math.min(maxX, Math.max(0, newX)),
          y: Math.min(maxY, Math.max(0, newY)),
        });
      }
    },
    [isDragging, position.x, position.y],
  );

  const endDrag = useCallback(() => {
    setIsDragging(false);
  }, []);

  useEffect(() => {
    const handle = keyboardRef.current;
    if (handle) {
      handle.addEventListener("touchstart", startDrag);
      handle.addEventListener("mousedown", startDrag);
    }

    document.addEventListener("mouseup", endDrag);
    document.addEventListener("touchend", endDrag);

    document.addEventListener("mousemove", onDrag);
    document.addEventListener("touchmove", onDrag);

    return () => {
      if (handle) {
        handle.removeEventListener("touchstart", startDrag);
        handle.removeEventListener("mousedown", startDrag);
      }

      document.removeEventListener("mouseup", endDrag);
      document.removeEventListener("touchend", endDrag);

      document.removeEventListener("mousemove", onDrag);
      document.removeEventListener("touchmove", onDrag);
    };
  }, [endDrag, onDrag, startDrag]);

  // TODO implement meta key and meta key modifer
  // TODO implement hold functionality for key combos. (add a hold button, add all keys to an array, when released send as one)
  const onKeyDown = useCallback(
    (key: string) => {
      const cleanKey = key.replace(/[()]/g, "");
      // Mappings
      const { key: mappedKey, shift, altLeft, altRight } = chars[cleanKey] ?? {};

      const isKeyShift = key === "{shift}" || key === "ShiftLeft" || key === "ShiftRight";
      const isKeyCaps = key === "CapsLock";
      const keyHasShiftModifier = (key.includes("(") && key !== "(") || shift;

      //TODO remove debug logs
      console.log(layoutName)

      // Handle toggle of layout for shift or caps lock
      const toggleLayout = () => {
        if (mappingsEnabled) {
          setLayoutName(prevLayout => (prevLayout === "mappedLower" ? "mappedUpper" : "mappedLower"));
        } else {
          setLayoutName(prevLayout => (prevLayout === "default" ? "shift" : "default"));
        }
      };

      if (key === "CtrlAltDelete") {
        sendKeyboardEvent(
          [keys["Delete"]],
          [modifiers["ControlLeft"], modifiers["AltLeft"]],
        );
        setTimeout(resetKeyboardState, 100);
        return;
      }

      if (key === "AltMetaEscape") {
        sendKeyboardEvent(
          [keys["Escape"]],
          [modifiers["MetaLeft"], modifiers["AltLeft"]],
        );

        setTimeout(resetKeyboardState, 100);
        return;
      }

      if (isKeyShift || (!(layoutName == "shift" || layoutName == "mappedUpper") && isCapsLockActive)) {
        toggleLayout();
      }

      if (layoutName == "shift" || layoutName == "mappedUpper") {
        if (!isCapsLockActive) {
          toggleLayout();
        }

        if (isKeyCaps && isCapsLockActive) {
          toggleLayout();
          setIsCapsLockActive(false);
          sendKeyboardEvent([keys["CapsLock"]], []);
          return;
        }
      }

      // Handle caps lock state change
      if (isKeyCaps) {
        toggleLayout();
        setIsCapsLockActive(!isCapsLockActive);
      }

      //TODO remove debug logs
      console.log(cleanKey)
      console.log(chars[cleanKey])

      console.log(mappedKey)

      // Collect new active keys and modifiers
      const newKeys = keys[mappedKey ?? cleanKey] ? [keys[mappedKey ?? cleanKey]] : [];
      const newModifiers =
      [
        ((shift || isKeyShift)? modifiers['ShiftLeft'] : 0),
        (altLeft? modifiers['AltLeft'] : 0),
        (altRight? modifiers['AltRight'] : 0),
      ].filter(Boolean);

      console.log(newModifiers);

      // Update current keys and modifiers
      sendKeyboardEvent(newKeys, [...new Set(newModifiers)]);

      // If shift was used as a modifier and caps lock is not active, revert to default layout
      if (keyHasShiftModifier && !isCapsLockActive) {
        setLayoutName(mappingsEnabled ? "mappedLower" : "default");
      }

      setTimeout(resetKeyboardState, 100);
    },
    [isCapsLockActive, sendKeyboardEvent, resetKeyboardState, setIsCapsLockActive, mappingsEnabled, chars, keys, modifiers, layoutName],
  );

  const virtualKeyboard = useHidStore(state => state.isVirtualKeyboardEnabled);
  const setVirtualKeyboard = useHidStore(state => state.setVirtualKeyboardEnabled);

  return (
    <div
      className="transition-all duration-500 ease-in-out"
      style={{
        marginBottom: virtualKeyboard ? "0px" : `-${350}px`,
      }}
    >
      <AnimatePresence>
        {virtualKeyboard && (
          <motion.div
            initial={{ opacity: 0, y: "100%" }}
            animate={{ opacity: 1, y: "0%" }}
            exit={{ opacity: 0, y: "100%" }}
            transition={{
              duration: 0.5,
              ease: "easeInOut",
            }}
          >
            <div
              className={cx(
                !showAttachedVirtualKeyboard
                  ? "fixed left-0 top-0 z-50 select-none"
                  : "relative",
              )}
              ref={keyboardRef}
              style={{
                ...(!showAttachedVirtualKeyboard
                  ? { transform: `translate(${newPosition.x}px, ${newPosition.y}px)` }
                  : {}),
              }}
            >
              <Card
                className={cx("overflow-hidden", {
                  "rounded-none": showAttachedVirtualKeyboard,
                })}
              >
                <div className="flex items-center justify-center border-b border-b-slate-800/30 bg-white px-2 py-1 dark:border-b-slate-300/20 dark:bg-slate-800">
                  <div className="absolute left-2 flex items-center gap-x-2">
                    {showAttachedVirtualKeyboard ? (
                      <Button
                        size="XS"
                        theme="light"
                        text="Detach"
                        onClick={() => setShowAttachedVirtualKeyboard(false)}
                      />
                    ) : (
                      <Button
                        size="XS"
                        theme="light"
                        text="Attach"
                        LeadingIcon={AttachIcon}
                        onClick={() => setShowAttachedVirtualKeyboard(true)}
                      />
                    )}
                  </div>
                  <h2 className="select-none self-center font-sans text-[12px] text-slate-700 dark:text-slate-300">
                    Virtual Keyboard
                  </h2>
                  <div className="absolute right-2">
                    <Button
                      size="XS"
                      theme="light"
                      text="Hide"
                      LeadingIcon={ChevronDownIcon}
                      onClick={() => setVirtualKeyboard(false)}
                    />
                  </div>
                </div>

                <div>
                  <div className="flex flex-col bg-blue-50/80 md:flex-row dark:bg-slate-700">
                    <Keyboard
                      baseClass="simple-keyboard-main"
                      layoutName={layoutName}
                      onKeyPress={onKeyDown}
                      buttonTheme={[
                        {
                          class: "combination-key",
                          buttons: "CtrlAltDelete AltMetaEscape",
                        },
                      ]}
                      display={keyDisplayMap}
                      layout={{
                        default: [
                          "CtrlAltDelete AltMetaEscape",
                          "Escape F1 F2 F3 F4 F5 F6 F7 F8 F9 F10 F11 F12",
                          "Backquote Digit1 Digit2 Digit3 Digit4 Digit5 Digit6 Digit7 Digit8 Digit9 Digit0 Minus Equal Backspace",
                          "Tab KeyQ KeyW KeyE KeyR KeyT KeyY KeyU KeyI KeyO KeyP BracketLeft BracketRight Backslash",
                          "CapsLock KeyA KeyS KeyD KeyF KeyG KeyH KeyJ KeyK KeyL Semicolon Quote Enter",
                          "ShiftLeft KeyZ KeyX KeyC KeyV KeyB KeyN KeyM Comma Period Slash ShiftRight",
                          "ControlLeft AltLeft MetaLeft Space MetaRight AltRight",
                        ],
                        shift: [
                          "CtrlAltDelete AltMetaEscape",
                          "Escape F1 F2 F3 F4 F5 F6 F7 F8 F9 F10 F11 F12",
                          "(Backquote) (Digit1) (Digit2) (Digit3) (Digit4) (Digit5) (Digit6) (Digit7) (Digit8) (Digit9) (Digit0) (Minus) (Equal) (Backspace)",
                          "Tab (KeyQ) (KeyW) (KeyE) (KeyR) (KeyT) (KeyY) (KeyU) (KeyI) (KeyO) (KeyP) (BracketLeft) (BracketRight) (Backslash)",
                          "CapsLock (KeyA) (KeyS) (KeyD) (KeyF) (KeyG) (KeyH) (KeyJ) (KeyK) (KeyL) (Semicolon) (Quote) Enter",
                          "ShiftLeft (KeyZ) (KeyX) (KeyC) (KeyV) (KeyB) (KeyN) (KeyM) (Comma) (Period) (Slash) ShiftRight",
                          "ControlLeft AltLeft MetaLeft Space MetaRight AltRight",
                        ],
                        mappedLower: [
                          "CtrlAltDelete AltMetaEscape",
                          "Escape F1 F2 F3 F4 F5 F6 F7 F8 F9 F10 F11 F12",
                          "` 1 2 3 4 5 6 7 8 9 0 - = Backspace",
                          "Tab q w e r t y u i o p [ ] \\",
                          "CapsLock a s d f g h j k l ; ' Enter",
                          "ShiftLeft z x c v b n m , . / ShiftRight",
                          "ControlLeft AltLeft MetaLeft Space MetaRight AltRight"
                        ],
  
                        mappedUpper: [
                          "CtrlAltDelete AltMetaEscape",
                          "Escape F1 F2 F3 F4 F5 F6 F7 F8 F9 F10 F11 F12",
                          "~ ! @ # $ % ^ & * ( ) _ + Backspace",
                          "Tab Q W E R T Y U I O P { } |",
                          "CapsLock A S D F G H J K L : \" Enter",
                          "ShiftLeft Z X C V B N M < > ? ShiftRight",
                          "ControlLeft AltLeft MetaLeft Space MetaRight AltRight"
                        ],
                      }}
                      disableButtonHold={true}
                      mergeDisplay={true}
                      debug={false}
                    />

                    <div className="controlArrows">
                      <Keyboard
                        baseClass="simple-keyboard-control"
                        theme="simple-keyboard hg-theme-default hg-layout-default"
                        layout={{
                          default: ["Home Pageup", "Delete End Pagedown"],
                        }}
                        display={{
                          Home: "home",
                          Pageup: "pageup",
                          Delete: "delete",
                          End: "end",
                          Pagedown: "pagedown",
                        }}
                        syncInstanceInputs={true}
                        onKeyPress={onKeyDown}
                        mergeDisplay={true}
                        debug={false}
                      />
                      <Keyboard
                        baseClass="simple-keyboard-arrows"
                        theme="simple-keyboard hg-theme-default hg-layout-default"
                        display={{
                          ArrowLeft: "←",
                          ArrowRight: "→",
                          ArrowUp: "↑",
                          ArrowDown: "↓",
                        }}
                        layout={{
                          default: ["ArrowUp", "ArrowLeft ArrowDown ArrowRight"],
                        }}
                        onKeyPress={onKeyDown}
                        debug={false}
                      />
                    </div>
                  </div>
                </div>
              </Card>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

export default KeyboardWrapper;