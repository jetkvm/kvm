import { useCallback, useEffect, useRef, useState } from "react";
import { useLoaderData, useParams } from "react-router";
import type { LoaderFunction, LoaderFunctionArgs, Params } from "react-router";
import useWebSocket from "react-use-websocket";

import { CLOUD_API, DEVICE_API } from "@/ui.config";
import api from "@/api";
import { checkDeviceAuth, checkCloudAuth, isOnDevice, isInCloud } from "@/main";
import {
  useRTCStore,
  User,
  useHidStore,
  KeyboardLedState,
  KeysDownState,
  useSettingsStore,
} from "@hooks/stores";
import { JsonRpcRequest, JsonRpcResponse, RpcMethodNotFound, useJsonRpc } from "@hooks/useJsonRpc";
import WebRTCVideo from "@components/WebRTCVideo";
import DetachedToolbar from "@components/DetachedToolbar";
import { m } from "@localizations/messages.js";
import { doRpcHidHandshake } from "@hooks/useHidRpc";
import {
  LoadingConnectionOverlay,
  ConnectionFailedOverlay,
  PeerConnectionDisconnectedOverlay,
} from "@components/VideoOverlay";

interface LocalLoaderResp {
  authMode: "password" | "noPassword" | null;
}

interface CloudLoaderResp {
  deviceName: string;
  user: User | null;
  iceConfig: {
    iceServers: { credential?: string; urls: string | string[]; username?: string };
  } | null;
}

const deviceLoader = async () => {
  const device = await checkDeviceAuth();
  // For device mode, get the device name from the API
  const deviceResp = await api.GET(`${DEVICE_API}/device`);
  let deviceName = m.jetkvm_device();
  if (deviceResp.ok) {
    const deviceData = await deviceResp.json();
    deviceName = deviceData.deviceName || deviceData.id || m.jetkvm_device();
  }
  return { authMode: device.authMode, deviceName } as LocalLoaderResp & { deviceName: string };
};

const cloudLoader = async (params: Params<string>): Promise<CloudLoaderResp> => {
  const user = await checkCloudAuth();
  const iceResp = await api.POST(`${CLOUD_API}/webrtc/ice_config`);
  const iceConfig = await iceResp.json();
  const deviceResp = await api.GET(`${CLOUD_API}/devices/${params.id}`);

  if (!deviceResp.ok) {
    if (deviceResp.status === 404) {
      throw new Response("Device not found", { status: 404 });
    }
    throw new Error("Error fetching device");
  }

  const { device } = (await deviceResp.json()) as {
    device: { id: string; name: string; user: { googleId: string } };
  };

  return { user, iceConfig, deviceName: device.name || device.id } as CloudLoaderResp;
};

const loader: LoaderFunction = ({ params }: LoaderFunctionArgs) => {
  return isOnDevice ? deviceLoader() : cloudLoader(params);
};

export default function DetachedRoute() {
  const loaderResp = useLoaderData() as
    | (LocalLoaderResp & { deviceName?: string })
    | CloudLoaderResp;
  const iceConfig = "iceConfig" in loaderResp ? loaderResp.iceConfig : null;

  const params = useParams() as { id: string };

  const {
    peerConnection,
    setPeerConnection,
    peerConnectionState,
    setPeerConnectionState,
    setMediaStream,
    setRpcDataChannel,
    rpcDataChannel,
    setTransceiver,
    setRpcHidChannel,
    setRpcHidUnreliableNonOrderedChannel,
    setRpcHidUnreliableChannel,
    setRpcHidProtocolVersion,
  } = useRTCStore();

  const isLegacySignalingEnabled = useRef(false);
  const [connectionFailed, setConnectionFailed] = useState(false);
  const [loadingMessage, setLoadingMessage] = useState(m.connecting_to_device());

  const connectionFailedRef = useRef(false);
  useEffect(() => {
    connectionFailedRef.current = connectionFailed;
  }, [connectionFailed]);

  const cleanupAndStopReconnecting = useCallback(
    function cleanupAndStopReconnecting() {
      console.log("Closing peer connection");
      setConnectionFailed(true);
      if (peerConnection) {
        setPeerConnectionState(peerConnection.connectionState);
      }
      connectionFailedRef.current = true;
      peerConnection?.close();
      signalingAttempts.current = 0;
    },
    [peerConnection, setPeerConnectionState],
  );

  const signalingAttempts = useRef(0);
  const setRemoteSessionDescription = useCallback(
    async function setRemoteSessionDescription(
      pc: RTCPeerConnection,
      remoteDescription: RTCSessionDescriptionInit,
    ) {
      setLoadingMessage(m.setting_remote_description());
      try {
        await pc.setRemoteDescription(new RTCSessionDescription(remoteDescription));
        setLoadingMessage(m.establishing_secure_connection());
      } catch (error) {
        console.error("[setRemoteSessionDescription] Failed to set remote description:", error);
        cleanupAndStopReconnecting();
        return;
      }

      let attempts = 0;
      const checkInterval = setInterval(() => {
        attempts++;
        if (pc.sctp?.state === "connected") {
          clearInterval(checkInterval);
          setLoadingMessage(m.connection_established());
        } else if (attempts >= 10) {
          cleanupAndStopReconnecting();
          clearInterval(checkInterval);
        }
      }, 1000);
    },
    [cleanupAndStopReconnecting],
  );

  const ignoreOffer = useRef(false);
  const isSettingRemoteAnswerPending = useRef(false);
  const makingOffer = useRef(false);
  const reconnectAttemptsRef = useRef(2000);
  const wsProtocol = window.location.protocol === "https:" ? "wss:" : "ws:";

  const reconnectInterval = (attempt: number) => {
    return Math.min(500 * 2 ** attempt, 10000);
  };

  const { sendMessage, getWebSocket } = useWebSocket(
    isOnDevice
      ? `${wsProtocol}//${window.location.host}/webrtc/signaling/client`
      : `${CLOUD_API.replace("http", "ws")}/webrtc/signaling/client?id=${params.id}`,
    {
      heartbeat: true,
      retryOnError: true,
      reconnectAttempts: reconnectAttemptsRef.current,
      reconnectInterval: reconnectInterval,
      onReconnectStop: () => {
        cleanupAndStopReconnecting();
      },
      shouldReconnect() {
        return !isLegacySignalingEnabled.current;
      },
      onOpen() {
        console.debug("[Websocket] onOpen");
      },
      onMessage(event: WebSocketEventMap["message"]) {
        const message = event;
        if (message.data === "pong") return;

        const parsedMessage = JSON.parse(message.data);

        if (parsedMessage.type === "device-metadata") {
          const { deviceVersion } = parsedMessage.data;
          if (!deviceVersion) {
            isLegacySignalingEnabled.current = true;
            getWebSocket()?.close();
          } else {
            isLegacySignalingEnabled.current = false;
          }
          setupPeerConnection();
        }

        if (!peerConnection) return;

        if (parsedMessage.type === "answer") {
          const readyForOffer =
            !makingOffer &&
            (peerConnection?.signalingState === "stable" || isSettingRemoteAnswerPending.current);

          ignoreOffer.current = parsedMessage.type === "offer" && !readyForOffer;
          if (ignoreOffer.current) return;

          isSettingRemoteAnswerPending.current = parsedMessage.type === "answer";

          const sd = atob(parsedMessage.data);
          const remoteSessionDescription = JSON.parse(sd);

          setRemoteSessionDescription(
            peerConnection,
            new RTCSessionDescription(remoteSessionDescription),
          );

          isSettingRemoteAnswerPending.current = false;
        } else if (parsedMessage.type === "new-ice-candidate") {
          const candidate = parsedMessage.data;
          peerConnection.addIceCandidate(candidate);
        }
      },
    },
  );

  const sendWebRTCSignal = useCallback(
    (type: string, data: unknown) => {
      sendMessage(JSON.stringify({ type, data }), false);
    },
    [sendMessage],
  );

  const legacyHTTPSignaling = useCallback(
    async (pc: RTCPeerConnection) => {
      const sd = btoa(JSON.stringify(pc.localDescription));
      const sessionUrl = `${CLOUD_API}/webrtc/session`;

      setLoadingMessage(
        m.getting_remote_session_description({ attempt: signalingAttempts.current + 1 }),
      );
      const res = await api.POST(sessionUrl, {
        sd,
        ...(isOnDevice ? {} : { id: params.id }),
      });

      const json = await res.json();
      if (!res.ok) {
        cleanupAndStopReconnecting();
        return;
      }

      setLoadingMessage(m.setting_remote_session_description());

      const decodedSd = atob(json.sd);
      const parsedSd = JSON.parse(decodedSd);
      setRemoteSessionDescription(pc, new RTCSessionDescription(parsedSd));
    },
    [cleanupAndStopReconnecting, params.id, setRemoteSessionDescription],
  );

  const setupPeerConnection = useCallback(async () => {
    setConnectionFailed(false);
    setLoadingMessage(m.connecting_to_device());

    let pc: RTCPeerConnection;
    try {
      setLoadingMessage(m.creating_peer_connection());
      pc = new RTCPeerConnection({
        ...(isInCloud && iceConfig?.iceServers ? { iceServers: [iceConfig?.iceServers] } : {}),
      });

      setPeerConnectionState(pc.connectionState);
      setLoadingMessage(m.setting_up_connection_to_device());
    } catch (e) {
      console.error(`[setupPeerConnection] Error creating peer connection: ${e}`);
      setTimeout(() => {
        cleanupAndStopReconnecting();
      }, 1000);
      return;
    }

    pc.onconnectionstatechange = () => {
      setPeerConnectionState(pc.connectionState);
    };

    pc.onnegotiationneeded = async () => {
      try {
        makingOffer.current = true;
        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        const sd = btoa(JSON.stringify(pc.localDescription));
        const isNewSignalingEnabled = isLegacySignalingEnabled.current === false;
        if (isNewSignalingEnabled) {
          sendWebRTCSignal("offer", { sd: sd });
        }
      } catch (e) {
        console.error(`[setupPeerConnection] Error creating offer: ${e}`);
        cleanupAndStopReconnecting();
      } finally {
        makingOffer.current = false;
      }
    };

    pc.onicecandidate = ({ candidate }) => {
      if (!candidate) return;
      if (candidate.candidate === "") return;
      sendWebRTCSignal("new-ice-candidate", candidate);
    };

    pc.onicegatheringstatechange = event => {
      const pc = event.currentTarget as RTCPeerConnection;
      if (pc.iceGatheringState === "complete") {
        setLoadingMessage(m.ice_gathering_completed());
        if (isLegacySignalingEnabled.current) {
          legacyHTTPSignaling(pc);
        }
      } else if (pc.iceGatheringState === "gathering") {
        setLoadingMessage(m.gathering_ice_candidates());
      }
    };

    pc.ontrack = function (event) {
      setMediaStream(event.streams[0]);
    };

    setTransceiver(pc.addTransceiver("video", { direction: "recvonly" }));

    const rpcDataChannel = pc.createDataChannel("rpc");
    rpcDataChannel.onclose = () => {
      setRpcDataChannel(null);
    };
    rpcDataChannel.onerror = (ev: Event) =>
      console.error(`Error on DataChannel '${rpcDataChannel.label}': ${ev}`);
    rpcDataChannel.onopen = () => {
      setRpcDataChannel(rpcDataChannel);
    };

    const rpcHidChannel = pc.createDataChannel("hidrpc");
    rpcHidChannel.binaryType = "arraybuffer";
    rpcHidChannel.onclose = () => console.log("rpcHidChannel has closed");
    rpcHidChannel.onerror = (ev: Event) =>
      console.error(`Error on rpcHidChannel '${rpcHidChannel.label}': ${ev}`);
    rpcHidChannel.onopen = () => {
      setRpcHidChannel(rpcHidChannel);
    };
    doRpcHidHandshake(rpcHidChannel, setRpcHidProtocolVersion);

    const rpcHidUnreliableChannel = pc.createDataChannel("hidrpc-unreliable-ordered", {
      ordered: true,
      maxRetransmits: 0,
    });
    rpcHidUnreliableChannel.binaryType = "arraybuffer";
    rpcHidUnreliableChannel.onclose = () => console.log("rpcHidUnreliableChannel has closed");
    rpcHidUnreliableChannel.onerror = (ev: Event) =>
      console.error(`Error on rpcHidUnreliableChannel '${rpcHidUnreliableChannel.label}': ${ev}`);
    rpcHidUnreliableChannel.onopen = () => {
      setRpcHidUnreliableChannel(rpcHidUnreliableChannel);
    };

    const rpcHidUnreliableNonOrderedChannel = pc.createDataChannel("hidrpc-unreliable-nonordered", {
      ordered: false,
      maxRetransmits: 0,
    });
    rpcHidUnreliableNonOrderedChannel.binaryType = "arraybuffer";
    rpcHidUnreliableNonOrderedChannel.onclose = () =>
      console.log("rpcHidUnreliableNonOrderedChannel has closed");
    rpcHidUnreliableNonOrderedChannel.onerror = (ev: Event) =>
      console.error(
        `Error on rpcHidUnreliableNonOrderedChannel '${rpcHidUnreliableNonOrderedChannel.label}': ${ev}`,
      );
    rpcHidUnreliableNonOrderedChannel.onopen = () => {
      setRpcHidUnreliableNonOrderedChannel(rpcHidUnreliableNonOrderedChannel);
    };

    setPeerConnection(pc);
  }, [
    cleanupAndStopReconnecting,
    iceConfig?.iceServers,
    legacyHTTPSignaling,
    sendWebRTCSignal,
    setMediaStream,
    setPeerConnection,
    setPeerConnectionState,
    setRpcDataChannel,
    setRpcHidChannel,
    setRpcHidUnreliableNonOrderedChannel,
    setRpcHidUnreliableChannel,
    setRpcHidProtocolVersion,
    setTransceiver,
  ]);

  useEffect(() => {
    if (peerConnectionState === "failed") {
      cleanupAndStopReconnecting();
    }
  }, [peerConnectionState, cleanupAndStopReconnecting]);

  // Cleanup effect
  const { clearInboundRtpStats, clearCandidatePairStats } = useRTCStore();

  useEffect(() => {
    return () => {
      peerConnection?.close();
    };
  }, [peerConnection]);

  useEffect(() => {
    return () => {
      clearInboundRtpStats();
      clearCandidatePairStats();
      setPeerConnection(null);
      setRpcDataChannel(null);
    };
  }, [clearCandidatePairStats, clearInboundRtpStats, setPeerConnection, setRpcDataChannel]);

  // HID state management
  const { setKeyboardLedState, setKeysDownState } = useHidStore();
  const setHidRpcDisabled = useRTCStore(state => state.setHidRpcDisabled);

  // Settings for detached window
  const { showDetachedToolbar } = useSettingsStore();

  function onJsonRpcRequest(resp: JsonRpcRequest) {
    if (resp.method === "keyboardLedState") {
      const ledState = resp.params as KeyboardLedState;
      setKeyboardLedState(ledState);
    }

    if (resp.method === "keysDownState") {
      const downState = resp.params as KeysDownState;
      setKeysDownState(downState);
    }
  }

  const { send } = useJsonRpc(onJsonRpcRequest);

  const [needLedState, setNeedLedState] = useState(true);

  useEffect(() => {
    if (rpcDataChannel?.readyState !== "open") return;
    if (!needLedState) return;

    send("getKeyboardLedState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to get keyboard led state", resp.error);
        return;
      } else {
        const ledState = resp.result as KeyboardLedState;
        setKeyboardLedState(ledState);
      }
      setNeedLedState(false);
    });
  }, [rpcDataChannel?.readyState, send, setKeyboardLedState, needLedState]);

  const [needKeyDownState, setNeedKeyDownState] = useState(true);

  useEffect(() => {
    if (rpcDataChannel?.readyState !== "open") return;
    if (!needKeyDownState) return;

    send("getKeyDownState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        if (resp.error.code === RpcMethodNotFound) {
          setHidRpcDisabled(true);
        } else {
          console.error("Failed to get key down state", resp.error);
        }
      } else {
        const downState = resp.result as KeysDownState;
        setKeysDownState(downState);
      }
      setNeedKeyDownState(false);
    });
  }, [needKeyDownState, rpcDataChannel?.readyState, send, setKeysDownState, setHidRpcDisabled]);

  // Fetch hostname from network state
  const [hostname, setHostname] = useState<string | null>(null);
  const [needHostname, setNeedHostname] = useState(true);

  useEffect(() => {
    if (rpcDataChannel?.readyState !== "open") return;
    if (!needHostname) return;

    send("getNetworkState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to get network state", resp.error);
      } else {
        const networkState = resp.result as { hostname?: string };
        if (networkState?.hostname) {
          setHostname(networkState.hostname);
        }
      }
      setNeedHostname(false);
    });
  }, [rpcDataChannel?.readyState, send, needHostname]);

  // Set window title using hostname
  useEffect(() => {
    document.title = hostname ? `JetKVM: ${hostname}` : "JetKVM";
  }, [hostname]);

  const handleClose = useCallback(() => {
    window.close();
  }, []);

  // Connection status overlay
  const hasConnectionFailed =
    connectionFailed || ["failed", "closed"].includes(peerConnectionState ?? "");
  const isPeerConnectionLoading =
    ["connecting", "new"].includes(peerConnectionState ?? "") || peerConnection === null;
  const isDisconnected = peerConnectionState === "disconnected";
  const showOverlay =
    peerConnectionState !== "connected" &&
    (hasConnectionFailed || isPeerConnectionLoading || isDisconnected);

  return (
    <div className="flex h-screen w-screen flex-col overflow-hidden bg-black">
      {showDetachedToolbar && (
        <DetachedToolbar
          deviceName={hostname || m.jetkvm_device()}
          connectionState={peerConnectionState}
          onClose={handleClose}
        />
      )}
      <div className="relative flex-1 overflow-hidden">
        <WebRTCVideo
          hasConnectionIssues={showOverlay}
          hideActionBar={!showDetachedToolbar}
          isDetachedWindow={true}
        />
        {showOverlay && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center p-4">
            <div className="pointer-events-auto relative h-full max-h-[720px] w-full max-w-7xl rounded-md">
              {isDisconnected ? (
                <PeerConnectionDisconnectedOverlay show={true} />
              ) : hasConnectionFailed ? (
                <ConnectionFailedOverlay show={true} setupPeerConnection={setupPeerConnection} />
              ) : isPeerConnectionLoading ? (
                <LoadingConnectionOverlay show={true} text={loadingMessage} />
              ) : null}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

DetachedRoute.loader = loader;
