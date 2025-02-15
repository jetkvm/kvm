import { useCallback, useEffect, useRef, useState } from "react";
import { cx } from "@/cva.config";
import { Transition } from "@headlessui/react";
import {
  HidState,
  UpdateState,
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
import {
  LoaderFunctionArgs,
  Params,
  redirect,
  useLoaderData,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { checkAuth, isInCloud, isOnDevice } from "@/main";
import DashboardNavbar from "@components/Header";
import { useInterval } from "usehooks-ts";
import SettingsSidebar from "@/components/sidebar/settings";
import ConnectionStatsSidebar from "@/components/sidebar/connectionStats";
import { JsonRpcRequest, useJsonRpc } from "@/hooks/useJsonRpc";
import UpdateDialog from "@components/UpdateDialog";
import UpdateInProgressStatusCard from "../components/UpdateInProgressStatusCard";
import api from "../api";
import { DeviceStatus } from "./welcome-local";
import FocusTrap from "focus-trap-react";
import OtherSessionConnectedModal from "@/components/OtherSessionConnectedModal";
import Terminal from "@components/Terminal";
import { CLOUD_API, SIGNAL_API } from "@/ui.config";

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

export interface LocalDevice {
  authMode: "password" | "noPassword" | null;
  deviceId: string;
}

const deviceLoader = async () => {
  const res = await api
    .GET(`${SIGNAL_API}/device/status`)
    .then(res => res.json() as Promise<DeviceStatus>);

  if (!res.isSetup) return redirect("/welcome");

  const deviceRes = await api.GET(`${SIGNAL_API}/device`);
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

  const deviceResp = await api.GET(
    `${CLOUD_API}/devices/${params.id}`,
  );

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

  const setPeerConnectionState = useRTCStore(state => state.setPeerConnectionState);
  const setMediaMediaStream = useRTCStore(state => state.setMediaStream);
  const setPeerConnection = useRTCStore(state => state.setPeerConnection);
  const setDiskChannel = useRTCStore(state => state.setDiskChannel);
  const setRpcDataChannel = useRTCStore(state => state.setRpcDataChannel);
  const setTransceiver = useRTCStore(state => state.setTransceiver);

  const navigate = useNavigate();
  const {
    otaState,
    setOtaState,
    isUpdateDialogOpen,
    setIsUpdateDialogOpen,
    setModalView,
  } = useUpdateStore();

  const [isOtherSessionConnectedModalOpen, setIsOtherSessionConnectedModalOpen] =
    useState(false);

  const sdp = useCallback(
    async (event: RTCPeerConnectionIceEvent, pc: RTCPeerConnection) => {
      if (!pc) return;
      if (event.candidate !== null) return;

      try {
        const sd = btoa(JSON.stringify(pc.localDescription));
        const res = await api.POST(`${SIGNAL_API}/webrtc/session`, {
          sd,
          // When on device, we don't need to specify the device id, as it's already known
          ...(isOnDevice ? {} : { id: params.id }),
        });

        const json = await res.json();

        if (isOnDevice) {
          if (res.status === 401) {
            return navigate("/login-local");
          }
        }

        if (isInCloud) {
          // The cloud API returns a 401 if the user is not logged in
          // Most likely the session has expired
          if (res.status === 401) return navigate("/login");

          // If can be a few things
          // - In cloud mode, the cloud api would return a 404, if the device hasn't contacted the cloud yet
          // - In device mode, the device api would timeout, the fetch would throw an error, therefore the catch block would be hit
          // Regardless, we should close the peer connection and let the useInterval handle reconnecting
          if (!res.ok) {
            pc?.close();
            console.error(`Error setting SDP - Status: ${res.status}}`, json);
            return;
          }
        }

        pc.setRemoteDescription(
          new RTCSessionDescription(JSON.parse(atob(json.sd))),
        ).catch(e => console.log(`Error setting remote description: ${e}`));
      } catch (error) {
        console.error(`Error setting SDP: ${error}`);
        pc?.close();
      }
    },
    [navigate, params.id],
  );

  const connectWebRTC = useCallback(async () => {
    console.log("Attempting to connect WebRTC");
    const pc = new RTCPeerConnection({
      // We only use STUN or TURN servers if we're in the cloud
      ...(isInCloud && iceConfig?.iceServers
        ? { iceServers: [iceConfig?.iceServers] }
        : {}),
    });

    // Set up event listeners and data channels
    pc.onconnectionstatechange = () => {
      setPeerConnectionState(pc.connectionState);
    };

    pc.onicecandidate = event => sdp(event, pc);

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

    try {
      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
      setPeerConnection(pc);
    } catch (e) {
      console.error(`Error creating offer: ${e}`);
    }
  }, [
    iceConfig?.iceServers,
    sdp,
    setDiskChannel,
    setMediaMediaStream,
    setPeerConnection,
    setPeerConnectionState,
    setRpcDataChannel,
    setTransceiver,
  ]);

  // WebRTC connection management
  useInterval(() => {
    if (
      ["connected", "connecting", "new"].includes(peerConnection?.connectionState ?? "")
    ) {
      return;
    }
    // We don't want to connect if another session is connected
    if (isOtherSessionConnectedModalOpen) return;
    connectWebRTC();
  }, 3000);

  // On boot, if the connection state is undefined, we connect to the WebRTC
  useEffect(() => {
    if (peerConnection?.connectionState === undefined) {
      connectWebRTC();
    }
  }, [connectWebRTC, peerConnection?.connectionState]);

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

  function onJsonRpcRequest(resp: JsonRpcRequest) {
    if (resp.method === "otherSessionConnected") {
      console.log("otherSessionConnected", resp.params);
      setIsOtherSessionConnectedModalOpen(true);
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
          setIsUpdateDialogOpen(true);
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
      setModalView("updateCompleted");
      setIsUpdateDialogOpen(true);
      setQueryParams({});
    }
  }, [queryParams, setIsUpdateDialogOpen, setModalView, setQueryParams]);

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

  useEffect(() => {
    kvmTerminal?.addEventListener("message", e => {
      console.log(e.data);
    });

    return () => {
      kvmTerminal?.removeEventListener("message", e => {
        console.log(e.data);
      });
    };
  }, [kvmTerminal]);

  return (
    <>
      <Transition show={!isUpdateDialogOpen && otaState.updating}>
        <div className="pointer-events-none fixed inset-0 z-10 mx-auto flex h-full w-full max-w-xl translate-y-8 items-start justify-center">
          <div className="transition duration-1000 ease-in data-[closed]:opacity-0">
            <UpdateInProgressStatusCard
              setIsUpdateDialogOpen={setIsUpdateDialogOpen}
              setModalView={setModalView}
            />
          </div>
        </div>
      </Transition>
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

          <div className="flex h-full overflow-hidden">
            <WebRTCVideo />
            <SidebarContainer sidebarView={sidebarView} />
          </div>
        </div>
      </div>
      <UpdateDialog open={isUpdateDialogOpen} setOpen={setIsUpdateDialogOpen} />
      <OtherSessionConnectedModal
        open={isOtherSessionConnectedModalOpen}
        setOpen={state => {
          if (!state) connectWebRTC().then(r => r);

          // It takes some time for the WebRTC connection to be established, so we wait a bit before closing the modal
          setTimeout(() => {
            setIsOtherSessionConnectedModalOpen(state);
          }, 1000);
        }}
      />
      {kvmTerminal && (
        <Terminal type="kvm" dataChannel={kvmTerminal} title="KVM Terminal" />
      )}
      {serialConsole && (
        <Terminal type="serial" dataChannel={serialConsole} title="Serial Console" />
      )}
    </>
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
        <Transition show={sidebarView === "system"} unmount={false}>
          <div className="absolute inset-0">
            <SettingsSidebar />
          </div>
        </Transition>
        <Transition show={sidebarView === "connection-stats"} unmount={false}>
          <div className="absolute inset-0">
            <ConnectionStatsSidebar />
          </div>
        </Transition>
      </div>
    </div>
  );
}

KvmIdRoute.loader = loader;
