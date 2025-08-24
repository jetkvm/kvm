import { useCallback, useEffect } from "react";

import { useRTCStore } from "@/hooks/stores";

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

const callbackStore = new Map<number | string, (resp: JsonRpcResponse) => void>();
let requestCounter = 0;

export function useJsonRpc(onRequest?: (payload: JsonRpcRequest) => void) {
  const rpcDataChannel = useRTCStore(state => state.rpcDataChannel);

  const send = useCallback(
    (method: string, params: unknown, callback?: (resp: JsonRpcResponse) => void) => {
      if (rpcDataChannel?.readyState !== "open") return;
      requestCounter++;
      const payload = { jsonrpc: "2.0", method, params, id: requestCounter };
      // Store the callback if it exists
      if (callback) callbackStore.set(payload.id, callback);

      rpcDataChannel.send(JSON.stringify(payload));
    },
    [rpcDataChannel],
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

      if ("error" in payload) console.error(payload.error);
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
  }, [rpcDataChannel, onRequest]);

  return { send };
}
