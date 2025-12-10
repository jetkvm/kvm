import { useRTCStore } from "@/hooks/stores";
import { sleep } from "@/utils";

// JSON-RPC utility for use outside of React components

export interface JsonRpcCallOptions {
  method: string;
  params?: unknown;
  attemptTimeoutMs?: number;
  maxAttempts?: number;
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
// This waits indefinitely for the channel to be ready, only aborting via the signal
// Throws if the channel instance changed while waiting (stale connection detected)
async function waitForRtcReady(signal: AbortSignal): Promise<RTCDataChannel> {
  const pollInterval = 100;
  let lastSeenChannel: RTCDataChannel | null = null;

  while (!signal.aborted) {
    const state = useRTCStore.getState();
    const currentChannel = state.rpcDataChannel;

    // Channel instance changed (new connection replaced old one)
    if (lastSeenChannel && currentChannel && lastSeenChannel !== currentChannel) {
      console.debug("[waitForRtcReady] Channel instance changed, aborting wait");
      throw new Error("RTC connection changed while waiting for readiness");
    }

    // Channel was removed from store (connection closed)
    if (lastSeenChannel && !currentChannel) {
      console.debug("[waitForRtcReady] Channel was removed from store, aborting wait");
      throw new Error("RTC connection was closed while waiting for readiness");
    }

    // No channel yet, keep waiting
    if (!currentChannel) {
      await sleep(pollInterval);
      continue;
    }

    // Track this channel instance
    lastSeenChannel = currentChannel;

    // Channel is ready!
    if (currentChannel.readyState === "open") {
      return currentChannel;
    }

    await sleep(pollInterval);
  }

  // Signal was aborted for some reason
  console.debug("[waitForRtcReady] Aborted via signal");
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
  const maxAttempts = options.maxAttempts ?? 1;
  const timeout = options.attemptTimeoutMs || 5000;

  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    // Exponential backoff for retries that starts at 500ms up to a maximum of 10 seconds
    const backoffMs = Math.min(500 * Math.pow(2, attempt), 10000);
    let timeoutId: ReturnType<typeof setTimeout> | null = null;

    try {
      // Wait for RTC readiness without timeout - this allows time for WebRTC to connect
      const readyAbortController = new AbortController();
      const rpcDataChannel = await waitForRtcReady(readyAbortController.signal);

      // Now apply timeout only to the actual RPC request/response
      const rpcAbortController = new AbortController();
      timeoutId = setTimeout(() => rpcAbortController.abort(), timeout);

      // Send RPC request and wait for response
      const response = await sendRpcRequest<T>(
        rpcDataChannel,
        options,
        rpcAbortController.signal,
      );

      // Retry on error if attempts remain
      if (response.error && attempt < maxAttempts - 1) {
        await sleep(backoffMs);
        continue;
      }

      return response;
    } catch (error) {
      // Retry on timeout/error if attempts remain
      if (attempt < maxAttempts - 1) {
        await sleep(backoffMs);
        continue;
      }

      throw error instanceof Error
        ? error
        : new Error(`JSON-RPC call failed after ${timeout}ms`);
    } finally {
      if (timeoutId !== null) {
        clearTimeout(timeoutId);
      }
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
  willDisableAutoUpdate?: boolean;
  error?: string;
}

const UPDATE_STATUS_RPC_TIMEOUT_MS = 10000;
const UPDATE_STATUS_RPC_MAX_ATTEMPTS = 6;

export async function getUpdateStatus() {
  const response = await callJsonRpc<SystemVersionInfo>({
    method: "getUpdateStatus",
    // This function calls our api server to see if there are any updates available.
    // It can be called on page load right after a restart, so we need to give it time to
    // establish a connection to the api server.
    maxAttempts: UPDATE_STATUS_RPC_MAX_ATTEMPTS,
    attemptTimeoutMs: UPDATE_STATUS_RPC_TIMEOUT_MS,
  });

  if (response.error) throw response.error;
  return response.result;
}

export async function getLocalVersion(
  options?: Partial<JsonRpcCallOptions>,
): Promise<VersionInfo> {
  const response = await callJsonRpc<VersionInfo>({
    method: "getLocalVersion",
    ...options,
  });
  if (response.error) throw response.error;
  return response.result;
}

export type UpdateComponent = "app" | "system";
export type UpdateComponents = Partial<Record<UpdateComponent, string>>;

export interface updateParams {
  components?: UpdateComponents;
}

export async function checkUpdateComponents(
  params: updateParams,
  includePreRelease: boolean,
) {
  const response = await callJsonRpc<SystemVersionInfo>({
    method: "checkUpdateComponents",
    params: {
      params,
      includePreRelease,
    },
    // maxAttempts is set to 1,
    // because it currently retry for all errors,
    // and we don't want to retry if the error is not a network error
    maxAttempts: 1,
    attemptTimeoutMs: UPDATE_STATUS_RPC_TIMEOUT_MS,
  });
  if (response.error) throw response.error;
  return response.result;
}
