import { useRTCStore } from "@/hooks/stores";

// JSON-RPC utility for use outside of React components
export interface JsonRpcCallOptions {
  method: string;
  params?: unknown;
  timeout?: number;
}

export interface JsonRpcCallResponse {
  jsonrpc: string;
  result?: unknown;
  error?: {
    code: number;
    message: string;
    data?: unknown;
  };
  id: number | string | null;
}

let rpcCallCounter = 0;

export function callJsonRpc(options: JsonRpcCallOptions): Promise<JsonRpcCallResponse> {
  return new Promise((resolve, reject) => {
    // Access the RTC store directly outside of React context
    const rpcDataChannel = useRTCStore.getState().rpcDataChannel;

    if (!rpcDataChannel || rpcDataChannel.readyState !== "open") {
      reject(new Error("RPC data channel not available"));
      return;
    }

    rpcCallCounter++;
    const requestId = `rpc_${Date.now()}_${rpcCallCounter}`;

    const request = {
      jsonrpc: "2.0",
      method: options.method,
      params: options.params || {},
      id: requestId,
    };

    const timeout = options.timeout || 5000;
    let timeoutId: number | undefined; // eslint-disable-line prefer-const

    const messageHandler = (event: MessageEvent) => {
      try {
        const response = JSON.parse(event.data) as JsonRpcCallResponse;
        if (response.id === requestId) {
          clearTimeout(timeoutId);
          rpcDataChannel.removeEventListener("message", messageHandler);
          resolve(response);
        }
      } catch (error) {
        // Ignore parse errors from other messages
      }
    };

    timeoutId = setTimeout(() => {
      rpcDataChannel.removeEventListener("message", messageHandler);
      reject(new Error(`JSON-RPC call timed out after ${timeout}ms`));
    }, timeout);

    rpcDataChannel.addEventListener("message", messageHandler);
    rpcDataChannel.send(JSON.stringify(request));
  });
}

// Specific network settings API calls
export async function getNetworkSettings() {
  const response = await callJsonRpc({ method: "getNetworkSettings" });
  if (response.error) {
    throw new Error(response.error.message);
  }
  return response.result;
}

export async function setNetworkSettings(settings: unknown) {
  const response = await callJsonRpc({
    method: "setNetworkSettings",
    params: { settings },
  });
  if (response.error) {
    throw new Error(response.error.message);
  }
  return response.result;
}

export async function getNetworkState() {
  const response = await callJsonRpc({ method: "getNetworkState" });
  if (response.error) {
    throw new Error(response.error.message);
  }
  return response.result;
}

export async function renewDHCPLease() {
  const response = await callJsonRpc({ method: "renewDHCPLease" });
  if (response.error) {
    throw new Error(response.error.message);
  }
  return response.result;
}
