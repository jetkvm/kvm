import { useCallback, useEffect } from "react";
import { create } from "zustand";

import { useCapability, useRTCStore, useVideoStore } from "@hooks/stores";
import { guestRpc } from "@hooks/useGuestClipboard";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

// One data channel message: a transfer id followed by file bytes. The device
// forwards each message as one chunk on the USB link. Only a message that
// uses up the transfer's credit may carry under 1 KiB: the device has room
// for the framing of few small messages.
const chunkBytes = 32 * 1024;
// Outbound bytes queued in the data channel before sending pauses. A file
// pushed in one burst overruns the JetKVM's UDP receive buffer, and the loss
// collapses the connection's congestion window for the rest of the transfer,
// so this stays well under it.
const channelHighWater = 96 * 1024;
// How often a transfer's progress reaches the status card.
const progressIntervalMs = 100;
// Backstop for a bufferedamountlow event that arrives before its listener.
const drainPollMs = 250;

export type GuestFileTransferStatus = "waiting" | "ok" | "failed" | "cancelled";

export interface GuestFileTransferView {
  id: number;
  name: string;
  size: number;
  sent: number;
  status: GuestFileTransferStatus;
}

// The status card shows one drop at a time; `cancel` is set while that drop
// is still sending, and a second drop is refused until it clears.
interface GuestFileTransferState {
  visible: boolean;
  transfers: GuestFileTransferView[];
  cancel: (() => void) | null;
  begin: (transfers: GuestFileTransferView[], cancel: () => void) => void;
  update: (id: number, patch: Partial<GuestFileTransferView>) => void;
  finish: () => void;
  hide: () => void;
}

export const useGuestFileTransferStore = create<GuestFileTransferState>(set => ({
  visible: false,
  transfers: [],
  cancel: null,
  begin: (transfers, cancel) => set({ visible: true, transfers, cancel }),
  update: (id, patch) =>
    set(state => ({
      transfers: state.transfers.map(t => (t.id === id ? { ...t, ...patch } : t)),
    })),
  finish: () => set({ cancel: null }),
  hide: () => set({ visible: false, cancel: null }),
}));

interface DropResult {
  channel: string;
  transfers: number[];
}

interface ChannelMessage {
  type: "credit" | "done";
  transfer?: number;
  bytes?: number;
  status?: "ok" | "failed" | "cancelled";
}

interface Outgoing {
  file: File;
  offset: number;
  credit: number;
  pumping: boolean;
  shown: number;
}

function hasFiles(e: DragEvent) {
  return !!e.dataTransfer && Array.from(e.dataTransfer.types).includes("Files");
}

// Files only: a dropped folder cannot be read as one file, so it is left
// out and reported.
function droppedFiles(e: DragEvent): { files: File[]; folders: number } {
  const transfer = e.dataTransfer;
  if (!transfer) return { files: [], folders: 0 };
  const items = Array.from(transfer.items ?? []);
  if (items.length === 0) return { files: Array.from(transfer.files), folders: 0 };
  const files: File[] = [];
  let folders = 0;
  for (const item of items) {
    if (item.kind !== "file") continue;
    if (item.webkitGetAsEntry?.()?.isDirectory) {
      folders++;
      continue;
    }
    const file = item.getAsFile();
    if (file) files.push(file);
  }
  return { files, folders };
}

// Resolves once the channel's outbound queue is back under the high-water
// mark, or the channel has left the open state and will never drain.
// bufferedamountlow fires only as the buffer crosses the threshold
// downwards, so a crossing between the check and the listener would never
// be seen; polling covers it.
function waitForDrain(channel: RTCDataChannel) {
  const drained = () => channel.bufferedAmount <= channelHighWater || channel.readyState !== "open";
  return new Promise<void>(done => {
    if (drained()) return done();
    let poll = 0;
    const settle = () => {
      for (const event of ["bufferedamountlow", "close", "error"])
        channel.removeEventListener(event, settle);
      window.clearInterval(poll);
      done();
    };
    for (const event of ["bufferedamountlow", "close", "error"])
      channel.addEventListener(event, settle);
    poll = window.setInterval(() => {
      if (drained()) settle();
    }, drainPollMs);
  });
}

// Sends every file of a drop over its data channel, within the credit the
// device relays from the guest.
function stream(files: File[], result: DropResult) {
  const store = useGuestFileTransferStore.getState();
  const channel = useRTCStore.getState().peerConnection?.createDataChannel(result.channel);
  if (!channel) {
    notifications.error(m.guest_file_channel_failed());
    return;
  }
  channel.binaryType = "arraybuffer";
  channel.bufferedAmountLowThreshold = channelHighWater;
  const byId = new Map<number, Outgoing>();
  result.transfers.forEach((id, index) => {
    byId.set(id, { file: files[index], offset: 0, credit: 0, pumping: false, shown: 0 });
  });

  let finished = false;
  const finish = () => {
    if (finished) return;
    finished = true;
    store.finish();
    if (channel.readyState === "open" || channel.readyState === "connecting") channel.close();
  };
  // Clearing byId is what stops the pumps: they re-check membership around
  // every await.
  const endAll = (status: GuestFileTransferStatus) => {
    for (const id of byId.keys()) store.update(id, { status });
    byId.clear();
    finish();
  };
  store.begin(
    result.transfers.map((id, index) => ({
      id,
      name: files[index].name,
      size: files[index].size,
      sent: 0,
      status: "waiting" as GuestFileTransferStatus,
    })),
    () => endAll("cancelled"),
  );

  const pump = async (id: number) => {
    const entry = byId.get(id);
    if (!entry || entry.pumping) return;
    entry.pumping = true;
    try {
      while (byId.has(id) && entry.credit > 0 && entry.offset < entry.file.size) {
        const n = Math.min(chunkBytes, entry.credit, entry.file.size - entry.offset);
        const bytes = await entry.file.slice(entry.offset, entry.offset + n).arrayBuffer();
        await waitForDrain(channel);
        if (!byId.has(id) || channel.readyState !== "open") return;
        const message = new Uint8Array(4 + bytes.byteLength);
        new DataView(message.buffer).setUint32(0, id, true);
        message.set(new Uint8Array(bytes), 4);
        channel.send(message);
        entry.offset += n;
        entry.credit -= n;
        // The card renders a progress bar: repainting it for every chunk
        // costs more than the transfer does.
        const now = performance.now();
        if (now - entry.shown >= progressIntervalMs || entry.offset === entry.file.size) {
          entry.shown = now;
          store.update(id, { sent: entry.offset });
        }
      }
    } catch (error) {
      notifications.error(
        m.guest_file_read_failed({ name: entry.file.name, error: String(error) }),
      );
      byId.delete(id);
      store.update(id, { status: "failed" });
      if (channel.readyState === "open")
        channel.send(JSON.stringify({ type: "cancel", transfer: id }));
    } finally {
      entry.pumping = false;
    }
  };

  channel.onmessage = (event: MessageEvent) => {
    if (typeof event.data !== "string") return;
    let message: ChannelMessage;
    try {
      message = JSON.parse(event.data) as ChannelMessage;
    } catch {
      // The device speaks JSON on this channel; anything else means the
      // drop cannot be trusted to finish.
      return endAll("failed");
    }
    if (message.transfer === undefined) return;
    const entry = byId.get(message.transfer);
    if (message.type === "credit" && entry) {
      entry.credit += message.bytes ?? 0;
      void pump(message.transfer);
    } else if (message.type === "done") {
      byId.delete(message.transfer);
      const status = message.status ?? "failed";
      store.update(message.transfer, {
        status,
        ...(status === "ok" && entry ? { sent: entry.file.size } : {}),
      });
      if (byId.size === 0) finish();
    }
  };
  // Chrome can deliver the device's first credit before the open event, while
  // the pump cannot send yet; the open event starts it again.
  channel.onopen = () => byId.forEach((_, id) => void pump(id));
  channel.onclose = () => endAll("cancelled");
  channel.onerror = () => endAll("failed");
}

// Drag and drop from the user's desktop onto the video. The files are
// written to the remote host’s Downloads folder, with a home-folder fallback.
// Only a device with OS Services takes files; on any other the page handles
// drops as before.
export default function useGuestFileDrop(videoRef: React.RefObject<HTMLVideoElement | null>) {
  const osServices = useCapability("os_services");
  const reportedReceive = useVideoStore(state => state.guestFile?.receive ?? false);
  const receive = osServices && reportedReceive;

  const onDrop = useCallback(
    (e: DragEvent) => {
      if (!hasFiles(e) || !receive) return;
      e.preventDefault();
      const { files, folders } = droppedFiles(e);
      if (folders > 0) notifications.error(m.guest_file_folders_skipped());
      if (files.length === 0) return;
      if (useGuestFileTransferStore.getState().cancel) {
        notifications.error(m.guest_file_drop_busy());
        return;
      }
      const entries = files.map(f => ({ name: f.name, size: f.size }));
      guestRpc<DropResult>("guestFileDrop", { drop: { files: entries } })
        .then(result => stream(files, result))
        .catch((error: unknown) => {
          notifications.error(m.guest_file_drop_failed({ error: String(error) }));
        });
    },
    [receive],
  );

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !osServices) return;
    const controller = new AbortController();
    const { signal } = controller;
    const allow = (e: DragEvent) => {
      if (!hasFiles(e) || !receive) return;
      e.preventDefault();
      if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
    };
    video.addEventListener("dragover", allow, { signal });
    video.addEventListener("drop", onDrop, { signal });
    // A drop anywhere else on the page must not navigate away from the
    // session.
    const swallow = (e: DragEvent) => {
      if (hasFiles(e)) e.preventDefault();
    };
    document.addEventListener("dragover", swallow, { signal });
    document.addEventListener("drop", swallow, { signal });
    return () => controller.abort();
  }, [osServices, receive, onDrop, videoRef]);
}
