import { ChevronDownIcon } from "@heroicons/react/16/solid";
import { AnimatePresence, motion } from "framer-motion";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Keyboard from "react-simple-keyboard";
import { LuKeyboard } from "react-icons/lu";

import Card from "@components/Card";
// eslint-disable-next-line import/order
import { Button, LinkButton } from "@components/Button";

import "react-simple-keyboard/build/css/index.css";

import DetachIconRaw from "@/assets/detach-icon.svg";
import { cx } from "@/cva.config";
import { useHidStore, useUiStore } from "@/hooks/stores";
import useKeyboard from "@/hooks/useKeyboard";
import useKeyboardLayout from "@/hooks/useKeyboardLayout";
import { decodeModifiers, keys, latchingKeys, modifiers } from "@/keyboardMappings";

export const DetachIcon = ({ className }: { className?: string }) => {
  return <img src={DetachIconRaw} alt="Detach Icon" className={className} />;
};

function KeyboardWrapper() {
  const keyboardRef = useRef<HTMLDivElement>(null);
  const { isAttachedVirtualKeyboardVisible, setAttachedVirtualKeyboardVisibility } =
    useUiStore();
  const { keyboardLedState, keysDownState, isVirtualKeyboardEnabled, setVirtualKeyboardEnabled } =
    useHidStore();
  const { handleKeyPress, executeMacro } = useKeyboard();
  const { selectedKeyboard } = useKeyboardLayout();

  const [isDragging, setIsDragging] = useState(false);
  const [position, setPosition] = useState({ x: 0, y: 0 });
  const [newPosition, setNewPosition] = useState({ x: 0, y: 0 });

  const keyDisplayMap = useMemo(() => {
    return selectedKeyboard.keyDisplayMap;
  }, [selectedKeyboard]);

  const virtualKeyboard = useMemo(() => {
    return selectedKeyboard.virtualKeyboard;
  }, [selectedKeyboard]);

  const { isShiftActive } = useMemo(() => {
    return decodeModifiers(keysDownState.modifier);
  }, [keysDownState]);

  const isCapsLockActive = useMemo(() => {
    return keyboardLedState.caps_lock;
  }, [keyboardLedState]);

  const mainLayoutName = useMemo(() => {
    // if you have the CapsLock "latched", then the shift state is inverted
    const effectiveShift = isCapsLockActive ? false === isShiftActive : isShiftActive;
    return effectiveShift ? "shift" : "default";
  }, [isCapsLockActive, isShiftActive]);

  const keyNamesForDownKeys = useMemo(() => {
    const activeModifierMask = keysDownState.modifier || 0;
    const modifierNames = Object.entries(modifiers)
      .filter(([_, mask]) => (activeModifierMask & mask) !== 0)
      .map(([name, _]) => name);

    const keysDown = keysDownState.keys || [];
    const keyNames = Object.entries(keys)
      .filter(([_, value]) => keysDown.includes(value))
      .map(([name, _]) => name);

    return [...modifierNames, ...keyNames, " "]; // we have to have at least one space to avoid keyboard whining
  }, [keysDownState]);

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
    // Is the keyboard detached or attached?
    if (isAttachedVirtualKeyboardVisible) return;

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
  }, [isAttachedVirtualKeyboardVisible, endDrag, onDrag, startDrag]);

  const onKeyUp = useCallback(async (_: string, e: MouseEvent | undefined) => {
    e?.preventDefault();
    e?.stopPropagation();
  }, []);

  const onKeyDown = useCallback(
    async (key: string, e: MouseEvent | undefined) => {
      e?.preventDefault();
      e?.stopPropagation();

      // handle the fake key-macros we have defined for common combinations
      if (key === "CtrlAltDelete") {
        await executeMacro([
          { keys: ["Delete"], modifiers: ["ControlLeft", "AltLeft"], delay: 100 },
        ]);
        return;
      }

      if (key === "AltMetaEscape") {
        await executeMacro([
          { keys: ["Escape"], modifiers: ["AltLeft", "MetaLeft"], delay: 100 },
        ]);
        return;
      }

      if (key === "CtrlAltBackspace") {
        await executeMacro([
          { keys: ["Backspace"], modifiers: ["ControlLeft", "AltLeft"], delay: 100 },
        ]);
        return;
      }

      // if they press any of the latching keys, we send a keypress down event and the release it automatically (on timer)
      if (latchingKeys.includes(key)) {
        console.debug(`Latching key pressed: ${key} sending down and delayed up pair`);
        handleKeyPress(keys[key], true);
        setTimeout(() => handleKeyPress(keys[key], false), 100);
        return;
      }

      // if they press any of the dynamic keys, we send a keypress down event but we don't release it until they click it again
      if (Object.keys(modifiers).includes(key)) {
        const currentlyDown = keyNamesForDownKeys.includes(key);
        console.debug(
          `Dynamic key pressed: ${key} was currently down: ${currentlyDown}, toggling state`,
        );
        handleKeyPress(keys[key], !currentlyDown);
        return;
      }

      // otherwise, just treat it as a down+up pair
      const cleanKey = key.replace(/[()]/g, "");
      console.debug(`Regular key pressed: ${cleanKey} sending down and up pair`);
      handleKeyPress(keys[cleanKey], true);
      setTimeout(() => handleKeyPress(keys[cleanKey], false), 50);
    },
    [executeMacro, handleKeyPress, keyNamesForDownKeys],
  );

  return (
    <div
      className="transition-all duration-500 ease-in-out"
      style={{
        marginBottom: isVirtualKeyboardEnabled ? "0px" : `-${350}px`,
      }}
    >
      <AnimatePresence>
        {isVirtualKeyboardEnabled && (
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
                !isAttachedVirtualKeyboardVisible
                  ? "fixed top-0 left-0 z-10 select-none"
                  : "relative",
              )}
              ref={keyboardRef}
              style={{
                ...(!isAttachedVirtualKeyboardVisible
                  ? { transform: `translate(${newPosition.x}px, ${newPosition.y}px)` }
                  : {}),
              }}
            >
              <Card
                className={cx("overflow-hidden", {
                  "rounded-none": isAttachedVirtualKeyboardVisible,
                  "keyboard-detached": !isAttachedVirtualKeyboardVisible,
                })}
              >
                <div className="flex items-center justify-center border-b border-b-slate-800/30 bg-white px-2 py-4 dark:border-b-slate-300/20 dark:bg-slate-800">
                  <div className="absolute left-2 flex items-center gap-x-2">
                    {isAttachedVirtualKeyboardVisible ? (
                      <Button
                        size="XS"
                        theme="light"
                        text="Detach"
                        onClick={() => setAttachedVirtualKeyboardVisibility(false)}
                      />
                    ) : (
                      <Button
                        size="XS"
                        theme="light"
                        text="Attach"
                        onClick={() => setAttachedVirtualKeyboardVisibility(true)}
                      />
                    )}
                  </div>
                  <h2 className="self-center font-sans text-sm leading-none font-medium text-slate-700 select-none dark:text-slate-300">
                    Virtual Keyboard
                  </h2>
                  <div className="absolute right-2 flex items-center gap-x-2">
                    <div className="hidden md:flex gap-x-2 items-center">
                      <LinkButton
                        size="XS"
                        to="settings/keyboard"
                        theme="light"
                        text={selectedKeyboard.name}
                        LeadingIcon={LuKeyboard}
                      />
                      <div className="h-[20px] w-px bg-slate-800/20 dark:bg-slate-200/20" />
                    </div>

                    <Button
                      size="XS"
                      theme="light"
                      text="Hide"
                      LeadingIcon={ChevronDownIcon}
                      onClick={() => setVirtualKeyboardEnabled(false)}
                    />
                  </div>
                </div>

                <div>
                  <div className="flex flex-col bg-blue-50/80 md:flex-row dark:bg-slate-700">
                    <Keyboard
                      baseClass="simple-keyboard-main"
                      layoutName={mainLayoutName}
                      onKeyPress={onKeyDown}
                      onKeyReleased={onKeyUp}
                      buttonTheme={[
                        {
                          class: "combination-key",
                          buttons: "CtrlAltDelete AltMetaEscape CtrlAltBackspace",
                        },
                        {
                          class: "down-key",
                          buttons: keyNamesForDownKeys.join(" "),
                        },
                      ]}
                      display={keyDisplayMap}
                      layout={virtualKeyboard.main}
                      disableButtonHold={true}
                      enableLayoutCandidates={false}
                      preventMouseDownDefault={true}
                      preventMouseUpDefault={true}
                      stopMouseDownPropagation={true}
                      stopMouseUpPropagation={true}
                    />

                    <div className="controlArrows">
                      <Keyboard
                        baseClass="simple-keyboard-control"
                        theme="simple-keyboard hg-theme-default hg-layout-default"
                        layoutName="default"
                        onKeyPress={onKeyDown}
                        onKeyReleased={onKeyUp}
                        display={keyDisplayMap}
                        layout={virtualKeyboard.control}
                        disableButtonHold={true}
                        enableLayoutCandidates={false}
                        preventMouseDownDefault={true}
                        preventMouseUpDefault={true}
                        stopMouseDownPropagation={true}
                        stopMouseUpPropagation={true}
                      />
                      <Keyboard
                        baseClass="simple-keyboard-arrows"
                        theme="simple-keyboard hg-theme-default hg-layout-default"
                        onKeyPress={onKeyDown}
                        onKeyReleased={onKeyUp}
                        display={keyDisplayMap}
                        layout={virtualKeyboard.arrows}
                        disableButtonHold={true}
                        enableLayoutCandidates={false}
                        preventMouseDownDefault={true}
                        preventMouseUpDefault={true}
                        stopMouseDownPropagation={true}
                        stopMouseUpPropagation={true}
                      />
                    </div>
                    {/* TODO add optional number pad */}
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
