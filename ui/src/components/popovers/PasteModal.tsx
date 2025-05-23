import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { LuCornerDownLeft } from "react-icons/lu";
import { ExclamationCircleIcon } from "@heroicons/react/16/solid";
import { useClose } from "@headlessui/react";

import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import { TextAreaWithLabel } from "@components/TextArea";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { useHidStore, useRTCStore, useUiStore, useSettingsStore } from "@/hooks/stores";
import { keys, modifiers } from "@/keyboardMappings";
import { layouts, chars } from "@/keyboardLayouts";
import notifications from "@/notifications";

const hidKeyboardPayload = (keys: number[], modifier: number) => {
  return { keys, modifier };
};

const modifierCode = (shift?: boolean, altRight?: boolean) => {
  return (shift ? modifiers["ShiftLeft"] : 0)
       | (altRight ? modifiers["AltRight"] : 0)
}
const noModifier = 0

export default function PasteModal() {
  const TextAreaRef = useRef<HTMLTextAreaElement>(null);
  const setPasteMode = useHidStore(state => state.setPasteModeEnabled);
  const setDisableVideoFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);

  const [send] = useJsonRpc();
  const rpcDataChannel = useRTCStore(state => state.rpcDataChannel);

  const [invalidChars, setInvalidChars] = useState<string[]>([]);
  const close = useClose();

  const keyboardLayout = useSettingsStore(state => state.keyboardLayout);
  const setKeyboardLayout = useSettingsStore(
    state => state.setKeyboardLayout,
  );

  // this ensures we always get the original en-US if it hasn't been set yet
  const safeKeyboardLayout = useMemo(() => {
    if (keyboardLayout && keyboardLayout.length > 0)
      return keyboardLayout;
    return "en-US";
  }, [keyboardLayout]);

  useEffect(() => {
    send("getKeyboardLayout", {}, resp => {
      if ("error" in resp) return;
      setKeyboardLayout(resp.result as string);
    });
  }, [send, setKeyboardLayout]);

  const onCancelPasteMode = useCallback(() => {
    setPasteMode(false);
    setDisableVideoFocusTrap(false);
    setInvalidChars([]);
  }, [setDisableVideoFocusTrap, setPasteMode]);

  const onConfirmPaste = useCallback(async () => {
    setPasteMode(false);
    setDisableVideoFocusTrap(false);
    if (rpcDataChannel?.readyState !== "open" || !TextAreaRef.current) return;
    if (!safeKeyboardLayout) return;
    if (!chars[safeKeyboardLayout]) return;
    const text = TextAreaRef.current.value;

    try {
      for (const char of text) {
        const { key, shift, altRight, deadKey, accentKey } = chars[safeKeyboardLayout][char]
        if (!key) continue;

        const keyz = [ keys[key] ];
        const modz = [ modifierCode(shift, altRight) ];

        if (deadKey) {
            keyz.push(keys["Space"]);
            modz.push(noModifier);
        }
        if (accentKey) {
            keyz.unshift(keys[accentKey.key])
            modz.unshift(modifierCode(accentKey.shift, accentKey.altRight))
        }

        for (const [index, kei] of keyz.entries()) {
          await new Promise<void>((resolve, reject) => {
            send(
              "keyboardReport",
              hidKeyboardPayload([kei], modz[index]),
              params => {
                if ("error" in params) return reject(params.error);
                send("keyboardReport", hidKeyboardPayload([], 0), params => {
                  if ("error" in params) return reject(params.error);
                  resolve();
                });
              },
            );
          });
        }
      }
    } catch (error) {
      console.error(error);
      notifications.error("Failed to paste text");
    }
  }, [rpcDataChannel?.readyState, send, setDisableVideoFocusTrap, setPasteMode, safeKeyboardLayout]);

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
                  <div className="w-full" onKeyUp={e => e.stopPropagation()}>
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
                              .filter(char => !chars[safeKeyboardLayout][char]),
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
                    Sending text using keyboard layout: {layouts[safeKeyboardLayout]}
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
