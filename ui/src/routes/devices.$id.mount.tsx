import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router";
import {
  LuLink,
  LuRadioReceiver,
  LuCheck,
  LuUpload,
} from "react-icons/lu";
import { PlusCircleIcon, ExclamationTriangleIcon } from "@heroicons/react/20/solid";
import { TrashIcon } from "@heroicons/react/16/solid";

import DebianIcon from "@assets/debian-icon.png";
import UbuntuIcon from "@assets/ubuntu-icon.png";
import FedoraIcon from "@assets/fedora-icon.png";
import OpenSUSEIcon from "@assets/opensuse-icon.png";
import ArchIcon from "@assets/arch-icon.png";
import NetBootIcon from "@assets/netboot-icon.svg";
import LogoBlueIcon from "@assets/logo-blue.svg";
import LogoWhiteIcon from "@assets/logo-white.svg";
import { cx } from "@/cva.config";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import AutoHeight from "@components/AutoHeight";
import { Button } from "@components/Button";
import Card, { GridCard } from "@components/Card";
import Fieldset from "@components/Fieldset";
import { InputFieldWithLabel } from "@/components/InputField";
import { formatters } from "@/utils";
import { DEVICE_API } from "@/ui.config";
import { isOnDevice } from "@/main";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

import {
  MountMediaState,
  RemoteVirtualMediaState,
  useMountMediaStore,
  useRTCStore,
} from "../hooks/stores";

export default function MountRoute() {
  const navigate = useNavigate();
  return <Dialog onClose={() => navigate("..")} />;
}

export function Dialog({ onClose }: Readonly<{ onClose: () => void }>) {
  const {
    modalView,
    setModalView,
    setRemoteVirtualMediaState,
    errorMessage,
    setErrorMessage,
  } = useMountMediaStore();
  const navigate = useNavigate();

  const [incompleteFileName, setIncompleteFileName] = useState<string | null>(null);
  const [mountInProgress, setMountInProgress] = useState(false);
  function clearMountMediaState() {
    setRemoteVirtualMediaState(null);
  }

  const { send } = useJsonRpc();
  async function syncRemoteVirtualMediaState() {
    return new Promise((resolve, reject) => {
      send("getVirtualMediaState", {}, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          reject(new Error(resp.error.message));
        } else {
          setRemoteVirtualMediaState(
            resp as unknown as MountMediaState["remoteVirtualMediaState"],
          );
          resolve(null);
        }
      });
    });
  }

  function triggerError(message: string) {
    setErrorMessage(message);
    setModalView("error");
  }

  function handleUrlMount(url: string, mode: RemoteVirtualMediaState["mode"]) {
    console.log(`Mounting ${url} as ${mode}`);

    setMountInProgress(true);
    send("mountWithHTTP", { url, mode }, (resp: JsonRpcResponse) => {
      if ("error" in resp) triggerError(resp.error.message);

      clearMountMediaState();
      syncRemoteVirtualMediaState()
        .then(() => navigate(".."))
        .catch(err => {
          triggerError(err instanceof Error ? err.message : String(err));
        })
        .finally(() => {
          setMountInProgress(false);
        });
    });
  }

  function handleStorageMount(fileName: string, mode: RemoteVirtualMediaState["mode"]) {
    console.log(`Mounting ${fileName} as ${mode}`);

    setMountInProgress(true);
    send("mountWithStorage", { filename: fileName, mode }, (resp: JsonRpcResponse) => {
      if ("error" in resp) triggerError(resp.error.message);

      clearMountMediaState();
      syncRemoteVirtualMediaState()
        .then(() => {
          navigate("..");
        })
        .catch(err => {
          triggerError(err instanceof Error ? err.message : String(err));
        })
        .finally(() => {
          // We do this because the mounting is too fast and the UI gets choppy
          // and the modal exit animation for like 500ms
          setTimeout(() => {
            setMountInProgress(false);
          }, 500);
        });
    });

    clearMountMediaState();
  }

  const [selectedMode, setSelectedMode] = useState<"url" | "device">("url");
  return (
    <AutoHeight>
      <div
        className={cx("mx-auto max-w-4xl px-4 transition-all duration-300 ease-in-out", {
          "max-w-4xl": modalView === "mode",
          "max-w-2xl": modalView === "device",
          "max-w-xl":
            modalView === "url" ||
            modalView === "upload" ||
            modalView === "error",
        })}
      >
        <GridCard cardClassName="relative w-full text-left pointer-events-auto">
          <div className="p-10">
            <div className="flex flex-col items-start justify-start space-y-4 text-left">
              <img
                src={LogoBlueIcon}
                alt={m.jetkvm_logo()}
                className="block h-[24px] dark:hidden"
              />
              <img
                src={LogoWhiteIcon}
                alt={m.jetkvm_logo()}
                className="hidden h-[24px] dark:mt-0! dark:block"
              />
              {modalView === "mode" && (
                <ModeSelectionView
                  onClose={() => onClose()}
                  selectedMode={selectedMode}
                  setSelectedMode={setSelectedMode}
                />
              )}

              {modalView === "url" && (
                <UrlView
                  mountInProgress={mountInProgress}
                  onBack={() => {
                    setMountInProgress(false);
                    setModalView("mode");
                  }}
                  onMount={(url, mode) => {
                    handleUrlMount(url, mode);
                  }}
                />
              )}

              {modalView === "device" && (
                <DeviceFileView
                  onBack={() => {
                    setMountInProgress(false);
                    setModalView("mode");
                  }}
                  mountInProgress={mountInProgress}
                  onMountStorageFile={(fileName, mode) => {
                    handleStorageMount(fileName, mode);
                  }}
                  onNewImageClick={incompleteFile => {
                    setIncompleteFileName(incompleteFile || null);
                    setModalView("upload");
                  }}
                />
              )}

              {modalView === "upload" && (
                <UploadFileView
                  onBack={() => setModalView("device")}
                  onCancelUpload={() => {
                    setModalView("device");
                    // Implement cancel upload logic here
                  }}
                  incompleteFileName={incompleteFileName || undefined}
                />
              )}

              {modalView === "error" && (
                <ErrorView
                  errorMessage={errorMessage}
                  onClose={() => {
                    onClose();
                    setErrorMessage(null);
                  }}
                  onRetry={() => {
                    setModalView("mode");
                    setErrorMessage(null);
                  }}
                />
              )}
            </div>
          </div>
        </GridCard>
      </div>
    </AutoHeight>
  );
}

function ModeSelectionView({
  onClose,
  selectedMode,
  setSelectedMode,
}: {
  onClose: () => void;
  selectedMode: "url" | "device";
  setSelectedMode: (mode: "url" | "device") => void;
}) {
  const { setModalView } = useMountMediaStore();

  return (
    <div className="w-full space-y-4">
      <div className="animate-fadeIn space-y-0 opacity-0">
        <h2 className="text-lg leading-tight font-bold dark:text-white">
          {m.mount_virtual_media_source()}
        </h2>
        <div className="text-sm leading-snug text-slate-600 dark:text-slate-400">
          {m.mount_virtual_media_source_description()}
        </div>
      </div>
      <div className="grid gap-4 md:grid-cols-3">
        {[
          {
            label: m.mount_url_mount(),
            value: "url",
            description: m.mount_url_description(),
            icon: LuLink,
            tag: m.experimental(),
            disabled: false,
          },
          {
            label: m.mount_jetkvm_storage(),
            value: "device",
            description: m.mount_jetkvm_storage_description(),
            icon: LuRadioReceiver,
            tag: null,
            disabled: false,
          },
        ].map(({ label, description, value: mode, icon: Icon, tag, disabled }, index) => (
          <div
            key={label}
            className="animate-fadeIn opacity-0"
            style={{
              animationDuration: "0.7s",
              animationDelay: `${25 * (index * 5)}ms`,
            }}
          >
            <Card
              className={cx(
                "w-full min-w-[250px] cursor-pointer bg-white shadow-xs transition-all duration-100 hover:shadow-md dark:bg-slate-800",
                {
                  "ring-2 ring-blue-700": selectedMode === mode,
                  "hover:ring-2 hover:ring-blue-500": selectedMode !== mode && !disabled,
                  "cursor-not-allowed!": disabled,
                },
              )}
            >
              <div
                className="relative z-50 flex flex-col items-start p-4 select-none"
                onClick={() =>
                  disabled ? null : setSelectedMode(mode as "url" | "device")
                }
              >
                <div>
                  <Card>
                    <div className="p-1">
                      <Icon className="h-4 w-4 shrink-0 text-blue-700 dark:text-blue-400" />
                    </div>
                  </Card>
                </div>
                <div className="mt-2 space-y-0">
                  <p className="block pt-1 text-xs text-red-500">
                    {tag ? tag : <>&nbsp;</>}
                  </p>

                  <h3 className="text-sm font-medium dark:text-white">{label}</h3>
                  <p className="text-sm text-gray-700 dark:text-slate-400">
                    {description}
                  </p>
                </div>
                <input
                  type="radio"
                  name="localAuthMode"
                  value={mode}
                  disabled={disabled}
                  checked={selectedMode === mode}
                  className="absolute top-4 right-4 form-radio h-4 w-4 rounded-full text-blue-700"
                />
              </div>
            </Card>
          </div>
        ))}
      </div>
      <div
        className="flex animate-fadeIn justify-end opacity-0"
        style={{
          animationDuration: "0.7s",
          animationDelay: "0.2s",
        }}
      >
        <div className="flex gap-x-2 pt-2">
          <Button size="MD" theme="blank" onClick={onClose} text="Cancel" />
          <Button
            size="MD"
            theme="primary"
            onClick={() => {
              setModalView(selectedMode);
            }}
            text={m.continue()}
          />
        </div>
      </div>
    </div>
  );
}

function UrlView({
  onBack,
  onMount,
  mountInProgress,
}: {
  onBack: () => void;
  onMount: (url: string, usbMode: RemoteVirtualMediaState["mode"]) => void;
  mountInProgress: boolean;
}) {
  const [usbMode, setUsbMode] = useState<RemoteVirtualMediaState["mode"]>("CDROM");
  const [url, setUrl] = useState<string>("");
  const [isUrlValid, setIsUrlValid] = useState(false);

  const popularImages = [
    {
      name: "Ubuntu 24.04 LTS",
      url: "https://releases.ubuntu.com/24.04.3/ubuntu-24.04.3-desktop-amd64.iso",
      icon: UbuntuIcon,
    },
    {
      name: "Debian 13 Trixie",
      url: "https://cdimage.debian.org/debian-cd/current/amd64/iso-cd/debian-13.0.0-amd64-netinst.iso",
      icon: DebianIcon,
    },
    {
      name: "Debian 12 Bookworm (old-stable)",
      url: "https://cdimage.debian.org/mirror/cdimage/archive/12.11.0/amd64/iso-cd/debian-12.11.0-amd64-netinst.iso",
      icon: DebianIcon,
    },
    {
      name: "Fedora 42",
      url: "https://download.fedoraproject.org/pub/fedora/linux/releases/42/Workstation/x86_64/iso/Fedora-Workstation-Live-42-1.1.x86_64.iso",
      icon: FedoraIcon,
    },
    {
      name: "openSUSE Leap 16.0",
      url: "https://download.opensuse.org/distribution/leap/16.0/offline/Leap-16.0-online-installer-x86_64.install.iso",
      icon: OpenSUSEIcon,
    },
    {
      name: "openSUSE Tumbleweed",
      url: "https://download.opensuse.org/tumbleweed/iso/openSUSE-Tumbleweed-NET-x86_64-Current.iso",
      icon: OpenSUSEIcon,
    },
    {
      name: "Arch Linux",
      url: "https://archlinux.doridian.net/iso/latest/archlinux-x86_64.iso",
      icon: ArchIcon,
    },
    {
      name: "netboot.xyz",
      url: "https://boot.netboot.xyz/ipxe/netboot.xyz.iso",
      icon: NetBootIcon,
      description: "Boot and install various operating systems over network",
    },
  ];

  const urlRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (urlRef.current) {
      setIsUrlValid(urlRef.current.validity.valid);
    }
  }, [url]);

  function handleUrlChange(url: string) {
    setUrl(url);
    if (url.endsWith(".iso")) {
      setUsbMode("CDROM");
    } else if (url.endsWith(".img")) {
      setUsbMode("Disk");
    }
  }

  return (
    <div className="w-full space-y-4">
      <ViewHeader
        title={m.mount_view_url_title()}
        description={m.mount_view_url_description()}
      />

      <div
        className="animate-fadeIn opacity-0"
        style={{
          animationDuration: "0.7s",
        }}
      >
        <InputFieldWithLabel
          placeholder="https://example.com/image.iso"
          type="url"
          label={m.mount_url_input_label()}
          ref={urlRef}
          value={url}
          onChange={e => handleUrlChange(e.target.value)}
        />
      </div>
      <div
        className="flex w-full animate-fadeIn items-end justify-between opacity-0"
        style={{
          animationDuration: "0.7s",
          animationDelay: "0.1s",
        }}
      >
        <Fieldset disabled={!isUrlValid || url.length === 0}>
          <UsbModeSelector usbMode={usbMode} setUsbMode={setUsbMode} />
        </Fieldset>
        <div className="flex space-x-2">
          <Button size="MD" theme="blank" text={m.back()} onClick={onBack} />
          <Button
            size="MD"
            theme="primary"
            loading={mountInProgress}
            text={m.mount_button_mount_url()}
            onClick={() => onMount(url, usbMode)}
            disabled={
              mountInProgress || !isUrlValid || url.length === 0
            }
          />
        </div>
      </div>

      <hr className="border-slate-800/30 dark:border-slate-300/20" />
      <div
        className="animate-fadeIn opacity-0"
        style={{
          animationDuration: "0.7s",
          animationDelay: "0.2s",
        }}
      >
        <h2 className="mb-2 text-sm font-semibold text-black dark:text-white">
          {m.mount_popular_images()}
        </h2>
        <Card className="w-full divide-y divide-slate-800/20 dark:divide-slate-300/20">
          {popularImages.map((image, index) => (
            <div key={index} className="flex items-center justify-between gap-x-4 p-3.5">
              <div className="flex items-center gap-x-4">
                <img src={image.icon} alt={`${image.name} Icon`} className="w-6" />
                <div className="flex flex-col gap-y-1">
                  <h3 className="text-sm leading-none font-semibold dark:text-white">
                    {formatters.truncateMiddle(image.name, 40)}
                  </h3>
                  {image.description && (
                    <p className="text-xs text-slate-600 dark:text-slate-400">
                      {image.description}
                    </p>
                  )}
                  <p className="text-xs leading-none text-slate-800 dark:text-slate-300">
                    {formatters.truncateMiddle(image.url, 50)}
                  </p>
                </div>
              </div>
              <Button
                size="XS"
                theme="light"
                text={m.mount_button_select()}
                onClick={() => handleUrlChange(image.url)}
              />
            </div>
          ))}
        </Card>
      </div>
    </div>
  );
}

export interface StorageFile {
  name: string;
  size: string;
  createdAt: Date;
}

function DeviceFileView({
  onMountStorageFile,
  mountInProgress,
  onBack,
  onNewImageClick,
}: {
  onMountStorageFile: (name: string, mode: RemoteVirtualMediaState["mode"]) => void;
  mountInProgress: boolean;
  onBack: () => void;
  onNewImageClick: (incompleteFileName?: string) => void;
}) {
  const [onStorageFiles, setOnStorageFiles] = useState<StorageFile[]>([]);

  const [selected, setSelected] = useState<string | null>(null);
  const [usbMode, setUsbMode] = useState<RemoteVirtualMediaState["mode"]>("CDROM");
  const [currentPage, setCurrentPage] = useState(1);
  const filesPerPage = 5;

  const { send } = useJsonRpc();

  interface StorageSpace {
    bytesUsed: number;
    bytesFree: number;
  }
  const [storageSpace, setStorageSpace] = useState<StorageSpace | null>(null);

  const percentageUsed = useMemo(() => {
    if (!storageSpace) return 0;
    return Number(
      (
        (storageSpace.bytesUsed / (storageSpace.bytesUsed + storageSpace.bytesFree)) *
        100
      ).toFixed(1),
    );
  }, [storageSpace]);

  const bytesUsed = useMemo(() => {
    if (!storageSpace) return 0;
    return storageSpace.bytesUsed;
  }, [storageSpace]);

  const bytesFree = useMemo(() => {
    if (!storageSpace) return 0;
    return storageSpace.bytesFree;
  }, [storageSpace]);

  const syncStorage = useCallback(() => {
    send("listStorageFiles", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.mount_error_list_storage({ error: resp.error }));
        return;
      }
      const { files } = resp.result as StorageFiles;
      const formattedFiles = files.map(file => ({
        name: file.filename,
        size: formatters.bytes(file.size),
        createdAt: new Date(file?.createdAt),
      }));

      setOnStorageFiles(formattedFiles);
    });

    send("getStorageSpace", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.mount_error_get_storage_space({ error: resp.error }));
        return;
      }

      const space = resp.result as StorageSpace;
      setStorageSpace(space);
    });
  }, [send, setOnStorageFiles, setStorageSpace]);

  interface StorageFiles {
    files: {
      filename: string;
      size: number;
      createdAt: string;
    }[];
  }

  useEffect(() => {
    syncStorage();
  }, [syncStorage]);

  function handleDeleteFile(file: StorageFile) {
    console.log("Deleting file:", file);
    send("deleteStorageFile", { filename: file.name }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(m.mount_error_delete_file({ error: resp.error }));
        return;
      }

      syncStorage();
    });
  }

  function handleOnSelectFile(file: StorageFile) {
    setSelected(file.name);
    if (file.name.endsWith(".iso")) {
      setUsbMode("CDROM");
    } else if (file.name.endsWith(".img")) {
      setUsbMode("Disk");
    }
  }

  const indexOfLastFile = currentPage * filesPerPage;
  const indexOfFirstFile = indexOfLastFile - filesPerPage;
  const currentFiles = onStorageFiles.slice(indexOfFirstFile, indexOfLastFile);
  const totalPages = Math.ceil(onStorageFiles.length / filesPerPage);

  const handlePreviousPage = () => {
    setCurrentPage(prev => Math.max(prev - 1, 1));
  };

  const handleNextPage = () => {
    setCurrentPage(prev => Math.min(prev + 1, totalPages));
  };

  return (
    <div className="w-full space-y-4">
      <ViewHeader
        title={m.mount_view_device_title()}
        description={m.mount_view_device_description()}
      />
      <div
        className="w-full animate-fadeIn opacity-0"
        style={{
          animationDuration: "0.7s",
          animationDelay: "0.1s",
        }}
      >
        <Card>
          {onStorageFiles.length === 0 ? (
            <div className="flex items-center justify-center py-8 text-center">
              <div className="space-y-3">
                <div className="space-y-1">
                  <PlusCircleIcon className="mx-auto h-6 w-6 text-blue-700 dark:text-blue-500" />
                  <h3 className="text-sm leading-none font-semibold text-black dark:text-white">
                    {m.mount_no_images_title()}
                  </h3>
                  <p className="text-xs leading-none text-slate-700 dark:text-slate-300">
                    {m.mount_no_images_description()}
                  </p>
                </div>
                <div>
                  <Button
                    size="SM"
                    theme="primary"
                    text={m.mount_upload_title()}
                    onClick={() => onNewImageClick()}
                  />
                </div>
              </div>
            </div>
          ) : (
            <div className="w-full divide-y divide-slate-800/20 dark:divide-slate-300/20">
              {currentFiles.map((file, index) => (
                <PreUploadedImageItem
                  key={index}
                  name={file.name}
                  size={file.size}
                  uploadedAt={file.createdAt}
                  isIncomplete={file.name.endsWith(".incomplete")}
                  isSelected={selected === file.name}
                  onDelete={() => {
                    const selectedFile = onStorageFiles.find(f => f.name === file.name);
                    if (!selectedFile) return;
                    if (
                      window.confirm(m.mount_confirm_delete({ name: selectedFile.name }))
                    ) {
                      handleDeleteFile(selectedFile);
                    }
                  }}
                  onSelect={() => handleOnSelectFile(file)}
                  onContinueUpload={() => onNewImageClick(file.name)}
                />
              ))}

              {onStorageFiles.length > filesPerPage && (
                <div className="flex items-center justify-between px-3 py-2">
                  <p className="text-sm text-slate-700 dark:text-slate-300">
                    {m.mount_button_showing_results({
                      from: indexOfFirstFile + 1,
                      to: Math.min(indexOfLastFile, onStorageFiles.length),
                      total: onStorageFiles.length
                    })}
                  </p>
                  <div className="flex items-center gap-x-2">
                    <Button
                      size="XS"
                      theme="light"
                      text={m.previous()}
                      onClick={handlePreviousPage}
                      disabled={currentPage === 1}
                    />
                    <Button
                      size="XS"
                      theme="light"
                      text={m.next()}
                      onClick={handleNextPage}
                      disabled={currentPage === totalPages}
                    />
                  </div>
                </div>
              )}
            </div>
          )}
        </Card>
      </div>

      {onStorageFiles.length > 0 ? (
        <div
          className="flex animate-fadeIn items-end justify-between opacity-0"
          style={{
            animationDuration: "0.7s",
            animationDelay: "0.15s",
          }}
        >
          <Fieldset disabled={selected === null}>
            <UsbModeSelector usbMode={usbMode} setUsbMode={setUsbMode} />
          </Fieldset>
          <div className="flex items-center gap-x-2">
            <Button size="MD" theme="blank" text="Back" onClick={() => onBack()} />
            <Button
              size="MD"
              disabled={selected === null || mountInProgress}
              theme="primary"
              text={m.mount_button_mount_file()}
              loading={mountInProgress}
              onClick={() =>
                onMountStorageFile(
                  onStorageFiles.find(f => f.name === selected)?.name || "",
                  usbMode,
                )
              }
            />
          </div>
        </div>
      ) : (
        <div
          className="flex animate-fadeIn items-end justify-end opacity-0"
          style={{
            animationDuration: "0.7s",
            animationDelay: "0.15s",
          }}
        >
          <div className="flex items-center gap-x-2">
            <Button size="MD" theme="light" text="Back" onClick={() => onBack()} />
          </div>
        </div>
      )}
      <hr className="border-slate-800/20 dark:border-slate-300/20" />
      <div
        className="animate-fadeIn space-y-2 opacity-0"
        style={{
          animationDuration: "0.7s",
          animationDelay: "0.20s",
        }}
      >
        <div className="flex justify-between text-sm">
          <span className="font-medium text-black dark:text-white">
            {m.mount_available_storage()}
          </span>
          <span className="text-slate-700 dark:text-slate-300">
            {m.mount_percentage_used({ percentageUsed })}
          </span>
        </div>
        <div className="h-3.5 w-full overflow-hidden rounded-xs bg-slate-200 dark:bg-slate-700">
          <div
            className="h-full rounded-xs bg-blue-700 transition-all duration-300 ease-in-out dark:bg-blue-500"
            style={{ width: `${percentageUsed}%` }}
          ></div>
        </div>
        <div className="flex justify-between text-sm text-slate-600">
          <span className="text-slate-700 dark:text-slate-300">
            {m.mount_bytes_used({ bytesUsed: formatters.bytes(bytesUsed) })}
          </span>
          <span className="text-slate-700 dark:text-slate-300">
            {m.mount_bytes_free({ bytesFree: formatters.bytes(bytesFree) })}
          </span>
        </div>
      </div>

      {onStorageFiles.length > 0 && (
        <div
          className="w-full animate-fadeIn opacity-0"
          style={{
            animationDuration: "0.7s",
            animationDelay: "0.25s",
          }}
        >
          <Button
            size="MD"
            theme="light"
            fullWidth
            text={m.mount_button_upload_new_image()}
            onClick={() => onNewImageClick()}
          />
        </div>
      )}
    </div>
  );
}

function UploadFileView({
  onBack,
  onCancelUpload,
  incompleteFileName,
}: {
  onBack: () => void;
  onCancelUpload: () => void;
  incompleteFileName?: string;
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

  const { send } = useJsonRpc();
  const rtcDataChannelRef = useRef<RTCDataChannel | null>(null);

  useEffect(() => {
    const ref = rtcDataChannelRef.current;
    return () => {
      console.log("unmounting");
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
      notifications.error(m.mount_upload_failed_datachannel());
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
      notifications.error(m.mount_upload_failed_rtc({ error: error }));
      setUploadState("idle");
      console.log("Upload state set to 'idle'");
    };
  }

  async function handleHttpUpload(
    file: File,
    alreadyUploadedBytes: number,
    dataChannel: string,
  ) {
    const uploadUrl = `${DEVICE_API}/storage/upload?uploadId=${dataChannel}`;

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

      if (
        incompleteFileName &&
        file.name !== incompleteFileName.replace(".incomplete", "")
      ) {
        setFileError(
          m.mount_please_select_file({ name: incompleteFileName.replace(".incomplete", "") }),
        );
        return;
      }

      setFileError(null);
      console.log(`File selected: ${file.name}, size: ${file.size} bytes`);
      setUploadedFileName(file.name);
      setUploadedFileSize(file.size);
      setUploadState("uploading");
      console.log("Upload state set to 'uploading'");

      send("startStorageFileUpload", { filename: file.name, size: file.size }, (resp: JsonRpcResponse) => {
        console.log("startStorageFileUpload response:", resp);
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
        title={m.mount_upload_title()}
        description={
          incompleteFileName
            ? m.mount_continue_uploading_with_name({ name: incompleteFileName.replace(".incomplete", "") })
            : m.mount_upload_description()
        }
      />
      <div
        className="animate-fadeIn space-y-2 opacity-0"
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
                "cursor-pointer hover:bg-blue-50/50 dark:hover:bg-blue-900/50":
                  uploadState === "idle",
              })}
            >
              <div className="h-[186px] w-full px-4">
                <div className="flex h-full flex-col items-center justify-center text-center">
                  {uploadState === "idle" && (
                    <div className="space-y-1">
                      <div className="inline-block">
                        <Card>
                          <div className="p-1">
                            <PlusCircleIcon className="h-4 w-4 shrink-0 text-blue-500 dark:text-blue-400" />
                          </div>
                        </Card>
                      </div>
                      <h3 className="text-sm leading-none font-semibold text-black dark:text-white">
                        {incompleteFileName
                          ? m.mount_click_to_select_incomplete({ name: incompleteFileName.replace(".incomplete", "") })
                          : m.mount_click_to_select_file()}
                      </h3>
                      <p className="text-xs leading-none text-slate-700 dark:text-slate-300">
                        {m.mount_supported_formats()}
                      </p>
                    </div>
                  )}

                  {uploadState === "uploading" && (
                    <div className="w-full max-w-sm space-y-2 text-left">
                      <div className="inline-block">
                        <Card>
                          <div className="p-1">
                            <LuUpload className="h-4 w-4 shrink-0 text-blue-500 dark:text-blue-400" />
                          </div>
                        </Card>
                      </div>
                      <h3 className="leading-non text-lg font-semibold text-black dark:text-white">
                        {m.mount_uploading_with_name({ name: formatters.truncateMiddle(uploadedFileName, 30) })}
                      </h3>
                      <p className="text-xs leading-none text-slate-700 dark:text-slate-300">
                        {formatters.bytes(uploadedFileSize || 0)}
                      </p>
                      <div className="w-full space-y-2">
                        <div className="h-3.5 w-full overflow-hidden rounded-full bg-slate-300 dark:bg-slate-700">
                          <div
                            className="h-3.5 rounded-full bg-blue-700 transition-all duration-500 ease-linear dark:bg-blue-500"
                            style={{ width: `${uploadProgress}%` }}
                          ></div>
                        </div>
                        <div className="flex justify-between text-xs text-slate-600 dark:text-slate-400">
                          <span>{m.mount_uploading()}</span>{" "}
                          <span>
                            {uploadSpeed !== null
                              ? `${formatters.bytes(uploadSpeed)}/s`
                              : m.mount_calculating()}
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
                            <LuCheck className="h-4 w-4 shrink-0 text-blue-500 dark:text-blue-400" />
                          </div>
                        </Card>
                      </div>
                      <h3 className="text-sm leading-none font-semibold text-black dark:text-white">
                        {m.mount_upload_successful()}
                      </h3>
                      <p className="text-xs leading-none text-slate-700 dark:text-slate-300">
                        {m.mount_uploaded_has_been_uploaded({ name: formatters.truncateMiddle(uploadedFileName, 40) })}
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
          accept=".iso, .img"
        />
        {fileError && (
          <p className="mt-2 text-sm text-red-600 dark:text-red-400">{fileError}</p>
        )}
      </div>

      {/* Display upload error if present */}
      {uploadError && (
        <div
          className="mt-2 animate-fadeIn truncate text-sm text-red-600 dark:text-red-400 opacity-0"
          style={{ animationDuration: "0.7s" }}
        >
          {m.mount_upload_error({ error: String(uploadError) })}
        </div>
      )}

      <div
        className="flex w-full animate-fadeIn items-end opacity-0"
        style={{
          animationDuration: "0.7s",
          animationDelay: "0.1s",
        }}
      >
        <div className="flex w-full justify-end space-x-2">
          {uploadState === "uploading" ? (
            <Button
              size="MD"
              theme="light"
              text={m.mount_button_cancel_upload()}
              onClick={() => {
                onCancelUpload();
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
              theme={uploadState === "success" ? "primary" : "light"}
              text={m.mount_button_back_to_overview()}
              onClick={onBack}
            />
          )}
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
          <ExclamationTriangleIcon className="h-6 w-6" />
          <h2 className="text-lg leading-tight font-bold">{m.mount_error_title()}</h2>
        </div>
        <p className="text-sm leading-snug text-slate-600">
          {m.mount_error_description()}
        </p>
      </div>
      {errorMessage && (
        <Card className="border border-red-200 bg-red-50 p-4">
          <p className="text-sm font-medium text-red-800">{errorMessage}</p>
        </Card>
      )}
      <div className="flex justify-end space-x-2">
        <Button size="SM" theme="light" text={m.close()} onClick={onClose} />
        <Button size="SM" theme="primary" text={m.mount_button_back_to_overview()} onClick={onRetry} />
      </div>
    </div>
  );
}

function PreUploadedImageItem({
  name,
  size,
  uploadedAt,
  isSelected,
  isIncomplete,
  onSelect,
  onDelete,
  onContinueUpload,
}: {
  name: string;
  size: string;
  uploadedAt: Date;
  isSelected: boolean;
  isIncomplete: boolean;
  onSelect: () => void;
  onDelete: () => void;
  onContinueUpload: () => void;
}) {
  const [isHovering, setIsHovering] = useState(false);
  return (
    <label
      htmlFor={name}
      className={cx(
        "flex w-full cursor-pointer items-center justify-between p-3 transition-colors",
        {
          "bg-blue-50 dark:bg-blue-900/20": isSelected,
          "hover:bg-gray-50 dark:hover:bg-slate-700/50": !isSelected,
          "cursor-default": isIncomplete,
        },
      )}
      onMouseEnter={() => setIsHovering(true)}
      onMouseLeave={() => setIsHovering(false)}
      onClick={() => {
        if (!isIncomplete) {
          onSelect();
        }
      }}
    >
      <div className="flex items-center gap-x-4">
        <div className="space-y-0.5 select-none">
          <div className="text-sm leading-none font-semibold dark:text-white">
            {formatters.truncateMiddle(name, 45)}
          </div>
          <div className="flex items-center text-sm">
            <div className="flex items-center gap-x-1 text-slate-600 dark:text-slate-400">
              {formatters.date(new Date(uploadedAt), { month: "short" })}
            </div>
            <div className="mx-1 h-[10px] w-px bg-slate-300 text-slate-300 dark:bg-slate-600"></div>
            <div className="text-gray-600 dark:text-slate-400">{size}</div>
          </div>
        </div>
      </div>
      <div className="relative flex items-center gap-x-3 select-none">
        <div
          className={cx("opacity-0 transition-opacity duration-200", {
            "w-auto opacity-100": isHovering,
          })}
        >
          <Button
            size="XS"
            theme="light"
            LeadingIcon={TrashIcon}
            text={m.delete()}
            onClick={e => {
              e.stopPropagation();
              onDelete();
            }}
            className="text-red-500 dark:text-red-400"
          />
        </div>
        {!isIncomplete ? (
          <input
            type="radio"
            checked={isSelected}
            onChange={onSelect}
            name={name}
            className="form-radio h-3 w-3 border-slate-800/30 bg-white text-blue-700 focus:ring-blue-500 disabled:opacity-30 dark:border-slate-300/20 dark:bg-slate-800"
            onClick={e => e.stopPropagation()} // Prevent double-firing of onSelect
          />
        ) : (
          <Button
            size="XS"
            theme="light"
            text={m.mount_button_continue_upload()}
            onClick={e => {
              e.stopPropagation();
              onContinueUpload();
            }}
          />
        )}
      </div>
    </label>
  );
}

function ViewHeader({ title, description }: { title: string; description: string }) {
  return (
    <div className="space-y-0">
      <h2 className="text-lg leading-tight font-bold text-black dark:text-white">
        {title}
      </h2>
      <div className="text-sm leading-snug text-slate-600 dark:text-slate-400">
        {description}
      </div>
    </div>
  );
}

function UsbModeSelector({
  usbMode,
  setUsbMode,
}: {
  usbMode: RemoteVirtualMediaState["mode"];
  setUsbMode: (mode: RemoteVirtualMediaState["mode"]) => void;
}) {
  return (
    <div className="flex flex-col items-start space-y-1 select-none">
      <label className="text-sm font-semibold text-black dark:text-white">Mount as</label>
      <div className="flex space-x-4">
        <label htmlFor="cdrom" className="flex items-center">
          <input
            type="radio"
            id="cdrom"
            name="mountType"
            onChange={() => setUsbMode("CDROM")}
            checked={usbMode === "CDROM"}
            className="form-radio h-3 w-3 rounded-full border-slate-800/30 bg-white text-blue-700 transition-opacity focus:ring-blue-500 disabled:opacity-30 dark:bg-slate-800"
          />
          <span className="ml-2 text-sm font-medium text-slate-900 dark:text-white">
            {m.mount_mode_cdrom()}
          </span>
        </label>
        <label htmlFor="disk" className="flex items-center">
          <input
            type="radio"
            id="disk"
            name="mountType"
            checked={usbMode === "Disk"}
            onChange={() => setUsbMode("Disk")}
            className="form-radio h-3 w-3 rounded-full border-slate-800/30 bg-white text-blue-700 transition-opacity focus:ring-blue-500 disabled:opacity-30 dark:bg-slate-800"
          />
          <span className="ml-2 text-sm font-medium text-slate-900 dark:text-white">
            {m.mount_mode_disk()}
          </span>
        </label>
      </div>
    </div>
  );
}
