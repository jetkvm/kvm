import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useClose } from "@headlessui/react";
import { ExclamationCircleIcon } from "@heroicons/react/16/solid";
import { LuCornerDownLeft, LuEye, LuEyeOff } from "react-icons/lu";

import { cx } from "@/cva.config";
import { m } from "@localizations/messages.js";
import { useHidStore, useSettingsStore, useUiStore } from "@hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import useKeyboard, { type MacroStep } from "@hooks/useKeyboard";
import useKeyboardLayout from "@hooks/useKeyboardLayout";
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
  const [textValue, setTextValue] = useState("");
  const [hideText, setHideText] = useState(false);
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
  const delayClassName = useMemo(() => (debugMode ? "" : "hidden"), [debugMode]);

  const { setKeyboardLayout } = useSettingsStore();
  const { selectedKeyboard } = useKeyboardLayout();

  useEffect(() => {
    void send("getKeyboardLayout", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setKeyboardLayout(resp.result as string);
    });
  }, [send, setKeyboardLayout]);

  const onCancelPasteMode = useCallback(() => {
    void cancelExecuteMacro();
    setDisableVideoFocusTrap(false);
    setInvalidChars([]);
  }, [setDisableVideoFocusTrap, cancelExecuteMacro]);

  const updateInvalidChars = useCallback(
    (value: string) => {
      const chars = [
        ...new Set(
          [...(new Intl.Segmenter().segment(value) ?? [])]
            .map(x => x.segment.normalize("NFC"))
            .filter(char => !selectedKeyboard?.chars[char]),
        ),
      ];
      setInvalidChars(chars);
    },
    [selectedKeyboard],
  );

  const onConfirmPaste = useCallback(async () => {
    if (!selectedKeyboard) return;

    const text = textValue;

    try {
      const macroSteps: MacroStep[] = [];

      for (const char of text) {
        const normalizedChar = char.normalize("NFC");
        const keyprops = selectedKeyboard.chars[normalizedChar];
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
          delay,
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
      notifications.error(m.paste_modal_failed_paste({ error: String(error) }));
    }
  }, [selectedKeyboard, executeMacro, delay, textValue]);

  useEffect(() => {
    TextAreaRef.current?.focus();
  }, [hideText]);

  return (
    <GridCard>
      <div className="space-y-4 p-4 py-3">
        <div className="grid h-full grid-rows-(--grid-headerBody)">
          <div className="h-full space-y-4">
            <div className="space-y-4">
              <SettingsPageHeader title={m.paste_text()} description={m.paste_text_description()} />

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
                    <div className="space-y-1">
                      <TextAreaWithLabel
                        ref={TextAreaRef}
                        label={
                          <div className="flex items-center justify-between">
                            <div className="font-display text-[13px] leading-snug font-semibold text-black dark:text-white">
                              {m.paste_modal_paste_from_host()}
                            </div>
                            <button
                              type="button"
                              onClick={() => setHideText(!hideText)}
                              className="flex items-center gap-1 text-xs font-normal text-slate-500 hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-200"
                            >
                              {hideText ? (
                                <>
                                  <LuEyeOff className="h-3.5 w-3.5" />
                                  {m.paste_modal_show_text()}
                                </>
                              ) : (
                                <>
                                  <LuEye className="h-3.5 w-3.5" />
                                  {m.paste_modal_hide_text()}
                                </>
                              )}
                            </button>
                          </div>
                        }
                        rows={4}
                        value={textValue}
                        style={hideText ? { WebkitTextSecurity: "disc" } : undefined}
                        onKeyUp={e => e.stopPropagation()}
                        maxLength={pasteMaxLength}
                        onKeyDown={e => {
                          e.stopPropagation();
                          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                            e.preventDefault();
                            void onConfirmPaste();
                          } else if (e.key === "Escape") {
                            e.preventDefault();
                            onCancelPasteMode();
                          }
                        }}
                        onChange={e => {
                          const value = e.target.value;
                          setTextValue(value);
                          updateInvalidChars(value);
                        }}
                      />
                    </div>

                    {invalidChars.length > 0 && (
                      <div className="mt-2 flex items-center gap-x-2">
                        <ExclamationCircleIcon className="h-4 w-4 text-red-500 dark:text-red-400" />
                        <span className="text-xs text-red-500 dark:text-red-400">
                          {hideText
                            ? m.paste_modal_invalid_chars_hidden()
                            : `${m.paste_modal_invalid_chars_intro()} ${invalidChars.join(", ")}`}
                        </span>
                      </div>
                    )}
                  </div>
                </div>
                <div className={cx("text-xs text-slate-600 dark:text-slate-400", delayClassName)}>
                  <InputFieldWithLabel
                    type="number"
                    label={m.paste_modal_delay_between_keys()}
                    placeholder={m.paste_modal_delay_between_keys()}
                    min={50}
                    max={65534}
                    value={delayValue}
                    onChange={e => {
                      setDelayValue(parseInt(e.target.value, 10));
                    }}
                  />
                  {delayValue < 50 ||
                    (delayValue > 65534 && (
                      <div className="mt-2 flex items-center gap-x-2">
                        <ExclamationCircleIcon className="h-4 w-4 text-red-500 dark:text-red-400" />
                        <span className="text-xs text-red-500 dark:text-red-400">
                          {m.paste_modal_delay_out_of_range({ min: 50, max: 65534 })}
                        </span>
                      </div>
                    ))}
                </div>
                <div className="space-y-4">
                  <p className="text-xs text-slate-600 dark:text-slate-400">
                    {m.paste_modal_sending_using_layout({
                      iso: selectedKeyboard.isoCode,
                      name: selectedKeyboard.name,
                    })}
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
            text={m.cancel()}
            onClick={() => {
              onCancelPasteMode();
              close();
            }}
          />
          <Button
            size="SM"
            theme="primary"
            text={m.paste_modal_confirm_paste()}
            disabled={isPasteInProgress}
            onClick={onConfirmPaste}
            LeadingIcon={LuCornerDownLeft}
          />
        </div>
      </div>
    </GridCard>
  );
}
