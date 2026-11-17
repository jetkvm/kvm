import { useCallback, useEffect } from "react";

import { useRTCStore, useFailsafeModeStore } from "@hooks/stores";

export interface JsonRpcRequest {
  jsonrpc: string;
  method: string;
  params: object;
  id: number | string;
}

export interface JsonRpcError {
  code: number;
  data?: string;
  message: string;
}

export interface JsonRpcSuccessResponse {
  jsonrpc: string;
  result: boolean | number | object | string | [];
  id: string | number;
}

export interface JsonRpcErrorResponse {
  jsonrpc: string;
  error: JsonRpcError;
  id: string | number;
}

export type JsonRpcResponse = JsonRpcSuccessResponse | JsonRpcErrorResponse;

export const RpcMethodNotFound = -32601;

const callbackStore = new Map<number | string, (resp: JsonRpcResponse) => void>();
let requestCounter = 0;

// Map of blocked RPC methods by failsafe reason
const blockedMethodsByReason: Record<string, string[]> = {
  native: [
    "setStreamQualityFactor",
    "setVideoCodecPreference",
    "getEDID",
    "setEDID",
    "getHostDisplayIdleMode",
    "setHostDisplayIdleMode",
    "getVideoLogStatus",
    "setDisplayRotation",
    "getVideoSleepMode",
    "setVideoSleepMode",
    "getVideoState",
  ],
};

// Device events that arrive before any component listens. The device sends
// its connect-time events (localVersion, deviceCapabilities, videoInputState,
// ...) as soon as the rpc channel opens, but useJsonRpc attaches its listener
// in an effect that runs after the channel is stored on open. A DataChannel
// message with no listener is lost, so the channel's creator holds them.
const heldEvents = new WeakMap<RTCDataChannel, { events: JsonRpcRequest[]; stop: () => void }>();

// Call right after creating the rpc channel, before it can open.
export function holdRpcEvents(channel: RTCDataChannel) {
  const events: JsonRpcRequest[] = [];
  const hold = (e: MessageEvent) => {
    try {
      const payload = JSON.parse(e.data) as JsonRpcResponse | JsonRpcRequest;
      if ("method" in payload) events.push(payload);
    } catch {
      // Not JSON-RPC; the subscriber would ignore it too.
    }
  };
  channel.addEventListener("message", hold);
  heldEvents.set(channel, { events, stop: () => channel.removeEventListener("message", hold) });
}

export function useJsonRpc(
  onRequest?: (payload: JsonRpcRequest) => void,
  // The one subscriber that receives the events held by holdRpcEvents.
  options?: { receiveHeldEvents?: boolean },
) {
  const receiveHeldEvents = options?.receiveHeldEvents ?? false;
  const { rpcDataChannel } = useRTCStore();
  const { isFailsafeMode, reason } = useFailsafeModeStore();

  const send = useCallback(
    async (method: string, params: unknown, callback?: (resp: JsonRpcResponse) => void) => {
      if (rpcDataChannel?.readyState !== "open") return;

      // Check if method is blocked in failsafe mode
      if (isFailsafeMode && reason) {
        const blockedMethods = blockedMethodsByReason[reason] || [];
        if (blockedMethods.includes(method)) {
          console.warn(`RPC method "${method}" is blocked in failsafe mode (reason: ${reason})`);

          // Call callback with error if provided
          if (callback) {
            const errorResponse: JsonRpcErrorResponse = {
              jsonrpc: "2.0",
              error: {
                code: -32000,
                message: "Method unavailable in failsafe mode",
                data: `This feature is unavailable while in failsafe mode (${reason})`,
              },
              id: requestCounter + 1,
            };
            callback(errorResponse);
          }
          return;
        }
      }

      requestCounter++;
      const payload = { jsonrpc: "2.0", method, params, id: requestCounter };
      // Store the callback if it exists
      if (callback) callbackStore.set(payload.id, callback);

      // The channel can close between the readyState check and the send.
      // Drop the callback so it cannot wait forever for a reply.
      try {
        rpcDataChannel.send(JSON.stringify(payload));
      } catch (error) {
        callbackStore.delete(payload.id);
        console.error(`Failed to send RPC method "${method}"`, error);
      }
    },
    [rpcDataChannel, isFailsafeMode, reason],
  );

  useEffect(() => {
    if (!rpcDataChannel) return;

    const messageHandler = (e: MessageEvent) => {
      const payload = JSON.parse(e.data) as JsonRpcResponse | JsonRpcRequest;

      // The "API" can also "request" data from the client
      // If the payload has a method, it's a request
      if ("method" in payload) {
        if (onRequest) onRequest(payload);
        return;
      }

      if ("error" in payload) console.error("RPC error", payload);
      if (!payload.id) return;

      const callback = callbackStore.get(payload.id);
      if (callback) {
        // Delete first so a throwing callback cannot leave its entry behind.
        callbackStore.delete(payload.id);
        callback(payload);
      }
    };

    rpcDataChannel.addEventListener("message", messageHandler);

    // Hand over the held events. No message is dispatched between adding
    // the listener above and this loop, so none is lost or delivered twice.
    const held = receiveHeldEvents ? heldEvents.get(rpcDataChannel) : undefined;
    if (held && onRequest) {
      heldEvents.delete(rpcDataChannel);
      held.stop();
      for (const event of held.events) onRequest(event);
    }

    return () => {
      rpcDataChannel.removeEventListener("message", messageHandler);
    };
  }, [rpcDataChannel, onRequest, receiveHeldEvents]);

  return { send };
}
