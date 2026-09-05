import { lazy, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Outlet,
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

import { cx } from "@/cva.config";
import { CLOUD_API } from "@/ui.config";
import api from "@/api";
import { checkAuth, isInCloud, isOnDevice } from "@/main";
import {
  KeyboardLedState,
  KeysDownState,
  NetworkState,
  OtaState,
  PostRebootAction,
  USBStates,
  useHidStore,
  useNetworkStateStore,
  User,
  useRTCStore,
  useUiStore,
  useUpdateStore,
  useVideoStore,
  VideoState,
  useFailsafeModeStore,
  useSettingsStore,
} from "@hooks/stores";
import { JsonRpcRequest, JsonRpcResponse, RpcMethodNotFound, useJsonRpc } from "@hooks/useJsonRpc";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { useVersion } from "@hooks/useVersion";
import WebRTCVideo from "@components/WebRTCVideo";
import DashboardNavbar from "@components/Header";
const ConnectionStatsSidebar = lazy(() => import("@components/sidebar/connectionStats"));
const Terminal = lazy(() => import("@components/Terminal"));
const UpdateInProgressStatusCard = lazy(() => import("@components/UpdateInProgressStatusCard"));
import Modal from "@components/Modal";
import { FailSafeModeOverlay } from "@components/FailSafeModeOverlay";
import {
  ConnectionFailedOverlay,
  LoadingConnectionOverlay,
  PeerConnectionDisconnectedOverlay,
  RebootingOverlay,
} from "@components/VideoOverlay";
import { FeatureFlagProvider } from "@providers/FeatureFlagProvider";
import { m } from "@localizations/messages.js";
import { doRpcHidHandshake, useHidRpc } from "@hooks/useHidRpc";
import useKeyboard from "@hooks/useKeyboard";
import { registerTestHandlers, cleanupTestHooks } from "@/test/testHooks";
import { useSignaling } from "@/webrtc/signaling";

export type AuthMode = "password" | "noPassword" | null;

interface LocalLoaderResp {
  authMode: AuthMode;
}

interface CloudLoaderResp {
  deviceName: string;
  user: User | null;
  iceConfig: {
    iceServers: { credential?: string; urls: string | string[]; username?: string };
  } | null;
}

export interface LocalDevice {
  authMode: AuthMode;
  deviceId: string;
}

const deviceLoader = async () => {
  const device = await checkAuth();
  return { authMode: device.authMode } as LocalLoaderResp;
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

  return { user, iceConfig, deviceName: device.name || device.id } as CloudLoaderResp;
};

const loader: LoaderFunction = ({ params }: LoaderFunctionArgs) => {
  return isOnDevice ? deviceLoader() : cloudLoader(params);
};

export default function KvmIdRoute() {
  const loaderResp = useLoaderData();
  // Depending on the mode, we set the appropriate variables
  const user = "user" in loaderResp ? loaderResp.user : null;
  const deviceName = "deviceName" in loaderResp ? loaderResp.deviceName : null;
  const iceConfig = "iceConfig" in loaderResp ? loaderResp.iceConfig : null;
  const authMode = "authMode" in loaderResp ? loaderResp.authMode : null;

  const params = useParams() as { id: string };
  const {
    sidebarView,
    setSidebarView,
    disableVideoFocusTrap,
    setDisableVideoFocusTrap,
    rebootState,
    setRebootState,
    isEmbedMode,
    setEmbedMode,
  } = useUiStore();
  const [queryParams, setQueryParams] = useSearchParams();
  const hasEmbedParam = queryParams.has("embed");

  // Latch embed mode once from ?embed query param — persists across in-app
  // navigation even if the query param is lost (e.g. opening settings)
  useEffect(() => {
    if (hasEmbedParam && !isEmbedMode) {
      setEmbedMode(true);
    }
  }, [hasEmbedParam, isEmbedMode, setEmbedMode]);

  const settingsHideHeaderBar = useSettingsStore(state => state.hideHeaderBar);
  const settingsHideStatusBar = useSettingsStore(state => state.hideStatusBar);
  const hideHeaderBar = isEmbedMode || settingsHideHeaderBar;
  const hideStatusBar = isEmbedMode || settingsHideStatusBar;

  const {
    peerConnection,
    setPeerConnection,
    peerConnectionState,
    setMediaStream,
    bumpMediaStreamTrackVersion,
    setRpcDataChannel,
    isTurnServerInUse,
    setTurnServerInUse,
    rpcDataChannel,
    setTransceiver,
    setRpcHidChannel,
    setRpcHidUnreliableNonOrderedChannel,
    setRpcHidUnreliableChannel,
    setRpcHidProtocolVersion,
    terminalChannel,
    setTerminalChannel,
  } = useRTCStore();

  const location = useLocation();
  const [displayHostname, setDisplayHostname] = useState<string | null>(null);

  const navigate = useNavigate();
  const { otaState, setOtaState, setModalView } = useUpdateStore();

  // Everything that must be part of the initial offer is attached here; the
  // signaling adapter owns the peer connection itself.
  const attachPeerConnection = useCallback(
    (pc: RTCPeerConnection) => {
      pc.ontrack = function (event) {
        // Assemble a single canonical MediaStream from every incoming track.
        // We don't trust event.streams[0]: when the answer SDP omits a=msid
        // for a track (pion does this for audio in some configurations),
        // Firefox hands us a fresh synthetic stream that would overwrite the
        // canonical one and strip tracks already attached to it.
        let stream = useRTCStore.getState().mediaStream;
        if (!stream) {
          stream = new MediaStream();
          setMediaStream(stream);
        }
        if (!stream.getTracks().some(t => t.id === event.track.id)) {
          stream.addTrack(event.track);
          bumpMediaStreamTrackVersion();
        }
      };

      setTransceiver(pc.addTransceiver("video", { direction: "recvonly" }));
      // Always offer audio; the backend gates it on device config and leaves
      // the m-line inactive when disabled.
      pc.addTransceiver("audio", { direction: "recvonly" });

      const rpcDataChannel = pc.createDataChannel("rpc");
      rpcDataChannel.onclose = () => {
        console.log("rpcDataChannel has closed");
        setRpcDataChannel(null);
      };
      rpcDataChannel.onerror = (ev: Event) =>
        console.error(`Error on DataChannel '${rpcDataChannel.label}': ${ev.type}`);
      rpcDataChannel.onopen = () => {
        setRpcDataChannel(rpcDataChannel);
      };

      const rpcHidChannel = pc.createDataChannel("hidrpc");
      rpcHidChannel.binaryType = "arraybuffer";
      rpcHidChannel.onclose = () => console.log("rpcHidChannel has closed");
      rpcHidChannel.onerror = (ev: Event) =>
        console.error(`Error on rpcHidChannel '${rpcHidChannel.label}': ${ev.type}`);
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
        console.error(
          `Error on rpcHidUnreliableChannel '${rpcHidUnreliableChannel.label}': ${ev.type}`,
        );
      rpcHidUnreliableChannel.onopen = () => {
        setRpcHidUnreliableChannel(rpcHidUnreliableChannel);
      };

      const rpcHidUnreliableNonOrderedChannel = pc.createDataChannel(
        "hidrpc-unreliable-nonordered",
        {
          ordered: false,
          maxRetransmits: 0,
        },
      );
      rpcHidUnreliableNonOrderedChannel.binaryType = "arraybuffer";
      rpcHidUnreliableNonOrderedChannel.onclose = () =>
        console.log("rpcHidUnreliableNonOrderedChannel has closed");
      rpcHidUnreliableNonOrderedChannel.onerror = (ev: Event) =>
        console.error(
          `Error on rpcHidUnreliableNonOrderedChannel '${rpcHidUnreliableNonOrderedChannel.label}': ${ev.type}`,
        );
      rpcHidUnreliableNonOrderedChannel.onopen = () => {
        setRpcHidUnreliableNonOrderedChannel(rpcHidUnreliableNonOrderedChannel);
      };

      // Create terminal channel as part of initial offer
      const terminalDataChannel = pc.createDataChannel("terminal");
      terminalDataChannel.onclose = () => console.log("terminalDataChannel has closed");
      terminalDataChannel.onerror = (ev: Event) =>
        console.error(`Error on terminalDataChannel '${terminalDataChannel.label}': ${ev.type}`);
      terminalDataChannel.onopen = () => {
        setTerminalChannel(terminalDataChannel);
      };
    },
    [
      bumpMediaStreamTrackVersion,
      setMediaStream,
      setRpcDataChannel,
      setRpcHidChannel,
      setRpcHidUnreliableNonOrderedChannel,
      setRpcHidUnreliableChannel,
      setRpcHidProtocolVersion,
      setTerminalChannel,
      setTransceiver,
    ],
  );

  // We only use STUN or TURN servers if we're in the cloud
  const iceServers = useMemo(
    () => (isInCloud && iceConfig?.iceServers ? [iceConfig.iceServers] : undefined),
    [iceConfig?.iceServers],
  );

  const {
    connect: setupPeerConnection,
    loadingMessage,
    connectionFailed,
  } = useSignaling({
    deviceId: params.id,
    iceServers,
    onPeerConnection: attachPeerConnection,
  });

  // Cleanup effect
  const { clearInboundRtpStats, clearCandidatePairStats } = useRTCStore();

  // For some reason, we have to have this unmount separate from the cleanup effect above
  useEffect(() => {
    return () => {
      clearInboundRtpStats();
      clearCandidatePairStats();
      setSidebarView(null);
      setPeerConnection(null);
      setRpcDataChannel(null);
      setRpcHidChannel(null);
      setRpcHidUnreliableChannel(null);
      setRpcHidUnreliableNonOrderedChannel(null);
      setRpcHidProtocolVersion(null);
      setTerminalChannel(null);
    };
  }, [
    clearCandidatePairStats,
    clearInboundRtpStats,
    setPeerConnection,
    setSidebarView,
    setRpcDataChannel,
    setRpcHidChannel,
    setRpcHidUnreliableChannel,
    setRpcHidUnreliableNonOrderedChannel,
    setRpcHidProtocolVersion,
    setTerminalChannel,
  ]);

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
    api
      .POST(`${CLOUD_API}/webrtc/turn_activity`, {
        bytesReceived: bytesReceivedDelta,
        bytesSent: bytesSentDelta,
      })
      .catch(() => {
        // we don't care about errors here, but we don't want unhandled promise rejections
      });
  }, 10000);

  const { setNetworkState } = useNetworkStateStore();
  const { setHdmiState } = useVideoStore();
  const { keyboardLedState, setKeyboardLedState, keysDownState, setKeysDownState, setUsbState } =
    useHidStore();
  const setHidRpcDisabled = useRTCStore(state => state.setHidRpcDisabled);
  const { setFailsafeMode } = useFailsafeModeStore();

  // Keyboard handler for E2E tests
  const { handleKeyPress, pauseKeepAlive } = useKeyboard();

  // Mouse handler for E2E tests
  const { reportAbsMouseEvent, rpcHidReady } = useHidRpc();

  const [hasUpdated, setHasUpdated] = useState(false);
  const { navigateTo } = useDeviceUiNavigation();

  function onJsonRpcRequest(resp: JsonRpcRequest) {
    if (resp.method === "otherSessionConnected") {
      navigateTo("/other-session");
    }

    if (resp.method === "usbState") {
      const usbState = resp.params as unknown as USBStates;
      console.debug("Setting USB state", usbState);
      setUsbState(usbState);
    }

    if (resp.method === "videoInputState") {
      const hdmiState = resp.params as Parameters<VideoState["setHdmiState"]>[0];
      console.debug("Setting HDMI state", hdmiState);
      setHdmiState(hdmiState);
    }

    if (resp.method === "networkState") {
      console.debug("Setting network state", resp.params);
      setNetworkState(resp.params as NetworkState);
    }

    if (resp.method === "keyboardLedState") {
      const ledState = resp.params as KeyboardLedState;
      console.debug("Setting keyboard led state", ledState);
      setKeyboardLedState(ledState);
    }

    if (resp.method === "keysDownState") {
      const downState = resp.params as KeysDownState;
      console.debug("Setting key down state:", downState);
      setKeysDownState(downState);
    }

    if (resp.method === "otaState") {
      const otaState = resp.params as OtaState;
      console.debug("Setting OTA state", otaState);
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

        // This is to prevent the otaState from handling page refreshes after an update
        // We've recently implemented a new general rebooting flow, so we don't need to handle this specific ota-rebooting case
        // However, with old devices, we wont get the `willReboot` message, so we need to keep this for backwards compatibility
        // only for the cloud version with an old device
        if (rebootState?.isRebooting) return;

        const currentUrl = new URL(window.location.href);
        currentUrl.search = "";
        currentUrl.searchParams.set("updateSuccess", "true");
        window.location.href = currentUrl.toString();
      }
    }

    if (resp.method === "willReboot") {
      const action = resp.params as PostRebootAction | undefined;
      setRebootState({
        isRebooting: true,
        postRebootAction: {
          healthCheck: action?.healthCheck || "/device/status",
          redirectTo: action?.redirectTo || "/",
        },
      });
      navigateTo("/");
    }

    if (resp.method === "failsafeMode") {
      const { active, reason } = resp.params as { active: boolean; reason: string };
      console.debug("Setting failsafe mode", { active, reason });
      setFailsafeMode(active, reason);
    }
  }

  const { send } = useJsonRpc(onJsonRpcRequest);

  // Mouse movement handler for E2E tests (needs send from useJsonRpc)
  const handleAbsMouseMove = useCallback(
    (x: number, y: number, buttons: number) => {
      if (rpcHidReady) {
        reportAbsMouseEvent(x, y, buttons);
      } else {
        send("absMouseReport", { x, y, buttons });
      }
    },
    [reportAbsMouseEvent, rpcHidReady, send],
  );

  useEffect(() => {
    if (rpcDataChannel?.readyState !== "open") return;
    console.log("Requesting video state");
    send("getVideoState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const hdmiState = resp.result as Parameters<VideoState["setHdmiState"]>[0];
      console.debug("Setting HDMI state", hdmiState);
      setHdmiState(hdmiState);
    });

    console.log("Requesting network settings");
    send("getNetworkSettings", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const result = resp.result as { hostname?: string };
      if (result.hostname) {
        setDisplayHostname(result.hostname);
      } else {
        setDisplayHostname(null);
      }
    });
  }, [rpcDataChannel?.readyState, send, setHdmiState]);

  const [needLedState, setNeedLedState] = useState(true);

  // request keyboard led state from the device
  useEffect(() => {
    if (rpcDataChannel?.readyState !== "open") return;
    if (!needLedState) return;
    console.log("Requesting keyboard led state");

    send("getKeyboardLedState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        console.error("Failed to get keyboard led state", resp.error);
        return;
      } else {
        const ledState = resp.result as KeyboardLedState;
        console.debug("Keyboard led state: ", ledState);
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
    console.log("Requesting keys down state");

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
        console.debug("Keyboard key down state", downState);
        setKeysDownState(downState);
      }
      setNeedKeyDownState(false);
    });
  }, [
    keysDownState,
    needKeyDownState,
    rpcDataChannel?.readyState,
    send,
    setKeysDownState,
    setHidRpcDisabled,
  ]);

  // When the update is successful, we need to refresh the client javascript and show a success modal
  useEffect(() => {
    if (queryParams.get("updateSuccess")) {
      navigateTo("/settings/general/update", { state: { updateSuccess: true } });
    }
  }, [navigate, navigateTo, queryParams, setModalView, setQueryParams]);

  // Serial console - still created via useEffect for now
  const [serialConsole, setSerialConsole] = useState<RTCDataChannel | null>(null);

  // One channel per peer connection: a channel from a previous peer is dead
  // after a reconnect, so it is closed and replaced rather than kept.
  useEffect(() => {
    const channel = peerConnection ? peerConnection.createDataChannel("serial") : null;
    setSerialConsole(channel);
    return () => channel?.close();
  }, [peerConnection]);

  // CDC-ACM console data channel
  const [cdcACMConsole, setCdcACMConsole] = useState<RTCDataChannel | null>(null);

  useEffect(() => {
    const channel = peerConnection ? peerConnection.createDataChannel("cdcacm") : null;
    setCdcACMConsole(channel);
    return () => channel?.close();
  }, [peerConnection]);

  // Register E2E test hooks
  useEffect(() => {
    registerTestHandlers({
      handleKeyPress,
      pauseKeepAlive,
      handleAbsMouseMove,
      getKeyboardLedState: () => useHidStore.getState().keyboardLedState,
      getKeysDownState: () => useHidStore.getState().keysDownState,
      getPeerConnectionState: () => useRTCStore.getState().peerConnectionState,
      getRpcHidProtocolVersion: () => useRTCStore.getState().rpcHidProtocolVersion,
      getMediaStream: () => useRTCStore.getState().mediaStream,
      getHdmiState: () => useVideoStore.getState().hdmiState,
      getVideoElement: () => useVideoStore.getState().videoElement,
      getKvmTerminal: () => useRTCStore.getState().terminalChannel,
      getRpcDataChannel: () => useRTCStore.getState().rpcDataChannel,
      getPeerConnection: () => useRTCStore.getState().peerConnection,
    });
    return cleanupTestHooks;
  }, [handleKeyPress, pauseKeepAlive, handleAbsMouseMove]);

  const outlet = useOutlet();
  const onModalClose = useCallback(() => {
    if (location.pathname !== "/other-session") navigateTo("/");

    // Re-disable the focus trap if a terminal is still active, otherwise
    // FocusTrap reclaims focus and the terminal becomes unresponsive.
    const { terminalType } = useUiStore.getState();
    if (terminalType !== "none") {
      setDisableVideoFocusTrap(true);
    }
  }, [navigateTo, location.pathname, setDisableVideoFocusTrap]);

  const { appVersion, getLocalVersion } = useVersion();

  useEffect(() => {
    if (appVersion) return;

    getLocalVersion();
  }, [appVersion, getLocalVersion]);

  const { isFailsafeMode, reason: failsafeReason } = useFailsafeModeStore();

  const ConnectionStatusElement = useMemo(() => {
    const isOtherSession = location.pathname.includes("other-session");
    if (isOtherSession) return null;

    // Rebooting takes priority over connection status
    if (rebootState?.isRebooting) {
      return (
        <RebootingOverlay
          show={true}
          postRebootAction={rebootState.postRebootAction}
          deviceId={params.id}
        />
      );
    }

    if (isFailsafeMode && failsafeReason) {
      return <FailSafeModeOverlay reason={failsafeReason} />;
    }

    const hasConnectionFailed =
      connectionFailed || ["failed", "closed"].includes(peerConnectionState ?? "");

    const isPeerConnectionLoading =
      ["connecting", "new"].includes(peerConnectionState ?? "") || peerConnection === null;

    const isDisconnected = peerConnectionState === "disconnected";

    if (peerConnectionState === "connected") return null;
    if (isDisconnected) {
      return <PeerConnectionDisconnectedOverlay show={true} />;
    }

    if (hasConnectionFailed)
      return <ConnectionFailedOverlay show={true} setupPeerConnection={setupPeerConnection} />;

    if (isPeerConnectionLoading) {
      return <LoadingConnectionOverlay show={true} text={loadingMessage} />;
    }

    return null;
  }, [
    location.pathname,
    rebootState?.isRebooting,
    rebootState?.postRebootAction,
    params.id,
    isFailsafeMode,
    failsafeReason,
    connectionFailed,
    peerConnectionState,
    peerConnection,
    setupPeerConnection,
    loadingMessage,
  ]);

  return (
    <FeatureFlagProvider appVersion={appVersion}>
      <title>{displayHostname ? `${displayHostname} - JetKVM` : "JetKVM"}</title>
      {!isEmbedMode && !outlet && otaState.updating && (
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
        {!hideHeaderBar && (
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
        )}

        <div
          className={cx("grid h-full select-none", {
            "grid-rows-(--grid-headerBody)": !hideHeaderBar,
          })}
        >
          {!hideHeaderBar && (
            <DashboardNavbar
              primaryLinks={isOnDevice ? [] : [{ title: "Cloud Devices", to: "/devices" }]}
              showConnectionStatus={true}
              isLoggedIn={authMode === "password" || !!user}
              userEmail={user?.email}
              picture={user?.picture}
              kvmName={deviceName ?? m.jetkvm_device()}
              hostname={displayHostname}
            />
          )}

          <div className="relative flex h-full w-full overflow-hidden">
            {isFailsafeMode && failsafeReason === "native" ? null : (
              <WebRTCVideo
                hasConnectionIssues={!!ConnectionStatusElement}
                hideStatusBar={hideStatusBar}
              />
            )}
            <div
              style={{ animationDuration: "500ms" }}
              className="pointer-events-none absolute inset-0 flex animate-slideUpFade items-center justify-center p-4"
            >
              <div className="relative h-full max-h-[720px] w-full max-w-7xl rounded-md">
                {isFailsafeMode && failsafeReason ? (
                  <FailSafeModeOverlay reason={failsafeReason} />
                ) : (
                  !!ConnectionStatusElement && ConnectionStatusElement
                )}
              </div>
            </div>
            <SidebarContainer sidebarView={sidebarView} />
          </div>
        </div>
      </div>

      <div
        className="z-50"
        role="form"
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
      </div>

      {terminalChannel && (
        <Terminal type="kvm" dataChannel={terminalChannel} title={m.kvm_terminal()} />
      )}

      {serialConsole && (
        <Terminal type="serial" dataChannel={serialConsole} title={m.serial_console()} />
      )}

      {cdcACMConsole && (
        <Terminal type="cdcacm" dataChannel={cdcACMConsole} title="USB Serial Console" />
      )}
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
