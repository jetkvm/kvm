import { useClose } from "@headlessui/react";
import { ExclamationCircleIcon } from "@heroicons/react/16/solid";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { LuCornerDownLeft } from "react-icons/lu";

import { cx } from "@/cva.config";
import { useHidStore, useSettingsStore, useUiStore } from "@/hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import useKeyboard, { type MacroStep } from "@/hooks/useKeyboard";
import useKeyboardLayout from "@/hooks/useKeyboardLayout";
import notifications from "@/notifications";
import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import { InputFieldWithLabel } from "@components/InputField";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { TextAreaWithLabel } from "@components/TextArea";

// uint32 max value / 4
const pasteMaxLength = 1073741824;
const defaultDelay = 20;

export default function PasteModal() {
  const TextAreaRef = useRef<HTMLTextAreaElement>(null);
  const { isPasteInProgress } = useHidStore();
  const { setDisableVideoFocusTrap } = useUiStore();

  const { send } = useJsonRpc();
  const { executeMacro, cancelExecuteMacro } = useKeyboard();

  const [invalidChars, setInvalidChars] = useState<string[]>([]);
  const [delayValue, setDelayValue] = useState(defaultDelay);
  const delay = useMemo(() => {
    if (delayValue < 0 || delayValue > 65534) {
      return defaultDelay;
    }
    return delayValue;
  }, [delayValue]);
  const close = useClose();

  const debugMode = useSettingsStore(state => state.debugMode);
  const delayClassName = useMemo(() => debugMode ? "" : "hidden", [debugMode]);

  const { setKeyboardLayout } = useSettingsStore();
  const { selectedKeyboard } = useKeyboardLayout();

  useEffect(() => {
    send("getKeyboardLayout", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setKeyboardLayout(resp.result as string);
    });
  }, [send, setKeyboardLayout]);

  const onCancelPasteMode = useCallback(() => {
    cancelExecuteMacro();
    setDisableVideoFocusTrap(false);
    setInvalidChars([]);
  }, [setDisableVideoFocusTrap, cancelExecuteMacro]);

  const onConfirmPaste = useCallback(async () => {
    if (!TextAreaRef.current || !selectedKeyboard) return;

    const text = TextAreaRef.current.value;

    try {
      const macroSteps: MacroStep[] = [];

      for (const char of text) {
        const keyprops = selectedKeyboard.chars[char];
        if (!keyprops) continue;

        const { key, shift, altRight, deadKey, accentKey } = keyprops;
        if (!key) continue;

        // if this is an accented character, we need to send that accent FIRST
        if (accentKey) {
          const accentModifiers: string[] = [];
          if (accentKey.shift) accentModifiers.push("ShiftLeft");
          if (accentKey.altRight) accentModifiers.push("AltRight");

          macroSteps.push({
            keys: [String(accentKey.key)],
            modifiers: accentModifiers.length > 0 ? accentModifiers : null,
            delay,
          });
        }

        // now send the actual key
        const modifiers: string[] = [];
        if (shift) modifiers.push("ShiftLeft");
        if (altRight) modifiers.push("AltRight");

        macroSteps.push({
          keys: [String(key)],
          modifiers: modifiers.length > 0 ? modifiers : null,
          delay
        });

        // if what was requested was a dead key, we need to send an unmodified space to emit
        // just the accent character
        if (deadKey) macroSteps.push({ keys: ["Space"], modifiers: null, delay });
      }

      if (macroSteps.length > 0) {
        await executeMacro(macroSteps);
      }
    } catch (error) {
      console.error("Failed to paste text:", error);
      notifications.error("Failed to paste text");
    }
  }, [selectedKeyboard, executeMacro, delay]);

  useEffect(() => {
    if (TextAreaRef.current) {
      TextAreaRef.current.focus();
    }
  }, []);

  return (
    <GridCard>
      <div className="space-y-4 p-4 py-3">
        <div className="grid h-full grid-rows-(--grid-headerBody)">
          <div className="h-full space-y-4">
            <div className="space-y-4">
              <SettingsPageHeader
                title="Paste text"
                description="Paste text from your client to the remote host"
              />

              <div
                className="animate-fadeIn space-y-2 opacity-0"
                style={{
                  animationDuration: "0.7s",
                  animationDelay: "0.1s",
                }}
              >
                <div>
                  <div
                    className="w-full"
                    onKeyUp={e => e.stopPropagation()}
                    onKeyDown={e => e.stopPropagation()}
                    onKeyDownCapture={e => e.stopPropagation()}
                    onKeyUpCapture={e => e.stopPropagation()}
                  >
                    <TextAreaWithLabel
                      ref={TextAreaRef}
                      label="Paste from host"
                      rows={4}
                      onKeyUp={e => e.stopPropagation()}
                      maxLength={pasteMaxLength}
                      onKeyDown={e => {
                        e.stopPropagation();
                        if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                          e.preventDefault();
                          onConfirmPaste();
                        } else if (e.key === "Escape") {
                          e.preventDefault();
                          onCancelPasteMode();
                        }
                      }}
                      onChange={e => {
                        const value = e.target.value;
                        const invalidChars = [
                          ...new Set(
                            // @ts-expect-error TS doesn't recognize Intl.Segmenter in some environments
                            [...new Intl.Segmenter().segment(value)]
                              .map(x => x.segment)
                              .filter(char => !selectedKeyboard.chars[char]),
                          ),
                        ];

                        setInvalidChars(invalidChars);
                      }}
                    />

                    {invalidChars.length > 0 && (
                      <div className="mt-2 flex items-center gap-x-2">
                        <ExclamationCircleIcon className="h-4 w-4 text-red-500 dark:text-red-400" />
                        <span className="text-xs text-red-500 dark:text-red-400">
                          The following characters won&apos;t be pasted:{" "}
                          {invalidChars.join(", ")}
                        </span>
                      </div>
                    )}
                  </div>
                </div>
                <div className={cx("text-xs text-slate-600 dark:text-slate-400", delayClassName)}>
                  <InputFieldWithLabel
                    type="number"
                    label="Delay between keys"
                    placeholder="Delay between keys"
                    min={50}
                    max={65534}
                    value={delayValue}
                    onChange={e => {
                      setDelayValue(parseInt(e.target.value, 10));
                    }}
                  />
                  {delayValue < 50 || delayValue > 65534 && (
                    <div className="mt-2 flex items-center gap-x-2">
                      <ExclamationCircleIcon className="h-4 w-4 text-red-500 dark:text-red-400" />
                      <span className="text-xs text-red-500 dark:text-red-400">
                        Delay must be between 50 and 65534
                      </span>
                    </div>
                  )}
                </div>
                <div className="space-y-4">
                  <p className="text-xs text-slate-600 dark:text-slate-400">
                    Sending text using keyboard layout: {selectedKeyboard.isoCode}-
                    {selectedKeyboard.name}
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div
          className="flex animate-fadeIn items-center justify-end gap-x-2 opacity-0"
          style={{
            animationDuration: "0.7s",
            animationDelay: "0.2s",
          }}
        >
          <Button
            size="SM"
            theme="blank"
            text="Cancel"
            onClick={() => {
              onCancelPasteMode();
              close();
            }}
          />
          <Button
            size="SM"
            theme="primary"
            text="Confirm Paste"
            disabled={isPasteInProgress}
            onClick={onConfirmPaste}
            LeadingIcon={LuCornerDownLeft}
          />
        </div>
      </div>
    </GridCard>
  );
}
