import { useCallback, useMemo, useState } from "react";
import { LuCornerDownLeft } from "react-icons/lu";
import { ExclamationCircleIcon } from "@heroicons/react/16/solid";
import { type LoggerMessage } from "tesseract.js";

import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import { TextAreaWithLabel } from "@components/TextArea";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { useUiStore } from "@/hooks/stores";
import useOCR from "@/hooks/useOCR";
import { cx } from "@/cva.config";

export default function OCRModal({ videoElmRef }: { videoElmRef?: React.RefObject<HTMLVideoElement | null> }) {
  const { setDisableVideoFocusTrap } = useUiStore();
  const [ocrStatus, setOcrStatus] = useState<LoggerMessage>();
  const [ocrError, setOcrError] = useState<string | null>(null);
  const handleOcrError = useCallback((error: any) => { // eslint-disable-line @typescript-eslint/no-explicit-any
    if (typeof error === "string") {
      setOcrError(error);
    } else {
      setOcrError(error.message);
    }
  }, [setOcrError]);
  const [ocrText, setOcrText] = useState<string | null>(null);

  const { ocrImage } = useOCR();

  const onConfirmOCR = useCallback(async () => {
    setDisableVideoFocusTrap(true);

    setOcrText(null);
    setOcrError(null);
    setOcrStatus(undefined);

    if (!videoElmRef?.current) {
      setOcrError("Video element not found");
      return;
    }

    setOcrStatus({
      status: "Capturing image",
      progress: 0,
      jobId: "",
      userJobId: "",
      workerId: "",
    });

    // create a canvas from the video element then capture the image from the canvas
    const video = videoElmRef?.current;
    const canvas = document.createElement("canvas");
    canvas.width = video.videoWidth;
    canvas.height = video.videoHeight;

    const ctx = canvas.getContext("2d");
    ctx?.drawImage(video, 0, 0, canvas.width, canvas.height);

    const text = await ocrImage(["eng"], canvas, { logger: setOcrStatus, errorHandler: handleOcrError });
    setOcrText(text);

    setOcrStatus(undefined);
    setOcrError(null);
  }, [
    videoElmRef,
    setDisableVideoFocusTrap,
    ocrImage,
    setOcrStatus,
    setOcrError,
    handleOcrError,
  ]);

  const ocrProgress = useMemo(() => {
    if (!ocrStatus?.progress) return 0;
    return Math.round(ocrStatus?.progress * 100);
  }, [ocrStatus]);

  return (
    <GridCard>
      <div className="space-y-4 p-4 py-3">
        <div className="grid h-full grid-rows-(--grid-headerBody)">
          <div className="h-full space-y-4">
            <div className="space-y-4">
              <SettingsPageHeader
                title="OCR"
                description="OCR text from the video"
              />

              <div
                className={cx("animate-fadeIn space-y-2 opacity-0", ocrText === null ? "hidden" : "")}
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
                  >
                    <TextAreaWithLabel
                      value={ocrText || ""}
                      label="Text"
                      rows={4}
                      readOnly
                      spellCheck={false}
                      data-lt="false"
                      data-gram="false"
                      className="font-mono"
                    />
                  </div>
                </div>
              </div>

              {ocrStatus && <div className={cx("animate-fadeIn space-y-2 opacity-0")}
                style={{
                  animationDuration: "0.7s",
                  animationDelay: "0.1s",
                }}
              >
                <div className="space-y-1 flex justify-between">
                  <p className="text-xs text-slate-600 dark:text-slate-400 capitalize">
                    {ocrStatus?.status}
                  </p>
                  <span className="text-xs text-slate-600 dark:text-slate-400">{ocrProgress}%</span>
                </div>
                <div className="h-2.5 w-full overflow-hidden rounded-full bg-slate-300">
                  <div
                    style={{ width: ocrProgress + "%" }}
                    className="h-2.5 bg-blue-700 transition-all duration-1000 ease-in-out"
                  ></div>
                </div>
              </div>}

              {ocrError && (
                <div className="flex items-center gap-x-2">
                  <ExclamationCircleIcon className="h-4 w-4 text-red-500 dark:text-red-400" />
                  <span className="text-xs text-red-500 dark:text-red-400">
                    {ocrError}
                  </span>
                </div>
              )}
            </div>
          </div>
        </div>
        <div className="gap-y-4">
          <p className="text-xs text-slate-600 dark:text-slate-400">
            Internet connectivity might be required to download Tesseract OCR trained data.
          </p>
        </div>
        <div className="flex animate-fadeIn items-center justify-end gap-x-2 opacity-0"
          style={{
            animationDuration: "0.7s",
            animationDelay: "0.2s",
          }}
        >
          <Button
            size="SM"
            theme="primary"
            text="Start OCR"
            onClick={onConfirmOCR}
            LeadingIcon={LuCornerDownLeft}
          />
        </div>
      </div>
    </GridCard>
  );
}
