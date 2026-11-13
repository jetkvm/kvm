import { useCallback, useEffect, useRef, useState } from "react";

import { m } from "@localizations/messages.js";
import {
  RemoteVirtualMediaState,
  StorageDownloadState,
  useCapability,
  useMountMediaStore,
  useRTCStore,
  useStorageDownloadStore,
} from "@hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import notifications from "@/notifications";

// The OS Services release bucket. The release step in the os-services
// repository writes each release's offline ISO, which holds the installers
// for Windows, macOS and Linux, to <version>/, and the newest stable one
// also to jetkvm-os-services.iso.
const OS_SERVICES_BUCKET_URL = "https://os-services.jetkvm.com";

// The offline ISO of a release, or of the newest stable release without a
// version.
function offlineInstallerUrl(version?: string) {
  return version
    ? `${OS_SERVICES_BUCKET_URL}/${version}/IP-KVM-OS-Services_${version}.iso`
    : `${OS_SERVICES_BUCKET_URL}/jetkvm-os-services.iso`;
}

// The offline ISO's file on the TF card, named as in the bucket.
function offlineInstallerFilename(version?: string) {
  return version ? `IP-KVM-OS-Services_${version}.iso` : "jetkvm-os-services.iso";
}

const OFFLINE_INSTALLER_FILENAME = /^(IP-KVM-OS-Services_.+|jetkvm-os-services)\.iso$/;
const MIB = 1024 * 1024;
// Longer than any reply here takes, a mount's USB re-enumeration included.
const CALL_TIMEOUT_MS = 60_000;

// "Saving the installer to the TF card: 42% (37.1 of 88.5 MiB)" while the
// firmware downloads the offline installer, else null.
export function useOsServicesInstallerProgress() {
  const download = useStorageDownloadStore(state => state.download);
  if (download?.state !== "downloading" || !OFFLINE_INSTALLER_FILENAME.test(download.filename)) {
    return null;
  }
  if (download.total <= 0) return m.os_services_install_download_starting();
  return m.os_services_install_downloading({
    percent: Math.floor((download.bytes / download.total) * 100),
    downloaded: (download.bytes / MIB).toFixed(1),
    total: (download.total / MIB).toFixed(1),
  });
}

// True while the KVM has a TF card with a mounted filesystem, null until
// the KVM answers. It asks again every few seconds, so a card inserted
// while the page is open counts. A device without removable storage is
// never asked.
export function useOsServicesInstallerCard() {
  const { send } = useJsonRpc();
  const removableStorage = useCapability("removable_storage");
  const [ready, setReady] = useState<boolean | null>(null);
  useEffect(() => {
    if (!removableStorage) return;
    const read = () =>
      void send("getStorageState", {}, response => {
        if ("error" in response) return;
        const card = response.result as { cardPresent?: boolean; filesystemMounted?: boolean };
        setReady(!!card.cardPresent && !!card.filesystemMounted);
      });
    read();
    const timer = setInterval(read, 3000);
    return () => clearInterval(timer);
  }, [send, removableStorage]);
  return ready;
}

// Resolves with the final state of the download of `filename`. Events carry
// the progress; a poll covers an event that never arrives.
function waitForDownload(
  filename: string,
  call: (method: string) => Promise<JsonRpcResponse>,
): Promise<StorageDownloadState> {
  return new Promise(resolve => {
    const finished = (download: StorageDownloadState | null) =>
      download?.filename === filename && (download.state === "done" || download.state === "error");
    let poll: ReturnType<typeof setInterval> | undefined = undefined;
    const unsubscribe = useStorageDownloadStore.subscribe(({ download }) => {
      if (!finished(download)) return;
      unsubscribe();
      clearInterval(poll);
      resolve(download!);
    });
    poll = setInterval(() => {
      void call("getStorageDownloadState").then(response => {
        if ("error" in response) return;
        const download = response.result as StorageDownloadState;
        if (finished(download)) {
          useStorageDownloadStore.getState().setDownload(download);
        } else if (download.state !== "downloading" || download.filename !== filename) {
          // The device restarted, or the download is no longer this one.
          unsubscribe();
          clearInterval(poll);
          resolve({ ...download, state: "error", error: m.os_services_install_download_stopped() });
        }
      });
    }, 3000);
  });
}

// Mounts an OS Services installer as a CD-ROM on the target computer.
// mount() mounts the installer disc in the KVM's firmware at once, with no
// TF card and no network on the KVM: its one page links to the download for
// the target computer, which needs internet. mountOffline() is for a target
// computer without internet, on a device with removable storage: the
// firmware first saves the offline ISO, of the given release or else the
// newest stable one, to the TF card, as read from the card the installer
// starts in seconds, where reading it over HTTPS takes minutes. Media that
// is already mounted stays:
// the user unmounts it first. Without USB emulation or mass storage the
// target computer would see no disc, so the user turns them on first.
export function useOsServicesInstaller() {
  const { send } = useJsonRpc();
  // The download outlasts a WebRTC reconnect; later requests use the
  // current channel.
  const sendRef = useRef(send);
  useEffect(() => {
    sendRef.current = send;
  }, [send]);
  const setRemoteVirtualMediaState = useMountMediaStore(state => state.setRemoteVirtualMediaState);
  const downloading = useStorageDownloadStore(state => state.download?.state === "downloading");
  const [mounting, setMounting] = useState(false);

  const run = useCallback(
    async (offline: boolean, version?: string) => {
      // A request answers with an error when the channel is closed or closes
      // before the reply, so the button never stays busy.
      const call = (method: string, params: object = {}) =>
        new Promise<JsonRpcResponse>(resolve => {
          const lost = (): JsonRpcResponse => ({
            jsonrpc: "2.0",
            error: { code: -32000, message: m.os_services_install_connection_lost() },
            // A local answer, never matched against a request.
            id: -1,
          });
          if (useRTCStore.getState().rpcDataChannel?.readyState !== "open") {
            resolve(lost());
            return;
          }
          const timer = setTimeout(() => resolve(lost()), CALL_TIMEOUT_MS);
          void sendRef.current(method, params, response => {
            clearTimeout(timer);
            resolve(response);
          });
        });
      const filename = offlineInstallerFilename(version);
      // Saves the offline ISO to the TF card, or gives the reason it is not
      // there. A release's ISO never changes, so one already on the card is
      // used as it is. The newest-stable name can hold a newer release: the
      // firmware compares the size of the card's copy with the server's and
      // keeps the copy only when they match.
      const saveOffline = async (): Promise<string | null> => {
        const storage = await call("getStorageState");
        if ("error" in storage) return storage.error.data || storage.error.message;
        const card = storage.result as { cardPresent?: boolean; filesystemMounted?: boolean };
        if (!card.cardPresent || !card.filesystemMounted) return m.os_services_install_no_card();
        if (version) {
          const files = await call("listStorageFiles");
          if ("error" in files) return files.error.data || files.error.message;
          const onCard = (files.result as { files: { filename: string }[] }).files.some(
            file => file.filename === filename,
          );
          if (onCard) return null;
        }
        const started = await call("downloadToStorage", {
          url: offlineInstallerUrl(version),
          filename,
        });
        if ("error" in started) return started.error.data || started.error.message;
        useStorageDownloadStore.getState().setDownload({
          state: "downloading",
          filename,
          bytes: 0,
          total: 0,
          reused: false,
          videoPaused: false,
          error: "",
        });
        const result = await waitForDownload(filename, call);
        return result.state === "error" ? result.error : null;
      };
      // The reason the installer is not mounted, or null once it is.
      const attempt = async (): Promise<string | null> => {
        const emulation = await call("getUsbEmulationState");
        if ("error" in emulation) return emulation.error.data || emulation.error.message;
        if (emulation.result === false) return m.os_services_install_usb_emulation_off();
        const devices = await call("getUsbDevices");
        if ("error" in devices) return devices.error.data || devices.error.message;
        if ((devices.result as { mass_storage?: boolean }).mass_storage === false) {
          return m.os_services_install_mass_storage_off();
        }
        const media = await call("getVirtualMediaState");
        if ("error" in media) return media.error.data || media.error.message;
        if (media.result) return m.os_services_install_media_busy();

        let mounted: JsonRpcResponse;
        if (offline) {
          const error = await saveOffline();
          if (error !== null) return error;
          mounted = await call("mountWithStorage", { filename, mode: "CDROM" });
        } else {
          mounted = await call("mountBuiltinInstaller");
        }
        if ("error" in mounted) return mounted.error.data || mounted.error.message;
        const after = await call("getVirtualMediaState");
        if (!("error" in after)) {
          setRemoteVirtualMediaState(after.result as unknown as RemoteVirtualMediaState | null);
        }
        return null;
      };

      setMounting(true);
      const error = await attempt();
      setMounting(false);
      if (error === null) notifications.success(m.os_services_install_mounted());
      else notifications.error(m.os_services_install_mount_failed({ error }));
    },
    [setRemoteVirtualMediaState],
  );

  const mount = useCallback(() => run(false), [run]);
  const mountOffline = useCallback((version?: string) => run(true, version), [run]);
  return { mount, mountOffline, mounting: mounting || downloading };
}
