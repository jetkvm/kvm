import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import pkg from "react-use-websocket";

import { CLOUD_API } from "@/ui.config";
import api from "@/api";
import { isOnDevice } from "@/main";
import { useRTCStore, useUiStore } from "@hooks/stores";
import { m } from "@localizations/messages.js";
import { isLinuxDesktop } from "@/utils";

import type { SignalingHook } from "./types";

const useWebSocket = pkg.default ?? pkg;

/**
 * Removes H.265 from each video transceiver's preferred codec list so the
 * generated SDP offer does not advertise it. Called immediately before
 * createOffer() on Linux only — see jetkvm/kvm#1413 for context.
 */
function stripH265FromVideoTransceivers(pc: RTCPeerConnection) {
  try {
    const caps = RTCRtpReceiver.getCapabilities?.("video");
    if (!caps) return;
    const filtered = caps.codecs.filter(c => c.mimeType !== "video/H265");
    if (filtered.length === caps.codecs.length) return;

    for (const t of pc.getTransceivers()) {
      // addTransceiver("video", ...) populates receiver.track with kind "video".
      if (t.receiver.track?.kind !== "video") continue;
      t.setCodecPreferences(filtered);
    }
    console.debug("[setupPeerConnection] Linux: stripped H.265 from video codec preferences");
  } catch (e) {
    console.warn("[setupPeerConnection] setCodecPreferences failed", e);
  }
}

const reconnectInterval = (attempt: number) => {
  // Exponential backoff with a max of 10 seconds between attempts
  return Math.min(500 * 2 ** attempt, 10000);
};

/**
 * JetKVM's stock signaling: the browser is the offerer and talks to the
 * device (directly, or relayed by the cloud) over a WebSocket at
 * `/webrtc/signaling/client`. Devices that predate the WebSocket flow answer
 * the `device-metadata` message without a version, in which case the offer
 * is posted over HTTP once ICE gathering completes.
 */
export const useWebSocketSignaling: SignalingHook = ({
  deviceId,
  iceServers,
  onPeerConnection,
}) => {
  const {
    peerConnection,
    setPeerConnection,
    peerConnectionState,
    setPeerConnectionState,
    setMediaStream,
  } = useRTCStore();
  const setRebootState = useUiStore(state => state.setRebootState);
  const navigate = useNavigate();

  const isLegacySignalingEnabled = useRef(false);
  const [connectionFailed, setConnectionFailed] = useState(false);
  const [loadingMessage, setLoadingMessage] = useState(m.connecting_to_device());

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
      setLoadingMessage(m.setting_remote_description());

      try {
        await pc.setRemoteDescription(new RTCSessionDescription(remoteDescription));
        console.log(
          "[setRemoteSessionDescription] Remote description set successfully to: " +
            remoteDescription.sdp,
        );
        setLoadingMessage(m.establishing_secure_connection());
      } catch (error) {
        console.error("[setRemoteSessionDescription] Failed to set remote description:", error);
        cleanupAndStopReconnecting();
        return;
      }

      // Replace the interval-based check with a more reliable approach
      let attempts = 0;
      const checkInterval = setInterval(() => {
        attempts++;

        // When vivaldi has disabled "Broadcast IP for Best WebRTC Performance", this never connects
        if (pc.sctp?.state === "connected") {
          console.log("[setRemoteSessionDescription] Remote description set");
          clearInterval(checkInterval);
          setLoadingMessage(m.connection_established());
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
        } else {
          console.log("[setRemoteSessionDescription] Waiting for connection, state:", {
            connectionState: pc.connectionState,
            iceConnectionState: pc.iceConnectionState,
          });
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

  const { sendMessage, getWebSocket } = useWebSocket(
    isOnDevice
      ? `${wsProtocol}//${window.location.host}/webrtc/signaling/client`
      : `${CLOUD_API.replace("http", "ws")}/webrtc/signaling/client?id=${deviceId}`,
    {
      heartbeat: true,
      retryOnError: true,
      reconnectAttempts: reconnectAttemptsRef.current,
      reconnectInterval: reconnectInterval,
      onReconnectStop: (numAttempts: number) => {
        console.debug("Reconnect stopped after ", numAttempts, "attempts");
        cleanupAndStopReconnecting();
      },

      shouldReconnect(event: WebSocketEventMap["close"]) {
        console.debug("[Websocket] shouldReconnect", event);
        return !isLegacySignalingEnabled.current;
      },

      onClose(event: WebSocketEventMap["close"]) {
        console.debug("[Websocket] onClose", event);
        // We don't want to close everything down, we wait for the reconnect to stop instead
      },

      onError(event: WebSocketEventMap["error"]) {
        console.error("[Websocket] onError", event);
        // We don't want to close everything down, we wait for the reconnect to stop instead
      },

      onOpen() {
        console.debug("[Websocket] onOpen");
        // We want to clear the reboot state when the websocket connection is opened
        // Currently the flow is:
        // 1. User clicks reboot
        // 2. Device sends event 'willReboot'
        // 3. We set the reboot state
        // 4. Reboot modal is shown
        // 5. WS tries to reconnect
        // 6. WS reconnects
        // 7. This function is called and now we clear the reboot state
        setRebootState({ isRebooting: false, postRebootAction: null });
      },

      onMessage(event: WebSocketEventMap["message"]) {
        const message = event;
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
          const { deviceVersion } = parsedMessage.data;
          console.debug("[Websocket] Received device-metadata message");
          console.debug("[Websocket] Device version", deviceVersion);
          // If the device version is not set, we can assume the device is using the legacy signaling
          if (!deviceVersion) {
            console.log("[Websocket] Device is using legacy signaling");

            // Now we don't need the websocket connection anymore, as we've established that we need to use the legacy signaling
            // which does everything over HTTP(at least from the perspective of the client)
            isLegacySignalingEnabled.current = true;
            getWebSocket()?.close();
          } else {
            console.log("[Websocket] Device is using new signaling");
            isLegacySignalingEnabled.current = false;
          }

          setupPeerConnection();
        }

        if (!peerConnection) return;

        if (parsedMessage.type === "answer") {
          console.debug("[Websocket] Received answer");
          const readyForOffer =
            // If we're making an offer, we don't want to accept an answer
            !makingOffer &&
            // If the peer connection is stable or we're setting the remote answer pending, we're ready for an offer
            (peerConnection?.signalingState === "stable" || isSettingRemoteAnswerPending.current);

          // If we're not ready for an offer, we don't want to accept an offer
          ignoreOffer.current = parsedMessage.type === "offer" && !readyForOffer;
          if (ignoreOffer.current) return;

          // Set so we don't accept an answer while we're setting the remote description
          isSettingRemoteAnswerPending.current = parsedMessage.type === "answer";
          console.debug(
            "[Websocket] Setting remote answer pending",
            isSettingRemoteAnswerPending.current,
          );

          const sd = atob(parsedMessage.data);
          const remoteSessionDescription = JSON.parse(sd);

          setRemoteSessionDescription(
            peerConnection,
            new RTCSessionDescription(remoteSessionDescription),
          );

          // Reset the remote answer pending flag
          isSettingRemoteAnswerPending.current = false;
        } else if (parsedMessage.type === "new-ice-candidate") {
          console.debug("[Websocket] Received new-ice-candidate");
          const candidate = parsedMessage.data;
          peerConnection.addIceCandidate(candidate);
        }
      },
    },
  );

  const sendWebRTCSignal = useCallback(
    (type: string, data: unknown) => {
      // Second argument tells the library not to queue the message, and send it once the connection is established again.
      // We have event handlers that handle the connection set up, so we don't need to queue the message.
      sendMessage(JSON.stringify({ type, data }), false);
    },
    [sendMessage],
  );

  const legacyHTTPSignaling = useCallback(
    async (pc: RTCPeerConnection) => {
      const sd = btoa(JSON.stringify(pc.localDescription));

      // Legacy mode == UI in cloud with updated code connecting to older device version.
      // In device mode, old devices wont serve this JS, and on newer devices legacy mode wont be enabled
      const sessionUrl = `${CLOUD_API}/webrtc/session`;

      console.log("Trying to get remote session description");
      setLoadingMessage(
        m.getting_remote_session_description({
          attempt: signalingAttempts.current + 1,
        }),
      );
      const res = await api.POST(sessionUrl, {
        sd,
        // When on device, we don't need to specify the device id, as it's already known
        ...(isOnDevice ? {} : { id: deviceId }),
      });

      const json = await res.json();
      if (res.status === 401) return navigate(isOnDevice ? "/login-local" : "/login");
      if (!res.ok) {
        console.error("Error getting SDP", { status: res.status, json });
        cleanupAndStopReconnecting();
        return;
      }

      console.debug("Successfully got Remote Session Description. Setting.");
      setLoadingMessage(m.setting_remote_session_description());

      const decodedSd = atob(json.sd);
      const parsedSd = JSON.parse(decodedSd);
      setRemoteSessionDescription(pc, new RTCSessionDescription(parsedSd));
    },
    [cleanupAndStopReconnecting, deviceId, navigate, setRemoteSessionDescription],
  );

  const setupPeerConnection = useCallback(async () => {
    console.debug("[setupPeerConnection] Setting up peer connection");
    setConnectionFailed(false);
    setLoadingMessage(m.connecting_to_device());

    // Drop the previous PC's MediaStream — its tracks are ended and
    // would sit ahead of the new live tracks, breaking playback.
    setMediaStream(null);

    let pc: RTCPeerConnection;
    try {
      console.debug("[setupPeerConnection] Creating peer connection");
      setLoadingMessage(m.creating_peer_connection());
      pc = new RTCPeerConnection({
        ...(iceServers ? { iceServers } : {}),
      });

      setPeerConnectionState(pc.connectionState);
      console.debug("[setupPeerConnection] Peer connection created", pc);
      setLoadingMessage(m.setting_up_connection_to_device());
    } catch (e) {
      console.error(`[setupPeerConnection] Error creating peer connection: ${String(e)}`);
      setTimeout(() => {
        cleanupAndStopReconnecting();
      }, 1000);
      return;
    }

    // Set up event listeners and data channels
    pc.onconnectionstatechange = () => {
      console.debug("[setupPeerConnection] Connection state changed", pc.connectionState);
      setPeerConnectionState(pc.connectionState);
    };

    pc.onnegotiationneeded = async () => {
      try {
        console.debug("[setupPeerConnection] Creating offer");
        makingOffer.current = true;

        if (isLinuxDesktop()) {
          stripH265FromVideoTransceivers(pc);
        }

        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        const sd = btoa(JSON.stringify(pc.localDescription));
        const isNewSignalingEnabled = isLegacySignalingEnabled.current === false;
        if (isNewSignalingEnabled) {
          sendWebRTCSignal("offer", { sd: sd });
        } else {
          console.log("Legacy signaling. Waiting for ICE Gathering to complete...");
        }
      } catch (e) {
        console.error(
          `[setupPeerConnection] Error creating offer: ${String(e)}`,
          new Date().toISOString(),
        );
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
        console.debug("ICE Gathering completed");
        setLoadingMessage(m.ice_gathering_completed());

        if (isLegacySignalingEnabled.current) {
          // We can now start the https/ws connection to get the remote session description from the KVM device
          legacyHTTPSignaling(pc);
        }
      } else if (pc.iceGatheringState === "gathering") {
        console.debug("ICE Gathering Started");
        setLoadingMessage(m.gathering_ice_candidates());
      }
    };

    onPeerConnection(pc);

    setPeerConnection(pc);
  }, [
    cleanupAndStopReconnecting,
    iceServers,
    legacyHTTPSignaling,
    onPeerConnection,
    sendWebRTCSignal,
    setMediaStream,
    setPeerConnection,
    setPeerConnectionState,
  ]);

  useEffect(() => {
    if (peerConnectionState === "failed") {
      console.warn("Connection failed, closing peer connection");
      cleanupAndStopReconnecting();
    }
  }, [peerConnectionState, cleanupAndStopReconnecting]);

  useEffect(() => {
    return () => {
      peerConnection?.close();
    };
  }, [peerConnection]);

  return { connect: setupPeerConnection, loadingMessage, connectionFailed };
};
