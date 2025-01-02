import Card, { GridCard } from "@/components/Card";
import { useEffect, useRef, useState } from "react";
import { Button } from "@components/Button";
import LogoBlueIcon from "@/assets/logo-blue.svg";
import LogoWhiteIcon from "@/assets/logo-white.svg";
import Modal from "@components/Modal";
import {
  PluginManifest,
  usePluginStore,
  useRTCStore,
} from "../hooks/stores";
import { cx } from "../cva.config";
import {
  LuCheck,
  LuUpload,
} from "react-icons/lu";
import { formatters } from "@/utils";
import { PlusCircleIcon } from "@heroicons/react/20/solid";
import AutoHeight from "./AutoHeight";
import { useJsonRpc } from "../hooks/useJsonRpc";
import { ExclamationTriangleIcon } from "@heroicons/react/20/solid";
import notifications from "../notifications";
import { isOnDevice } from "../main";
import { ViewHeader } from "./MountMediaDialog";

export default function UploadPluginModal({
  open,
  setOpen,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
}) {
  return (
    <Modal open={open} onClose={() => setOpen(false)}>
      <Dialog setOpen={setOpen} />
    </Modal>
  );
}

function Dialog({ setOpen }: { setOpen: (open: boolean) => void }) {
  const {
    pluginUploadModalView,
    setPluginUploadModalView,
    pluginUploadFilename,
    setPluginUploadFilename,
    pluginUploadManifest,
    setPluginUploadManifest,
  } = usePluginStore();
  const [send] = useJsonRpc();
  const [extractError, setExtractError] = useState<string | null>(null);

  function extractPlugin(filename: string) {
    send("pluginExtract", { filename }, resp => {
      if ("error" in resp) {
        setExtractError(resp.error.data || resp.error.message);
        return
      }

      setPluginUploadManifest(resp.result as PluginManifest);
    });
  }

  return (
    <AutoHeight>
      <div
        className="mx-auto max-w-4xl px-4 transition-all duration-300 ease-in-out max-w-xl"
      >
        <GridCard cardClassName="relative w-full text-left pointer-events-auto">
          <div className="p-10">
            <div className="flex flex-col items-start justify-start space-y-4 text-left">
              <img
                src={LogoBlueIcon}
                alt="JetKVM Logo"
                className="h-[24px] dark:hidden block"
              />
              <img
                src={LogoWhiteIcon}
                alt="JetKVM Logo"
                className="h-[24px] dark:block hidden dark:!mt-0"
              />

              {!extractError && pluginUploadModalView === "upload" && <UploadFileView
                onBack={() => {
                  setOpen(false)
                }}
                onUploadCompleted={(filename) => {
                  setPluginUploadFilename(filename)
                  setPluginUploadModalView("install")
                  extractPlugin(filename)
                }}
              />}

              {extractError && (
                <ErrorView
                  errorMessage={extractError}
                  onClose={() => {
                    setOpen(false)
                    setPluginUploadFilename(null)
                    setExtractError(null)
                  }}
                  onRetry={() => {
                    setExtractError(null)
                    setPluginUploadFilename(null)
                    setPluginUploadModalView("upload")
                  }}
                />
              )}

              {!extractError && pluginUploadModalView === "install" && <InstallPluginView
                filename={pluginUploadFilename!}
                manifest={pluginUploadManifest}
                onInstall={() => {
                  setOpen(false)
                  setPluginUploadFilename(null)
                  // TODO: Open plugin settings dialog
                }}
                onBack={() => {
                  setPluginUploadModalView("upload")
                  setPluginUploadFilename(null)
                }}
              />}
            </div>
          </div>
        </GridCard>
      </div>
    </AutoHeight>
  );
}

// This is pretty much a copy-paste from the UploadFileView component in the MountMediaDialog just with the media terminology changed and the rpc method changed. 
// TODO: refactor to a shared component
function UploadFileView({
  onBack,
  onUploadCompleted,
}: {
  onBack: () => void;
  onUploadCompleted: (filename: string) => void;
}) {
  const [uploadState, setUploadState] = useState<"idle" | "uploading" | "success">(
    "idle",
  );
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadedFileName, setUploadedFileName] = useState<string | null>(null);
  const [uploadedFileSize, setUploadedFileSize] = useState<number | null>(null);
  const [uploadSpeed, setUploadSpeed] = useState<number | null>(null);
  const [fileError, setFileError] = useState<string | null>(null);
  const [uploadError, setUploadError] = useState<string | null>(null);

  const [send] = useJsonRpc();
  const rtcDataChannelRef = useRef<RTCDataChannel | null>(null);

  useEffect(() => {
    const ref = rtcDataChannelRef.current;
    return () => {
      if (ref) {
        ref.onopen = null;
        ref.onerror = null;
        ref.onmessage = null;
        ref.onclose = null;
        ref.close();
      }
    };
  }, []);

  function handleWebRTCUpload(
    file: File,
    alreadyUploadedBytes: number,
    dataChannel: string,
  ) {
    const rtcDataChannel = useRTCStore
      .getState()
      .peerConnection?.createDataChannel(dataChannel);

    if (!rtcDataChannel) {
      console.error("Failed to create data channel for file upload");
      notifications.error("Failed to create data channel for file upload");
      setUploadState("idle");
      console.log("Upload state set to 'idle'");

      return;
    }

    rtcDataChannelRef.current = rtcDataChannel;

    const lowWaterMark = 256 * 1024;
    const highWaterMark = 1 * 1024 * 1024;
    rtcDataChannel.bufferedAmountLowThreshold = lowWaterMark;

    let lastUploadedBytes = alreadyUploadedBytes;
    let lastUpdateTime = Date.now();
    const speedHistory: number[] = [];

    rtcDataChannel.onmessage = e => {
      try {
        const { AlreadyUploadedBytes, Size } = JSON.parse(e.data) as {
          AlreadyUploadedBytes: number;
          Size: number;
        };

        const now = Date.now();
        const timeDiff = (now - lastUpdateTime) / 1000; // in seconds
        const bytesDiff = AlreadyUploadedBytes - lastUploadedBytes;

        if (timeDiff > 0) {
          const instantSpeed = bytesDiff / timeDiff; // bytes per second

          // Add to speed history, keeping last 5 readings
          speedHistory.push(instantSpeed);
          if (speedHistory.length > 5) {
            speedHistory.shift();
          }

          // Calculate average speed
          const averageSpeed =
            speedHistory.reduce((a, b) => a + b, 0) / speedHistory.length;

          setUploadSpeed(averageSpeed);
          setUploadProgress((AlreadyUploadedBytes / Size) * 100);
        }

        lastUploadedBytes = AlreadyUploadedBytes;
        lastUpdateTime = now;
      } catch (e) {
        console.error("Error processing RTC Data channel message:", e);
      }
    };

    rtcDataChannel.onopen = () => {
      let pauseSending = false; // Pause sending when the buffered amount is high
      const chunkSize = 4 * 1024; // 4KB chunks

      let offset = alreadyUploadedBytes;
      const sendNextChunk = () => {
        if (offset >= file.size) {
          rtcDataChannel.close();
          setUploadState("success");
          onUploadCompleted(file.name);
          return;
        }

        if (pauseSending) return;

        const chunk = file.slice(offset, offset + chunkSize);
        chunk.arrayBuffer().then(buffer => {
          rtcDataChannel.send(buffer);

          if (rtcDataChannel.bufferedAmount >= highWaterMark) {
            pauseSending = true;
          }

          offset += buffer.byteLength;
          console.log(`Chunk sent: ${offset} / ${file.size} bytes`);
          sendNextChunk();
        });
      };

      sendNextChunk();
      rtcDataChannel.onbufferedamountlow = () => {
        console.log("RTC Data channel buffered amount low");
        pauseSending = false; // Now the data channel is ready to send more data
        sendNextChunk();
      };
    };

    rtcDataChannel.onerror = error => {
      console.error("RTC Data channel error:", error);
      notifications.error(`Upload failed: ${error}`);
      setUploadState("idle");
      console.log("Upload state set to 'idle'");
    };
  }

  async function handleHttpUpload(
    file: File,
    alreadyUploadedBytes: number,
    dataChannel: string,
  ) {
    const uploadUrl = `${import.meta.env.VITE_SIGNAL_API}/storage/upload?uploadId=${dataChannel}`;

    const xhr = new XMLHttpRequest();
    xhr.open("POST", uploadUrl, true);

    let lastUploadedBytes = alreadyUploadedBytes;
    let lastUpdateTime = Date.now();
    const speedHistory: number[] = [];

    xhr.upload.onprogress = event => {
      if (event.lengthComputable) {
        const totalUploaded = alreadyUploadedBytes + event.loaded;
        const totalSize = file.size;

        const now = Date.now();
        const timeDiff = (now - lastUpdateTime) / 1000; // in seconds
        const bytesDiff = totalUploaded - lastUploadedBytes;

        if (timeDiff > 0) {
          const instantSpeed = bytesDiff / timeDiff; // bytes per second

          // Add to speed history, keeping last 5 readings
          speedHistory.push(instantSpeed);
          if (speedHistory.length > 5) {
            speedHistory.shift();
          }

          // Calculate average speed
          const averageSpeed =
            speedHistory.reduce((a, b) => a + b, 0) / speedHistory.length;

          setUploadSpeed(averageSpeed);
          setUploadProgress((totalUploaded / totalSize) * 100);
        }

        lastUploadedBytes = totalUploaded;
        lastUpdateTime = now;
      }
    };

    xhr.onload = () => {
      if (xhr.status === 200) {
        setUploadState("success");
        onUploadCompleted(file.name);
      } else {
        console.error("Upload error:", xhr.statusText);
        setUploadError(xhr.statusText);
        setUploadState("idle");
      }
    };

    xhr.onerror = () => {
      console.error("XHR error:", xhr.statusText);
      setUploadError(xhr.statusText);
      setUploadState("idle");
    };

    // Prepare the data to send
    const blob = file.slice(alreadyUploadedBytes);

    // Send the file data
    xhr.send(blob);
  }

  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (file) {
      // Reset the upload error when a new file is selected
      setUploadError(null);

      setFileError(null);
      console.log(`File selected: ${file.name}, size: ${file.size} bytes`);
      setUploadedFileName(file.name);
      setUploadedFileSize(file.size);
      setUploadState("uploading");
      console.log("Upload state set to 'uploading'");

      send("pluginStartUpload", { filename: file.name, size: file.size }, resp => {
        console.log("pluginStartUpload response:", resp);
        if ("error" in resp) {
          console.error("Upload error:", resp.error.message);
          setUploadError(resp.error.data || resp.error.message);
          setUploadState("idle");
          console.log("Upload state set to 'idle'");
          return;
        }

        const { alreadyUploadedBytes, dataChannel } = resp.result as {
          alreadyUploadedBytes: number;
          dataChannel: string;
        };

        console.log(
          `Already uploaded bytes: ${alreadyUploadedBytes}, Data channel: ${dataChannel}`,
        );

        if (isOnDevice) {
          handleHttpUpload(file, alreadyUploadedBytes, dataChannel);
        } else {
          handleWebRTCUpload(file, alreadyUploadedBytes, dataChannel);
        }
      });
    }
  };

  return (
    <div className="w-full space-y-4">
      <ViewHeader
        title="Upload Plugin"
        description="Select a plugin archive TAR to upload to the JetKVM"
      />
      <div
        className="space-y-2 opacity-0 animate-fadeIn"
        style={{
          animationDuration: "0.7s",
        }}
      >
        <div
          onClick={() => {
            if (uploadState === "idle") {
              document.getElementById("file-upload")?.click();
            }
          }}
          className="block select-none"
        >
          <div className="group">
            <Card
              className={cx("transition-all duration-300", {
                "cursor-pointer hover:bg-blue-900/50 dark:hover:bg-blue-900/50": uploadState === "idle",
              })}
            >
              <div className="h-[186px] w-full px-4">
                <div className="flex flex-col items-center justify-center h-full text-center">
                  {uploadState === "idle" && (
                    <div className="space-y-1">
                      <div className="inline-block">
                        <Card>
                          <div className="p-1">
                            <PlusCircleIcon className="w-4 h-4 text-blue-500 dark:text-blue-400 shrink-0" />
                          </div>
                        </Card>
                      </div>
                      <h3 className="text-sm font-semibold leading-none text-black dark:text-white">
                        Click to select a file
                      </h3>
                      <p className="text-xs leading-none text-slate-700 dark:text-slate-300">
                        Supported formats: TAR, TAR.GZ
                      </p>
                    </div>
                  )}

                  {uploadState === "uploading" && (
                    <div className="w-full max-w-sm space-y-2 text-left">
                      <div className="inline-block">
                        <Card>
                          <div className="p-1">
                            <LuUpload className="w-4 h-4 text-blue-500 dark:text-blue-400 shrink-0" />
                          </div>
                        </Card>
                      </div>
                      <h3 className="text-lg font-semibold text-black leading-non dark:text-white">
                        Uploading {formatters.truncateMiddle(uploadedFileName, 30)}
                      </h3>
                      <p className="text-xs leading-none text-slate-700 dark:text-slate-300">
                        {formatters.bytes(uploadedFileSize || 0)}
                      </p>
                      <div className="w-full space-y-2">
                        <div className="h-3.5 w-full overflow-hidden rounded-full bg-slate-300 dark:bg-slate-700">
                          <div
                            className="h-3.5 rounded-full bg-blue-700 dark:bg-blue-500 transition-all duration-500 ease-linear"
                            style={{ width: `${uploadProgress}%` }}
                          ></div>
                        </div>
                        <div className="flex justify-between text-xs text-slate-600 dark:text-slate-400">
                          <span>Uploading...</span>
                          <span>
                            {uploadSpeed !== null
                              ? `${formatters.bytes(uploadSpeed)}/s`
                              : "Calculating..."}
                          </span>
                        </div>
                      </div>
                    </div>
                  )}

                  {uploadState === "success" && (
                    <div className="space-y-1">
                      <div className="inline-block">
                        <Card>
                          <div className="p-1">
                            <LuCheck className="w-4 h-4 text-blue-500 dark:text-blue-400 shrink-0" />
                          </div>
                        </Card>
                      </div>
                      <h3 className="text-sm font-semibold leading-none text-black dark:text-white">
                        Upload successful
                      </h3>
                      <p className="text-xs leading-none text-slate-700 dark:text-slate-300">
                        {formatters.truncateMiddle(uploadedFileName, 40)} has been
                        uploaded
                      </p>
                    </div>
                  )}
                </div>
              </div>
            </Card>
          </div>
        </div>
        <input
          id="file-upload"
          type="file"
          onChange={handleFileChange}
          className="hidden"
          // Can't put .tar.gz as browsers don't support 2 dots
          accept=".tar, .gz"
        />
        {fileError && <p className="mt-2 text-sm text-red-600 dark:text-red-400">{fileError}</p>}
      </div>

      {/* Display upload error if present */}
      {uploadError && (
        <div
          className="mt-2 text-sm text-red-600 truncate opacity-0 dark:text-red-400 animate-fadeIn"
          style={{ animationDuration: "0.7s" }}
        >
          Error: {uploadError}
        </div>
      )}

      <div
        className="flex items-end w-full opacity-0 animate-fadeIn"
        style={{
          animationDuration: "0.7s",
          animationDelay: "0.1s",
        }}
      >
        <div className="flex justify-end w-full space-x-2">
          {uploadState === "uploading" ? (
            <Button
              size="MD"
              theme="light"
              text="Cancel Upload"
              onClick={() => {
                onBack();
                setUploadState("idle");
                setUploadProgress(0);
                setUploadedFileName(null);
                setUploadedFileSize(null);
                setUploadSpeed(null);
              }}
            />
          ) : (
            <Button
              size="MD"
              theme="light"
              text="Back"
              onClick={onBack}
            />
          )}
        </div>
      </div>
    </div>
  );
}

function InstallPluginView({
  filename,
  manifest,
  onInstall,
  onBack,
}: {
  filename: string;
  manifest: PluginManifest | null;
  onInstall: () => void;
  onBack: () => void;
}) {
  const [send] = useJsonRpc();
  const [error, setError] = useState<string | null>(null);
  const [installing, setInstalling] = useState(false);

  function handleInstall() {
    if (installing) return;
    setInstalling(true);
    send("pluginInstall", { name: manifest!.name, version: manifest!.version }, resp => {
      if ("error" in resp) {
        setError(resp.error.message);
        return
      }

      setInstalling(false);
      onInstall();
    });
  }

  return (
    <div className="w-full space-y-4">
      <ViewHeader
        title="Install Plugin"
        description={
          !manifest ?
            `Extracting plugin from ${filename}...` :
            `Do you want to install the plugin?`
        }
      />
      {manifest && (
        <div className="space-y-2">
          <div className="text-sm text-slate-700 dark:text-slate-300">
            <h3 className="text-lg font-semibold">{manifest.name}</h3>
            <p className="text-xs">{manifest.description}</p>
            <p className="text-xs">
              Version: {manifest.version}
            </p>
            <p className="text-xs">
              <a
                href={manifest.homepage}
                target="_blank"
                rel="noreferrer"
                className="text-blue-500 dark:text-blue-400"
              >
                {manifest.homepage}
              </a>
            </p>
          </div>
        </div>
      )}
      {error && (
        <div
          className="mt-2 text-sm text-red-600 truncate opacity-0 dark:text-red-400 animate-fadeIn"
          style={{ animationDuration: "0.7s" }}
        >
          Error: {error}
        </div>
      )}
      <div
        className="space-y-2 opacity-0 animate-fadeIn"
        style={{
          animationDuration: "0.7s",
        }}
      >
        <div className="flex justify-end w-full space-x-2">
          <Button
            size="MD"
            theme="light"
            text="Cancel"
            onClick={() => {
              // TODO: Delete the orphaned extraction
              setError(null);
              onBack();
            }}
          />
          <Button
            size="MD"
            theme="primary"
            text="Install"
            onClick={handleInstall}
          />
        </div>
      </div>
    </div>
  );
}

function ErrorView({
  errorMessage,
  onClose,
  onRetry,
}: {
  errorMessage: string | null;
  onClose: () => void;
  onRetry: () => void;
}) {
  return (
    <div className="w-full space-y-4">
      <div className="space-y-2">
        <div className="flex items-center space-x-2 text-red-600">
          <ExclamationTriangleIcon className="w-6 h-6" />
          <h2 className="text-lg font-bold leading-tight">Plugin Extract Error</h2>
        </div>
        <p className="text-sm leading-snug text-slate-600">
          An error occurred while attempting to extract the plugin. Please ensure the plugin is valid and try again.
        </p>
      </div>
      {errorMessage && (
        <Card className="p-4 border border-red-200 bg-red-50">
          <p className="text-sm font-medium text-red-800">{errorMessage}</p>
        </Card>
      )}
      <div className="flex justify-end space-x-2">
        <Button size="SM" theme="light" text="Close" onClick={onClose} />
        <Button size="SM" theme="primary" text="Back to Upload" onClick={onRetry} />
      </div>
    </div>
  );
}