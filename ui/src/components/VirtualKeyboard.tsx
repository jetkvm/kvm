import { useShallow } from "zustand/react/shallow";
import { ChevronDownIcon } from "@heroicons/react/16/solid";
import { AnimatePresence, motion } from "framer-motion";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Keyboard from "react-simple-keyboard";

import Card from "@components/Card";
// eslint-disable-next-line import/order
import { Button } from "@components/Button";

import "react-simple-keyboard/build/css/index.css";

import AttachIconRaw from "@/assets/attach-icon.svg";
import DetachIconRaw from "@/assets/detach-icon.svg";
import { cx } from "@/cva.config";
import { useHidStore, useSettingsStore, useUiStore } from "@/hooks/stores";
import useKeyboard from "@/hooks/useKeyboard";
import { keyDisplayMap, keys, modifiers } from "@/keyboardMappings";

export const DetachIcon = ({ className }: { className?: string }) => {
  return <img src={DetachIconRaw} alt="Detach Icon" className={className} />;
};

const AttachIcon = ({ className }: { className?: string }) => {
  return <img src={AttachIconRaw} alt="Attach Icon" className={className} />;
};

function KeyboardWrapper() {
  const [layoutName, setLayoutName] = useState("default");
  const [depressedButtons, setDepressedButtons] = useState("");

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

  const isCapsLockActive = useHidStore(useShallow(state => state.keyboardLedState?.caps_lock));

  // HID related states
  const keyboardLedStateSyncAvailable = useHidStore(state => state.keyboardLedStateSyncAvailable);
  const keyboardLedSync = useSettingsStore(state => state.keyboardLedSync);
  const isKeyboardLedManagedByHost = useMemo(() =>
    keyboardLedSync !== "browser" && keyboardLedStateSyncAvailable,
    [keyboardLedSync, keyboardLedStateSyncAvailable],
  );

  const setIsCapsLockActive = useHidStore(state => state.setIsCapsLockActive);

  const isShiftActive = useHidStore(state => state.isShiftActive);
  const setIsShiftActive = useHidStore(state => state.setIsShiftActive);

  const isCtrlActive = useHidStore(state => state.isCtrlActive);
  const setIsCtrlActive = useHidStore(state => state.setIsCtrlActive);

  const isAltActive = useHidStore(state => state.isAltActive);
  const setIsAltActive = useHidStore(state => state.setIsAltActive);

  const isMetaActive = useHidStore(state => state.isMetaActive);
  const setIsMetaActive = useHidStore(state => state.setIsMetaActive);

  const isAltGrActive = useHidStore(state => state.isAltGrActive);
  const setIsAltGrActive = useHidStore(state => state.setIsAltGrActive);

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

  useEffect(() => {
    // if you have the CapsLock "down", then the shift state is inverted
    const effectiveShift = isCapsLockActive ? false === isShiftActive : isShiftActive;
    setLayoutName(effectiveShift ? "shift" : "default");
  },
    [setLayoutName, isCapsLockActive, isShiftActive]
  );

  // this causes the buttons to look depressed/clicked depending on the sticky state
  useEffect(() => {
    let buttons = "None ";  // make sure we name at least one (fake) button
    if (isCapsLockActive) buttons += "CapsLock ";
    if (isShiftActive) buttons += "ShiftLeft ShiftRight ";
    if (isCtrlActive) buttons += "ControlLeft ControlRight ";
    if (isAltActive) buttons += "AltLeft AltRight ";
    if (isMetaActive) buttons += "MetaLeft MetaRight ";
    setDepressedButtons(buttons.trimEnd());
  },
    [setDepressedButtons, isCapsLockActive, isShiftActive, isCtrlActive, isAltActive, isMetaActive, isAltGrActive]
  );

  const onKeyPress = useCallback((key: string) => {
    // handle the fake combo keys first
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

    if (key === "CtrlAltBackspace") {
      sendKeyboardEvent(
        [keys["Backspace"]],
        [modifiers["ControlLeft"], modifiers["AltLeft"]],
      );

      setTimeout(resetKeyboardState, 100);
      return;
    }

    // strip away the parens for shifted characters
    const cleanKey = key.replace(/[()]/g, "");

    const passthrough = ["PrintScreen", "SystemRequest", "Pause", "Break", "ScrollLock", "Enter", "Space"].find((value) => value === cleanKey);

    if (passthrough) {
      emitkeycode(cleanKey);
      return;
    }

    // adjust the sticky state of the Shift/Ctrl/Alt/Meta/AltGr
    if (key === "CapsLock" && !isKeyboardLedManagedByHost)
      setIsCapsLockActive(!isCapsLockActive);
    else if (key === "ShiftLeft" || key === "ShiftRight")
      setIsShiftActive(!isShiftActive);
    else if (key === "ControlLeft" || key === "ControlRight")
      setIsCtrlActive(!isCtrlActive);
    else if (key === "AltLeft" || key === "AltRight")
      setIsAltActive(!isAltActive);
    else if (key === "MetaLeft" || key === "MetaRight")
      setIsMetaActive(!isMetaActive);
    else if (key === "AltGr")
      setIsAltGrActive(!isAltGrActive);

    emitkeycode(cleanKey);

    function emitkeycode(key: string) {
      const effectiveMods: number[] = [];

      if (isShiftActive)
        effectiveMods.push(modifiers["ShiftLeft"]);

      if (isCtrlActive)
        effectiveMods.push(modifiers["ControlLeft"]);

      if (isAltActive)
        effectiveMods.push(modifiers["AltLeft"]);

      if (isMetaActive)
        effectiveMods.push(modifiers["MetaLeft"]);

      if (isAltGrActive) {
        effectiveMods.push(modifiers["MetaRight"]);
        effectiveMods.push(modifiers["CtrlLeft"]);
      }

      const keycode = keys[key];
      if (keycode) {
        // send the keycode with modifiers
        sendKeyboardEvent([keycode], effectiveMods);
      }

      // release the key (if one pressed), but retain the modifiers
      setTimeout(() => sendKeyboardEvent([], effectiveMods), 50);
    }
  },
    [isKeyboardLedManagedByHost,
      setIsCapsLockActive, isCapsLockActive,
      setIsShiftActive, isShiftActive,
      setIsCtrlActive, isCtrlActive,
      setIsAltActive, isAltActive,
      setIsMetaActive, isMetaActive,
      setIsAltGrActive, isAltGrActive,
      sendKeyboardEvent, resetKeyboardState
    ],
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
                      onKeyPress={onKeyPress}
                      buttonTheme={[
                        {
                          class: "combination-key",
                          buttons: "CtrlAltDelete AltMetaEscape CtrlAltBackspace",
                        },
                        {
                          class: "depressed-key",
                          buttons: depressedButtons
                        },
                      ]}
                      display={keyDisplayMap}
                      layout={{
                        default: [
                          "CtrlAltDelete AltMetaEscape CtrlAltBackspace",
                          "Escape F1 F2 F3 F4 F5 F6 F7 F8 F9 F10 F11 F12",
                          "Backquote Digit1 Digit2 Digit3 Digit4 Digit5 Digit6 Digit7 Digit8 Digit9 Digit0 Minus Equal Backspace",
                          "Tab KeyQ KeyW KeyE KeyR KeyT KeyY KeyU KeyI KeyO KeyP BracketLeft BracketRight Backslash",
                          "CapsLock KeyA KeyS KeyD KeyF KeyG KeyH KeyJ KeyK KeyL Semicolon Quote Enter",
                          "ShiftLeft KeyZ KeyX KeyC KeyV KeyB KeyN KeyM Comma Period Slash ShiftRight",
                          "ControlLeft AltLeft MetaLeft Space MetaRight AltRight",
                        ],
                        shift: [
                          "CtrlAltDelete AltMetaEscape CtrlAltBackspace",
                          "Escape F1 F2 F3 F4 F5 F6 F7 F8 F9 F10 F11 F12",
                          "(Backquote) (Digit1) (Digit2) (Digit3) (Digit4) (Digit5) (Digit6) (Digit7) (Digit8) (Digit9) (Digit0) (Minus) (Equal) (Backspace)",
                          "Tab (KeyQ) (KeyW) (KeyE) (KeyR) (KeyT) (KeyY) (KeyU) (KeyI) (KeyO) (KeyP) (BracketLeft) (BracketRight) (Backslash)",
                          "CapsLock (KeyA) (KeyS) (KeyD) (KeyF) (KeyG) (KeyH) (KeyJ) (KeyK) (KeyL) (Semicolon) (Quote) Enter",
                          "ShiftLeft (KeyZ) (KeyX) (KeyC) (KeyV) (KeyB) (KeyN) (KeyM) (Comma) (Period) (Slash) ShiftRight",
                          "ControlLeft AltLeft MetaLeft Space MetaRight AltRight",
                        ],
                      }}
                      disableButtonHold={true}
                    />

                    <div className="controlArrows">
                      <Keyboard
                        baseClass="simple-keyboard-control"
                        theme="simple-keyboard hg-theme-default hg-layout-default"
                        layoutName={layoutName}
                        onKeyPress={onKeyPress}
                        display={keyDisplayMap}
                        layout={{
                          default: ["PrintScreen ScrollLock Pause", "Insert Home PageUp", "Delete End PageDown"],
                          shift: ["(PrintScreen) ScrollLock (Pause)", "Insert Home PageUp", "Delete End PageDown"],
                        }}
                        disableButtonHold={true}
                      />
                      <Keyboard
                        baseClass="simple-keyboard-arrows"
                        theme="simple-keyboard hg-theme-default hg-layout-default"
                        onKeyPress={onKeyPress}
                        display={keyDisplayMap}
                        layout={{
                          default: ["ArrowUp", "ArrowLeft ArrowDown ArrowRight"],
                          shift: ["ArrowUp", "ArrowLeft ArrowDown ArrowRight"],
                        }}
                        disableButtonHold={true}
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
