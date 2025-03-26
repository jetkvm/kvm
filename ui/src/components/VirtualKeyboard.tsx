import { useCallback, useEffect, useRef, useState } from "react";
import Keyboard from "react-simple-keyboard";
import { ChevronDownIcon } from "@heroicons/react/16/solid";
import { motion, AnimatePresence } from "framer-motion";

import Card from "@components/Card";
// eslint-disable-next-line import/order
import { Button } from "@components/Button";

import "react-simple-keyboard/build/css/index.css";

import { useHidStore, useUiStore } from "@/hooks/stores";
import { cx } from "@/cva.config";
import { keys, modifiers } from "@/keyboardMappings";
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
  const [layoutName, setLayoutName] = useState("default");

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

  const onKeyDown = useCallback(
    (key: string) => {
      const isKeyShift = key === "{shift}" || key === "ShiftLeft" || key === "ShiftRight";
      const isKeyCaps = key === "CapsLock";
      const cleanKey = key.replace(/[()]/g, "");
      const keyHasShiftModifier = key.includes("(");

      // Handle toggle of layout for shift or caps lock
      const toggleLayout = () => {
        setLayoutName(prevLayout => (prevLayout === "default" ? "shift" : "default"));
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

      if (isKeyShift || isKeyCaps) {
        toggleLayout();

        if (isCapsLockActive) {
          setIsCapsLockActive(false);
          sendKeyboardEvent([keys["CapsLock"]], []);
          return;
        }
      }

      // Handle caps lock state change
      if (isKeyCaps) {
        setIsCapsLockActive(!isCapsLockActive);
      }

      // Collect new active keys and modifiers
      const newKeys = keys[cleanKey] ? [keys[cleanKey]] : [];
      const newModifiers =
        keyHasShiftModifier && !isCapsLockActive ? [modifiers["ShiftLeft"]] : [];

      // Update current keys and modifiers
      sendKeyboardEvent(newKeys, newModifiers);

      // If shift was used as a modifier and caps lock is not active, revert to default layout
      if (keyHasShiftModifier && !isCapsLockActive) {
        setLayoutName("default");
      }

      setTimeout(resetKeyboardState, 100);
    },
    [isCapsLockActive, sendKeyboardEvent, resetKeyboardState, setIsCapsLockActive],
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
                      display={{
                        CtrlAltDelete: "Ctrl + Alt + Delete",
                        AltMetaEscape: "Alt + Meta + Escape",
                        Escape: "esc",
                        Tab: "tab",
                        Backspace: "backspace",
                        "(Backspace)": "backspace",
                        Enter: "enter",
                        CapsLock: "caps lock",
                        ShiftLeft: "shift",
                        ShiftRight: "shift",
                        ControlLeft: "ctrl",
                        AltLeft: "alt",
                        AltRight: "alt",
                        MetaLeft: "meta",
                        MetaRight: "meta",
                        KeyQ: "q",
                        KeyW: "w",
                        KeyE: "e",
                        KeyR: "r",
                        KeyT: "t",
                        KeyY: "y",
                        KeyU: "u",
                        KeyI: "i",
                        KeyO: "o",
                        KeyP: "p",
                        KeyA: "a",
                        KeyS: "s",
                        KeyD: "d",
                        KeyF: "f",
                        KeyG: "g",
                        KeyH: "h",
                        KeyJ: "j",
                        KeyK: "k",
                        KeyL: "l",
                        KeyZ: "z",
                        KeyX: "x",
                        KeyC: "c",
                        KeyV: "v",
                        KeyB: "b",
                        KeyN: "n",
                        KeyM: "m",

                        "(KeyQ)": "Q",
                        "(KeyW)": "W",
                        "(KeyE)": "E",
                        "(KeyR)": "R",
                        "(KeyT)": "T",
                        "(KeyY)": "Y",
                        "(KeyU)": "U",
                        "(KeyI)": "I",
                        "(KeyO)": "O",
                        "(KeyP)": "P",
                        "(KeyA)": "A",
                        "(KeyS)": "S",
                        "(KeyD)": "D",
                        "(KeyF)": "F",
                        "(KeyG)": "G",
                        "(KeyH)": "H",
                        "(KeyJ)": "J",
                        "(KeyK)": "K",
                        "(KeyL)": "L",
                        "(KeyZ)": "Z",
                        "(KeyX)": "X",
                        "(KeyC)": "C",
                        "(KeyV)": "V",
                        "(KeyB)": "B",
                        "(KeyN)": "N",
                        "(KeyM)": "M",
                        Digit1: "1",
                        Digit2: "2",
                        Digit3: "3",
                        Digit4: "4",
                        Digit5: "5",
                        Digit6: "6",
                        Digit7: "7",
                        Digit8: "8",
                        Digit9: "9",
                        Digit0: "0",

                        "(Digit1)": "!",
                        "(Digit2)": "@",
                        "(Digit3)": "#",
                        "(Digit4)": "$",
                        "(Digit5)": "%",
                        "(Digit6)": "^",
                        "(Digit7)": "&",
                        "(Digit8)": "*",
                        "(Digit9)": "(",
                        "(Digit0)": ")",
                        Minus: "-",
                        "(Minus)": "_",

                        Equal: "=",
                        "(Equal)": "+",
                        BracketLeft: "[",
                        BracketRight: "]",
                        "(BracketLeft)": "{",
                        "(BracketRight)": "}",
                        Backslash: "\\",
                        "(Backslash)": "|",

                        Semicolon: ";",
                        "(Semicolon)": ":",
                        Quote: "'",
                        "(Quote)": '"',
                        Comma: ",",
                        "(Comma)": "<",
                        Period: ".",
                        "(Period)": ">",
                        Slash: "/",
                        "(Slash)": "?",
                        Space: " ",
                        Backquote: "`",
                        "(Backquote)": "~",
                        IntlBackslash: "\\",

                        F1: "F1",
                        F2: "F2",
                        F3: "F3",
                        F4: "F4",
                        F5: "F5",
                        F6: "F6",
                        F7: "F7",
                        F8: "F8",
                        F9: "F9",
                        F10: "F10",
                        F11: "F11",
                        F12: "F12",
                      }}
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
