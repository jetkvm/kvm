import { WAIT_LIMIT_MS, type Facts } from "./diagnose";

export const healthy: Facts = {
  mode: "local",
  browser: "chrome",
  linux: false,
  addressIsIp: true,
  webrtcSupported: true,
  h264Supported: true,
  computerOnline: true,
  socketOpen: true,
  httpReachable: true,
  cloudOnline: null,
  answerState: "accepted",
  reachedThroughOtherAddress: null,
  selfTestConnected: null,
  selfTestH264: null,
  iceWasConnected: true,
  peerConnected: true,
  hdmiState: "ready",
  usbState: "configured",
  edidIsDefault: true,
  receivingBytes: true,
  decodingFrames: true,
  codec: "H.264",
  videoPlaying: true,
  playError: null,
  msWaiting: WAIT_LIMIT_MS,
};

export const cases: { name: string; facts: Partial<Facts> }[] = [
  { name: "Healthy session", facts: {} },
  { name: "1. WebRTC off (Firefox)", facts: { browser: "firefox", webrtcSupported: false } },
  {
    name: "2. No H.264 (Firefox on Linux)",
    facts: { browser: "firefox", linux: true, h264Supported: false },
  },
  { name: "3.1 Waiting for the socket", facts: { socketOpen: false, msWaiting: 2000 } },
  {
    name: "Waiting for video",
    facts: { receivingBytes: false, decodingFrames: false, videoPlaying: false, msWaiting: 2000 },
  },
  {
    name: "3.7 This computer is offline",
    facts: { socketOpen: false, httpReachable: false, computerOnline: false },
  },
  { name: "3.2 KVM does not respond (local)", facts: { socketOpen: false, httpReachable: false } },
  {
    name: "3.3 KVM offline (cloud)",
    facts: { mode: "cloud", socketOpen: false, httpReachable: null, cloudOnline: false },
  },
  {
    name: "3.4 KVM does not respond (cloud, online)",
    facts: { mode: "cloud", socketOpen: false, httpReachable: null, cloudOnline: true },
  },
  { name: "3.5 Socket blocked (Safari on IP)", facts: { browser: "safari", socketOpen: false } },
  { name: "3.6 Socket blocked (other)", facts: { socketOpen: false } },
  {
    name: "4.1 Network blocked (local, Chrome)",
    facts: { iceWasConnected: false, peerConnected: false },
  },
  {
    name: "4.2 Network blocked (cloud, Firefox)",
    facts: { mode: "cloud", browser: "firefox", iceWasConnected: false, peerConnected: false },
  },
  {
    name: "4.3 Self-test failed",
    facts: { iceWasConnected: false, peerConnected: false, selfTestConnected: false },
  },
  {
    name: "4.4 Self-test passed",
    facts: { iceWasConnected: false, peerConnected: false, selfTestConnected: true },
  },
  {
    name: "4.5 KVM behind another address",
    facts: {
      iceWasConnected: false,
      peerConnected: false,
      selfTestConnected: true,
      reachedThroughOtherAddress: true,
    },
  },
  {
    name: "4.6 Browser rejected the answer",
    facts: { iceWasConnected: false, peerConnected: false, answerState: "rejected" },
  },
  {
    name: "4.7 KVM did not answer",
    facts: { iceWasConnected: false, peerConnected: false, answerState: "none" },
  },
  { name: "5. KVM does not complete", facts: { peerConnected: false } },
  {
    name: "6.1 No signal, USB suspended",
    facts: { hdmiState: "no_signal", usbState: "suspended" },
  },
  {
    name: "6.2 No signal, USB not attached",
    facts: { hdmiState: "no_signal", usbState: "not attached" },
  },
  { name: "6.3 No signal, USB active", facts: { hdmiState: "no_signal" } },
  {
    name: "6.4 No signal, USB active, custom EDID",
    facts: { hdmiState: "no_signal", edidIsDefault: false },
  },
  { name: "6.5 Out of range", facts: { hdmiState: "out_of_range" } },
  { name: "6.6 Unstable signal", facts: { hdmiState: "no_lock" } },
  {
    name: "7. KVM sends no video",
    facts: { receivingBytes: false, decodingFrames: false, videoPlaying: false },
  },
  {
    name: "8.1 Cannot decode H.265",
    facts: { codec: "H.265", decodingFrames: false, videoPlaying: false },
  },
  {
    name: "8.2 Cannot decode H.264 (Chrome)",
    facts: { decodingFrames: false, videoPlaying: false },
  },
  {
    name: "8.4 H.264 self-test failed",
    facts: { decodingFrames: false, videoPlaying: false, selfTestH264: false },
  },
  {
    name: "8.5 H.264 self-test passed",
    facts: { decodingFrames: false, videoPlaying: false, selfTestH264: true },
  },
  {
    name: "8.3 Cannot decode H.264 (Firefox on Linux)",
    facts: { browser: "firefox", linux: true, decodingFrames: false, videoPlaying: false },
  },
  {
    name: "9.1 Autoplay blocked (Edge)",
    facts: { browser: "edge", videoPlaying: false, playError: "NotAllowedError" },
  },
  { name: "9.2 Video does not play", facts: { videoPlaying: false, playError: "AbortError" } },
];
