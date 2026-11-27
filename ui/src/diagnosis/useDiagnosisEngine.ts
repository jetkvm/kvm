import { useEffect, useMemo, useRef, useState } from "react";

import { useHidStore, useRTCStore, useVideoStore } from "@hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { isOnDevice } from "@/main";
import { isLinuxDesktop } from "@/utils";

import { diagnose, type Browser, type Facts } from "./diagnose";
import { runSelfTest, type SelfTestResult } from "./selfTest";
import { useDiagnosisStore } from "./store";

const POLL_MS = 1000;
const SELF_TEST_DELAY_MS = 2000;
const RATE_WINDOW_MS = 2000;
const HTTP_PROBE_TIMEOUT_MS = 2000;

function detectBrowser(): Browser {
  const ua = navigator.userAgent;
  if (ua.includes("Edg/")) return "edge";
  if (ua.includes("Firefox/")) return "firefox";
  if (ua.includes("Chrome/") || ua.includes("Chromium/")) return "chrome";
  if (ua.includes("Safari/")) return "safari";
  return "other";
}

function detectH264() {
  if (typeof RTCRtpReceiver === "undefined" || !RTCRtpReceiver.getCapabilities) return true;
  const codecs = RTCRtpReceiver.getCapabilities("video")?.codecs;
  if (!codecs) return true;
  return codecs.some(codec => codec.mimeType.toLowerCase() === "video/h264");
}

const readEnvironment = () => ({
  mode: isOnDevice ? ("local" as const) : ("cloud" as const),
  browser: detectBrowser(),
  linux: isLinuxDesktop(),
  addressIsIp: /^[\d.]+$|:/.test(window.location.hostname),
  webrtcSupported: typeof RTCPeerConnection !== "undefined",
  h264Supported: detectH264(),
});

interface Sample {
  at: number;
  bytes: number;
  frames: number;
}

interface VideoFacts {
  receivingBytes: boolean;
  decodingFrames: boolean;
  codec: Facts["codec"];
  videoPlaying: boolean;
}

const noVideo: VideoFacts = {
  receivingBytes: false,
  decodingFrames: false,
  codec: null,
  videoPlaying: false,
};

function useIceWasConnected(peerConnection: RTCPeerConnection | null) {
  const [iceWasConnected, setIceWasConnected] = useState(false);

  useEffect(() => {
    if (!peerConnection) return;
    const read = () =>
      setIceWasConnected(previous => {
        const state = peerConnection.iceConnectionState;
        return previous || state === "connected" || state === "completed";
      });
    setIceWasConnected(false);
    read();
    peerConnection.addEventListener("iceconnectionstatechange", read);
    return () => peerConnection.removeEventListener("iceconnectionstatechange", read);
  }, [peerConnection]);

  return iceWasConnected;
}

function useComputerOnline() {
  const [online, setOnline] = useState(navigator.onLine);

  useEffect(() => {
    const update = () => setOnline(navigator.onLine);
    window.addEventListener("online", update);
    window.addEventListener("offline", update);
    return () => {
      window.removeEventListener("online", update);
      window.removeEventListener("offline", update);
    };
  }, []);

  return online;
}

function useSelfTest(peerConnection: RTCPeerConnection | null, wanted: boolean) {
  const [result, setResult] = useState<SelfTestResult | null>(null);
  const testedPeer = useRef<RTCPeerConnection | null>(null);
  const currentPeer = useRef(peerConnection);

  useEffect(() => {
    if (!peerConnection) return;
    currentPeer.current = peerConnection;
    setResult(null);
  }, [peerConnection]);

  useEffect(() => {
    if (!wanted) return;
    const timer = setTimeout(() => {
      const peer = currentPeer.current;
      if (testedPeer.current === peer) return;
      testedPeer.current = peer;
      void runSelfTest().then(selfTest => {
        if (currentPeer.current === peer) setResult(selfTest);
      });
    }, SELF_TEST_DELAY_MS);
    return () => clearTimeout(timer);
  }, [wanted]);

  return result;
}

function pageAddressIsOther(kvmHostAddresses: string[]) {
  const { hostname } = window.location;
  if (import.meta.env.DEV || !isOnDevice) return null;
  if (!/^\d+\.\d+\.\d+\.\d+$/.test(hostname) || kvmHostAddresses.length === 0) return null;
  return !kvmHostAddresses.includes(hostname);
}

function useHttpReachable(socketOpen: boolean) {
  const [httpReachable, setHttpReachable] = useState<boolean | null>(null);

  useEffect(() => {
    if (!isOnDevice || socketOpen) {
      setHttpReachable(null);
      return;
    }
    let cancelled = false;
    const probe = async () => {
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), HTTP_PROBE_TIMEOUT_MS);
      try {
        const response = await fetch("/device/status", {
          cache: "no-store",
          signal: controller.signal,
        });
        if (!cancelled) setHttpReachable(response.ok);
      } catch {
        if (!cancelled) setHttpReachable(false);
      } finally {
        clearTimeout(timeout);
      }
    };
    void probe();
    const interval = setInterval(probe, 3000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [socketOpen]);

  return httpReachable;
}

function useEdidIsDefault(rpcOpen: boolean) {
  const { send } = useJsonRpc();
  const [edidIsDefault, setEdidIsDefault] = useState<boolean | null>(null);
  const edidChanges = useDiagnosisStore(state => state.edidChanges);

  useEffect(() => {
    if (!rpcOpen) {
      setEdidIsDefault(null);
      return;
    }
    send("getEDIDPresets", {}, (presets: JsonRpcResponse) => {
      if ("error" in presets) return;
      const defaultEdid = (presets.result as { edid: string }[])[0]?.edid;
      if (!defaultEdid) return;
      send("getEDID", {}, (current: JsonRpcResponse) => {
        if ("error" in current) return;
        setEdidIsDefault((current.result as string).toLowerCase() === defaultEdid.toLowerCase());
      });
    });
  }, [rpcOpen, send, edidChanges]);

  return edidIsDefault;
}

function useVideoFacts(peerConnection: RTCPeerConnection | null, connected: boolean) {
  const [video, setVideo] = useState<VideoFacts>(noVideo);
  const samples = useRef<Sample[]>([]);
  const setPlayError = useDiagnosisStore(state => state.setPlayError);

  useEffect(() => {
    samples.current = [];
    setPlayError(null);
    if (!peerConnection || !connected) {
      setVideo(noVideo);
      return;
    }
    let cancelled = false;
    let probedPlay = false;

    const poll = async () => {
      if (document.hidden) return;
      const stats = await peerConnection.getStats().catch(() => null);
      if (cancelled || !stats) return;

      let bytes = 0;
      let frames = 0;
      let codec: Facts["codec"] = null;
      stats.forEach(report => {
        if (report.type !== "inbound-rtp" || report.kind !== "video") return;
        bytes = report.bytesReceived ?? 0;
        frames = report.framesDecoded ?? 0;
        const mimeType = (stats.get(report.codecId)?.mimeType ?? "").toLowerCase();
        if (mimeType === "video/h264") codec = "H.264";
        if (mimeType === "video/h265") codec = "H.265";
      });

      const now = Date.now();
      samples.current = [
        ...samples.current.filter(sample => now - sample.at <= RATE_WINDOW_MS),
        { at: now, bytes, frames },
      ];
      if (samples.current.length < 2) return;
      const oldest = samples.current[0];
      const receivingBytes = bytes > oldest.bytes;
      const decodingFrames = frames > oldest.frames;

      const element = useVideoStore.getState().videoElement;
      const videoPlaying = !!element && !element.paused && element.readyState >= 3;

      if (element && element.paused && decodingFrames && !probedPlay) {
        probedPlay = true;
        element.play().then(
          () => setPlayError(null),
          (error: unknown) => setPlayError((error as DOMException | undefined)?.name ?? "Error"),
        );
      }

      setVideo({ receivingBytes, decodingFrames, codec, videoPlaying });
    };

    void poll();
    const interval = setInterval(poll, POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [peerConnection, connected, setPlayError]);

  return video;
}

export function useDiagnosisEngine({
  socketOpen,
  paused,
}: {
  socketOpen: boolean;
  paused: boolean;
}) {
  const peerConnection = useRTCStore(state => state.peerConnection);
  const peerConnectionState = useRTCStore(state => state.peerConnectionState);
  const rpcOpen = useRTCStore(state => state.rpcDataChannel?.readyState === "open");
  const hdmiState = useVideoStore(state => state.hdmiState);
  const usbState = useHidStore(state => state.usbState);
  const overrides = useDiagnosisStore(state => state.overrides);
  const playError = useDiagnosisStore(state => state.playError);
  const answerState = useDiagnosisStore(state => state.answerState);
  const kvmHostAddresses = useDiagnosisStore(state => state.kvmHostAddresses);
  const publish = useDiagnosisStore(state => state.publish);

  const [environment] = useState(readEnvironment);
  const peerConnected = peerConnectionState === "connected";
  const iceWasConnected = useIceWasConnected(peerConnection);
  const httpReachable = useHttpReachable(socketOpen);
  const edidIsDefault = useEdidIsDefault(rpcOpen);
  const video = useVideoFacts(peerConnection, peerConnected);
  const computerOnline = useComputerOnline();
  const selfTest = useSelfTest(
    peerConnection,
    (socketOpen && answerState === "accepted" && !iceWasConnected) ||
      (peerConnected && video.receivingBytes && !video.decodingFrames && video.codec === "H.264"),
  );
  const reachedThroughOtherAddress = pageAddressIsOther(kvmHostAddresses);

  const live = useMemo<Omit<Facts, "msWaiting">>(
    () => ({
      ...environment,
      computerOnline,
      socketOpen,
      httpReachable,
      cloudOnline: null,
      answerState,
      reachedThroughOtherAddress,
      selfTestConnected: selfTest?.connected ?? null,
      selfTestH264: selfTest?.h264 ?? null,
      iceWasConnected,
      peerConnected,
      hdmiState,
      usbState,
      edidIsDefault,
      ...video,
      playError,
    }),
    [
      environment,
      computerOnline,
      socketOpen,
      httpReachable,
      answerState,
      reachedThroughOtherAddress,
      selfTest,
      iceWasConnected,
      peerConnected,
      hdmiState,
      usbState,
      edidIsDefault,
      video,
      playError,
    ],
  );

  const merged = useMemo(() => ({ ...live, ...overrides }), [live, overrides]);
  const first = diagnose({ ...merged, msWaiting: 0 });
  const openQuestion = first.status === "waiting" ? first.question : 0;

  const waitingSince = useRef({ question: 0, at: Date.now() });
  if (waitingSince.current.question !== openQuestion) {
    waitingSince.current = { question: openQuestion, at: Date.now() };
  }

  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (openQuestion === 0) return;
    setNow(Date.now());
    const interval = setInterval(() => setNow(Date.now()), 500);
    return () => clearInterval(interval);
  }, [openQuestion]);

  useEffect(() => {
    const msWaiting = openQuestion === 0 ? 0 : Math.max(0, now - waitingSince.current.at);
    const liveFacts = { ...live, msWaiting };
    const facts = { ...liveFacts, ...overrides };
    publish(liveFacts, facts, paused ? { status: "ok" } : diagnose(facts));
  }, [live, overrides, openQuestion, now, paused, publish]);
}
