import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import { TextAreaWithLabel } from "@components/TextArea";
import { SectionHeader } from "@components/SectionHeader";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { useHidStore, useRTCStore, useUiStore } from "@/hooks/stores";
import notifications from "../../notifications";
import { useCallback, useEffect, useRef, useState } from "react";
import { LuCornerDownLeft } from "react-icons/lu";
import { ExclamationCircleIcon } from "@heroicons/react/16/solid";
import { useClose } from "@headlessui/react";
import { chars, keys, modifiers } from "@/keyboardMappings";

const hidKeyboardPayload = (keys: number[], modifier: number) => {
  return { keys, modifier };
};

export default function PasteModal() {
  const TextAreaRef = useRef<HTMLTextAreaElement>(null);
  const setPasteMode = useHidStore(state => state.setPasteModeEnabled);
  const setDisableVideoFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);

  const [send] = useJsonRpc();
  const rpcDataChannel = useRTCStore(state => state.rpcDataChannel);

  const [invalidChars, setInvalidChars] = useState<string[]>([]);
  const close = useClose();

  const onCancelPasteMode = useCallback(() => {
    setPasteMode(false);
    setDisableVideoFocusTrap(false);
    setInvalidChars([]);
  }, [setDisableVideoFocusTrap, setPasteMode]);

  const onConfirmPaste = useCallback(async () => {
    setPasteMode(false);
    setDisableVideoFocusTrap(false);
    if (rpcDataChannel?.readyState !== "open" || !TextAreaRef.current) return;

    const text = TextAreaRef.current.value;

    try {
      for (const char of text) {
        const { key, shift } = chars[char] ?? {};
        if (!key) continue;

        await new Promise<void>((resolve, reject) => {
          send(
            "keyboardReport",
            hidKeyboardPayload([keys[key]], shift ? modifiers["ShiftLeft"] : 0),
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
    } catch (error) {
      notifications.error("Failed to paste text");
    }
  }, [rpcDataChannel?.readyState, send, setDisableVideoFocusTrap, setPasteMode]);

  useEffect(() => {
    if (TextAreaRef.current) {
      TextAreaRef.current.focus();
    }
  }, []);

  return (
    <GridCard>
      <div className="p-4 py-3 space-y-4">
        <div className="grid h-full grid-rows-headerBody">
          <div className="h-full space-y-4">
            <div className="space-y-4">
              <SectionHeader
                title="Paste text"
                description="Paste text from your client to the remote host"
              />

              <div
                className="space-y-2 opacity-0 animate-fadeIn"
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
                              .filter(char => !chars[char]),
                          ),
                        ];

                        setInvalidChars(invalidChars);
                      }}
                    />

                    {invalidChars.length > 0 && (
                      <div className="flex items-center mt-2 gap-x-2">
                        <ExclamationCircleIcon className="w-4 h-4 text-red-500 dark:text-red-400" />
                        <span className="text-xs text-red-500 dark:text-red-400">
                          The following characters won&apos;t be pasted:{" "}
                          {invalidChars.join(", ")}
                        </span>
                      </div>
                    )}
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div
          className="flex items-center justify-end opacity-0 animate-fadeIn gap-x-2"
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
