import { useEffect } from "react";

import { useJsonRpc } from "@hooks/useJsonRpc";
import { useRTCStore } from "@hooks/stores";

const pauseLeaseCounts = new WeakMap<RTCDataChannel, number>();

/**
 * Pauses the device's video stream while a memory-hungry view is mounted,
 * so the device does not encode video and serve an upload or the media
 * dialog at the same time. Nested views share one lease per RPC channel, so moving from the
 * virtual media popover into the full dialog does not briefly resume video.
 */
export function useVideoStreamPause(enabled = true): void {
  const rpcDataChannel = useRTCStore(state => state.rpcDataChannel);
  const { send } = useJsonRpc();

  useEffect(() => {
    if (!enabled || !rpcDataChannel) return;

    let acquired = false;
    const acquire = () => {
      if (acquired || rpcDataChannel.readyState !== "open") return;
      const leases = pauseLeaseCounts.get(rpcDataChannel) ?? 0;
      if (leases === 0) send("setVideoStreamPaused", { paused: true });
      pauseLeaseCounts.set(rpcDataChannel, leases + 1);
      acquired = true;
    };
    if (rpcDataChannel.readyState === "open") {
      acquire();
    } else {
      rpcDataChannel.addEventListener("open", acquire, { once: true });
    }

    return () => {
      rpcDataChannel.removeEventListener("open", acquire);
      if (!acquired) return;
      const leases = pauseLeaseCounts.get(rpcDataChannel) ?? 1;
      if (leases > 1) {
        pauseLeaseCounts.set(rpcDataChannel, leases - 1);
        return;
      }
      pauseLeaseCounts.delete(rpcDataChannel);
      send("setVideoStreamPaused", { paused: false });
    };
  }, [enabled, rpcDataChannel, send]);
}
