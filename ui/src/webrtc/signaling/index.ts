import type { SignalingHook, SignalingOptions } from "./types";
import { useWebSocketSignaling } from "./useWebSocketSignaling";

export type { SignalingController, SignalingHook, SignalingOptions } from "./types";

export type SignalingKind = "jetkvm-websocket";

const adapters: Record<SignalingKind, SignalingHook> = {
  "jetkvm-websocket": useWebSocketSignaling,
};

export const DEFAULT_SIGNALING: SignalingKind = "jetkvm-websocket";

/**
 * Resolve the signaling adapter for a session. `kind` must be stable for the
 * lifetime of the calling component: adapters are hooks, so switching kinds
 * between renders is not supported.
 */
export function useSignaling(options: SignalingOptions, kind: SignalingKind = DEFAULT_SIGNALING) {
  const useAdapter = adapters[kind];
  return useAdapter(options);
}
