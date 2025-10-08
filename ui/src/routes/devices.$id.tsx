import { lazy, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Outlet,
  redirect,
  useLoaderData,
  useLocation,
  useNavigate,
  useOutlet,
  useParams,
  useSearchParams,
} from "react-router";
import type { LoaderFunction, LoaderFunctionArgs, Params } from "react-router";
import { useInterval } from "usehooks-ts";
import { FocusTrap } from "focus-trap-react";
import { motion, AnimatePresence } from "framer-motion";
import useWebSocket from "react-use-websocket";

import { CLOUD_API, DEVICE_API } from "@/ui.config";
import api from "@/api";
import { checkAuth, isInCloud, isOnDevice } from "@/main";
import { usePermissions, Permission } from "@/hooks/usePermissions";
import { cx } from "@/cva.config";
import {
  KeyboardLedState,
  KeysDownState,
  NetworkState,
  OtaState,
  USBStates,
  useHidStore,
  useNetworkStateStore,
  User,
  useRTCStore,
  useSettingsStore,
  useUiStore,
  useUpdateStore,
  useVideoStore,
  VideoState,
} from "@/hooks/stores";
import WebRTCVideo from "@components/WebRTCVideo";
import UnifiedSessionRequestDialog from "@components/UnifiedSessionRequestDialog";
import NicknameModal from "@components/NicknameModal";
import AccessDeniedOverlay from "@components/AccessDeniedOverlay";
import PendingApprovalOverlay from "@components/PendingApprovalOverlay";
import DashboardNavbar from "@components/Header";
const ConnectionStatsSidebar = lazy(() => import('@/components/sidebar/connectionStats'));
const Terminal = lazy(() => import('@components/Terminal'));
const UpdateInProgressStatusCard = lazy(() => import("@/components/UpdateInProgressStatusCard"));
import Modal from "@/components/Modal";
import { JsonRpcRequest, JsonRpcResponse, RpcMethodNotFound, useJsonRpc } from "@/hooks/useJsonRpc";
import {
  ConnectionFailedOverlay,
  LoadingConnectionOverlay,
  PeerConnectionDisconnectedOverlay,
} from "@/components/VideoOverlay";
import { useDeviceUiNavigation } from "@/hooks/useAppNavigation";
import { FeatureFlagProvider } from "@/providers/FeatureFlagProvider";
import { DeviceStatus } from "@routes/welcome-local";
import { useVersion } from "@/hooks/useVersion";
import { useSessionManagement } from "@/hooks/useSessionManagement";
import { useSessionStore, useSharedSessionStore } from "@/stores/sessionStore";
import { sessionApi } from "@/api/sessionApi";

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

export type AuthMode = "password" | "noPassword" | null;
export interface LocalDevice {
  authMode: AuthMode;
  deviceId: string;
}

const deviceLoader = async () => {
  const res = await api
    .GET(`${DEVICE_API}/device/status`)
    .then(res => res.json() as Promise<DeviceStatus>);

  if (!res.isSetup) return redirect("/welcome");

  const deviceRes = await api.GET(`${DEVICE_API}/device`);
  if (deviceRes.status === 401) return redirect("/login-local");
  if (deviceRes.ok) {
    const device = (await deviceRes.json()) as LocalDevice;
    return { authMode: device.authMode };
  }

  throw new Error("Error fetching device");
};

const cloudLoader = async (params: Params<string>): Promise<CloudLoaderResp> => {
  const user = await checkAuth();

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

  return { user, iceConfig, deviceName: device.name || device.id };
};

const loader: LoaderFunction = ({ params }: LoaderFunctionArgs) => {
  return import.meta.env.MODE === "device" ? deviceLoader() : cloudLoader(params);
};

export default function KvmIdRoute() {
  const loaderResp = useLoaderData() as LocalLoaderResp | CloudLoaderResp;
  // Depending on the mode, we set the appropriate variables
  const user = "user" in loaderResp ? loaderResp.user : null;
  const deviceName = "deviceName" in loaderResp ? loaderResp.deviceName : null;
  const iceConfig = "iceConfig" in loaderResp ? loaderResp.iceConfig : null;
  const authMode = "authMode" in loaderResp ? loaderResp.authMode : null;

  const params = useParams() as { id: string };
  const { sidebarView, setSidebarView, disableVideoFocusTrap, setDisableVideoFocusTrap } = useUiStore();
  const [ queryParams, setQueryParams ] = useSearchParams();

  const { 
    peerConnection, setPeerConnection,
    peerConnectionState, setPeerConnectionState,
    setMediaStream,
    setRpcDataChannel,
    isTurnServerInUse, setTurnServerInUse,
    rpcDataChannel,
    setTransceiver,
    setRpcHidChannel,
    setRpcHidUnreliableNonOrderedChannel,
    setRpcHidUnreliableChannel,
  } = useRTCStore();

  const location = useLocation();
  const isLegacySignalingEnabled = useRef(false);
  const [connectionFailed, setConnectionFailed] = useState(false);
  const [showNicknameModal, setShowNicknameModal] = useState(false);
  const [accessDenied, setAccessDenied] = useState(false);

  const navigate = useNavigate();
  const { otaState, setOtaState, setModalView } = useUpdateStore();
  const { currentSessionId, currentMode, setCurrentSession } = useSessionStore();
  const { nickname, setNickname } = useSharedSessionStore();
  const { setRequireSessionApproval, setRequireSessionNickname } = useSettingsStore();
  const [globalSessionSettings, setGlobalSessionSettings] = useState<{requireApproval: boolean, requireNickname: boolean} | null>(null);
  const { hasPermission } = usePermissions();

  const [loadingMessage, setLoadingMessage] = useState("Connecting to device...");
  const cleanupAndStopReconnecting = useCallback(
    function cleanupAndStopReconnecting() {

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

  // We need to track connectionFailed in a ref to avoid stale closure issues
  // This is necessary because syncRemoteSessionDescription is a callback that captures
  // the connectionFailed value at creation time, but we need the latest value
  // when the function is actually called. Without this ref, the function would use
  // a stale value of connectionFailed in some conditions.
  //
  // We still need the state variable for UI rendering, so we sync the ref with the state.
  // This pattern is a workaround for what useEvent hook would solve more elegantly
  // (which would give us a callback that always has access to latest state without re-creation).
  const connectionFailedRef = useRef(false);
  useEffect(() => {
    connectionFailedRef.current = connectionFailed;
  }, [connectionFailed]);

  const signalingAttempts = useRef(0);
  const setRemoteSessionDescription = useCallback(
    async function setRemoteSessionDescription(
      pc: RTCPeerConnection,
      remoteDescription: RTCSessionDescriptionInit,
    ) {
      setLoadingMessage("Setting remote description");

      try {
        await pc.setRemoteDescription(new RTCSessionDescription(remoteDescription));
        setLoadingMessage("Establishing secure connection...");
      } catch (error) {
        console.error(
          "[setRemoteSessionDescription] Failed to set remote description:",
          error,
        );
        cleanupAndStopReconnecting();
        return;
      }

      // Replace the interval-based check with a more reliable approach
      let attempts = 0;
      const checkInterval = setInterval(() => {
        attempts++;

        // When vivaldi has disabled "Broadcast IP for Best WebRTC Performance", this never connects
        if (pc.sctp?.state === "connected") {
          clearInterval(checkInterval);
          setLoadingMessage("Connection established");
        } else if (attempts >= 10) {
          console.warn(
            "[setRemoteSessionDescription] Failed to establish connection after 10 attempts",
            {
              connectionState: pc.connectionState,
              iceConnectionState: pc.iceConnectionState,
            },
          );
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

  const wsProtocol = window.location.protocol === "https:" ? "wss:" : "ws:";

  const { sendMessage, getWebSocket } = useWebSocket(
    isOnDevice
      ? `${wsProtocol}//${window.location.host}/webrtc/signaling/client`
      : `${CLOUD_API.replace("http", "ws")}/webrtc/signaling/client?id=${params.id}`,
    {
      heartbeat: true,
      retryOnError: true,
      reconnectAttempts: 15,
      reconnectInterval: 1000,
      onReconnectStop: () => {
        cleanupAndStopReconnecting();
      },

      shouldReconnect(_event) {
        // TODO: Why true?
        return true;
      },

      onClose(_event) {
        // We don't want to close everything down, we wait for the reconnect to stop instead
      },

      onError(event) {
        console.error("[Websocket] onError", event);
        // We don't want to close everything down, we wait for the reconnect to stop instead
      },
      onOpen() {
        // Connection established, message handling will begin
      },

      onMessage: message => {
        if (message.data === "pong") return;

        /*
          Currently the signaling process is as follows:
            After open, the other side will send a `device-metadata` message with the device version
            If the device version is not set, we can assume the device is using the legacy signaling
            Otherwise, we can assume the device is using the new signaling

            If the device is using the legacy signaling, we close the websocket connection
            and use the legacy HTTPSignaling function to get the remote session description

            If the device is using the new signaling, we don't need to do anything special, but continue to use the websocket connection
            to chat with the other peer about the connection
        */

        const parsedMessage = JSON.parse(message.data);
        if (parsedMessage.type === "device-metadata") {
          const { deviceVersion, sessionSettings } = parsedMessage.data;

          // Store session settings if provided
          if (sessionSettings) {
            setGlobalSessionSettings({
              requireNickname: sessionSettings.requireNickname || false,
              requireApproval: sessionSettings.requireApproval || false
            });
            // Also update the settings store for approval handling
            setRequireSessionApproval(sessionSettings.requireApproval || false);
            setRequireSessionNickname(sessionSettings.requireNickname || false);
          }

          // If the device version is not set, we can assume the device is using the legacy signaling
          if (!deviceVersion) {

            // Now we don't need the websocket connection anymore, as we've established that we need to use the legacy signaling
            // which does everything over HTTP(at least from the perspective of the client)
            isLegacySignalingEnabled.current = true;
            getWebSocket()?.close();
          } else {
            isLegacySignalingEnabled.current = false;
          }

          // Always setup peer connection first to establish RPC channel for nickname generation
          setupPeerConnection();

          // Check if nickname is required and not set - modal will be shown after RPC channel is ready
          const requiresNickname = sessionSettings?.requireNickname || false;

          if (requiresNickname && !nickname) {
            // Store that we need to show the nickname modal once RPC is ready
            // The useEffect in NicknameModal will handle waiting for RPC channel readiness
            setShowNicknameModal(true);
            setDisableVideoFocusTrap(true);
          }
        }

        if (!peerConnection) {
          console.warn("[Websocket] Ignoring message because peerConnection is not ready:", parsedMessage.type);
          return;
        }
        if (parsedMessage.type === "answer") {
          const readyForOffer =
            // If we're making an offer, we don't want to accept an answer
            !makingOffer &&
            // If the peer connection is stable or we're setting the remote answer pending, we're ready for an offer
            (peerConnection?.signalingState === "stable" ||
              isSettingRemoteAnswerPending.current);

          // If we're not ready for an offer, we don't want to accept an offer
          ignoreOffer.current = parsedMessage.type === "offer" && !readyForOffer;
          if (ignoreOffer.current) return;

          // Set so we don't accept an answer while we're setting the remote description
          isSettingRemoteAnswerPending.current = parsedMessage.type === "answer";

          const sd = atob(parsedMessage.data);
          const remoteSessionDescription = JSON.parse(sd);

          if (parsedMessage.sessionId && parsedMessage.mode) {
            handleSessionResponse({
              sessionId: parsedMessage.sessionId,
              mode: parsedMessage.mode
            });

            // Store sessionId via zustand (persists to sessionStorage for per-tab isolation)
            setCurrentSession(parsedMessage.sessionId, parsedMessage.mode);
            if (parsedMessage.requireNickname !== undefined && parsedMessage.requireApproval !== undefined) {
              setGlobalSessionSettings({
                requireNickname: parsedMessage.requireNickname,
                requireApproval: parsedMessage.requireApproval
              });
              // Also update the settings store for approval handling
              setRequireSessionApproval(parsedMessage.requireApproval);
              setRequireSessionNickname(parsedMessage.requireNickname);
            }

            // Show nickname modal if:
            // 1. Nickname is required by backend settings
            // 2. We don't already have a nickname
            // This happens even for pending sessions so the nickname is included in approval
            const hasNickname = parsedMessage.nickname && parsedMessage.nickname.length > 0;
            const requiresNickname = parsedMessage.requireNickname || globalSessionSettings?.requireNickname;

            if (requiresNickname && !hasNickname) {
              setShowNicknameModal(true);
              setDisableVideoFocusTrap(true);
            }
          }

          setRemoteSessionDescription(
            peerConnection,
            new RTCSessionDescription(remoteSessionDescription),
          );

          // Reset the remote answer pending flag
          isSettingRemoteAnswerPending.current = false;
        } else if (parsedMessage.type === "new-ice-candidate") {
          const candidate = parsedMessage.data;
          // Always try to add the ICE candidate - the browser will queue it internally if needed
          peerConnection.addIceCandidate(candidate).catch(error => {
            console.warn("[Websocket] Failed to add ICE candidate:", error);
          });
        }
      },
    },

    // Don't even retry once we declare failure
    !connectionFailed && isLegacySignalingEnabled.current === false,
  );

  const sendWebRTCSignal = useCallback(
    (type: string, data: unknown) => {
      // Second argument tells the library not to queue the message, and send it once the connection is established again.
      // We have event handlers that handle the connection set up, so we don't need to queue the message.
      const message = JSON.stringify({ type, data });
      const ws = getWebSocket();
      if (ws?.readyState === WebSocket.OPEN) {
        sendMessage(message, false);
      } else {
        console.warn(`[WebSocket] WebSocket not open, queuing message:`, message);
        sendMessage(message, true); // Queue the message
      }
    },
    [sendMessage, getWebSocket],
  );

  const legacyHTTPSignaling = useCallback(
    async (pc: RTCPeerConnection) => {
      const sd = btoa(JSON.stringify(pc.localDescription));

      // Legacy mode == UI in cloud with updated code connecting to older device version.
      // In device mode, old devices wont server this JS, and on newer devices legacy mode wont be enabled
      const sessionUrl = `${CLOUD_API}/webrtc/session`;

      setLoadingMessage(
        `Getting remote session description...  ${signalingAttempts.current > 0 ? `(attempt ${signalingAttempts.current + 1})` : ""}`,
      );
      const res = await api.POST(sessionUrl, {
        sd,
        userAgent: navigator.userAgent,
        // When on device, we don't need to specify the device id, as it's already known
        ...(isOnDevice ? {} : { id: params.id }),
      });

      const json = await res.json();
      if (res.status === 401) return navigate(isOnDevice ? "/login-local" : "/login");
      if (!res.ok) {
        console.error("Error getting SDP", { status: res.status, json });
        cleanupAndStopReconnecting();
        return;
      }

      setLoadingMessage("Setting remote session description...");

      const decodedSd = atob(json.sd);
      const parsedSd = JSON.parse(decodedSd);
      setRemoteSessionDescription(pc, new RTCSessionDescription(parsedSd));
    },
    [cleanupAndStopReconnecting, navigate, params.id, setRemoteSessionDescription],
  );

  const setupPeerConnection = useCallback(async () => {
    setConnectionFailed(false);
    setLoadingMessage("Connecting to device...");

    let pc: RTCPeerConnection;
    try {
      setLoadingMessage("Creating peer connection...");
      pc = new RTCPeerConnection({
        // We only use STUN or TURN servers if we're in the cloud
        ...(isInCloud && iceConfig?.iceServers
          ? { iceServers: [iceConfig?.iceServers] }
          : {}),
      });

      setPeerConnectionState(pc.connectionState);
      setLoadingMessage("Setting up connection to device...");
    } catch (e) {
      console.error(`[setupPeerConnection] Error creating peer connection: ${e}`);
      setTimeout(() => {
        cleanupAndStopReconnecting();
      }, 1000);
      return;
    }

    // Set up event listeners and data channels
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
          // Get nickname and sessionId from zustand stores
          // sessionId is per-tab (sessionStorage), nickname is shared (localStorage)
          const { currentSessionId: storeSessionId } = useSessionStore.getState();
          const { nickname: storeNickname } = useSharedSessionStore.getState();

          sendWebRTCSignal("offer", {
            sd: sd,
            sessionId: storeSessionId || undefined,
            userAgent: navigator.userAgent,
            sessionSettings: {
              nickname: storeNickname || undefined
            }
          });
        }
      } catch (e) {
        console.error(
          `[setupPeerConnection] Error creating offer: ${e}`,
          new Date().toISOString(),
        );
        cleanupAndStopReconnecting();
      } finally {
        makingOffer.current = false;
      }
    };

    pc.onicecandidate = ({ candidate }) => {
      if (!candidate) {
        return;
      }
      if (candidate.candidate === "") {
        return;
      }
      sendWebRTCSignal("new-ice-candidate", candidate);
    };

    pc.onicegatheringstatechange = event => {
      const pc = event.currentTarget as RTCPeerConnection;
      if (pc.iceGatheringState === "complete") {
        setLoadingMessage("ICE Gathering completed");

        if (isLegacySignalingEnabled.current) {
          // We can now start the https/ws connection to get the remote session description from the KVM device
          legacyHTTPSignaling(pc);
        }
      } else if (pc.iceGatheringState === "gathering") {
        setLoadingMessage("Gathering ICE candidates...");
      }
    };

    pc.ontrack = function (event) {
      setMediaStream(event.streams[0]);
    };

    setTransceiver(pc.addTransceiver("video", { direction: "recvonly" }));

    const rpcDataChannel = pc.createDataChannel("rpc");
    rpcDataChannel.onopen = () => {
      setRpcDataChannel(rpcDataChannel);

      // Fetch global session settings
      const fetchSettings = () => {
        // Only fetch settings if user has permission to read settings
        if (!hasPermission(Permission.SETTINGS_READ)) {
          return;
        }

        const id = Math.random().toString(36).substring(2);
        const message = JSON.stringify({ jsonrpc: "2.0", method: "getSessionSettings", params: {}, id });

        const handler = (event: MessageEvent) => {
          try {
            const response = JSON.parse(event.data);
            if (response.id === id) {
              rpcDataChannel.removeEventListener("message", handler);
              if (response.result) {
                setGlobalSessionSettings(response.result);
                // Also update the settings store for approval handling
                setRequireSessionApproval(response.result.requireApproval);
                setRequireSessionNickname(response.result.requireNickname);
              }
            }
          } catch {
            // Ignore parse errors
          }
        };

        rpcDataChannel.addEventListener("message", handler);
        rpcDataChannel.send(message);

        // Clean up after timeout
        setTimeout(() => {
          rpcDataChannel.removeEventListener("message", handler);
        }, 5000);
      };

      fetchSettings();
    };

    const rpcHidChannel = pc.createDataChannel("hidrpc");
    rpcHidChannel.binaryType = "arraybuffer";
    rpcHidChannel.onopen = () => {
      setRpcHidChannel(rpcHidChannel);
    };

    const rpcHidUnreliableChannel = pc.createDataChannel("hidrpc-unreliable-ordered", {
      ordered: true,
      maxRetransmits: 0,
    });
    rpcHidUnreliableChannel.binaryType = "arraybuffer";
    rpcHidUnreliableChannel.onopen = () => {
      setRpcHidUnreliableChannel(rpcHidUnreliableChannel);
    };

    const rpcHidUnreliableNonOrderedChannel = pc.createDataChannel("hidrpc-unreliable-nonordered", {
      ordered: false,
      maxRetransmits: 0,
    });
    rpcHidUnreliableNonOrderedChannel.binaryType = "arraybuffer";
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
    setTransceiver,
    hasPermission,
    setRequireSessionApproval,
    setRequireSessionNickname,
  ]);

  useEffect(() => {
    if (peerConnectionState === "failed") {
      console.warn("Connection failed, closing peer connection");
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

  // For some reason, we have to have this unmount separate from the cleanup effect above
  useEffect(() => {
    return () => {
      clearInboundRtpStats();
      clearCandidatePairStats();
      setSidebarView(null);
      setPeerConnection(null);
    };
  }, [clearCandidatePairStats, clearInboundRtpStats, setPeerConnection, setSidebarView]);

  // TURN server usage detection
  useEffect(() => {
    if (peerConnectionState !== "connected") return;
    const { localCandidateStats, remoteCandidateStats } = useRTCStore.getState();

    const lastLocalStat = Array.from(localCandidateStats).pop();
    if (!lastLocalStat?.length) return;
    const localCandidateIsUsingTurn = lastLocalStat[1].candidateType === "relay"; // [0] is the timestamp, which we don't care about here

    const lastRemoteStat = Array.from(remoteCandidateStats).pop();
    if (!lastRemoteStat?.length) return;
    const remoteCandidateIsUsingTurn = lastRemoteStat[1].candidateType === "relay"; // [0] is the timestamp, which we don't care about here

    setTurnServerInUse(localCandidateIsUsingTurn || remoteCandidateIsUsingTurn);
  }, [peerConnectionState, setTurnServerInUse]);

  // TURN server usage reporting
  const lastBytesReceived = useRef<number>(0);
  const lastBytesSent = useRef<number>(0);

  useInterval(() => {
    // Don't report usage if we're not using the turn server
    if (!isTurnServerInUse) return;
    const { candidatePairStats } = useRTCStore.getState();

    const lastCandidatePair = Array.from(candidatePairStats).pop();
    const report = lastCandidatePair?.[1];
    if (!report) return;

    let bytesReceivedDelta = 0;
    let bytesSentDelta = 0;

    if (report.bytesReceived) {
      bytesReceivedDelta = report.bytesReceived - lastBytesReceived.current;
      lastBytesReceived.current = report.bytesReceived;
    }

    if (report.bytesSent) {
      bytesSentDelta = report.bytesSent - lastBytesSent.current;
      lastBytesSent.current = report.bytesSent;
    }

    // Fire and forget
    api.POST(`${CLOUD_API}/webrtc/turn_activity`, {
      bytesReceived: bytesReceivedDelta,
      bytesSent: bytesSentDelta,
    });
  }, 10000);

  const { setNetworkState} = useNetworkStateStore();
  const { setHdmiState } = useVideoStore();
  const { 
    keyboardLedState,  setKeyboardLedState,
    keysDownState, setKeysDownState, setUsbState,
  } = useHidStore();
  const setHidRpcDisabled = useRTCStore(state => state.setHidRpcDisabled);

  const [hasUpdated, setHasUpdated] = useState(false);
  const { navigateTo } = useDeviceUiNavigation();

  function onJsonRpcRequest(resp: JsonRpcRequest) {
    // Handle session-related events
    if (resp.method === "sessionsUpdated" ||
        resp.method === "modeChanged" ||
        resp.method === "otherSessionConnected" ||
        resp.method === "primaryControlRequested" ||
        resp.method === "primaryControlApproved" ||
        resp.method === "primaryControlDenied" ||
        resp.method === "newSessionPending" ||
        resp.method === "sessionAccessDenied") {
      handleRpcEvent(resp.method, resp.params);

      // Show access denied overlay if our session was denied
      if (resp.method === "sessionAccessDenied") {
        setAccessDenied(true);
      }

      // Keep legacy behavior for otherSessionConnected
      if (resp.method === "otherSessionConnected") {
        navigateTo("/other-session");
      }
    }

    if (resp.method === "usbState") {
      const usbState = resp.params as unknown as USBStates;
      setUsbState(usbState);
    }

    if (resp.method === "videoInputState") {
      const hdmiState = resp.params as Parameters<VideoState["setHdmiState"]>[0];
      setHdmiState(hdmiState);
    }

    if (resp.method === "networkState") {
      setNetworkState(resp.params as NetworkState);
    }

    if (resp.method === "keyboardLedState") {
      const ledState = resp.params as KeyboardLedState;
      setKeyboardLedState(ledState);
    }

    if (resp.method === "keysDownState") {
      const downState = resp.params as KeysDownState;
      setKeysDownState(downState);
    }

    if (resp.method === "otaState") {
      const otaState = resp.params as OtaState;
      setOtaState(otaState);

      if (otaState.updating === true) {
        setHasUpdated(true);
      }

      if (hasUpdated && otaState.updating === false) {
        setHasUpdated(false);

        if (otaState.error) {
          setModalView("error");
          navigateTo("/settings/general/update");
          return;
        }

        const currentUrl = new URL(window.location.href);
        currentUrl.search = "";
        currentUrl.searchParams.set("updateSuccess", "true");
        window.location.href = currentUrl.toString();
      }
    }
  }

  const { send } = useJsonRpc(onJsonRpcRequest);

  const {
    handleSessionResponse,
    handleRpcEvent,
    primaryControlRequest,
    handleApprovePrimaryRequest,
    handleDenyPrimaryRequest,
    closePrimaryControlRequest,
    newSessionRequest,
    handleApproveNewSession,
    handleDenyNewSession,
    closeNewSessionRequest
  } = useSessionManagement(send);

  useEffect(() => {
    if (rpcDataChannel?.readyState !== "open") return;
    send("getVideoState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const hdmiState = resp.result as Parameters<VideoState["setHdmiState"]>[0];
      setHdmiState(hdmiState);
    });
  }, [rpcDataChannel?.readyState, send, setHdmiState]);

  const [needLedState, setNeedLedState] = useState(true);

  // request keyboard led state from the device
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
  }, [rpcDataChannel?.readyState, send, setKeyboardLedState, keyboardLedState, needLedState]);

  const [needKeyDownState, setNeedKeyDownState] = useState(true);

  // request keyboard key down state from the device
  useEffect(() => {
    if (rpcDataChannel?.readyState !== "open") return;
    if (!needKeyDownState) return;

    send("getKeyDownState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        // -32601 means the method is not supported
        if (resp.error.code === RpcMethodNotFound) {
          // if we don't support key down state, we know key press is also not available
          console.warn("Failed to get key down state, switching to old-school", resp.error);
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
  }, [keysDownState, needKeyDownState, rpcDataChannel?.readyState, send, setKeysDownState, setHidRpcDisabled]);

  // When the update is successful, we need to refresh the client javascript and show a success modal
  useEffect(() => {
    if (queryParams.get("updateSuccess")) {
      navigateTo("/settings/general/update", { state: { updateSuccess: true } });
    }
  }, [navigate, navigateTo, queryParams, setModalView, setQueryParams]);

  // System update
  const [kvmTerminal, setKvmTerminal] = useState<RTCDataChannel | null>(null);
  const [serialConsole, setSerialConsole] = useState<RTCDataChannel | null>(null);

  useEffect(() => {
    if (!peerConnection) return;
    if (!kvmTerminal) {
      setKvmTerminal(peerConnection.createDataChannel("terminal"));
    }

    if (!serialConsole) {
      setSerialConsole(peerConnection.createDataChannel("serial"));
    }
  }, [kvmTerminal, peerConnection, serialConsole]);

  const outlet = useOutlet();
  const onModalClose = useCallback(() => {
    if (location.pathname !== "/other-session") navigateTo("/");
  }, [navigateTo, location.pathname]);

  const { appVersion, getLocalVersion}  = useVersion();

  useEffect(() => {
    if (appVersion) return;
    if (!hasPermission(Permission.VIDEO_VIEW)) return;

    getLocalVersion();
  }, [appVersion, getLocalVersion, hasPermission]);

  const ConnectionStatusElement = useMemo(() => {
    const hasConnectionFailed =
      connectionFailed || ["failed", "closed"].includes(peerConnectionState ?? "");

    const isPeerConnectionLoading =
      ["connecting", "new"].includes(peerConnectionState ?? "") ||
      peerConnection === null;

    const isDisconnected = peerConnectionState === "disconnected";

    const isOtherSession = location.pathname.includes("other-session");

    if (isOtherSession) return null;
    if (peerConnectionState === "connected") return null;
    if (isDisconnected) {
      return <PeerConnectionDisconnectedOverlay show={true} />;
    }

    if (hasConnectionFailed)
      return (
        <ConnectionFailedOverlay show={true} setupPeerConnection={setupPeerConnection} />
      );

    if (isPeerConnectionLoading) {
      return <LoadingConnectionOverlay show={true} text={loadingMessage} />;
    }

    return null;
  }, [
    connectionFailed,
    loadingMessage,
    location.pathname,
    peerConnection,
    peerConnectionState,
    setupPeerConnection,
  ]);

  return (
    <FeatureFlagProvider appVersion={appVersion}>
      {!outlet && otaState.updating && (
        <AnimatePresence>
          <motion.div
            className="pointer-events-none fixed inset-0 top-16 z-10 mx-auto flex h-full w-full max-w-xl translate-y-8 items-start justify-center"
            initial={{ opacity: 0, y: -20 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -20 }}
            transition={{ duration: 0.3, ease: "easeInOut" }}
          >
            <UpdateInProgressStatusCard />
          </motion.div>
        </AnimatePresence>
      )}
      <div className="relative h-full">
        <FocusTrap
          paused={disableVideoFocusTrap}
          focusTrapOptions={{
            allowOutsideClick: true,
            escapeDeactivates: false,
            fallbackFocus: "#videoFocusTrap",
          }}
        >
          <div className="absolute top-0">
            <button className="absolute top-0" tabIndex={-1} id="videoFocusTrap" />
          </div>
        </FocusTrap>

        <div className="grid h-full grid-rows-(--grid-headerBody) select-none">
          <DashboardNavbar
            primaryLinks={isOnDevice ? [] : [{ title: "Cloud Devices", to: "/devices" }]}
            showConnectionStatus={true}
            isLoggedIn={authMode === "password" || !!user}
            userEmail={user?.email}
            picture={user?.picture}
            kvmName={deviceName ?? "JetKVM Device"}
          />


          <div className="relative flex h-full w-full overflow-hidden">
            {/* Only show video feed if nickname is set (when required) and not pending approval */}
            {(!showNicknameModal && currentMode !== "pending") ? (
              <>
                <WebRTCVideo />
                <div
                  style={{ animationDuration: "500ms" }}
                  className="animate-slideUpFade pointer-events-none absolute inset-0 flex items-center justify-center p-4"
                >
                  <div className="relative h-full max-h-[720px] w-full max-w-[1280px] rounded-md">
                    {!!ConnectionStatusElement && ConnectionStatusElement}
                  </div>
                </div>
              </>
            ) : (
              <div className="flex-1 bg-slate-900 flex items-center justify-center">
                <div className="text-slate-400 text-center">
                  {showNicknameModal && <p>Please set your nickname to continue</p>}
                  {currentMode === "pending" && <p>Waiting for session approval...</p>}
                </div>
              </div>
            )}
            <SidebarContainer sidebarView={sidebarView} />
          </div>
        </div>
      </div>

      <div
        className="z-50"
        onClick={e => e.stopPropagation()}
        onMouseUp={e => e.stopPropagation()}
        onMouseDown={e => e.stopPropagation()}
        onKeyUp={e => e.stopPropagation()}
        onKeyDown={e => {
          e.stopPropagation();
          if (e.key === "Escape") navigateTo("/");
        }}
      >
        <Modal open={outlet !== null} onClose={onModalClose}>
          {/* The 'used by other session' modal needs to have access to the connectWebRTC function */}
          <Outlet context={{ setupPeerConnection }} />
        </Modal>

        <NicknameModal
          isOpen={showNicknameModal}
          onSubmit={async (nickname) => {
            setNickname(nickname);
            setShowNicknameModal(false);
            setDisableVideoFocusTrap(false);

            if (currentSessionId && send) {
              try {
                await sessionApi.updateNickname(send, currentSessionId, nickname);
              } catch (error) {
                console.error("Failed to update nickname:", error);
              }
            }
          }}
          onSkip={() => {
            setShowNicknameModal(false);
            setDisableVideoFocusTrap(false);
          }}
        />
      </div>

      {kvmTerminal && (
        <Terminal type="kvm" dataChannel={kvmTerminal} title="KVM Terminal" />
      )}

      {serialConsole && (
        <Terminal type="serial" dataChannel={serialConsole} title="Serial Console" />
      )}

      {/* Unified Session Request Dialog */}
      {(primaryControlRequest || newSessionRequest) && (
        <UnifiedSessionRequestDialog
          request={
            primaryControlRequest
              ? {
                  id: primaryControlRequest.requestId,
                  type: "primary_control",
                  source: primaryControlRequest.source,
                  identity: primaryControlRequest.identity,
                  nickname: primaryControlRequest.nickname,
                }
              : newSessionRequest
              ? {
                  id: newSessionRequest.sessionId,
                  type: "session_approval",
                  source: newSessionRequest.source,
                  identity: newSessionRequest.identity,
                  nickname: newSessionRequest.nickname,
                }
              : null
          }
          onApprove={
            primaryControlRequest
              ? handleApprovePrimaryRequest
              : handleApproveNewSession
          }
          onDeny={
            primaryControlRequest
              ? handleDenyPrimaryRequest
              : handleDenyNewSession
          }
          onDismiss={
            primaryControlRequest
              ? closePrimaryControlRequest
              : closeNewSessionRequest
          }
          onClose={
            primaryControlRequest
              ? closePrimaryControlRequest
              : closeNewSessionRequest
          }
        />
      )}

      <AccessDeniedOverlay
        show={accessDenied}
        message="Your session access was denied by the primary session"
        onRequestApproval={async () => {
          if (!send) return;
          try {
            await sessionApi.requestSessionApproval(send);
            setAccessDenied(false);
          } catch (error) {
            console.error("Failed to re-request approval:", error);
          }
        }}
      />

      <PendingApprovalOverlay
        show={currentMode === "pending"}
      />
    </FeatureFlagProvider>
  );
}

interface SidebarContainerProps {
  readonly sidebarView: string | null;
}

function SidebarContainer(props: SidebarContainerProps) {
  const { sidebarView } = props;
  return (
    <div
      className={cx(
        "flex shrink-0 border-l border-l-slate-800/20 transition-all duration-500 ease-in-out dark:border-l-slate-300/20",
        { "border-x-transparent": !sidebarView },
      )}
      style={{ width: sidebarView ? "493px" : 0 }}
    >
      <div className="relative w-[493px] shrink-0">
        <AnimatePresence>
          {sidebarView === "connection-stats" && (
            <motion.div
              className="absolute inset-0"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{
                duration: 0.5,
                ease: "easeInOut",
              }}
            >
              <ConnectionStatsSidebar />
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </div>
  );
}

KvmIdRoute.loader = loader;
