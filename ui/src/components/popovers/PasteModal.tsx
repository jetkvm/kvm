import { useCallback, useEffect, useRef, useState } from "react";
import { LuCornerDownLeft } from "react-icons/lu";
import { ExclamationCircleIcon } from "@heroicons/react/16/solid";
import { useClose } from "@headlessui/react";

import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import { TextAreaWithLabel } from "@components/TextArea";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { useHidStore, useRTCStore, useUiStore, useSettingsStore } from "@/hooks/stores";
import { keys, modifiers } from "@/keyboardMappings";
import { KeyStroke } from "@/keyboardLayouts";
import useKeyboardLayout from "@/hooks/useKeyboardLayout";
import notifications from "@/notifications";

const hidKeyboardPayload = (modifier: number, keys: number[]) => {
  return { modifier, keys };
};

const modifierCode = (shift?: boolean, altRight?: boolean) => {
  return (shift ? modifiers.ShiftLeft : 0)
       | (altRight ? modifiers.AltRight : 0)
}
const noModifier = 0

export default function PasteModal() {
  const TextAreaRef = useRef<HTMLTextAreaElement>(null);
  const { setPasteModeEnabled } = useHidStore();
  const { setDisableVideoFocusTrap } = useUiStore();

  const { send } = useJsonRpc();
  const { rpcDataChannel } = useRTCStore();

  const [invalidChars, setInvalidChars] = useState<string[]>([]);
  const close = useClose();

  const { setKeyboardLayout } = useSettingsStore();
  const { selectedKeyboard }  = useKeyboardLayout();

  useEffect(() => {
    send("getKeyboardLayout", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setKeyboardLayout(resp.result as string);
    });
  }, [send, setKeyboardLayout]);

  const onCancelPasteMode = useCallback(() => {
    setPasteModeEnabled(false);
    setDisableVideoFocusTrap(false);
    setInvalidChars([]);
  }, [setDisableVideoFocusTrap, setPasteModeEnabled]);

  const onConfirmPaste = useCallback(async () => {
    setPasteModeEnabled(false);
    setDisableVideoFocusTrap(false);

    if (rpcDataChannel?.readyState !== "open" || !TextAreaRef.current) return;
    if (!selectedKeyboard) return;

    const text = TextAreaRef.current.value;

    try {
      for (const char of text) {
        const keyprops = selectedKeyboard.chars[char];
        if (!keyprops) continue;

        const { key, shift, altRight, deadKey, accentKey } = keyprops;
        if (!key) continue;

        // if this is an accented character, we need to send that accent FIRST
        if (accentKey) {
          await sendKeystroke({modifier: modifierCode(accentKey.shift, accentKey.altRight), keys: [ keys[accentKey.key] ] })
        }

        // now send the actual key
        await sendKeystroke({ modifier: modifierCode(shift, altRight), keys: [ keys[key] ]});

        // if what was requested was a dead key, we need to send an unmodified space to emit
        // just the accent character
        if (deadKey) {
           await sendKeystroke({ modifier: noModifier, keys: [ keys["Space"] ] });
        }

        // now send a message with no keys down to "release" the keys
        await sendKeystroke({ modifier: 0, keys: [] });
      }
    } catch (error) {
      console.error("Failed to paste text:", error);
      notifications.error("Failed to paste text");
    }

    async function sendKeystroke(stroke: KeyStroke) {
      await new Promise<void>((resolve, reject) => {
        send(
          "keyboardReport",
          hidKeyboardPayload(stroke.modifier, stroke.keys),
          params => {
            if ("error" in params) return reject(params.error);
            resolve();
          }
        );
      });
    }
  }, [selectedKeyboard, rpcDataChannel?.readyState, send, setDisableVideoFocusTrap, setPasteModeEnabled]);

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
                className="animate-fadeIn opacity-0 space-y-2"
                style={{
                  animationDuration: "0.7s",
                  animationDelay: "0.1s",
                }}
              >
                <div>
                  <div className="w-full" onKeyUp={e => e.stopPropagation()} onKeyDown={e => e.stopPropagation()}>
                    <TextAreaWithLabel
                      ref={TextAreaRef}
                      label="Paste from host"
                      rows={4}
                      onKeyUp={e => e.stopPropagation()}
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
                <div className="space-y-4">
                  <p className="text-xs text-slate-600 dark:text-slate-400">
                    Sending text using keyboard layout: {selectedKeyboard.isoCode}-{selectedKeyboard.name}
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div
          className="flex animate-fadeIn opacity-0 items-center justify-end gap-x-2"
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
            onClick={onConfirmPaste}
            LeadingIcon={LuCornerDownLeft}
          />
        </div>
      </div>
    </GridCard>
  );
}
