import { useCallback, useEffect, useRef, useState } from "react";
import {
  LoaderFunctionArgs,
  Outlet,
  Params,
  redirect,
  useLoaderData,
  useLocation,
  useNavigate,
  useOutlet,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useInterval } from "usehooks-ts";
import FocusTrap from "focus-trap-react";
import { motion, AnimatePresence } from "framer-motion";

import { cx } from "@/cva.config";
import {
  DeviceSettingsState,
  HidState,
  UpdateState,
  useDeviceSettingsStore,
  useDeviceStore,
  useHidStore,
  useMountMediaStore,
  User,
  useRTCStore,
  useUiStore,
  useUpdateStore,
  useVideoStore,
  VideoState,
} from "@/hooks/stores";
import WebRTCVideo from "@components/WebRTCVideo";
import { checkAuth, isInCloud, isOnDevice } from "@/main";
import DashboardNavbar from "@components/Header";
import ConnectionStatsSidebar from "@/components/sidebar/connectionStats";
import { JsonRpcRequest, useJsonRpc } from "@/hooks/useJsonRpc";
import Terminal from "@components/Terminal";
import { CLOUD_API, DEVICE_API } from "@/ui.config";

import UpdateInProgressStatusCard from "../components/UpdateInProgressStatusCard";
import api from "../api";
import Modal from "../components/Modal";
import { useDeviceUiNavigation } from "../hooks/useAppNavigation";
import { FeatureFlagProvider } from "../providers/FeatureFlagProvider";
import notifications from "../notifications";
import {
  ConnectionErrorOverlay,
  LoadingConnectionOverlay,
} from "../components/VideoOverlay";

import { SystemVersionInfo } from "./devices.$id.settings.general.update";
import { DeviceStatus } from "./welcome-local";

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

const loader = async ({ params }: LoaderFunctionArgs) => {
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
  const sidebarView = useUiStore(state => state.sidebarView);
  const [queryParams, setQueryParams] = useSearchParams();

  const setIsTurnServerInUse = useRTCStore(state => state.setTurnServerInUse);
  const peerConnection = useRTCStore(state => state.peerConnection);
  const setMediaMediaStream = useRTCStore(state => state.setMediaStream);
  const setPeerConnection = useRTCStore(state => state.setPeerConnection);
  const setDiskChannel = useRTCStore(state => state.setDiskChannel);
  const setRpcDataChannel = useRTCStore(state => state.setRpcDataChannel);
  const setTransceiver = useRTCStore(state => state.setTransceiver);
  const location = useLocation();

  const [connectionFailed, setConnectionFailed] = useState(false);

  const navigate = useNavigate();
  const { otaState, setOtaState, setModalView } = useUpdateStore();

  const [loadingMessage, setLoadingMessage] = useState("Connecting to device...");
  const closePeerConnection = useCallback(
    function closePeerConnection() {
      console.log("Closing peer connection");

      setConnectionFailed(true);
      connectionFailedRef.current = true;

      peerConnection?.close();
      signalingAttempts.current = 0;
    },
    [peerConnection],
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
  const syncRemoteSessionDescription = useCallback(
    async function syncRemoteSessionDescription(pc: RTCPeerConnection) {
      try {
        if (!pc) return;

        const sd = btoa(JSON.stringify(pc.localDescription));

        const sessionUrl = isOnDevice
          ? `${DEVICE_API}/webrtc/session`
          : `${CLOUD_API}/webrtc/session`;

        console.log("Trying to get remote session description");
        setLoadingMessage(
          `Getting remote session description...  ${signalingAttempts.current > 0 ? `(attempt ${signalingAttempts.current + 1})` : ""}`,
        );
        const res = await api.POST(sessionUrl, {
          sd,
          // When on device, we don't need to specify the device id, as it's already known
          ...(isOnDevice ? {} : { id: params.id }),
        });

        const json = await res.json();
        if (res.status === 401) return navigate(isOnDevice ? "/login-local" : "/login");
        if (!res.ok) {
          console.error("Error getting SDP", { status: res.status, json });
          throw new Error("Error getting SDP");
        }

        console.log("Successfully got Remote Session Description. Setting.");
        setLoadingMessage("Setting remote session description...");

        const decodedSd = atob(json.sd);
        const parsedSd = JSON.parse(decodedSd);
        pc.setRemoteDescription(new RTCSessionDescription(parsedSd));

        await new Promise((resolve, reject) => {
          console.log("Waiting for remote description to be set");
          const maxAttempts = 10;
          const interval = 1000;
          let attempts = 0;

          const checkInterval = setInterval(() => {
            attempts++;
            // When vivaldi has disabled "Broadcast IP for Best WebRTC Performance", this never connects
            if (pc.sctp?.state === "connected") {
              console.log("Remote description set");
              clearInterval(checkInterval);
              resolve(true);
            } else if (attempts >= maxAttempts) {
              console.log(
                `Failed to get remote description after ${maxAttempts} attempts`,
              );
              closePeerConnection();
              clearInterval(checkInterval);
              reject(
                new Error(
                  `Failed to get remote description after ${maxAttempts} attempts`,
                ),
              );
            } else {
              console.log("Waiting for remote description to be set");
            }
          }, interval);
        });
      } catch (error) {
        console.error("Error getting SDP", { error });
        console.log("Connection failed", connectionFailedRef.current);
        if (connectionFailedRef.current) return;
        if (signalingAttempts.current < 5) {
          signalingAttempts.current++;
          await new Promise(resolve => setTimeout(resolve, 500));
          console.log("Attempting to get SDP again", signalingAttempts.current);
          syncRemoteSessionDescription(pc);
        } else {
          closePeerConnection();
        }
      }
    },
    [closePeerConnection, navigate, params.id],
  );

  const setupPeerConnection = useCallback(async () => {
    console.log("Setting up peer connection");
    setConnectionFailed(false);
    setLoadingMessage("Connecting to device...");

    let pc: RTCPeerConnection;
    try {
      console.log("Creating peer connection");
      setLoadingMessage("Creating peer connection...");
      pc = new RTCPeerConnection({
        // We only use STUN or TURN servers if we're in the cloud
        ...(isInCloud && iceConfig?.iceServers
          ? { iceServers: [iceConfig?.iceServers] }
          : {}),
      });
      console.log("Peer connection created", pc);
      setLoadingMessage("Peer connection created");
    } catch (e) {
      console.error(`Error creating peer connection: ${e}`);
      setTimeout(() => {
        closePeerConnection();
      }, 1000);
      return;
    }

    // Set up event listeners and data channels
    pc.onconnectionstatechange = () => {
      console.log("Connection state changed", pc.connectionState);
    };

    pc.onicegatheringstatechange = event => {
      const pc = event.currentTarget as RTCPeerConnection;
      console.log("ICE Gathering State Changed", pc.iceGatheringState);
      if (pc.iceGatheringState === "complete") {
        console.log("ICE Gathering completed");
        setLoadingMessage("ICE Gathering completed");

        // We can now start the https/ws connection to get the remote session description from the KVM device
        syncRemoteSessionDescription(pc);
      } else if (pc.iceGatheringState === "gathering") {
        console.log("ICE Gathering Started");
        setLoadingMessage("Gathering ICE candidates...");
      }
    };

    pc.ontrack = function (event) {
      setMediaMediaStream(event.streams[0]);
    };

    setTransceiver(pc.addTransceiver("video", { direction: "recvonly" }));

    const rpcDataChannel = pc.createDataChannel("rpc");
    rpcDataChannel.onopen = () => {
      setRpcDataChannel(rpcDataChannel);
    };

    const diskDataChannel = pc.createDataChannel("disk");
    diskDataChannel.onopen = () => {
      setDiskChannel(diskDataChannel);
    };

    setPeerConnection(pc);

    try {
      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
    } catch (e) {
      console.error(`Error creating offer: ${e}`, new Date().toISOString());
      closePeerConnection();
    }
  }, [
    closePeerConnection,
    iceConfig?.iceServers,
    setDiskChannel,
    setMediaMediaStream,
    setPeerConnection,
    setRpcDataChannel,
    setTransceiver,
    syncRemoteSessionDescription,
  ]);

  // On boot, if the connection state is undefined, we connect to the WebRTC
  useEffect(() => {
    if (peerConnection?.connectionState === undefined) {
      setupPeerConnection();
    }
  }, [setupPeerConnection, peerConnection?.connectionState]);

  // Cleanup effect
  const clearInboundRtpStats = useRTCStore(state => state.clearInboundRtpStats);
  const clearCandidatePairStats = useRTCStore(state => state.clearCandidatePairStats);
  const setSidebarView = useUiStore(state => state.setSidebarView);

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
    if (peerConnection?.connectionState !== "connected") return;
    const { localCandidateStats, remoteCandidateStats } = useRTCStore.getState();

    const lastLocalStat = Array.from(localCandidateStats).pop();
    if (!lastLocalStat?.length) return;
    const localCandidateIsUsingTurn = lastLocalStat[1].candidateType === "relay"; // [0] is the timestamp, which we don't care about here

    const lastRemoteStat = Array.from(remoteCandidateStats).pop();
    if (!lastRemoteStat?.length) return;
    const remoteCandidateIsUsingTurn = lastRemoteStat[1].candidateType === "relay"; // [0] is the timestamp, which we don't care about here

    setIsTurnServerInUse(localCandidateIsUsingTurn || remoteCandidateIsUsingTurn);
  }, [peerConnection?.connectionState, setIsTurnServerInUse]);

  // TURN server usage reporting
  const isTurnServerInUse = useRTCStore(state => state.isTurnServerInUse);
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

  const setUsbState = useHidStore(state => state.setUsbState);
  const setHdmiState = useVideoStore(state => state.setHdmiState);

  const [hasUpdated, setHasUpdated] = useState(false);
  const { navigateTo } = useDeviceUiNavigation();

  function onJsonRpcRequest(resp: JsonRpcRequest) {
    if (resp.method === "otherSessionConnected") {
      navigateTo("/other-session");
    }

    if (resp.method === "usbState") {
      setUsbState(resp.params as unknown as HidState["usbState"]);
    }

    if (resp.method === "videoInputState") {
      setHdmiState(resp.params as Parameters<VideoState["setHdmiState"]>[0]);
    }

    if (resp.method === "otaState") {
      const otaState = resp.params as UpdateState["otaState"];
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

  const rpcDataChannel = useRTCStore(state => state.rpcDataChannel);
  const [send] = useJsonRpc(onJsonRpcRequest);

  useEffect(() => {
    if (rpcDataChannel?.readyState !== "open") return;
    send("getVideoState", {}, resp => {
      if ("error" in resp) return;
      setHdmiState(resp.result as Parameters<VideoState["setHdmiState"]>[0]);
    });
  }, [rpcDataChannel?.readyState, send, setHdmiState]);

  // eslint-disable-next-line @typescript-eslint/ban-ts-comment
  // @ts-expect-error
  window.send = send;

  // When the update is successful, we need to refresh the client javascript and show a success modal
  useEffect(() => {
    if (queryParams.get("updateSuccess")) {
      navigateTo("/settings/general/update", { state: { updateSuccess: true } });
    }
  }, [navigate, navigateTo, queryParams, setModalView, setQueryParams]);

  const diskChannel = useRTCStore(state => state.diskChannel)!;
  const file = useMountMediaStore(state => state.localFile)!;
  useEffect(() => {
    if (!diskChannel || !file) return;
    diskChannel.onmessage = async e => {
      console.log("Received", e.data);
      const data = JSON.parse(e.data);
      const blob = file.slice(data.start, data.end);
      const buf = await blob.arrayBuffer();
      const header = new ArrayBuffer(16);
      const headerView = new DataView(header);
      headerView.setBigUint64(0, BigInt(data.start), false); // start offset, big-endian
      headerView.setBigUint64(8, BigInt(buf.byteLength), false); // length, big-endian
      const fullData = new Uint8Array(header.byteLength + buf.byteLength);
      fullData.set(new Uint8Array(header), 0);
      fullData.set(new Uint8Array(buf), header.byteLength);
      diskChannel.send(fullData);
    };
  }, [diskChannel, file]);

  // System update
  const disableKeyboardFocusTrap = useUiStore(state => state.disableVideoFocusTrap);

  const [kvmTerminal, setKvmTerminal] = useState<RTCDataChannel | null>(null);
  const [serialConsole, setSerialConsole] = useState<RTCDataChannel | null>(null);

  useEffect(() => {
    if (!peerConnection) return;
    if (!kvmTerminal) {
      console.log('Creating data channel "terminal"');
      setKvmTerminal(peerConnection.createDataChannel("terminal"));
    }

    if (!serialConsole) {
      console.log('Creating data channel "serial"');
      setSerialConsole(peerConnection.createDataChannel("serial"));
    }
  }, [kvmTerminal, peerConnection, serialConsole]);

  const outlet = useOutlet();
  const onModalClose = useCallback(() => {
    if (location.pathname !== "/other-session") navigateTo("/");
  }, [navigateTo, location.pathname]);

  const appVersion = useDeviceStore(state => state.appVersion);
  const setAppVersion = useDeviceStore(state => state.setAppVersion);
  const setSystemVersion = useDeviceStore(state => state.setSystemVersion);

  useEffect(() => {
    if (appVersion) return;

    send("getUpdateStatus", {}, async resp => {
      if ("error" in resp) {
        notifications.error("Failed to get device version");
      } else {
        const result = resp.result as SystemVersionInfo;
        setAppVersion(result.local.appVersion);
        setSystemVersion(result.local.systemVersion);
      }
    });
  }, [appVersion, send, setAppVersion, setSystemVersion]);

  const setScrollSensitivity = useDeviceSettingsStore(
    state => state.setScrollSensitivity,
  );

  // Initialize device settings
  useEffect(
    function initializeDeviceSettings() {
      send("getScrollSensitivity", {}, resp => {
        if ("error" in resp) return;
        setScrollSensitivity(resp.result as DeviceSettingsState["scrollSensitivity"]);
      });
    },
    [send, setScrollSensitivity],
  );

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
          paused={disableKeyboardFocusTrap}
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
        <div className="grid h-full select-none grid-rows-headerBody">
          <DashboardNavbar
            primaryLinks={isOnDevice ? [] : [{ title: "Cloud Devices", to: "/devices" }]}
            showConnectionStatus={true}
            isLoggedIn={authMode === "password" || !!user}
            userEmail={user?.email}
            picture={user?.picture}
            kvmName={deviceName || "JetKVM Device"}
          />

          <div className="flex h-full w-full overflow-hidden">
            <div className="pointer-events-none fixed inset-0 isolate z-50 flex h-full w-full items-center justify-center">
              <div className="my-2 h-full max-h-[720px] w-full max-w-[1280px] rounded-md">
                <LoadingConnectionOverlay
                  show={
                    !connectionFailed &&
                    (["connecting", "new"].includes(
                      peerConnection?.connectionState || "",
                    ) ||
                      peerConnection === null) &&
                    !location.pathname.includes("other-session")
                  }
                  text={loadingMessage}
                />
                <ConnectionErrorOverlay
                  show={connectionFailed && !location.pathname.includes("other-session")}
                  setupPeerConnection={setupPeerConnection}
                />
              </div>
            </div>

            <WebRTCVideo />
            <SidebarContainer sidebarView={sidebarView} />
          </div>
        </div>
      </div>

      <div
        className="isolate"
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

      {kvmTerminal && (
        <Terminal type="kvm" dataChannel={kvmTerminal} title="KVM Terminal" />
      )}

      {serialConsole && (
        <Terminal type="serial" dataChannel={serialConsole} title="Serial Console" />
      )}
    </FeatureFlagProvider>
  );
}

function SidebarContainer({ sidebarView }: { sidebarView: string | null }) {
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
