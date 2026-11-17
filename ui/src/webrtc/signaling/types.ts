/**
 * Signaling adapters own the RTCPeerConnection lifecycle: creating the peer,
 * exchanging session descriptions and ICE candidates with the device, and
 * deciding when a connection attempt has failed. The device route only
 * attaches what it needs to a freshly created peer (transceivers, data
 * channels, track handling) through `onPeerConnection`.
 *
 * Adapters publish the peer and its connection state through the RTC store,
 * so everything downstream of the store is unaware of which adapter is in use.
 */

export interface SignalingOptions {
  /** Device identifier; used to address the device when not on-device. */
  readonly deviceId: string;
  /** ICE servers for this session. Omitted on the local network. */
  readonly iceServers?: RTCIceServer[];
  /**
   * Called synchronously for every new RTCPeerConnection, after the adapter
   * has installed its own handlers and before negotiation starts. Anything
   * that must be part of the initial offer (transceivers, data channels) is
   * added here.
   */
  readonly onPeerConnection: (pc: RTCPeerConnection) => void;
}

export interface SignalingController {
  /**
   * Create a new peer connection and negotiate it with the device. Safe to
   * call again after a failure; any previous peer is discarded.
   */
  readonly connect: () => Promise<void>;
  /** Progress text for the connection overlay. */
  readonly loadingMessage: string;
  /** True once the adapter has given up on the current attempt. */
  readonly connectionFailed: boolean;
}

/**
 * An adapter is a React hook so it can hold state, subscribe to the stores
 * and run effects for its transport. The hook identity must not change for
 * the lifetime of the route.
 */
export type SignalingHook = (options: SignalingOptions) => SignalingController;
