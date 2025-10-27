import { useRTCStore } from "@/hooks/stores";
import { sleep } from "@/utils";

// JSON-RPC utility for use outside of React components

export interface JsonRpcCallOptions {
  method: string;
  params?: unknown;
  timeout?: number;
  retriesOnError?: number;
}

export interface JsonRpcCallResponse<T = unknown> {
  jsonrpc: string;
  result?: T;
  error?: {
    code: number;
    message: string;
    data?: unknown;
  };
  id: number | string | null;
}

let rpcCallCounter = 0;

// Helper: wait for RTC data channel to be ready
async function waitForRtcReady(signal: AbortSignal): Promise<RTCDataChannel> {
  const pollInterval = 100;

  while (!signal.aborted) {
    const state = useRTCStore.getState();
    if (state.rpcDataChannel?.readyState === "open") {
      return state.rpcDataChannel;
    }
    await sleep(pollInterval);
  }

  throw new Error("RTC readiness check aborted");
}

// Helper: send RPC request and wait for response
async function sendRpcRequest<T>(
  rpcDataChannel: RTCDataChannel,
  options: JsonRpcCallOptions,
  signal: AbortSignal,
): Promise<JsonRpcCallResponse<T>> {
  return new Promise((resolve, reject) => {
    rpcCallCounter++;
    const requestId = `rpc_${Date.now()}_${rpcCallCounter}`;

    const request = {
      jsonrpc: "2.0",
      method: options.method,
      params: options.params || {},
      id: requestId,
    };

    const messageHandler = (event: MessageEvent) => {
      try {
        const response = JSON.parse(event.data) as JsonRpcCallResponse<T>;
        if (response.id === requestId) {
          cleanup();
          resolve(response);
        }
      } catch {
        // Ignore parse errors from other messages
      }
    };

    const abortHandler = () => {
      cleanup();
      reject(new Error("Request aborted"));
    };

    const cleanup = () => {
      rpcDataChannel.removeEventListener("message", messageHandler);
      signal.removeEventListener("abort", abortHandler);
    };

    signal.addEventListener("abort", abortHandler);
    rpcDataChannel.addEventListener("message", messageHandler);
    rpcDataChannel.send(JSON.stringify(request));
  });
}

// Function overloads for better typing
export function callJsonRpc<T>(
  options: JsonRpcCallOptions,
): Promise<JsonRpcCallResponse<T> & { result: T }>;
export function callJsonRpc(
  options: JsonRpcCallOptions,
): Promise<JsonRpcCallResponse<unknown>>;
export async function callJsonRpc<T = unknown>(
  options: JsonRpcCallOptions,
): Promise<JsonRpcCallResponse<T>> {
  const maxRetries = options.retriesOnError ?? 0;
  const timeout = options.timeout || 5000;

  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    const abortController = new AbortController();
    const timeoutId = setTimeout(() => abortController.abort(), timeout);

    try {
      // Wait for RTC readiness
      const rpcDataChannel = await waitForRtcReady(abortController.signal);

      // Send RPC request and wait for response
      const response = await sendRpcRequest<T>(
        rpcDataChannel,
        options,
        abortController.signal,
      );

      clearTimeout(timeoutId);

      // Retry on error if attempts remain
      if (response.error && attempt < maxRetries) {
        await sleep(1000);
        continue;
      }

      return response;
    } catch (error) {
      clearTimeout(timeoutId);

      // Retry on timeout/error if attempts remain
      if (attempt < maxRetries) {
        await sleep(1000);
        continue;
      }

      throw error instanceof Error
        ? error
        : new Error(`JSON-RPC call failed after ${timeout}ms`);
    }
  }

  // Should never reach here due to loop logic, but TypeScript needs this
  throw new Error("Unexpected error in callJsonRpc");
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

export interface VersionInfo {
  appVersion: string;
  systemVersion: string;
}

export interface SystemVersionInfo {
  local: VersionInfo;
  remote?: VersionInfo;
  systemUpdateAvailable: boolean;
  appUpdateAvailable: boolean;
  error?: string;
}

export async function getUpdateStatus() {
  const response = await callJsonRpc<SystemVersionInfo>({
    method: "getUpdateStatus",
    // This function calls our api server to see if there are any updates available.
    // It can be called on page load right after a restart, so we need to give it time to
    // establish a connection to the api server.
    timeout: 10000,
    retriesOnError: 5,
  });

  if (response.error) throw response.error;
  return response.result;
}

export async function getLocalVersion() {
  const response = await callJsonRpc<VersionInfo>({ method: "getLocalVersion" });
  if (response.error) throw response.error;
  return response.result;
}
