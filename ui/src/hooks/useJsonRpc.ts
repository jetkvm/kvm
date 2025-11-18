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
  video: [
    'setStreamQualityFactor',
    'getEDID',
    'setEDID',
    'getVideoLogStatus',
    'setDisplayRotation',
    'getVideoSleepMode',
    'setVideoSleepMode',
    'getVideoState',
  ],
};

export function useJsonRpc(onRequest?: (payload: JsonRpcRequest) => void) {
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

      rpcDataChannel.send(JSON.stringify(payload));
    },
    [rpcDataChannel, isFailsafeMode, reason]
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
        callback(payload);
        callbackStore.delete(payload.id);
      }
    };

    rpcDataChannel.addEventListener("message", messageHandler);

    return () => {
      rpcDataChannel.removeEventListener("message", messageHandler);
    };
  },
    [rpcDataChannel, onRequest]);

  return { send };
}
